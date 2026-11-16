package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

type roleGuard struct{ Role string }

func (g roleGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Role") == g.Role
}

type roleAlphaController struct {
	common.HTTP
	common.Guard
}

func (c roleAlphaController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "admin"})
	return c
}
func (c roleAlphaController) READ_alpha() string { return "alpha" }

type roleBetaController struct {
	common.HTTP
	common.Guard
}

func (c roleBetaController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "user"})
	return c
}
func (c roleBetaController) READ_beta() string { return "beta" }

func serveWithRole(app *App, path, role string) int {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-Role", role)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w.Code
}

func TestApp_SameGuardTypeDifferentConfig(t *testing.T) {
	app := New()
	app.Create(ModuleBuilder().Controllers(roleAlphaController{}, roleBetaController{}).Build())

	cases := []struct {
		path, role string
		want       int
	}{
		{"/alpha", "admin", http.StatusOK},
		{"/alpha", "user", http.StatusForbidden},
		{"/beta", "user", http.StatusOK},
		{"/beta", "admin", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := serveWithRole(app, c.path, c.role); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "bug 1, B1: "+c.path+" with role "+c.role+" must use its own controller's guard configuration"))
		}
	}
}

type nonRouteGuardController struct {
	common.HTTP
	common.Guard
}

func (c nonRouteGuardController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "admin"}, c.Helper)
	return c
}
func (c nonRouteGuardController) READ_open() string { return "open" }
func (c nonRouteGuardController) Helper() string    { return "helper" }

func TestApp_GuardBoundToNonRoutePanics(t *testing.T) {
	app := New()

	var rec any
	func() {
		defer func() { rec = recover() }()
		app.Create(ModuleBuilder().Controllers(nonRouteGuardController{}).Build())
	}()

	if rec == nil {
		t.Fatal(test.DiffMessage(nil, "a panic at Create()", "bug 4, B10: a guard bound to a non-route must not bind to every route"))
	}
	text, _ := rec.(error)
	if text == nil || !strings.Contains(text.Error(), "invalid guard:") || !strings.Contains(text.Error(), "Helper") {
		t.Error(test.DiffMessage(rec, "invalid guard: ... Helper", "B10: the panic names the layer and the handler"))
	}
}

var rethrowCalls int

type rethrowFilter struct{}

func (rethrowFilter) Catch(_ *ctx.HTTPContext, ex *exception.Exception) {
	rethrowCalls++
	panic(ex)
}

type finalFilter struct{}

func (finalFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Status(ex.GetCode())
	c.Text(ex.GetMessage())
}

type rethrowController struct {
	common.HTTP
	common.ExceptionFilter
}

func (c rethrowController) NewController() Controller {
	c.BindExceptionFilter(finalFilter{})
	c.BindExceptionFilter(rethrowFilter{})
	return c
}
func (c rethrowController) READ_gone() string {
	panic(exception.NotFoundException("nope"))
}

func TestApp_FilterRethrowKeepsCode(t *testing.T) {
	rethrowCalls = 0
	app := New()
	app.Create(ModuleBuilder().Controllers(rethrowController{}).Build())

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gone", nil))

	if rethrowCalls != 1 {
		t.Fatal(test.DiffMessage(rethrowCalls, 1, "fixture: the re-throwing filter runs first"))
	}
	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "bug 5, B21: a filter that re-throws keeps the status code"))
	}
	if body := w.Body.String(); body != "nope" {
		t.Error(test.DiffMessage(body, "nope", "bug 5, B21: a filter that re-throws keeps the message"))
	}
}
