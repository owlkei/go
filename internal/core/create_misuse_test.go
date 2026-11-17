package core

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

func msExpectPanic(t *testing.T, rule, want string, fn func()) {
	t.Helper()
	text, panicked := createPanicText(fn)
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", want, rule+": misuse panics"))
	}
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, rule+": the panic text"))
	}
}

func msCreated() *App {
	app := New()
	app.Create(NewModuleBuilder().Build())
	return app
}

type msMiddleware struct{}

func (msMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

func TestCreate_NilModulePanics(t *testing.T) {
	msExpectPanic(t, "M1", "invalid module: Create needs a root module", func() {
		New().Create(nil)
	})
}

func TestCreate_TwicePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M2", "invalid app: Create called twice", func() {
		app.Create(NewModuleBuilder().Build())
	})
}

func TestApp_BindGlobalMiddlewaresAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalMiddlewares must be called before Create", func() {
		app.BindGlobalMiddlewares(msMiddleware{})
	})
}

func TestApp_BindGlobalGuardsAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalGuards must be called before Create", func() {
		app.BindGlobalGuards(isoGuard{Role: "ms"})
	})
}

func TestApp_BindGlobalInterceptorsAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalInterceptors must be called before Create", func() {
		app.BindGlobalInterceptors(z1Interceptor{Name: "ms"})
	})
}

func TestApp_BindGlobalExceptionFiltersAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: BindGlobalExceptionFilters must be called before Create", func() {
		app.BindGlobalExceptionFilters(globalHTTPExceptionFilter{})
	})
}

func TestApp_EnableVersioningAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: EnableVersioning must be called before Create", func() {
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	})
}

func TestApp_EnableWSAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: EnableWS must be called before Create", func() {
		app.EnableWS(&WSOptions{Path: "/ws"})
	})
}

func TestApp_EnableDevtoolAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: EnableDevtool must be called before Create", func() {
		app.EnableDevtool()
	})
}

func TestApp_EnableAccessLogAfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "M3", "invalid app: EnableAccessLog must be called before Create", func() {
		app.EnableAccessLog()
	})
}

func TestApp_ConfigurationBeforeCreateDoesNotPanic(t *testing.T) {
	app := New()

	text, panicked := createPanicText(func() {
		app.BindGlobalMiddlewares(msMiddleware{}).
			BindGlobalGuards(isoGuard{Role: "ms-before"}).
			BindGlobalInterceptors(z1Interceptor{Name: "ms-before"}).
			EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}).
			EnableDevtool().
			EnableAccessLog()
		app.Create(NewModuleBuilder().Controllers(hkController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "M3: configuration before Create is fine"))
	}
}
