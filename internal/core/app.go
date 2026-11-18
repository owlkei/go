package core

import (
	"context"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/accesslog"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/devtool"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/memorybroker"
	"github.com/dangduoc08/ginject/internal/render"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/internal/versioning"
	"github.com/dangduoc08/ginject/log"
	"golang.org/x/net/websocket"
)

const traceparentHeader = "Traceparent"

var loggerKey = genFieldKey(reflect.TypeFor[log.Logger]())

type App struct {
	http      *HTTP
	renderDir string
	ctxPool   sync.Pool
	wsCtxPool sync.Pool

	tracer trace.Tracer

	isDevtoolEnabled bool
	devtool          *devtool.Devtool

	isAccessLogEnabled bool
	accesslog          *accesslog.AccessLog

	ws          *WS
	wsConfig    *WSOptions
	isWSEnabled bool

	container      *container
	isCreateCalled bool
	isCreated      bool

	globalMiddlewares      []common.MiddlewareFn
	globalGuarders         []any
	globalInterceptors     []any
	globalExceptionFilters []any

	wsGlobalGuards       []ctx.WSHandler
	wsGlobalInterceptors []wsGlobalInterceptor
	wsGlobalCatches      []common.WSCatch

	startupTimeout time.Duration

	broker memorybroker.Broker

	logger     log.Logger
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
		startupTimeout: defaultStartupTimeout,
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
	ctx.HTTPInit(c, &ex.writer, r)
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
	ex.writer.runHooks(ctx.HTTPTimestamp(c))
	if ex.traced {
		code := ex.writer.status()
		if code == 0 {
			code = http.StatusOK
		}
		e := completeEvent(ex.scope, c.ID(), trace.TransportHTTP, c.Method, c.URL.Path, code, time.Since(*ctx.HTTPTimestamp(c)))
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
			ex.writer.runHooks(ctx.HTTPTimestamp(c))
			if app.tracer.Enabled() {
				e := completeEvent(ex.scope, c.ID(), trace.TransportHTTP, c.Method, c.URL.Path, ctx.HTTPCode(c), time.Since(*ctx.HTTPTimestamp(c)))
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

	start := time.Now()
	createCtx, cancel := context.WithDeadline(context.Background(), start.Add(app.startupTimeout))
	defer cancel()

	app.stacks = newStackSampler(app.logger)
	app.container.addInterface(loggerKey, app.logger)
	app.container.isVersioningEnabled = app.http.isVersioningEnabled
	app.container.logger = app.logger
	app.container.createStart = start
	app.container.startupTimeout = app.startupTimeout
	app.container.createCtx = createCtx
	app.initRender()
	app.container.wire(m)
	app.checkAppWideLayers()
	app.checkRenderBindings()
	app.initWS()
	app.initRoutes()
	app.initMiddlewares()
	app.initExceptionFilters()
	app.initGuards()
	app.initInterceptors()
	app.initMainHandlers()
	app.setWSGates()
	app.http.finishRoutes()
	app.initDevtool()
	app.initAccessLog()
	readyAtCreate := slices.Clone(app.container.readyOrder)
	app.startEntries()
	app.callOnReady(readyAtCreate)

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
		app.tracer.Emit(completeEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.TransportWS, operation, target, 0, time.Since(ctx.WSTimestamp(c))))
	}
}

func (app *App) initAccessLog() {
	if !app.isAccessLogEnabled {
		return
	}

	app.accesslog = accesslog.NewAccessLog(&accesslog.AccessLogOptions{
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
		if ref, ok := app.container.gateByPattern[h.Pattern]; ok {
			route := app.http.routeOf(h)
			route.gate = ref.gate
			route.gateIndex = ref.index
		}
	}
	app.addPublicRoutes()
	app.addAssetRoute()
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
			if builtIn, ok := gef.(globalHTTPExceptionFilter); ok {
				builtIn.http = app.http
				app.http.addGlobalFilter(name, builtIn.Catch)
				continue
			}
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
				app.wsGlobalCatches = append(app.wsGlobalCatches, tracedWSCatch)
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
			ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.StageExceptionFilter, name, trace.TransportWS, time.Since(start)))
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
				app.wsGlobalGuards = append(app.wsGlobalGuards, mw)
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
			ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), stage, name, trace.TransportWS, time.Since(start)))
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
			app.tracer.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.StagePostInterceptor, name, trace.TransportWS, duration))
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
				app.wsGlobalInterceptors = append(app.wsGlobalInterceptors, wsGlobalInterceptor{name: name, intercept: wsIntercept})
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
	ctx.WSReset(c)
	app.wsCtxPool.Put(c)
}

func (app *App) BindGlobalGuards(guards ...any) *App {
	app.mustBeBeforeCreate("BindGlobalGuards")
	app.globalGuarders = append(app.globalGuarders, guards...)

	return app
}

func (app *App) BindGlobalInterceptors(interceptors ...any) *App {
	app.mustBeBeforeCreate("BindGlobalInterceptors")
	app.globalInterceptors = append(app.globalInterceptors, interceptors...)

	return app
}

func (app *App) BindGlobalExceptionFilters(exceptionFilters ...any) *App {
	app.mustBeBeforeCreate("BindGlobalExceptionFilters")
	app.globalExceptionFilters = append(app.globalExceptionFilters, exceptionFilters...)

	return app
}

func (app *App) BindGlobalMiddlewares(middlewares ...common.MiddlewareFn) *App {
	app.mustBeBeforeCreate("BindGlobalMiddlewares")
	app.globalMiddlewares = append(app.globalMiddlewares, middlewares...)

	return app
}

func (app *App) EnableVersioning(opts *versioning.Options) *App {
	app.mustBeBeforeCreate("EnableVersioning")
	var v versioning.Options
	if opts != nil {
		v = *opts
	}
	app.http.enableVersioning(v)

	return app
}

func (app *App) EnableRender(opts *RenderOptions) *App {
	app.mustBeBeforeCreate("EnableRender")
	if opts == nil || opts.FS == nil {
		panic(color.FmtRed("invalid render: EnableRender needs RenderOptions.FS"))
	}
	if app.http.render != nil {
		panic(color.FmtRed("invalid render: EnableRender called twice"))
	}
	fsys, dir := renderFS(opts)
	app.renderDir = dir
	app.http.render = render.New(fsys, opts.IsReload)
	app.container.render = app.http.render

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

func (app *App) EnableWS(cfg *WSOptions, middlewares ...common.MiddlewareFn) *App {
	app.mustBeBeforeCreate("EnableWS")
	app.isWSEnabled = true
	cfg.globalMiddlewares = middlewares
	app.wsConfig = cfg

	return app
}

func (app *App) SetLogger(logger log.LogAdapter) *App {
	app.mustBeBeforeCreate("SetLogger")
	if logger == nil {
		panic(color.FmtRed("invalid app: SetLogger needs a logger"))
	}
	app.logger = log.WrapLogger(logger, nil)

	return app
}

func (app *App) SetStartupTimeout(d time.Duration) *App {
	app.mustBeBeforeCreate("SetStartupTimeout")
	if d <= 0 {
		panic(color.FmtRed("invalid app: SetStartupTimeout needs a positive duration"))
	}
	app.startupTimeout = d

	return app
}

func (app *App) Logger() log.Logger {
	return app.logger
}

func (app *App) mustBeBeforeCreate(method string) {
	if app.isCreateCalled {
		panic(color.FmtRed("invalid app: %s must be called before Create", method))
	}
}

func (app *App) Get(p Provider) any {
	key := genProviderKey(p)
	app.container.mu.RLock()
	defer app.container.mu.RUnlock()
	return app.container.providerByKey[key]
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
		AddRender(app.renderSnapshot()).
		AddModules(app.container.moduleRows).
		Build()
}

func (app *App) callOnReady(modules []*Module) {
	app.readyOnce.Do(func() {
		for _, m := range modules {
			if m.OnReady != nil {
				m.OnReady()
			}
		}
	})
}
