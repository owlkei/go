package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

type ccMiddleware struct{}

func (ccMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if r.Header.Get("X-Stop") != "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	next()
}

type ccGuard struct{}

func (ccGuard) CanActivate(c *ctx.HTTPContext) bool { return c.Header().Get("X-Deny") == "" }

type ccInterceptor struct{}

func (ccInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(data any) any { return fmt.Sprint(data) + ">t" }))
}

type ccFilter struct{}

func (ccFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Status(ex.GetCode()).Text("cc:" + ex.GetMessage())
}

type ccController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl ccController) NewController() Controller {
	ctl.BindMiddleware(ccMiddleware{})
	ctl.BindGuard(ccGuard{})
	ctl.BindInterceptor(ccInterceptor{})
	ctl.BindExceptionFilter(ccFilter{})
	return ctl
}

func (ctl ccController) READ_ok() string        { return "ok" }
func (ctl ccController) CREATE_ok() string      { return "created" }
func (ctl ccController) READ_panic() string     { panic("secret") }
func (ctl ccController) READ_exception() string { panic(exception.BadRequestException("bad")) }

type ccCase struct {
	method  string
	target  string
	headers [2]string
	code    int
	body    string
}

func TestServeHTTP_ConcurrentAllPaths_NoRace(t *testing.T) {
	resetModuleGlobals()
	app := New()
	app.UseLogger(&mockLogger{})
	app.Create(ModuleBuilder().Controllers(ccController{}).Build())

	cases := []ccCase{
		{http.MethodGet, "/ok", [2]string{}, http.StatusOK, "ok>t"},
		{http.MethodPost, "/ok", [2]string{}, http.StatusCreated, "created>t"},
		{http.MethodHead, "/ok", [2]string{}, http.StatusOK, "ok>t"},
		{http.MethodGet, "/ok", [2]string{"X-Deny", "1"}, http.StatusForbidden, "cc:Access denied"},
		{http.MethodGet, "/ok", [2]string{"X-Stop", "1"}, http.StatusAccepted, ""},
		{http.MethodGet, "/panic", [2]string{}, http.StatusInternalServerError, "cc:Internal Server Error"},
		{http.MethodGet, "/exception", [2]string{}, http.StatusBadRequest, "cc:bad"},
		{http.MethodGet, "/nowhere", [2]string{}, http.StatusNotFound, wantNotFoundJSON},
		{http.MethodDelete, "/ok", [2]string{}, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON},
	}

	const goroutines = 64
	const requestsPerGoroutine = 36

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func() {
			defer wg.Done()
			for i := range requestsPerGoroutine {
				c := cases[(g+i)%len(cases)]
				r := httptest.NewRequest(c.method, c.target, nil)
				if c.headers[0] != "" {
					r.Header.Set(c.headers[0], c.headers[1])
				}
				w := httptest.NewRecorder()
				app.ServeHTTP(w, r)

				if w.Code != c.code || w.Body.String() != c.body {
					t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{c.code, c.body}, "concurrency: "+c.method+" "+c.target+" answers the same under load"))
				}
			}
		}()
	}
	wg.Wait()
}
