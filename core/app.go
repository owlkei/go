package core

import (
	"net/http"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
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
	"github.com/dangduoc08/ginject/trace"
	"github.com/dangduoc08/ginject/versioning"
	"golang.org/x/net/websocket"
)

const loggerKey = "github.com/dangduoc08/ginject/common/common.Logger"

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

	container      *container
	isCreateCalled bool
	isCreated      bool

	globalMiddlewares      []common.MiddlewareFn
	globalGuarders         []common.Guarder
	globalInterceptors     []common.Interceptable
	globalExceptionFilters []common.ExceptionFilterable

	broker memorybroker.Broker

	logger common.Logger

	maxRequestBodyBytes atomic.Int64
	serverDefaults      serverDefaults
	notifySignals       func() (<-chan os.Signal, func())

	readyOnce sync.Once

	serveMu        sync.Mutex
	serveState     serveState
	stopRequest    chan struct{}
	isRunningHooks bool
	shutdownOnce   sync.Once
	shutdownDone   chan struct{}
}

func New() *App {
	app := &App{
		http:      newHTTP(),
		ws:        nil,
		event:     event.NewEvent(),
		broker:    memorybroker.NewMemoryBroker(),
		container: newContainer(),
		logger:    log.NewLog(nil),
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
		serverDefaults: newServerDefaults(),
		notifySignals:  notifyShutdownSignals,
		shutdownDone:   make(chan struct{}),
	}
	app.maxRequestBodyBytes.Store(defaultMaxRequestBodyBytes)

	app.container.addInterface(publisherKey, common.Publisher(newPublisher(app.broker)))

	app.BindGlobalExceptionFilters(globalHTTPExceptionFilter{}, globalWSExceptionFilter{})

	return app
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil && r.Body != http.NoBody {
		if limit := app.maxRequestBodyBytes.Load(); limit > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
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
//  1. wire must run first: it initializes the modules (imports first, OnInit
//     then providers), injects the controllers and fills app.container, which
//     everything below reads from.
//  2. initWS must run right after that, and BEFORE initExceptionFilters/
//     initGuards/initInterceptors/initMainHandlers: those functions write
//     into app.ws.catchFnsByEvent / app.ws.eventMatcher whenever the app has
//     WS handlers. app.ws stays nil if EnableWS was never called, so every one
//     of those call sites is guarded with app.ws != nil.
//  3. initDevtool must run after the routes and layers exist: it reads them to
//     build the devtool snapshot.
//  4. callOnReady must run last, after all initialization is complete. Only
//     then is the app marked created, so a Create that did not finish runs no
//     OnShutdown.
//
// Do not reorder these calls without re-checking every init* function for
// an unguarded app.ws.* access.
func (app *App) Create(m *Module) {
	if m == nil {
		panic(color.FmtRed("invalid module: Create needs a root module"))
	}
	if app.isCreateCalled {
		panic(color.FmtRed("invalid app: Create called twice"))
	}
	app.isCreateCalled = true

	app.container.addInterface(loggerKey, app.logger)
	app.container.wire(m)
	app.initWS()
	app.initRoutes()
	app.initMiddlewares()
	app.initExceptionFilters()
	app.initGuards()
	app.initInterceptors()
	app.initMainHandlers()
	app.http.finishRoutes()
	app.initDevtool()
	app.initAccessLog()
	app.callOnReady()

	app.serveMu.Lock()
	app.isCreated = true
	app.serveMu.Unlock()
}

func (app *App) initWS() {
	if !app.isWSEnabled {
		return
	}

	app.wsConfig.injectedProviders = app.container.providerByKey
	app.wsConfig.container = app.container
	app.wsConfig.logger = app.logger
	app.wsConfig.event = app.event
	app.wsConfig.broker = &app.broker
	planByEvent := app.container.wsPlanByEvent
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
		Logger: app.logger,
	})
}

func (app *App) initRoutes() {
	app.http.event = app.event
	app.http.logger = app.logger
	for _, h := range app.container.httpMainHandlers {
		app.http.addRoute(h, app.container.httpPlanByPattern[h.Pattern])
	}
}

func (app *App) initExceptionFilters() {
	for i := len(app.container.httpExceptionFilters) - 1; i >= 0; i-- {
		ef := app.container.httpExceptionFilters[i]
		app.http.addFilter(ef, traceHTTPCatch(app.event, ef.Name, ef.Handler.(common.HTTPCatch)))
	}

	if app.ws != nil {
		for i := len(app.container.wsExceptionFilters) - 1; i >= 0; i-- {
			ef := app.container.wsExceptionFilters[i]
			catchFn := traceWSCatch(app.event, ef.Name, ef.Handler.(common.WSCatch))
			app.ws.catchFnsByEvent[ef.EventName] = append(app.ws.catchFnsByEvent[ef.EventName], catchFn)
		}
	}

	if len(app.globalExceptionFilters) > 0 {
		for i := len(app.globalExceptionFilters) - 1; i >= 0; i-- {
			gef := app.globalExceptionFilters[i]
			name := reflect.TypeOf(gef).String()
			newGef, err := injectDependencies(gef, "exceptionFilter", app.container.providerByKey, app.container)
			if err != nil {
				panic(err)
			}
			exceptionFilterable := app.container.construct(newGef.Interface(), "NewExceptionFilter")

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
				for _, h := range app.container.wsMainHandlers {
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

func (app *App) initMiddlewares() {
	for _, gm := range app.globalMiddlewares {
		app.http.addGlobalMiddleware(newHTTPMiddleware(gm, app.container.providerByKey, app.container))
	}

	for _, rm := range app.container.httpMiddlewares {
		app.http.addMiddleware(rm)
	}
}

func (app *App) initGuards() {
	if len(app.globalGuarders) > 0 {
		for _, gg := range app.globalGuarders {
			name := reflect.TypeOf(gg).String()
			newGG, err := injectDependencies(gg, "guard", app.container.providerByKey, app.container)
			if err != nil {
				panic(err)
			}
			guarder := app.container.construct(newGG.Interface(), "NewGuard")

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
				for _, h := range app.container.wsMainHandlers {
					app.ws.eventMatcher.AddMiddlewares(h.EventName, mw)
				}
			}
		}
	}

	for _, mg := range app.container.httpGuards {
		app.http.addGuard(mg)
	}

	if app.ws != nil {
		for _, mg := range app.container.wsGuards {
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

func (app *App) initInterceptors() {
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
			newGI, err := injectDependencies(gi, "interceptor", app.container.providerByKey, app.container)
			if err != nil {
				panic(err)
			}
			interceptable := app.container.construct(newGI.Interface(), "NewInterceptor")

			httpIntercept, isHTTPInterceptor := common.AsHTTPInterceptor(interceptable)
			wsIntercept, isWSInterceptor := common.AsWSInterceptor(interceptable)
			if !isHTTPInterceptor && !isWSInterceptor {
				panic(common.InterceptorShapeError(interceptable))
			}

			if isHTTPInterceptor {
				app.http.addGlobalInterceptor(name, httpIntercept)
			}

			if isWSInterceptor && app.ws != nil {
				for _, h := range app.container.wsMainHandlers {
					mw := traceWSHandler(app.event, trace.StagePreInterceptor, name, common.BuildWSInterceptMiddleware(name, wsIntercept))
					app.ws.eventMatcher.AddMiddlewares(h.EventName, mw)
				}
			}
		}
	}

	for _, mi := range app.container.httpInterceptors {
		app.http.addInterceptor(mi)
	}

	if app.ws != nil {
		for _, mi := range app.container.wsInterceptors {
			mw := traceWSHandler(app.event, trace.StagePreInterceptor, mi.Name, common.BuildWSInterceptMiddleware(mi.Name, mi.Handler.(common.WSIntercept)))
			app.ws.eventMatcher.AddMiddlewares(mi.EventName, mw)
		}
	}
}

func (app *App) initMainHandlers() {
	if app.ws != nil {
		for _, h := range app.container.wsMainHandlers {
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
	app.mustBeBeforeCreate("BindGlobalGuards")
	app.globalGuarders = append(app.globalGuarders, guarders...)

	return app
}

func (app *App) BindGlobalInterceptors(interceptors ...common.Interceptable) *App {
	app.mustBeBeforeCreate("BindGlobalInterceptors")
	app.globalInterceptors = append(app.globalInterceptors, interceptors...)

	return app
}

func (app *App) BindGlobalExceptionFilters(exceptionFilters ...common.ExceptionFilterable) *App {
	app.mustBeBeforeCreate("BindGlobalExceptionFilters")
	app.globalExceptionFilters = append(app.globalExceptionFilters, exceptionFilters...)

	return app
}

func (app *App) BindGlobalMiddlewares(middlewares ...common.MiddlewareFn) *App {
	app.mustBeBeforeCreate("BindGlobalMiddlewares")
	app.globalMiddlewares = append(app.globalMiddlewares, middlewares...)

	return app
}

func (app *App) EnableVersioning(v versioning.Versioning) *App {
	app.mustBeBeforeCreate("EnableVersioning")
	app.http.enableVersioning(v)

	return app
}

func (app *App) EnableDevtool() *App {
	app.mustBeBeforeCreate("EnableDevtool")
	app.isDevtoolEnabled = true

	return app
}

func (app *App) EnableAccessLog() *App {
	app.mustBeBeforeCreate("EnableAccessLog")
	app.isAccessLogEnabled = true

	return app
}

func (app *App) EnableWS(cfg *WSConfig, middlewares ...common.MiddlewareFn) *App {
	app.mustBeBeforeCreate("EnableWS")
	app.isWSEnabled = true
	cfg.globalMiddlewares = middlewares
	app.wsConfig = cfg

	return app
}

func (app *App) SetLogger(logger common.Logger) *App {
	app.mustBeBeforeCreate("SetLogger")
	if logger == nil {
		panic(color.FmtRed("invalid app: SetLogger needs a logger"))
	}
	app.logger = log.WrapLogger(logger, nil)

	return app
}

func (app *App) Logger() common.Logger {
	return app.logger
}

func (app *App) mustBeBeforeCreate(method string) {
	if app.isCreateCalled {
		panic(color.FmtRed("invalid app: %s must be called before Create", method))
	}
}

func (app *App) Get(p Provider) any {
	return app.container.providerByKey[genProviderKey(p)]
}

func (app *App) initDevtool() {
	if !app.isDevtoolEnabled {
		return
	}

	devtoolBuilder := devtool.DevtoolBuilder()

	app.devtool = devtoolBuilder.
		AddExceptionFilters(app.globalExceptionFilters, app.container.httpExceptionFilters).
		AddMiddlewares(app.globalMiddlewares, app.container.httpMiddlewares).
		AddGuarders(app.globalGuarders, app.container.httpGuards).
		AddInterceptors(app.globalInterceptors, app.container.httpInterceptors).
		AddVersioning(app.http.versioning).
		AddHTTPMainHandlers(app.container.httpMainHandlers).
		Build()
}

func (app *App) callOnReady() {
	app.readyOnce.Do(func() {
		for _, m := range app.container.modules {
			if m.OnReady != nil {
				m.OnReady()
			}
		}
	})
}
