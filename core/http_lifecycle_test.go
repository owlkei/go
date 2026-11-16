package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

type lcCaught struct {
	name string
	ex   exception.Exception
}

type lcFixture struct {
	mu         sync.Mutex
	log        []string
	caught     []lcCaught
	middleware map[string]func(http.ResponseWriter, *http.Request, ctx.Next)
	guard      map[string]func(*ctx.HTTPContext) bool
	intercept  map[string]func(*ctx.HTTPContext, *aggregation.Aggregation) any
	filter     map[string]func(*ctx.HTTPContext, *exception.Exception)
	handler    func(*ctx.HTTPContext) any
}

var lc lcFixture

func lcLog(entry string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.log = append(lc.log, entry)
}

func lcEntries(prefix string) []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	var out []string
	for _, entry := range lc.log {
		if strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func lcAll() []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return slices.Clone(lc.log)
}

func lcClearLog() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.log = nil
}

func lcCaughtBy(name string) (exception.Exception, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, c := range lc.caught {
		if c.name == name {
			return c.ex, true
		}
	}
	return exception.Exception{}, false
}

func lcHook[T any](m map[string]T, name string) (T, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	h, ok := m[name]
	return h, ok
}

func lcSetMiddleware(name string, h func(http.ResponseWriter, *http.Request, ctx.Next)) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.middleware[name] = h
}

func lcSetGuard(name string, h func(*ctx.HTTPContext) bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.guard[name] = h
}

func lcSetIntercept(name string, h func(*ctx.HTTPContext, *aggregation.Aggregation) any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.intercept[name] = h
}

func lcSetFilter(name string, h func(*ctx.HTTPContext, *exception.Exception)) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.filter[name] = h
}

func lcSetHandler(h func(*ctx.HTTPContext) any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.handler = h
}

func lcReset(t *testing.T) {
	t.Helper()
	resetModuleGlobals()
	lc.mu.Lock()
	lc.log = nil
	lc.caught = nil
	lc.middleware = map[string]func(http.ResponseWriter, *http.Request, ctx.Next){}
	lc.guard = map[string]func(*ctx.HTTPContext) bool{}
	lc.intercept = map[string]func(*ctx.HTTPContext, *aggregation.Aggregation) any{}
	lc.filter = map[string]func(*ctx.HTTPContext, *exception.Exception){}
	lc.handler = nil
	lc.mu.Unlock()
}

type lcMiddleware struct{ Name string }

func (m lcMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	lcLog("mw:" + m.Name)
	if h, ok := lcHook(lc.middleware, m.Name); ok {
		h(w, r, next)
		return
	}
	next()
}

type lcGuard struct{ Name string }

func (g lcGuard) CanActivate(c *ctx.HTTPContext) bool {
	lcLog("guard:" + g.Name)
	if h, ok := lcHook(lc.guard, g.Name); ok {
		return h(c)
	}
	return true
}

type lcInterceptor struct{ Name string }

func (i lcInterceptor) Intercept(c *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	lcLog("intercept:" + i.Name)
	if h, ok := lcHook(lc.intercept, i.Name); ok {
		return h(c, agg)
	}
	return agg.Pipe(agg.Transform(func(data any) any {
		lcLog("post:" + i.Name)
		return fmt.Sprint(data) + ">" + i.Name
	}))
}

type lcFilter struct{ Name string }

func (f lcFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	lcLog("filter:" + f.Name)
	lc.mu.Lock()
	lc.caught = append(lc.caught, lcCaught{name: f.Name, ex: *ex})
	lc.mu.Unlock()
	if h, ok := lcHook(lc.filter, f.Name); ok {
		h(c, ex)
		return
	}
	c.Status(ex.GetCode()).Text("F[" + f.Name + "]" + ex.GetMessage())
}

func lcHandle(c *ctx.HTTPContext) any {
	lcLog("handler")
	lc.mu.Lock()
	h := lc.handler
	lc.mu.Unlock()
	if h != nil {
		return h(c)
	}
	return "H"
}

type lcPanicPipe struct{}

func (lcPanicPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	panic(exception.BadRequestException("pipe boom"))
}

type lcController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl lcController) NewController() Controller {
	ctl.BindMiddleware(lcMiddleware{Name: "m1"})
	ctl.BindMiddleware(lcMiddleware{Name: "m2"})
	ctl.BindGuard(lcGuard{Name: "m1"})
	ctl.BindGuard(lcGuard{Name: "m2"})
	ctl.BindInterceptor(lcInterceptor{Name: "m1"})
	ctl.BindInterceptor(lcInterceptor{Name: "m2"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf1"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf2"})
	return ctl
}

func (ctl lcController) READ_ok(c *ctx.HTTPContext) any   { return lcHandle(c) }
func (ctl lcController) CREATE_ok(c *ctx.HTTPContext) any { return lcHandle(c) }
func (ctl lcController) READ_pipe(lcPanicPipe) string     { return "unreachable" }
func (ctl lcController) CREATE_bind(b ctx.Body) any {
	_ = b
	lcLog("handler")
	return "bound"
}

type lcBareController struct {
	common.HTTP
}

func (ctl lcBareController) NewController() Controller             { return ctl }
func (ctl lcBareController) READ_bare(c *ctx.HTTPContext) any      { return lcHandle(c) }
func (ctl lcBareController) CREATE_bare(c *ctx.HTTPContext) any    { return lcHandle(c) }
func (ctl lcBareController) DELETE_bare(c *ctx.HTTPContext) any    { return lcHandle(c) }
func (ctl lcBareController) UPDATE_bare(c *ctx.HTTPContext) any    { return lcHandle(c) }
func (ctl lcBareController) MODIFY_bare(c *ctx.HTTPContext) any    { return lcHandle(c) }
func (ctl lcBareController) PREFLIGHT_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

type lcMethodController struct {
	common.HTTP
}

func (ctl lcMethodController) NewController() Controller            { return ctl }
func (ctl lcMethodController) READ_readonly(c *ctx.HTTPContext) any { return lcHandle(c) }
func (ctl lcMethodController) CREATE_postonly(c *ctx.HTTPContext) any {
	return lcHandle(c)
}

type lcConfig struct {
	noGlobalFilters bool
	logger          common.Logger
	setup           func(*App)
}

func lcNewApp(t *testing.T, cfg lcConfig) *App {
	t.Helper()
	lcReset(t)
	app := New()
	if cfg.logger != nil {
		app.UseLogger(cfg.logger)
	}
	if cfg.setup != nil {
		cfg.setup(app)
	}
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"}, lcMiddleware{Name: "g2"})
	app.BindGlobalGuards(lcGuard{Name: "g1"}, lcGuard{Name: "g2"})
	app.BindGlobalInterceptors(lcInterceptor{Name: "g1"}, lcInterceptor{Name: "g2"})
	if !cfg.noGlobalFilters {
		app.BindGlobalExceptionFilters(lcFilter{Name: "gf1"}, lcFilter{Name: "gf2"})
	}
	app.Create(ModuleBuilder().Controllers(lcController{}, lcBareController{}, lcMethodController{}).Build())
	return app
}

func lcApp(t *testing.T) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{})
}

func lcServe(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func lcExpectEntries(t *testing.T, got, want []string, desc string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, desc))
	}
}

func lcExpectResponse(t *testing.T, w *httptest.ResponseRecorder, code int, body, desc string) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, desc+": status"))
	}
	if w.Body.String() != body {
		t.Error(test.DiffMessage(w.Body.String(), body, desc+": body"))
	}
}

func lcBefore(t *testing.T, earlierPrefix, laterPrefix, desc string) {
	t.Helper()
	all := lcAll()
	last, first := -1, -1
	for i, entry := range all {
		if strings.HasPrefix(entry, earlierPrefix) {
			last = i
		}
		if first == -1 && strings.HasPrefix(entry, laterPrefix) {
			first = i
		}
	}
	if last == -1 || first == -1 {
		t.Error(test.DiffMessage(all, "entries with both "+earlierPrefix+" and "+laterPrefix, desc))
		return
	}
	if last >= first {
		t.Error(test.DiffMessage(all, earlierPrefix+" entries all before "+laterPrefix+" entries", desc))
	}
}

func lcPanicWith(value any) func(*ctx.HTTPContext) any {
	return func(*ctx.HTTPContext) any { panic(value) }
}

func lcRethrow(_ *ctx.HTTPContext, ex *exception.Exception) { panic(ex) }

func lcFilterPanicWith(value any) func(*ctx.HTTPContext, *exception.Exception) {
	return func(*ctx.HTTPContext, *exception.Exception) { panic(value) }
}

func lcPanicMiddleware(value any) func(http.ResponseWriter, *http.Request, ctx.Next) {
	return func(http.ResponseWriter, *http.Request, ctx.Next) { panic(value) }
}

func TestLifecycle_GlobalMiddlewareBeforeModuleMiddleware(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("mw:"), []string{"mw:g1", "mw:g2", "mw:m1", "mw:m2"}, "L1: global middlewares run before module middlewares, each scope in bind order")
}

func TestLifecycle_MiddlewareBeforeGuard(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcBefore(t, "mw:", "guard:", "L2: every middleware has returned before the first guard starts")
}

func TestLifecycle_GlobalGuardBeforeModuleGuard(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("guard:"), []string{"guard:g1", "guard:g2", "guard:m1", "guard:m2"}, "L3: global guards run before module guards, each scope in bind order")
}

func TestLifecycle_GuardBeforeInterceptor(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcBefore(t, "guard:", "intercept:", "L4: every guard has returned true before the first Intercept starts")
}

func TestLifecycle_GlobalInterceptBeforeModuleIntercept(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("intercept:"), []string{"intercept:g1", "intercept:g2", "intercept:m1", "intercept:m2"}, "L5: global interceptors run before module interceptors, each scope in bind order")
}

func TestLifecycle_InterceptBeforeHandler(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcBefore(t, "intercept:", "handler", "L6: the handler runs after every Intercept has returned")
	lcExpectEntries(t, lcEntries("handler"), []string{"handler"}, "L6: the handler runs once")
}

func TestLifecycle_ModulePostAfterHandler(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcBefore(t, "handler", "post:", "L7: operators run after the handler")
	lcExpectEntries(t, lcEntries("post:")[:2], []string{"post:m2", "post:m1"}, "L7: module operators run first, in reverse bind order")
}

func TestLifecycle_GlobalPostAfterModulePost(t *testing.T) {
	app := lcApp(t)
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("post:"), []string{"post:m2", "post:m1", "post:g2", "post:g1"}, "L8: global operators run after the module ones, in reverse bind order")
}

func TestLifecycle_PostDataIsTheResponse(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusOK, "H>m2>m1>g2>g1", "L8, L9: each operator receives what the previous one produced and the last data is written")
}

func TestLifecycle_PostDataOfPOSTIs201(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodPost, "/ok")

	lcExpectResponse(t, w, http.StatusCreated, "H>m2>m1>g2>g1", "L9: the status is 201 when the request method is POST")
}

func TestLifecycle_MiddlewareWithoutNextStops(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("stopped"))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "L10: nothing after a global middleware that does not call next() runs")
	lcExpectResponse(t, w, http.StatusAccepted, "stopped", "L10: what the middleware wrote is the response")
}

func TestLifecycle_ModuleMiddlewareWithoutNextStops(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("m1", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		_, _ = w.Write([]byte("stopped"))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcAll(), []string{"mw:g1", "mw:g2", "mw:m1"}, "L10: nothing after a module middleware that does not call next() runs")
	lcExpectResponse(t, w, http.StatusOK, "stopped", "L10: what the middleware wrote is the response")
}

func TestLifecycle_MiddlewareWithoutNextWritesNothing(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g2", func(http.ResponseWriter, *http.Request, ctx.Next) {})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusOK, "", "L10: a middleware that writes nothing and stops leaves an empty 200")
	lcExpectEntries(t, lcAll(), []string{"mw:g1", "mw:g2"}, "L10: no filter, guard or handler runs after it")
}

func TestLifecycle_NextTwiceEqualsOnce(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", func(_ http.ResponseWriter, _ *http.Request, next ctx.Next) {
		next()
		next()
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("mw:"), []string{"mw:g1", "mw:g2", "mw:m1", "mw:m2"}, "L10: calling next() twice starts each later middleware once")
	lcExpectEntries(t, lcEntries("handler"), []string{"handler"}, "L10: calling next() twice runs the handler once")
	lcExpectResponse(t, w, http.StatusOK, "H>m2>m1>g2>g1", "L10: the response is the same as with one call")
}

func TestLifecycle_CodeAfterNextRunsBeforeNextLayer(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", func(_ http.ResponseWriter, _ *http.Request, next ctx.Next) {
		next()
		lcLog("after-next:g1")
	})
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("mw:")[:1], []string{"mw:g1"}, "L10: the first middleware starts first")
	all := lcAll()
	if len(all) < 3 || all[1] != "after-next:g1" || all[2] != "mw:g2" {
		t.Error(test.DiffMessage(all, "[mw:g1 after-next:g1 mw:g2 ...]", "L10: code after next() runs before the next layer starts"))
	}
}

func TestLifecycle_GuardFalseGoesToFilters(t *testing.T) {
	app := lcApp(t)
	lcSetGuard("g2", func(*ctx.HTTPContext) bool { return false })
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("guard:"), []string{"guard:g1", "guard:g2"}, "L11: no later guard runs")
	lcExpectEntries(t, lcEntries("intercept:"), nil, "L11: no interceptor runs")
	lcExpectEntries(t, lcEntries("handler"), nil, "L11: the handler counter stays 0")
	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L11: the route's filters receive the refusal")
	lcExpectResponse(t, w, http.StatusForbidden, "F[mf2]Access denied", "L11: the filter receives ForbiddenException(\"Access denied\")")
}

func TestLifecycle_InterceptWithoutPipeStopsHandler(t *testing.T) {
	app := lcApp(t)
	lcSetIntercept("m1", func(*ctx.HTTPContext, *aggregation.Aggregation) any { return "cached" })
	lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("handler"), nil, "bug 1, L12: the handler counter stays 0")
	lcExpectEntries(t, lcEntries("intercept:"), []string{"intercept:g1", "intercept:g2", "intercept:m1"}, "bug 1, L12: no later interceptor runs")
}

func TestLifecycle_ShortCircuitRunsEarlierPosts(t *testing.T) {
	app := lcApp(t)
	lcSetIntercept("m1", func(*ctx.HTTPContext, *aggregation.Aggregation) any { return "cached" })
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("post:"), []string{"post:g2", "post:g1"}, "L12: the operators of the interceptors that ran before it run in reverse order")
	lcExpectResponse(t, w, http.StatusOK, "cached>g2>g1", "L12: the return value is the data, wrapped by the global posts")
}

func TestLifecycle_ShortCircuitOnPOSTIs201(t *testing.T) {
	app := lcApp(t)
	lcSetIntercept("g1", func(*ctx.HTTPContext, *aggregation.Aggregation) any { return "cached" })
	w := lcServe(app, http.MethodPost, "/ok")

	lcExpectResponse(t, w, http.StatusCreated, "cached", "L12: the result is written by L9, 201 for POST")
	lcExpectEntries(t, lcEntries("handler"), nil, "L12: the handler does not run")
}

func TestLifecycle_GlobalMiddlewarePanicSkipsFilters(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", lcPanicMiddleware(exception.BadRequestException("mw boom")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"mw boom"}`, "L13: the HTTP layer answers with the exception")
	lcExpectEntries(t, lcEntries("filter:"), nil, "L13: no user exception filter runs")
	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "L13: nothing after the panicking middleware runs")
}

func TestLifecycle_GlobalMiddlewareStringPanicSkipsFilters(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", lcPanicMiddleware("pq: connection refused 10.0.0.5"))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "bug 3, L13: a string panic answers the fixed 500")
	lcExpectEntries(t, lcEntries("filter:"), nil, "L13: no user exception filter runs")
}

func TestLifecycle_ModuleMiddlewarePanicSkipsFilters(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("m2", lcPanicMiddleware(exception.BadRequestException("mw boom")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"mw boom"}`, "L14: the HTTP layer answers with the exception")
	lcExpectEntries(t, lcEntries("filter:"), nil, "L14: no user exception filter runs")
	lcExpectEntries(t, lcEntries("guard:"), nil, "L14: no guard runs")
}

func TestLifecycle_ModuleMiddlewareStringPanicSkipsFilters(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("m1", lcPanicMiddleware("secret"))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "bug 3, L14: a string panic answers the fixed 500")
	lcExpectEntries(t, lcEntries("filter:"), nil, "L14: no user exception filter runs")
}

func TestLifecycle_GuardPanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	lcSetGuard("m1", func(*ctx.HTTPContext) bool { panic(exception.BadRequestException("boom")) })
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L15: the module filter receives the error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]boom", "L15: the module filter receives the exception")
	lcExpectEntries(t, lcEntries("handler"), nil, "L15: the handler does not run")
}

func TestLifecycle_InterceptPanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	lcSetIntercept("g2", func(*ctx.HTTPContext, *aggregation.Aggregation) any { panic(exception.BadRequestException("boom")) })
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L16: the module filter receives the error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]boom", "L16: the module filter receives the exception")
	lcExpectEntries(t, lcEntries("handler"), nil, "L16: the handler does not run")
}

func TestLifecycle_HandlerPanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L17: the module filter receives the error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]boom", "L17: the module filter receives the exception")
}

func TestLifecycle_PipePanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/pipe")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L17: the module filter receives the pipe's error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]pipe boom", "L17: the module filter receives the exception")
}

func TestLifecycle_BindingPanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	r := httptest.NewRequest(http.MethodPost, "/bind", strings.NewReader("{not json"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L17: the module filter receives the binding's error")
	if w.Code != http.StatusBadRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusBadRequest, "L17: an invalid body is a 400 for the module filter"))
	}
	lcExpectEntries(t, lcEntries("handler"), nil, "L17: the handler does not run")
}

func TestLifecycle_PostOperatorPanicGoesToModuleFilter(t *testing.T) {
	app := lcApp(t)
	lcSetIntercept("m1", func(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
		return agg.Pipe(agg.Transform(func(any) any { panic(exception.BadRequestException("post boom")) }))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L18: the module filter receives the operator's error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]post boom", "L18: the module filter receives the exception")
}

func TestLifecycle_FilterOrder(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	for _, name := range []string{"mf1", "mf2", "gf1", "gf2"} {
		lcSetFilter(name, lcRethrow)
	}
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2", "filter:mf1", "filter:gf2", "filter:gf1"}, "L19: module filters (bound last first), then global filters (bound last first)")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"boom"}`, "L19: the built-in filter is last")
}

func TestLifecycle_ModuleFilterReturnSkipsGlobalFilters(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2"}, "L19: with a module filter that returns, no global filter runs")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[mf2]boom", "L19: the first module filter handled it")
}

func TestLifecycle_ModuleFilterRethrowReachesGlobalFilter(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcRethrow)
	lcSetFilter("mf1", lcRethrow)
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2", "filter:mf1", "filter:gf2"}, "L19: a rethrown error reaches the last bound global filter")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[gf2]boom", "L19: the global filter handled it")
}

func TestLifecycle_FilterPanicPassesWhatItPanickedWith(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcFilterPanicWith(exception.NotFoundException("other")))
	w := lcServe(app, http.MethodGet, "/ok")

	got, ok := lcCaughtBy("mf1")
	if !ok || got.GetCode() != http.StatusNotFound || got.GetMessage() != "other" {
		t.Error(test.DiffMessage(got, "404 other", "L20: the next filter receives what the filter panicked with"))
	}
	lcExpectResponse(t, w, http.StatusNotFound, "F[mf1]other", "L20: the next filter handled it")
}

func TestLifecycle_FilterRethrowKeepsTheExceptionUnchanged(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcRethrow)
	lcServe(app, http.MethodGet, "/ok")

	got, ok := lcCaughtBy("mf1")
	if !ok || got.GetCode() != http.StatusBadRequest || got.GetMessage() != "boom" {
		t.Error(test.DiffMessage(got, "400 boom", "L20: panic(ex) passes the exception on unchanged"))
	}
}

func TestLifecycle_NoModuleFilterUsesGlobalFilter(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:gf2"}, "L21: the first global filter receives the error")
	lcExpectResponse(t, w, http.StatusBadRequest, "F[gf2]boom", "L21: the global filter handled it")
}

func TestLifecycle_NoUserFilterUsesBuiltIn(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectEntries(t, lcEntries("filter:"), nil, "L21: no user filter runs")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"boom"}`, "L21: the built-in filter receives the error")
}

func TestLifecycle_FilterReturnHandles(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("gf2", func(*ctx.HTTPContext, *exception.Exception) {})
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:gf2"}, "L22: a filter that returns has handled the error")
	lcExpectResponse(t, w, http.StatusOK, "", "L22: the framework writes nothing more, a silent filter gives an empty 200")
}

func TestLifecycle_EveryFilterPanics(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcFilterPanicWith(exception.TeapotException("first")))
	lcSetFilter("mf1", lcFilterPanicWith(exception.GoneException("second")))
	lcSetFilter("gf2", lcFilterPanicWith("third"))
	lcSetFilter("gf1", lcFilterPanicWith(exception.ServiceUnavailableException("last")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcEntries("filter:"), []string{"filter:mf2", "filter:mf1", "filter:gf2", "filter:gf1"}, "L22: every filter runs")
	lcExpectResponse(t, w, http.StatusServiceUnavailable, `{"code":503,"error":"Service Unavailable","message":"last"}`, "L22: the HTTP layer's error response is written for the last panic")
}

func TestLifecycle_NoRouteRunsGlobalMiddlewares(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/nowhere")

	lcExpectEntries(t, lcAll(), []string{"mw:g1", "mw:g2"}, "L23: only the global middlewares run for a request without a route")
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "L23: the answer is N1's")
}

func TestLifecycle_NoRouteMiddlewareStopsThe404(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("mine"))
	})
	w := lcServe(app, http.MethodGet, "/nowhere")

	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "L23, L10: a middleware that does not call next() stops the request")
	lcExpectResponse(t, w, http.StatusTeapot, "mine", "L23, L10: its response replaces the 404")
}

func TestLifecycle_NoRouteMiddlewarePanicIsHTTPLayerResponse(t *testing.T) {
	app := lcApp(t)
	lcSetMiddleware("g2", lcPanicMiddleware("secret"))
	w := lcServe(app, http.MethodGet, "/nowhere")

	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "L23, L13: a panicking middleware answers the fixed 500")
	lcExpectEntries(t, lcEntries("filter:"), nil, "L23, L13: no filter runs")
}

func TestLifecycle_NoRouteMethodNotAllowedRunsGlobalMiddlewares(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/ok")

	lcExpectEntries(t, lcAll(), []string{"mw:g1", "mw:g2"}, "L23: a 405 runs only the global middlewares")
	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "L23: the answer is N2's")
}

func lcDialWS(t *testing.T, server *httptest.Server) (*websocket.Conn, error) {
	t.Helper()
	return websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
}

func TestLifecycle_WSHandshakeRunsOnlyWSMiddlewares(t *testing.T) {
	lcReset(t)
	app := New()
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.EnableWS(&WSConfig{Path: "/ws"}, lcMiddleware{Name: "w1"}, lcMiddleware{Name: "w2"})
	app.Create(ModuleBuilder().Controllers(lcBareController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := lcDialWS(t, server)
	if err != nil {
		t.Fatal(test.DiffMessage(err, nil, "L24: a handshake whose middlewares all call next() is accepted"))
	}
	defer func() { _ = conn.Close() }()

	lcExpectEntries(t, lcEntries("mw:"), []string{"mw:w1", "mw:w2"}, "L24: only the middlewares given to EnableWS run, once, at the handshake")
}

func TestLifecycle_WSHandshakeRefusedWithoutNext(t *testing.T) {
	lcReset(t)
	app := New()
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.EnableWS(&WSConfig{Path: "/ws"}, lcMiddleware{Name: "w1"}, lcMiddleware{Name: "w2"})
	app.Create(ModuleBuilder().Controllers(lcBareController{}).Build())
	lcSetMiddleware("w1", func(http.ResponseWriter, *http.Request, ctx.Next) {})
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := lcDialWS(t, server)
	if err == nil {
		_ = conn.Close()
		t.Error(test.DiffMessage(nil, "a refused handshake", "L24: a middleware that does not call next() refuses the handshake"))
	}
	lcExpectEntries(t, lcEntries("mw:"), []string{"mw:w1"}, "L24: no later middleware runs, and no global middleware")
}

func TestLifecycle_WSHandshakeRefusedByPanic(t *testing.T) {
	lcReset(t)
	app := New()
	app.EnableWS(&WSConfig{Path: "/ws"}, lcMiddleware{Name: "w1"})
	app.Create(ModuleBuilder().Controllers(lcBareController{}).Build())
	lcSetMiddleware("w1", lcPanicMiddleware("secret"))
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := lcDialWS(t, server)
	if err == nil {
		_ = conn.Close()
		t.Error(test.DiffMessage(nil, "a refused handshake", "L24: a middleware that panics refuses the handshake"))
	}
}

func TestLifecycle_StaleNextDoesNotContinueAnotherRequest(t *testing.T) {
	app := lcApp(t)
	var stale ctx.Next
	lcSetMiddleware("g1", func(_ http.ResponseWriter, _ *http.Request, next ctx.Next) {
		if stale == nil {
			stale = next
			next()
			return
		}
		stale()
	})
	lcServe(app, http.MethodGet, "/ok")
	lcClearLog()

	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "next closure per request: a next() kept from an earlier request does not let another request continue")
	lcExpectResponse(t, w, http.StatusOK, "", "next closure per request: the request that did not call its own next() stops")
}
