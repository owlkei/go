package core

import (
	stdHTTP "net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/routing"
	"github.com/dangduoc08/ginject/trace"
	"github.com/dangduoc08/ginject/versioning"
)

var (
	notFoundBody         = exception.NotFoundException(stdHTTP.StatusText(stdHTTP.StatusNotFound)).AppendJSON(nil)
	methodNotAllowedBody = exception.MethodNotAllowedException(stdHTTP.StatusText(stdHTTP.StatusMethodNotAllowed)).AppendJSON(nil)
	goneBody             = exception.GoneException("Deprecated URL usage").AppendJSON(nil)
	invalidURLBody       = exception.BadRequestException("Invalid URL path").AppendJSON(nil)

	accessDeniedException = exception.ForbiddenException("Access denied")

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

	versioning          *versioning.Versioning
	isVersioningEnabled bool

	event  *event.Event
	logger common.Logger

	routes            []*httpRoute
	routeByPattern    map[string]*httpRoute
	globalMiddlewares []httpMiddleware
	allowIndexes      []int
}

type httpRoute struct {
	middlewares  []httpMiddleware
	guards       []httpGuard
	interceptors []httpInterceptor
	plan         *httpHandlerPlan
	filters      []common.HTTPCatch
	isServe      bool
	serveIndex   int
}

type httpGuard struct {
	canActivate common.HTTPCanActivate
	name        string
}

type httpInterceptor struct {
	intercept common.HTTPIntercept
	name      string
}

type httpExchange struct {
	ctx.HTTPContext
	writer          responseWriter
	filters         []common.HTTPCatch
	middleware      *httpMiddleware
	middlewareStart time.Time
	traced          bool
}

func newHTTPExchange() *httpExchange {
	return &httpExchange{HTTPContext: *ctx.NewHTTPContext()}
}

func (ex *httpExchange) reset() {
	ex.HTTPContext.Reset()
	ex.writer.reset(nil)
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

func (http *HTTP) enableVersioning(v versioning.Versioning) {
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
	for _, route := range http.routes {
		route.guards = append(route.guards, httpGuard{canActivate: canActivate, name: name})
	}
}

func (http *HTTP) addGuard(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.guards = append(route.guards, httpGuard{canActivate: layer.Handler.(common.HTTPCanActivate), name: layer.Name})
	}
}

func (http *HTTP) addGlobalInterceptor(name string, intercept common.HTTPIntercept) {
	for _, route := range http.routes {
		route.interceptors = append(route.interceptors, httpInterceptor{intercept: intercept, name: name})
	}
}

func (http *HTTP) addInterceptor(layer common.HTTPLayer) {
	if route := http.routeOf(layer); route != nil {
		route.interceptors = append(route.interceptors, httpInterceptor{intercept: layer.Handler.(common.HTTPIntercept), name: layer.Name})
	}
}

func (http *HTTP) addGlobalFilter(catch common.HTTPCatch) {
	for _, route := range http.routes {
		route.filters = append(route.filters, catch)
	}
}

func (http *HTTP) addFilter(layer common.HTTPLayer, catch common.HTTPCatch) {
	if route := http.routeOf(layer); route != nil {
		route.filters = append(route.filters, catch)
	}
}

func (http *HTTP) finishRoutes() {
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
		version = http.versioning.GetVersion(c)
	}

	route, isGone := http.match(c, method, version)
	if !http.runMiddlewares(ex, route) {
		return
	}
	if route == nil {
		http.answerNoRoute(ex, method, version, isGone)
		return
	}
	http.serveRoute(ex, route)
}

func (http *HTTP) match(c *ctx.HTTPContext, method, version string) (*httpRoute, bool) {
	urlPath := c.URL.Path
	if http.isVersioningEnabled && version == "" {
		if item, values := http.route.Match(method, urlPath, versioning.NeutralVersion, c.ParamValues[:0]); item != nil {
			return http.bindRoute(c, item, values), false
		}
		item, _ := http.route.Match(method, urlPath, "", c.ParamValues[:0])
		return nil, item != nil
	}

	item, values := http.route.Match(method, urlPath, version, c.ParamValues[:0])
	if item == nil {
		item, values = http.route.Match(method, urlPath, versioning.NeutralVersion, values[:0])
		if item == nil {
			return nil, false
		}
	}
	return http.bindRoute(c, item, values), false
}

func (http *HTTP) bindRoute(c *ctx.HTTPContext, item *routing.RouterItem, values []string) *httpRoute {
	c.ParamKeys = item.ParamKeys
	c.ParamValues = values
	return http.routes[item.Index]
}

func (http *HTTP) runMiddlewares(ex *httpExchange, route *httpRoute) bool {
	var module []httpMiddleware
	if route != nil {
		module = route.middlewares
	}
	if len(http.globalMiddlewares) == 0 && len(module) == 0 {
		ex.Next = noopNext
		return true
	}

	proceed := false
	next := func() { proceed = true }
	ex.Next = next
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
	m.emit(http.event, ex.GetID(), time.Since(ex.middlewareStart), 0)
	return *proceed
}

func (http *HTTP) answerNoRoute(ex *httpExchange, method, version string, isGone bool) {
	if isGone {
		writeJSONError(&ex.writer, stdHTTP.StatusGone, goneBody)
		return
	}
	urlPath := ex.URL.Path
	mask := 0
	for _, i := range http.allowIndexes {
		if allowMethods[i] != method && http.hasRoute(allowMethods[i], urlPath, version, ex.ParamValues[:0]) {
			mask |= 1 << i
		}
	}
	if mask == 0 {
		writeJSONError(&ex.writer, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	ex.writer.Header()["Allow"] = []string{allowByMask[mask]}
	writeJSONError(&ex.writer, stdHTTP.StatusMethodNotAllowed, methodNotAllowedBody)
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
	if c.Method == stdHTTP.MethodPost {
		c.Code = stdHTTP.StatusCreated
	}

	for i := range route.guards {
		if !http.runGuard(ex, &route.guards[i]) {
			refusal := accessDeniedException
			http.catch(ex, &refusal)
			return
		}
	}

	for i := range route.interceptors {
		agg := aggregation.NewAggregation()
		agg.Name = route.interceptors[i].name
		c.AddAggregation(agg)
		value := http.runIntercept(ex, &route.interceptors[i], agg)
		if !agg.IsMainHandlerCalled {
			aggregations := c.Aggregations()
			http.writeResult(ex, route, reflect.ValueOf(http.runPosts(ex, aggregations[:len(aggregations)-1], value)))
			return
		}
		agg.InterceptorData = value
	}

	results := route.plan.call(c, http.event, ex.traced)
	result := reflect.Value{}
	switch len(results) {
	case 0:
	case 1:
		result = results[0]
	default:
		setStatusCode(c, results[0])
		result = results[1]
	}

	if aggregations := c.Aggregations(); len(aggregations) > 0 {
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
	http.event.Emit(trace.EventName, trace.Event{ID: ex.GetID(), Stage: trace.StageGuard, Name: g.name, Duration: time.Since(start)})
	return isAllowed
}

func (http *HTTP) runIntercept(ex *httpExchange, i *httpInterceptor, agg *aggregation.Aggregation) any {
	if !ex.traced {
		return i.intercept(&ex.HTTPContext, agg)
	}
	start := time.Now()
	value := i.intercept(&ex.HTTPContext, agg)
	http.event.Emit(trace.EventName, trace.Event{ID: ex.GetID(), Stage: trace.StagePreInterceptor, Name: i.name, Duration: time.Since(start)})
	return value
}

func (http *HTTP) runPosts(ex *httpExchange, aggregations []*aggregation.Aggregation, data any) any {
	for i := len(aggregations) - 1; i >= 0; i-- {
		agg := aggregations[i]
		agg.SetMainData(data)
		if !ex.traced {
			data = agg.Aggregate()
			continue
		}
		start := time.Now()
		data = agg.Aggregate()
		http.event.Emit(trace.EventName, trace.Event{ID: ex.GetID(), Stage: trace.StagePostInterceptor, Name: agg.Name, Duration: time.Since(start)})
	}
	return data
}

func (http *HTTP) writeResult(ex *httpExchange, route *httpRoute, result reflect.Value) {
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
		writeJSONError(&ex.writer, stdHTTP.StatusNotFound, notFoundBody)
		return
	}

	r := ex.Request
	if serveIndex != 0 {
		segments := strings.Split(str.RemoveDup(r.URL.Path, '/'), "/")
		root := dir
		if serveIndex < len(segments) {
			dir = path.Join(dir, strings.Join(segments[serveIndex:], "/"))
		}
		if dir != root && !strings.HasPrefix(dir, root+"/") {
			writeJSONError(&ex.writer, stdHTTP.StatusBadRequest, invalidURLBody)
			return
		}
	}

	info, err := os.Stat(dir)
	if err != nil {
		writeJSONError(&ex.writer, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	if info.IsDir() {
		index := filepath.Join(dir, "index.html")
		if indexInfo, err := os.Stat(index); err != nil || indexInfo.IsDir() {
			writeJSONError(&ex.writer, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			dir = index
		}
	}
	stdHTTP.ServeFile(&ex.writer, r, dir)
}

func (http *HTTP) recoverRequest(ex *httpExchange) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == stdHTTP.ErrAbortHandler {
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
		code = writeHTTPError(ex, http.exceptionOf(ex, rec))
	}
	if m := ex.middleware; m != nil {
		ex.middleware = nil
		m.emit(http.event, ex.GetID(), time.Since(ex.middlewareStart), code)
	}
}

func (http *HTTP) catch(ex *httpExchange, rec any) {
	for _, catchFn := range ex.filters {
		if ex.writer.status() != 0 {
			http.logAfterResponse(ex, rec)
			return
		}
		next, isHandled := callCatch(&ex.HTTPContext, catchFn, http.exceptionOf(ex, rec))
		if isHandled {
			return
		}
		if next == stdHTTP.ErrAbortHandler {
			panic(next)
		}
		rec = next
	}
	if ex.writer.status() != 0 {
		http.logAfterResponse(ex, rec)
		return
	}
	writeHTTPError(ex, http.exceptionOf(ex, rec))
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
	logPanic(http.logger, "UnhandledPanic", &ex.HTTPContext, rec)
	e := internalServerErrorException
	return &e
}

func (http *HTTP) logAfterResponse(ex *httpExchange, rec any) {
	value := rec
	if e, ok := asException(rec); ok {
		value = e.GetMessage()
	}
	c := &ex.HTTPContext
	http.logger.Error("ErrorAfterResponse", "id", c.GetID(), "method", c.Method, "path", c.URL.Path, "code", ex.writer.status(), "panic", value, "stack", panicStack())
}

func writeHTTPError(ex *httpExchange, e *exception.Exception) int {
	code := writeErrorResponse(&ex.writer, e)
	ex.Code = code
	return code
}
