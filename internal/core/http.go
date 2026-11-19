package core

import (
	stdHTTP "net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/render"
	"go.owlky.com/owlky/internal/routing"
	"go.owlky.com/owlky/internal/str"
	"go.owlky.com/owlky/internal/trace"
	"go.owlky.com/owlky/internal/tracecontext"
	"go.owlky.com/owlky/internal/versioning"
	"go.owlky.com/owlky/log"
)

const cacheControlHeader = "Cache-Control"

var (
	notFoundException         = exception.NotFoundException(stdHTTP.StatusText(stdHTTP.StatusNotFound))
	methodNotAllowedException = exception.MethodNotAllowedException(stdHTTP.StatusText(stdHTTP.StatusMethodNotAllowed))
	goneException             = exception.GoneException("Deprecated URL usage")

	notFoundBody         = notFoundException.AppendJSON(nil)
	methodNotAllowedBody = methodNotAllowedException.AppendJSON(nil)
	goneBody             = goneException.AppendJSON(nil)
	invalidURLBody       = exception.BadRequestException("Invalid URL path").AppendJSON(nil)

	accessDeniedException = exception.ForbiddenException("Access denied")

	noCacheValue = []string{"no-cache"}

	allowMethods = []string{
		stdHTTP.MethodGet,
		stdHTTP.MethodPost,
		stdHTTP.MethodPut,
		stdHTTP.MethodPatch,
		stdHTTP.MethodDelete,
		stdHTTP.MethodOptions,
	}
	allowByMask = buildAllowTable()
)

func buildAllowTable() []string {
	table := make([]string, 1<<len(allowMethods))
	for mask := range table {
		var methods []string
		for i, method := range allowMethods {
			if mask&(1<<i) == 0 {
				continue
			}
			methods = append(methods, method)
			if method == stdHTTP.MethodGet {
				methods = append(methods, stdHTTP.MethodHead)
			}
		}
		table[mask] = strings.Join(methods, ", ")
	}
	return table
}

type HTTP struct {
	route *routing.Router

	versioning          *versioning.Options
	isVersioningEnabled bool

	tracer *trace.Tracer
	logger log.Logger
	stacks *stackSampler
	render *render.Engine

	routes             []*httpRoute
	routeByPattern     map[string]*httpRoute
	assetByName        map[string]assetEntry
	globalMiddlewares  []httpMiddleware
	globalGuards       []httpGuard
	globalInterceptors []httpInterceptor
	globalFilters      []httpFilter
	allowIndexes       []int
}

type httpRoute struct {
	gate         *moduleGate
	middlewares  []httpMiddleware
	guards       []httpGuard
	interceptors []httpInterceptor
	plan         *httpHandlerPlan
	filters      []httpFilter
	public       *publicEntry
	template     string
	pageRoute    string
	isServe      bool
	isPage       bool
	isAsset      bool
	serveIndex   int
	gateIndex    int
}

type httpGuard struct {
	canActivate common.HTTPCanActivate
	name        string
}

type httpFilter struct {
	catch common.HTTPCatch
	name  string
}

type httpInterceptor struct {
	intercept common.HTTPIntercept
	name      string
}

type httpExchange struct {
	ctx.HTTPContext
	writer          responseWriter
	scope           *tracecontext.Scope
	filters         []httpFilter
	middleware      *httpMiddleware
	middlewareStart time.Time
	traced          bool
}

// removeForm deletes the temp files of a parsed multipart form. net/http removes only those of
// the request it passed in, and the exchange parses a copy.
func (ex *httpExchange) removeForm() {
	if r := ex.Request; r != nil && r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
		r.MultipartForm = nil
	}
}

func newHTTPExchange() *httpExchange {
	return &httpExchange{HTTPContext: *ctx.NewHTTPContext()}
}

func (ex *httpExchange) reset() {
	ex.removeForm()
	ctx.HTTPReset(&ex.HTTPContext)
	ex.writer.reset(nil)
	ex.scope = nil
	ex.filters = nil
	ex.middleware = nil
	ex.traced = false
}

func newHTTP() *HTTP {
	return &HTTP{
		route:          routing.NewRouter(),
		routeByPattern: make(map[string]*httpRoute),
	}
}

func (http *HTTP) enableVersioning(v versioning.Options) {
	if v.Key == "" {
		v.Key = "v"
	}
	http.versioning = &v
	http.isVersioningEnabled = true
}

func (http *HTTP) addRoute(mainHandler common.HTTPLayer, plan *httpHandlerPlan) {
	httpMethod := routing.OperationsMapHTTPMethods[mainHandler.Method]
	pattern := routing.MethodRouteVersionToPattern(httpMethod, mainHandler.Route, mainHandler.Version)
	http.route.AddInjectableHandler(httpMethod, mainHandler.Route, mainHandler.Version, mainHandler.Handler)

	route := http.routeByPattern[pattern]
	if route == nil {
		route = &httpRoute{}
		http.routeByPattern[pattern] = route
		http.routes = append(http.routes, route)
	}
	route.plan = plan
	route.isServe = mainHandler.Method == routing.SERVE
	route.isPage = mainHandler.IsPage
	route.template = mainHandler.Template
	route.pageRoute = strings.Trim(mainHandler.Route, "/")
	route.serveIndex = 0
	if r := mainHandler.Route; route.isServe && len(r) >= 2 && r[len(r)-2:] == "*/" {
		route.serveIndex = strings.Count(r, "/") - 1
	}
}

func (http *HTTP) routeOf(layer common.HTTPLayer) *httpRoute {
	httpMethod := routing.OperationsMapHTTPMethods[layer.Method]
	return http.routeByPattern[routing.MethodRouteVersionToPattern(httpMethod, layer.Route, layer.Version)]
}

func (http *HTTP) addGlobalMiddleware(m httpMiddleware) {
	http.globalMiddlewares = append(http.globalMiddlewares, m)
}

func (http *HTTP) addMiddleware(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.middlewares = append(route.middlewares, httpMiddleware{use: layer.Handler.(common.Use), name: layer.Name})
	}
}

func (http *HTTP) addGlobalGuard(name string, canActivate common.HTTPCanActivate) {
	guard := httpGuard{canActivate: canActivate, name: name}
	http.globalGuards = append(http.globalGuards, guard)
	for _, route := range http.routes {
		route.guards = append(route.guards, guard)
	}
}

func (http *HTTP) addGuard(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.guards = append(route.guards, httpGuard{canActivate: layer.Handler.(common.HTTPCanActivate), name: layer.Name})
	}
}

func (http *HTTP) addGlobalInterceptor(name string, intercept common.HTTPIntercept) {
	interceptor := httpInterceptor{intercept: intercept, name: name}
	http.globalInterceptors = append(http.globalInterceptors, interceptor)
	for _, route := range http.routes {
		route.interceptors = append(route.interceptors, interceptor)
	}
}

func (http *HTTP) addInterceptor(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.interceptors = append(route.interceptors, httpInterceptor{intercept: layer.Handler.(common.HTTPIntercept), name: layer.Name})
	}
}

func (http *HTTP) addGlobalFilter(name string, catch common.HTTPCatch) {
	filter := httpFilter{catch: catch, name: name}
	http.globalFilters = append(http.globalFilters, filter)
	for _, route := range http.routes {
		route.filters = append(route.filters, filter)
	}
}

func (http *HTTP) addFilter(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.filters = append(route.filters, httpFilter{catch: layer.Handler.(common.HTTPCatch), name: layer.Name})
	}
}

func (http *HTTP) finishRoutes() {
	for _, route := range http.routes {
		if route.filters == nil {
			route.filters = []httpFilter{}
		}
	}
	http.allowIndexes = http.allowIndexes[:0]
	for i, method := range allowMethods {
		for pattern := range http.routeByPattern {
			if m, _, _ := routing.PatternToMethodRouteVersion(pattern); m == method {
				http.allowIndexes = append(http.allowIndexes, i)
				break
			}
		}
	}
}

func (http *HTTP) handleRequest(ex *httpExchange) {
	defer http.recoverRequest(ex)

	c := &ex.HTTPContext
	method := c.Method
	if method == stdHTTP.MethodHead {
		method = stdHTTP.MethodGet
	}
	version := ""
	if http.isVersioningEnabled {
		version = versioning.Version(http.versioning, c)
	}

	route, isGone := http.match(c, method, version)
	if route == nil {
		if http.runMiddlewares(ex, nil) {
			http.answerNoRoute(ex, method, version, isGone)
		}
		return
	}
	if route.gate != nil {
		live := route.gate.live.Load()
		if live == nil {
			http.answerGated(ex, route)
			return
		}
		route = live.http[route.gateIndex]
	}
	if !http.runMiddlewares(ex, route) {
		return
	}
	http.serveRoute(ex, route)
}

func (http *HTTP) match(c *ctx.HTTPContext, method, version string) (*httpRoute, bool) {
	urlPath := c.URL.Path
	if http.isVersioningEnabled && version == "" {
		if item, values := http.route.Match(method, urlPath, versioning.NeutralVersion, ctx.HTTPParamValues(c)[:0]); item != nil {
			return http.bindRoute(c, item, values), false
		}
		item, _ := http.route.Match(method, urlPath, "", ctx.HTTPParamValues(c)[:0])
		return nil, item != nil
	}

	item, values := http.route.Match(method, urlPath, version, ctx.HTTPParamValues(c)[:0])
	if item == nil {
		item, values = http.route.Match(method, urlPath, versioning.NeutralVersion, values[:0])
		if item == nil {
			return nil, false
		}
	}
	return http.bindRoute(c, item, values), false
}

func (http *HTTP) bindRoute(c *ctx.HTTPContext, item *routing.RouterItem, values []string) *httpRoute {
	ctx.HTTPSetParams(c, item.ParamKeys, values)
	return http.routes[item.Index]
}

func (http *HTTP) runMiddlewares(ex *httpExchange, route *httpRoute) bool {
	var module []httpMiddleware
	if route != nil {
		module = route.middlewares
	}
	if len(http.globalMiddlewares) == 0 && len(module) == 0 {
		ctx.HTTPSetNext(&ex.HTTPContext, noopNext)
		return true
	}

	proceed := false
	next := func() { proceed = true }
	ctx.HTTPSetNext(&ex.HTTPContext, next)
	w := stdHTTP.ResponseWriter(&ex.writer)
	for i := range http.globalMiddlewares {
		if !http.runMiddleware(ex, &http.globalMiddlewares[i], w, next, &proceed) {
			return false
		}
	}
	for i := range module {
		if !http.runMiddleware(ex, &module[i], w, next, &proceed) {
			return false
		}
	}
	return true
}

func noopNext() {}

func (http *HTTP) runMiddleware(ex *httpExchange, m *httpMiddleware, w stdHTTP.ResponseWriter, next ctx.Next, proceed *bool) bool {
	*proceed = false
	if !ex.traced {
		m.use(ex.Request, w, next)
		return *proceed
	}
	ex.middleware = m
	ex.middlewareStart = time.Now()
	m.use(ex.Request, w, next)
	ex.middleware = nil
	m.emit(http.tracer, ex.scope, ex.ID(), time.Since(ex.middlewareStart), 0)
	return *proceed
}

func (http *HTTP) answerNoRoute(ex *httpExchange, method, version string, isGone bool) {
	if isGone {
		http.writeEarlyError(ex, &goneException, goneBody)
		return
	}
	urlPath := ex.URL.Path
	mask := 0
	for _, i := range http.allowIndexes {
		if allowMethods[i] != method && http.hasRoute(allowMethods[i], urlPath, version, ctx.HTTPParamValues(&ex.HTTPContext)[:0]) {
			mask |= 1 << i
		}
	}
	if mask == 0 {
		http.writeEarlyError(ex, &notFoundException, notFoundBody)
		return
	}
	ex.writer.Header()["Allow"] = []string{allowByMask[mask]}
	http.writeEarlyError(ex, &methodNotAllowedException, methodNotAllowedBody)
}

func (http *HTTP) hasRoute(method, urlPath, version string, buf []string) bool {
	if item, _ := http.route.Match(method, urlPath, version, buf); item != nil {
		return true
	}
	item, _ := http.route.Match(method, urlPath, versioning.NeutralVersion, buf)
	return item != nil
}

func (http *HTTP) serveRoute(ex *httpExchange, route *httpRoute) {
	c := &ex.HTTPContext
	ex.filters = route.filters
	if route.isPage {
		ctx.HTTPSetPage(c)
		if c.Method == stdHTTP.MethodPost {
			ctx.HTTPSetCode(c, stdHTTP.StatusUnprocessableEntity)
		}
	} else if c.Method == stdHTTP.MethodPost {
		ctx.HTTPSetCode(c, stdHTTP.StatusCreated)
	}

	for i := range route.guards {
		if !http.runGuard(ex, &route.guards[i]) {
			refusal := accessDeniedException
			http.catch(ex, &refusal)
			return
		}
	}
	if route.isAsset {
		http.serveAsset(ex)
		return
	}
	if route.public != nil {
		http.servePublic(ex, route.public)
		return
	}

	for i := range route.interceptors {
		agg := aggregation.NewAggregation()
		aggregation.SetName(agg, route.interceptors[i].name)
		ctx.HTTPAddAggregation(c, agg)
		value := http.runIntercept(ex, &route.interceptors[i], agg)
		if !aggregation.IsMainHandlerCalled(agg) {
			aggregations := ctx.HTTPAggregations(c)
			http.writeResult(ex, route, reflect.ValueOf(http.runPosts(ex, aggregations[:len(aggregations)-1], value)))
			return
		}
		aggregation.SetInterceptorData(agg, value)
	}

	results := route.plan.call(c, http.tracer, ex.traced)
	result := reflect.Value{}
	switch len(results) {
	case 0:
	case 1:
		result = results[0]
	default:
		setStatusCode(c, results[0])
		result = results[1]
	}

	if aggregations := ctx.HTTPAggregations(c); len(aggregations) > 0 {
		var data any
		if result.IsValid() {
			data = result.Interface()
		}
		result = reflect.ValueOf(http.runPosts(ex, aggregations, data))
	}
	http.writeResult(ex, route, result)
}

func (http *HTTP) runGuard(ex *httpExchange, g *httpGuard) bool {
	if !ex.traced {
		return g.canActivate(&ex.HTTPContext)
	}
	start := time.Now()
	isAllowed := g.canActivate(&ex.HTTPContext)
	http.tracer.Emit(stageEvent(ex.scope, ex.ID(), trace.StageGuard, g.name, trace.TransportHTTP, time.Since(start)))
	return isAllowed
}

func (http *HTTP) runIntercept(ex *httpExchange, i *httpInterceptor, agg *aggregation.Aggregation) any {
	if !ex.traced {
		return i.intercept(&ex.HTTPContext, agg)
	}
	start := time.Now()
	value := i.intercept(&ex.HTTPContext, agg)
	http.tracer.Emit(stageEvent(ex.scope, ex.ID(), trace.StagePreInterceptor, i.name, trace.TransportHTTP, time.Since(start)))
	return value
}

func (http *HTTP) runPosts(ex *httpExchange, aggregations []*aggregation.Aggregation, data any) any {
	for i := len(aggregations) - 1; i >= 0; i-- {
		agg := aggregations[i]
		aggregation.SetMainData(agg, data)
		if !ex.traced {
			data = aggregation.Aggregate(agg)
			continue
		}
		start := time.Now()
		data = aggregation.Aggregate(agg)
		http.tracer.Emit(stageEvent(ex.scope, ex.ID(), trace.StagePostInterceptor, aggregation.Name(agg), trace.TransportHTTP, time.Since(start)))
	}
	return data
}

func (http *HTTP) writeResult(ex *httpExchange, route *httpRoute, result reflect.Value) {
	if route.isPage {
		http.writePage(ex, route, result)
		return
	}
	if !route.isServe {
		returnHTTP(&ex.HTTPContext, result)
		return
	}
	var dir any
	if result.IsValid() {
		dir = result.Interface()
	}
	serveContent(ex, route.serveIndex, dir)
}

func serveContent(ex *httpExchange, serveIndex int, result any) {
	dir, ok := result.(string)
	if !ok {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}

	r := ex.Request
	if serveIndex != 0 {
		root := path.Clean(dir)
		segments := strings.Split(str.RemoveDup(r.URL.Path, '/'), "/")
		dir = root
		if serveIndex < len(segments) {
			dir = path.Join(root, strings.Join(segments[serveIndex:], "/"))
		}
		rel, ok := relativeToRoot(root, dir)
		if !ok {
			serveError(ex, stdHTTP.StatusBadRequest, invalidURLBody)
			return
		}
		serveFromRoot(ex, root, rel)
		return
	}

	info, err := os.Stat(dir)
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	if info.IsDir() {
		index := filepath.Join(dir, "index.html")
		if indexInfo, err := os.Stat(index); err != nil || indexInfo.IsDir() {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			dir = index
		}
	}
	setDefaultCacheControl(ex.writer.Header())
	stdHTTP.ServeFile(&ex.writer, r, dir)
}

func relativeToRoot(root, dir string) (string, bool) {
	if root == "." {
		if dir == ".." || strings.HasPrefix(dir, "../") {
			return "", false
		}
		if dir == "." {
			return "", true
		}
		return dir, true
	}
	if dir == root {
		return "", true
	}
	if !strings.HasPrefix(dir, root+"/") {
		return "", false
	}
	return dir[len(root)+1:], true
}

func serveFromRoot(ex *httpExchange, rootDir, rel string) {
	if hasHiddenSegment(rel) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	defer func() { _ = root.Close() }()
	name := rel
	if name == "" {
		name = "."
	}
	info, err := root.Stat(name)
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	if info.IsDir() {
		index := path.Join(name, "index.html")
		if indexInfo, err := root.Stat(index); err != nil || indexInfo.IsDir() {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		if strings.HasSuffix(ex.Request.URL.Path, "/") {
			name = index
		}
	}
	setDefaultCacheControl(ex.writer.Header())
	stdHTTP.ServeFileFS(&ex.writer, ex.Request, root.FS(), name)
}

func serveError(ex *httpExchange, code int, body []byte) {
	delete(ex.writer.Header(), cacheControlHeader)
	writeJSONError(&ex.writer, code, body)
}

func setDefaultCacheControl(h stdHTTP.Header) {
	if _, ok := h[cacheControlHeader]; !ok {
		h[cacheControlHeader] = noCacheValue
	}
}

func (http *HTTP) recoverRequest(ex *httpExchange) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == stdHTTP.ErrAbortHandler {
		ex.writer.runHooks(ctx.HTTPTimestamp(&ex.HTTPContext))
		ex.removeForm()
		panic(rec)
	}
	if ex.filters != nil {
		http.catch(ex, rec)
		return
	}

	code := ex.writer.status()
	if code != 0 {
		http.logAfterResponse(ex, rec)
	} else {
		code = http.writeEarlyError(ex, http.exceptionOf(ex, rec), nil)
		ctx.HTTPSetCode(&ex.HTTPContext, code)
	}
	if m := ex.middleware; m != nil {
		ex.middleware = nil
		m.emit(http.tracer, ex.scope, ex.ID(), time.Since(ex.middlewareStart), code)
	}
}

func (http *HTTP) catch(ex *httpExchange, rec any) {
	for i := range ex.filters {
		if ex.writer.status() != 0 {
			http.logAfterResponse(ex, rec)
			return
		}
		next, isHandled := http.runFilter(ex, &ex.filters[i], http.exceptionOf(ex, rec))
		if isHandled {
			return
		}
		if next == stdHTTP.ErrAbortHandler {
			ex.writer.runHooks(ctx.HTTPTimestamp(&ex.HTTPContext))
			ex.removeForm()
			panic(next)
		}
		rec = next
	}
	if ex.writer.status() != 0 {
		http.logAfterResponse(ex, rec)
		return
	}
	c := &ex.HTTPContext
	ctx.HTTPSetCode(c, http.writeError(c, &ex.writer, http.exceptionOf(ex, rec), ctx.HTTPIsPage(c)))
}

func (http *HTTP) runFilter(ex *httpExchange, f *httpFilter, e *exception.Exception) (any, bool) {
	if !ex.traced {
		return callCatch(&ex.HTTPContext, f.catch, e)
	}
	start := time.Now()
	next, isHandled := callCatch(&ex.HTTPContext, f.catch, e)
	http.tracer.Emit(stageEvent(ex.scope, ex.ID(), trace.StageExceptionFilter, f.name, trace.TransportHTTP, time.Since(start)))
	return next, isHandled
}

func callCatch(c *ctx.HTTPContext, catchFn common.HTTPCatch, e *exception.Exception) (next any, isHandled bool) {
	defer func() {
		if !isHandled {
			next = recover()
		}
	}()
	catchFn(c, e)
	return nil, true
}

func (http *HTTP) exceptionOf(ex *httpExchange, rec any) *exception.Exception {
	if e, ok := asException(rec); ok {
		return e
	}
	logPanic(http.logger, http.stacks, "UnhandledPanic", &ex.HTTPContext, rec)
	e := internalServerErrorException
	return &e
}

func (http *HTTP) logAfterResponse(ex *httpExchange, rec any) {
	value := rec
	if e, ok := asException(rec); ok {
		value = e.Message()
	}
	c := &ex.HTTPContext
	l := http.logger.From(c.Context())
	if stack, ok := http.stacks.stack(); ok {
		l.Error("ErrorAfterResponse", "method", c.Method, "path", c.URL.Path, "code", ex.writer.status(), "panic", value, "stack", stack)
		return
	}
	l.Error("ErrorAfterResponse", "method", c.Method, "path", c.URL.Path, "code", ex.writer.status(), "panic", value)
}
