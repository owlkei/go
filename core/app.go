package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/dangduoc08/ginject/accesslog"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/devtool"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/memorybroker"
	"github.com/dangduoc08/ginject/routing"
	"github.com/dangduoc08/ginject/trace"
	"github.com/dangduoc08/ginject/versioning"
	"golang.org/x/net/websocket"
)

var injectableInterfaces = []string{
	"github.com/dangduoc08/ginject/common/common.Logger",
}

const (
	DefaultMaxRequestBodyBytes = 10 << 20 // 10 MB

	DefaultMaxHeaderBytes = 1 << 20 // 1 MB

	DefaultReadHeaderTimeout = 2 * time.Second
	DefaultReadTimeout       = 5 * time.Second
	DefaultWriteTimeout      = 15 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
)

type App struct {
	http      *HTTP
	ctxPool   sync.Pool
	wsCtxPool sync.Pool

	event *event.Event

	isDevtoolEnabled bool
	devtool          *devtool.Devtool

	isAccessLogEnabled bool
	accesslog          *accesslog.AccessLog

	ws          *WS
	wsConfig    *WSConfig
	isWSEnabled bool

	module *Module

	globalMiddlewares      []common.MiddlewareFn
	globalGuarders         []common.Guarder
	globalInterceptors     []common.Interceptable
	globalExceptionFilters []common.ExceptionFilterable
	injectedProviders      map[string]Provider

	broker memorybroker.Broker

	Logger     common.Logger
	LogOptions *log.LogOptions

	maxRequestBodyBytes int64

	readyOnce    sync.Once
	shutdownOnce sync.Once
	shutdownChan chan struct{}
}

func New() *App {
	app := &App{
		http:   newHTTP(),
		ws:     nil,
		event:  event.NewEvent(),
		broker: memorybroker.NewMemoryBroker(),
		ctxPool: sync.Pool{
			New: func() any {
				return newHTTPExchange()
			},
		},
		wsCtxPool: sync.Pool{
			New: func() any {
				return ctx.NewWSContext()
			},
		},
		shutdownChan:        make(chan struct{}),
		maxRequestBodyBytes: DefaultMaxRequestBodyBytes,
	}

	globalInterfaceByKey.Store(publisherKey, common.Publisher(
		newPublisher(app.broker),
	))

	app.BindGlobalExceptionFilters(globalHTTPExceptionFilter{}, globalWSExceptionFilter{})

	return app
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil && r.Body != http.NoBody && app.maxRequestBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, app.maxRequestBodyBytes)
	}

	ex := app.ctxPool.Get().(*httpExchange)
	ex.writer.reset(w)
	c := &ex.HTTPContext
	c.Init(&ex.writer, r)

	if app.isWSEnabled && app.ws.isWSPath(r.URL.Path) {
		app.ws.upgrade(w, r, websocket.Server{
			Handshake: func(wsCfg *websocket.Config, r *http.Request) error {
				defer app.releaseCtx(ex)

				c.Request = r
				c.Status(http.StatusSwitchingProtocols)
				err := app.ws.handshake(c)
				if app.event.HasListeners(trace.EventName) {
					app.event.Emit(trace.EventName, trace.Event{
						ID:        c.GetID(),
						Stage:     trace.StageComplete,
						Transport: trace.TransportHTTP,
						Operation: c.Method,
						Target:    c.URL.Path,
						Code:      c.Code,
						Duration:  time.Since(c.Timestamp),
					})
				}
				return err
			},
			Handler: websocket.Handler(app.ws.handleRequest),
		})

		return
	}

	w.Header()[ctx.RequestID] = []string{c.GetID()}
	ex.traced = app.event.HasListeners(trace.EventName)
	app.http.handleRequest(ex)
	if ex.traced {
		code := ex.writer.status()
		if code == 0 {
			code = http.StatusOK
		}
		app.event.Emit(trace.EventName, trace.Event{
			ID:        c.GetID(),
			Stage:     trace.StageComplete,
			Transport: trace.TransportHTTP,
			Operation: c.Method,
			Target:    c.URL.Path,
			Code:      code,
			Duration:  time.Since(c.Timestamp),
		})
	}
	app.releaseCtx(ex)
}

// Order matters here and is easy to break by accident:
//
//  1. initProviders must run first — it builds injectedProviders and
//     app.module (parsed from m), which everything below reads from.
//  2. initWS must run right after that, and BEFORE initExceptionFilters/
//     initGuards/initInterceptors/initMainHandlers — those functions write
//     into app.ws.catchFnsByEvent / app.ws.eventMatcher whenever app.module
//     has WS handlers. app.ws stays nil if EnableWS was never called, so
//     every one of those call sites is guarded with app.ws != nil rather
//     than assuming it's set.
//  3. initDevtool must run last — it reads the fully-populated
//     module/route state to build the devtool snapshot.
//  4. callOnReady must run last after all initialization is complete.
//
// Do not reorder these calls without re-checking every init* function for
// an unguarded app.ws.* access.
func (app *App) Create(m *Module) {
	app.initLogger()
	injectedProviders := app.initProviders(m)
	app.initWS(injectedProviders)
	app.initRoutes()
	app.initMiddlewares(injectedProviders)
	app.initExceptionFilters(injectedProviders)
	app.initGuards(injectedProviders)
	app.initInterceptors(injectedProviders)
	app.initMainHandlers()
	app.http.finishRoutes()
	app.initDevtool()
	app.initAccessLog()
	app.callOnReady()
}

func (app *App) initWS(injectedProviders map[string]Provider) {
	if !app.isWSEnabled {
		return
	}

	app.wsConfig.injectedProviders = injectedProviders
	app.wsConfig.logger = app.Logger
	app.wsConfig.event = app.event
	app.wsConfig.broker = &app.broker
	planByEvent := app.module.wsPlanByEvent
	app.wsConfig.resolveAndCallHandler = func(pattern string, _ any, c *ctx.WSContext) []reflect.Value {
		return planByEvent[pattern].invoke(c, app.event)
	}
	app.wsConfig.newCtx = func() *ctx.WSContext { return app.wsCtxPool.Get().(*ctx.WSContext) }
	app.wsConfig.releaseCtx = app.releaseWSCtx
	app.ws = NewWS(app.wsConfig)
	app.ws.emitComplete = func(c *ctx.WSContext, operation, target string) {
		if !app.event.HasListeners(trace.EventName) {
			return
		}
		app.event.Emit(trace.EventName, trace.Event{
			ID:        c.GetID(),
			Stage:     trace.StageComplete,
			Transport: trace.TransportWS,
			Operation: operation,
			Target:    target,
			Duration:  time.Since(c.Timestamp),
		})
	}
}

func (app *App) initAccessLog() {
	if !app.isAccessLogEnabled {
		return
	}

	app.accesslog = accesslog.NewAccessLog(&accesslog.AccessLogConfig{
		Event:  app.event,
		Logger: app.Logger,
	})
}

func (app *App) initLogger() {
	if app.Logger == nil {
		app.Logger = log.NewLog(app.LogOptions)
	} else {
		var maskFields []string
		if app.LogOptions != nil {
			maskFields = app.LogOptions.MaskFields
		}
		app.Logger = log.WrapLogger(app.Logger, maskFields)
	}
	globalInterfaceByKey.Store(injectableInterfaces[0], app.Logger)
}

func (app *App) initProviders(m *Module) map[string]Provider {
	app.module = m.NewModule()

	injectedProviders := make(map[string]Provider, len(app.module.providers))
	for _, provider := range app.module.providers {
		injectedProviders[genProviderKey(provider)] = provider
	}
	app.injectedProviders = injectedProviders

	return injectedProviders
}

func (app *App) initRoutes() {
	app.http.event = app.event
	app.http.logger = app.Logger
	for _, h := range app.module.HTTPMainHandlers {
		app.http.addRoute(h, app.module.httpPlanByPattern[h.Pattern])
	}
}

func (app *App) initExceptionFilters(injectedProviders map[string]Provider) {
	for i := len(app.module.HTTPExceptionFilters) - 1; i >= 0; i-- {
		ef := app.module.HTTPExceptionFilters[i]
		app.http.addFilter(ef, traceHTTPCatch(app.event, ef.Name, ef.Handler.(common.HTTPCatch)))
	}

	if app.ws != nil {
		for i := len(app.module.WSExceptionFilters) - 1; i >= 0; i-- {
			ef := app.module.WSExceptionFilters[i]
			catchFn := traceWSCatch(app.event, ef.Name, ef.Handler.(common.WSCatch))
			app.ws.catchFnsByEvent[ef.EventName] = append(app.ws.catchFnsByEvent[ef.EventName], catchFn)
		}
	}

	if len(app.globalExceptionFilters) > 0 {
		for i := len(app.globalExceptionFilters) - 1; i >= 0; i-- {
			gef := app.globalExceptionFilters[i]
			name := reflect.TypeOf(gef).String()
			newGef, err := injectDependencies(gef, "exceptionFilter", injectedProviders)
			if err != nil {
				panic(err)
			}
			exceptionFilterable := common.Construct(newGef.Interface(), "NewExceptionFilter")

			httpCatch, isHTTPFilter := common.AsHTTPExceptionFilter(exceptionFilterable)
			wsCatch, isWSFilter := common.AsWSExceptionFilter(exceptionFilterable)
			if !isHTTPFilter && !isWSFilter {
				panic(common.ExceptionFilterShapeError(exceptionFilterable))
			}

			if isHTTPFilter {
				app.http.addGlobalFilter(traceHTTPCatch(app.event, name, httpCatch))
			}

			if isWSFilter && app.ws != nil {
				tracedWSCatch := traceWSCatch(app.event, name, wsCatch)
				for _, h := range app.module.WSMainHandlers {
					app.ws.catchFnsByEvent[h.EventName] = append(app.ws.catchFnsByEvent[h.EventName], tracedWSCatch)
				}
			}
		}
	}
}

func traceHTTPCatch(ev *event.Event, name string, fn common.HTTPCatch) common.HTTPCatch {
	return func(c *ctx.HTTPContext, ex *exception.Exception) {
		if !ev.HasListeners(trace.EventName) {
			fn(c, ex)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageExceptionFilter, Name: name, Duration: time.Since(start)})
		}()
		fn(c, ex)
	}
}

func traceWSCatch(ev *event.Event, name string, fn common.WSCatch) common.WSCatch {
	return func(c *ctx.WSContext, ex *exception.Exception) {
		if !ev.HasListeners(trace.EventName) {
			fn(c, ex)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageExceptionFilter, Name: name, Transport: trace.TransportWS, Duration: time.Since(start)})
		}()
		fn(c, ex)
	}
}

func (app *App) initMiddlewares(injectedProviders map[string]Provider) {
	for _, gm := range app.globalMiddlewares {
		app.http.addGlobalMiddleware(newHTTPMiddleware(gm, injectedProviders))
	}

	for _, rm := range app.module.HTTPMiddlewares {
		app.http.addMiddleware(rm)
	}
}

func (app *App) initGuards(injectedProviders map[string]Provider) {
	if len(app.globalGuarders) > 0 {
		for _, gg := range app.globalGuarders {
			name := reflect.TypeOf(gg).String()
			newGG, err := injectDependencies(gg, "guard", injectedProviders)
			if err != nil {
				panic(err)
			}
			guarder := common.Construct(newGG.Interface(), "NewGuard")

			httpCanActivate, isHTTPGuard := common.AsHTTPGuard(guarder)
			wsCanActivate, isWSGuard := common.AsWSGuard(guarder)
			if !isHTTPGuard && !isWSGuard {
				panic(common.GuardShapeError(guarder))
			}

			if isHTTPGuard {
				app.http.addGlobalGuard(name, httpCanActivate)
			}

			if isWSGuard && app.ws != nil {
				mw := traceWSHandler(app.event, trace.StageGuard, name, common.BuildWSGuardMiddleware(wsCanActivate))
				for _, h := range app.module.WSMainHandlers {
					app.ws.eventMatcher.AddMiddlewares(h.EventName, mw)
				}
			}
		}
	}

	for _, mg := range app.module.HTTPGuards {
		app.http.addGuard(mg)
	}

	if app.ws != nil {
		for _, mg := range app.module.WSGuards {
			mw := traceWSHandler(app.event, trace.StageGuard, mg.Name, common.BuildWSGuardMiddleware(mg.Handler.(common.WSCanActivate)))
			app.ws.eventMatcher.AddMiddlewares(mg.EventName, mw)
		}
	}
}

func traceWSHandler(ev *event.Event, stage, name string, h ctx.WSHandler) ctx.WSHandler {
	return func(c *ctx.WSContext) {
		if !ev.HasListeners(trace.EventName) {
			h(c)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: stage, Name: name, Transport: trace.TransportWS, Duration: time.Since(start)})
		}()
		h(c)
	}
}

func (app *App) initInterceptors(injectedProviders map[string]Provider) {
	if app.ws != nil {
		app.ws.emitPostInterceptor = func(c *ctx.WSContext, name string, duration time.Duration) {
			if !app.event.HasListeners(trace.EventName) {
				return
			}
			app.event.Emit(trace.EventName, trace.Event{
				ID:        c.GetID(),
				Stage:     trace.StagePostInterceptor,
				Name:      name,
				Transport: trace.TransportWS,
				Duration:  duration,
			})
		}
	}

	if len(app.globalInterceptors) > 0 {
		for _, gi := range app.globalInterceptors {
			name := reflect.TypeOf(gi).String()
			newGI, err := injectDependencies(gi, "interceptor", injectedProviders)
			if err != nil {
				panic(err)
			}
			interceptable := common.Construct(newGI.Interface(), "NewInterceptor")

			httpIntercept, isHTTPInterceptor := common.AsHTTPInterceptor(interceptable)
			wsIntercept, isWSInterceptor := common.AsWSInterceptor(interceptable)
			if !isHTTPInterceptor && !isWSInterceptor {
				panic(common.InterceptorShapeError(interceptable))
			}

			if isHTTPInterceptor {
				app.http.addGlobalInterceptor(name, httpIntercept)
			}

			if isWSInterceptor && app.ws != nil {
				for _, h := range app.module.WSMainHandlers {
					mw := traceWSHandler(app.event, trace.StagePreInterceptor, name, common.BuildWSInterceptMiddleware(name, wsIntercept))
					app.ws.eventMatcher.AddMiddlewares(h.EventName, mw)
				}
			}
		}
	}

	for _, mi := range app.module.HTTPInterceptors {
		app.http.addInterceptor(mi)
	}

	if app.ws != nil {
		for _, mi := range app.module.WSInterceptors {
			mw := traceWSHandler(app.event, trace.StagePreInterceptor, mi.Name, common.BuildWSInterceptMiddleware(mi.Name, mi.Handler.(common.WSIntercept)))
			app.ws.eventMatcher.AddMiddlewares(mi.EventName, mw)
		}
	}
}

func (app *App) initMainHandlers() {
	if app.ws != nil {
		for _, h := range app.module.WSMainHandlers {
			app.ws.eventMatcher.AddInjectableHandler(h.EventName, h.Handler)
		}
	}
}

func (app *App) releaseCtx(ex *httpExchange) {
	ex.reset()
	app.ctxPool.Put(ex)
}

func (app *App) releaseWSCtx(c *ctx.WSContext) {
	c.Reset()
	app.wsCtxPool.Put(c)
}

func (app *App) BindGlobalGuards(guarders ...common.Guarder) *App {
	app.globalGuarders = append(app.globalGuarders, guarders...)

	return app
}

func (app *App) BindGlobalInterceptors(interceptors ...common.Interceptable) *App {
	app.globalInterceptors = append(app.globalInterceptors, interceptors...)

	return app
}

func (app *App) BindGlobalExceptionFilters(exceptionFilters ...common.ExceptionFilterable) *App {
	app.globalExceptionFilters = append(app.globalExceptionFilters, exceptionFilters...)

	return app
}

func (app *App) BindGlobalMiddlewares(middlewares ...common.MiddlewareFn) *App {
	app.globalMiddlewares = append(app.globalMiddlewares, middlewares...)

	return app
}

func (app *App) EnableVersioning(v versioning.Versioning) *App {
	app.http.enableVersioning(v)

	return app
}

func (app *App) EnableDevtool() *App {
	app.isDevtoolEnabled = true

	return app
}

func (app *App) EnableAccessLog() *App {
	app.isAccessLogEnabled = true

	return app
}

func (app *App) EnableWS(cfg *WSConfig, middlewares ...common.MiddlewareFn) *App {
	app.isWSEnabled = true
	cfg.globalMiddlewares = middlewares
	app.wsConfig = cfg

	return app
}

func (app *App) UseLogger(logger common.Logger) *App {
	app.Logger = logger
	globalInterfaceByKey.Store(injectableInterfaces[0], app.Logger)

	return app
}

func (app *App) UseLogOptions(opts *log.LogOptions) *App {
	app.LogOptions = opts

	return app
}

func (app *App) Get(p Provider) any {
	return app.injectedProviders[genProviderKey(p)]
}

func (app *App) Listen(port int) error {
	for _, m := range app.module.collectModules() {
		if m.Name != "" {
			app.Logger.Info("InstanceLoader", "module", fmt.Sprintf("%v initialized", m.Name))
		}
	}

	var routeArr []string
	for _, h := range app.module.HTTPMainHandlers {
		routeArr = append(routeArr, h.Pattern)
	}
	sort.Strings(routeArr)

	for _, routeName := range routeArr {
		m, r, v := routing.PatternToMethodRouteVersion(routeName)
		if r == "" {
			r = "/"
		}
		args := []any{"method", m, "route", r}
		if v != "" {
			args = append(args, "version", v)
		}
		app.Logger.Info(
			"RouteExplorer",
			args...,
		)
	}

	if app.isWSEnabled {

		for _, eventName := range app.module.WSMainHandlers {
			app.Logger.Info(
				"WebSocketEvent",
				"event", eventName.EventName,
			)
		}
	}

	addr := fmt.Sprintf(":%v", port)

	server := &http.Server{
		Addr:    addr,
		Handler: app,

		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,

		MaxHeaderBytes: DefaultMaxHeaderBytes,
	}

	logBoostrap(port)

	return app.serveWithGracefulShutdown(server)
}

func logBoostrap(port int) {
	accessURLs := color.FmtBold("%s", color.FmtBGYellow(" GG! Here Are Your Access URLs: ")) + "\n"
	divider := color.FmtDim("--------------------------------------------") + "\n"
	host := color.FmtBold("%s", color.FmtWhite("Localhost: ")) + color.FmtMagenta("%v:%v", "localhost", port) + "\n"
	lan := color.FmtBold("%s", color.FmtWhite("      LAN: ")) + color.FmtMagenta("%v:%v", getLocalIP(), port) + "\n"
	close := color.FmtItalic("%s", color.FmtGreen("Press CTRL+C to stop")) + "\n"

	_, _ = fmt.Fprint(os.Stdout, "\n"+accessURLs+divider+host+lan+divider+close)
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func (app *App) serveWithGracefulShutdown(server *http.Server) error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	httpListernErrChan := make(chan error, 1)
	go func() {
		httpListernErrChan <- server.ListenAndServe()
	}()

	select {
	case err := <-httpListernErrChan:
		return err
	case sig := <-sigChan:
		app.Logger.Info("ShutdownSignal", "signal", sig.String())
		app.callOnShutdown()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return server.Shutdown(ctx)
	}
}

func (app *App) initDevtool() {
	if !app.isDevtoolEnabled {
		return
	}

	devtoolBuilder := devtool.DevtoolBuilder()

	app.devtool = devtoolBuilder.
		AddExceptionFilters(app.globalExceptionFilters, app.module.HTTPExceptionFilters).
		AddMiddlewares(app.globalMiddlewares, app.module.HTTPMiddlewares).
		AddGuarders(app.globalGuarders, app.module.HTTPGuards).
		AddInterceptors(app.globalInterceptors, app.module.HTTPInterceptors).
		AddVersioning(app.http.versioning).
		AddHTTPMainHandlers(app.module.HTTPMainHandlers).
		Build()
}

func (app *App) callOnReady() {
	app.readyOnce.Do(func() {
		for _, m := range app.module.collectModules() {
			if m.OnReady != nil {
				m.OnReady()
			}
		}
	})
}

func (app *App) callOnShutdown() {
	app.shutdownOnce.Do(func() {
		for _, m := range app.module.collectModules() {
			if m.OnShutdown != nil {
				m.OnShutdown()
			}
		}
		close(app.shutdownChan)
	})
}

func (app *App) Stop() {
	app.callOnShutdown()
}
