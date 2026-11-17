package core

import (
	"net/http"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/accesslog"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/devtool"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/memorybroker"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/trace"
	"github.com/dangduoc08/ginject/versioning"
	"golang.org/x/net/websocket"
)

const (
	loggerKey         = "github.com/dangduoc08/ginject/common/common.Logger"
	traceparentHeader = "Traceparent"
)

type App struct {
	http      *HTTP
	ctxPool   sync.Pool
	wsCtxPool sync.Pool

	tracer trace.Tracer

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

	logger     common.Logger
	stacks     *stackSampler
	isLogAsync bool

	maxRequestBodyBytes atomic.Int64
	serverDefaults      serverDefaults
	notifySignals       func() (<-chan os.Signal, func())

	readyOnce sync.Once

	serveMu        sync.Mutex
	serveState     serveState
	stopRequest    chan struct{}
	hooksGoroutine uint64
	shutdownOnce   sync.Once
	shutdownDone   chan struct{}
}

func New() *App {
	app := &App{
		http:      newHTTP(),
		ws:        nil,
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
	ex.scope = tracecontext.ScopeOf(c.Request.Context())
	requestID, traceparent := ex.scope.Headers()

	if app.isWSEnabled && app.ws.isWSPath(r.URL.Path) {
		app.serveWS(w, ex, requestID, traceparent)
		return
	}

	h := w.Header()
	h[ctx.RequestID] = requestID
	h[traceparentHeader] = traceparent
	ex.traced = app.tracer.Enabled()
	app.http.handleRequest(ex)
	ex.writer.runHooks(&c.Timestamp)
	if ex.traced {
		code := ex.writer.status()
		if code == 0 {
			code = http.StatusOK
		}
		e := completeEvent(ex.scope, c.GetID(), trace.TransportHTTP, c.Method, c.URL.Path, code, time.Since(c.Timestamp))
		e.ClientIP = tracecontext.ClientIP(c.Request)
		app.tracer.Emit(e)
	}
	app.releaseCtx(ex)
}

func (app *App) serveWS(w http.ResponseWriter, ex *httpExchange, requestID, traceparent []string) {
	c := &ex.HTTPContext
	isReleased := false
	app.ws.upgrade(w, c.Request, websocket.Server{
		Handshake: func(wsCfg *websocket.Config, _ *http.Request) error {
			isReleased = true
			defer app.releaseCtx(ex)

			if wsCfg.Header == nil {
				wsCfg.Header = make(http.Header, 2)
			}
			wsCfg.Header[ctx.RequestID] = requestID
			wsCfg.Header[traceparentHeader] = traceparent
			c.Status(http.StatusSwitchingProtocols)
			err := app.ws.handshake(c)
			ex.writer.runHooks(&c.Timestamp)
			if app.tracer.Enabled() {
				e := completeEvent(ex.scope, c.GetID(), trace.TransportHTTP, c.Method, c.URL.Path, c.Code, time.Since(c.Timestamp))
				e.ClientIP = tracecontext.ClientIP(c.Request)
				app.tracer.Emit(e)
			}
			return err
		},
		Handler: websocket.Handler(app.ws.handleRequest),
	})
	if !isReleased {
		app.releaseCtx(ex)
	}
}

func (app *App) Create(m *Module) {
	if m == nil {
		panic(color.FmtRed("invalid module: Create needs a root module"))
	}
	if app.isCreateCalled {
		panic(color.FmtRed("invalid app: Create called twice"))
	}
	app.isCreateCalled = true

	app.stacks = newStackSampler(app.logger)
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
	app.isLogAsync = true
	app.serveMu.Unlock()
	logpipe.Stdout().StartAsync()
}

func (app *App) initWS() {
	if !app.isWSEnabled {
		return
	}

	app.wsConfig.injectedProviders = app.container.providerByKey
	app.wsConfig.container = app.container
	app.wsConfig.logger = app.logger
	app.wsConfig.stacks = app.stacks
	app.wsConfig.tracer = &app.tracer
	app.wsConfig.broker = &app.broker
	planByEvent := app.container.wsPlanByEvent
	app.wsConfig.resolveAndCallHandler = func(pattern string, _ any, c *ctx.WSContext) []reflect.Value {
		return planByEvent[pattern].invoke(c, &app.tracer)
	}
	app.wsConfig.newCtx = func() *ctx.WSContext { return app.wsCtxPool.Get().(*ctx.WSContext) }
	app.wsConfig.releaseCtx = app.releaseWSCtx
	app.ws = NewWS(app.wsConfig)
	app.ws.emitComplete = func(c *ctx.WSContext, operation, target string) {
		if !app.tracer.Enabled() {
			return
		}
		app.tracer.Emit(completeEvent(tracecontext.ScopeOf(c.Context()), c.GetID(), trace.TransportWS, operation, target, 0, time.Since(c.Timestamp)))
	}
}

func (app *App) initAccessLog() {
	if !app.isAccessLogEnabled {
		return
	}

	app.accesslog = accesslog.NewAccessLog(&accesslog.AccessLogConfig{
		Tracer: &app.tracer,
		Logger: app.logger,
	})
}

func (app *App) initRoutes() {
	app.http.tracer = &app.tracer
	app.http.logger = app.logger
	app.http.stacks = app.stacks
	for _, h := range app.container.httpMainHandlers {
		app.http.addRoute(h, app.container.httpPlanByPattern[h.Pattern])
	}
}

func (app *App) initExceptionFilters() {
	for i := len(app.container.httpExceptionFilters) - 1; i >= 0; i-- {
		ef := app.container.httpExceptionFilters[i]
		app.http.addFilter(ef)
	}

	if app.ws != nil {
		for i := len(app.container.wsExceptionFilters) - 1; i >= 0; i-- {
			ef := app.container.wsExceptionFilters[i]
			catchFn := traceWSCatch(&app.tracer, ef.Name, ef.Handler.(common.WSCatch))
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
				app.http.addGlobalFilter(name, httpCatch)
			}

			if isWSFilter && app.ws != nil {
				tracedWSCatch := traceWSCatch(&app.tracer, name, wsCatch)
				for _, h := range app.container.wsMainHandlers {
					app.ws.catchFnsByEvent[h.EventName] = append(app.ws.catchFnsByEvent[h.EventName], tracedWSCatch)
				}
			}
		}
	}
}

func traceWSCatch(ev *trace.Tracer, name string, fn common.WSCatch) common.WSCatch {
	return func(c *ctx.WSContext, ex *exception.Exception) {
		if !ev.Enabled() {
			fn(c, ex)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.GetID(), trace.StageExceptionFilter, name, trace.TransportWS, time.Since(start)))
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
				mw := traceWSHandler(&app.tracer, trace.StageGuard, name, common.BuildWSGuardMiddleware(wsCanActivate))
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
			mw := traceWSHandler(&app.tracer, trace.StageGuard, mg.Name, common.BuildWSGuardMiddleware(mg.Handler.(common.WSCanActivate)))
			app.ws.eventMatcher.AddMiddlewares(mg.EventName, mw)
		}
	}
}

func traceWSHandler(ev *trace.Tracer, stage, name string, h ctx.WSHandler) ctx.WSHandler {
	return func(c *ctx.WSContext) {
		if !ev.Enabled() {
			h(c)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.GetID(), stage, name, trace.TransportWS, time.Since(start)))
		}()
		h(c)
	}
}

func (app *App) initInterceptors() {
	if app.ws != nil {
		app.ws.emitPostInterceptor = func(c *ctx.WSContext, name string, duration time.Duration) {
			if !app.tracer.Enabled() {
				return
			}
			app.tracer.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.GetID(), trace.StagePostInterceptor, name, trace.TransportWS, duration))
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
					mw := traceWSHandler(&app.tracer, trace.StagePreInterceptor, name, common.BuildWSInterceptMiddleware(name, wsIntercept))
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
			mw := traceWSHandler(&app.tracer, trace.StagePreInterceptor, mi.Name, common.BuildWSInterceptMiddleware(mi.Name, mi.Handler.(common.WSIntercept)))
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

func (app *App) SetLogger(logger common.LogAdapter) *App {
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
