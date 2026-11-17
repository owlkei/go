package core

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

const scImmutable = "public, max-age=31536000, immutable"

var scRoot string

type scController struct{ common.HTTP }

func (c scController) NewController() Controller  { return c }
func (c scController) SERVE_ANY_OF_files() string { return scRoot }
func (c scController) SERVE_logo_png_FILE() string {
	return filepath.Join(scRoot, "logo.png")
}
func (c scController) SERVE_missing_png_FILE() string {
	return filepath.Join(scRoot, "missing.png")
}
func (c scController) SERVE_docs() string { return scRoot }
func (c scController) SERVE_ANY_OF_assets(hc *ctx.HTTPContext) string {
	hc.ResponseWriter.Header().Set("Cache-Control", scImmutable)
	return scRoot
}

type scMiddleware struct{ Value string }

func (m scMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("Cache-Control", m.Value)
	next()
}

type scGuard struct{ Value string }

func (g scGuard) CanActivate(c *ctx.HTTPContext) bool {
	c.ResponseWriter.Header().Set("Cache-Control", g.Value)
	return true
}

func scApp(t *testing.T, setup func(*App)) *App {
	t.Helper()
	scRoot = filepath.Join(t.TempDir(), "public")
	writeServeFile(t, filepath.Join(scRoot, "logo.png"), "hello")
	writeServeFile(t, filepath.Join(scRoot, "css", "app.css"), "body{}")
	writeServeFile(t, filepath.Join(scRoot, "index.html"), "<p>home</p>")
	writeServeFile(t, filepath.Join(scRoot, ".env"), "SECRET=1")
	app := New()
	app.SetLogger(&mockLogger{})
	if setup != nil {
		setup(app)
	}
	app.Create(NewModuleBuilder().Controllers(scController{}).Build())
	return app
}

func scGet(app *App, target string, headers ...string) *httptest.ResponseRecorder {
	return createRequest(app, http.MethodGet, target, headers...)
}

func expectCacheControl(t *testing.T, w *httptest.ResponseRecorder, want, desc string) {
	t.Helper()
	got := w.Header().Values("Cache-Control")
	if want == "" {
		if len(got) != 0 {
			t.Error(test.DiffMessage(got, []string{}, desc))
		}
		return
	}
	if len(got) != 1 || got[0] != want {
		t.Error(test.DiffMessage(got, []string{want}, desc))
	}
}

func TestServe_DefaultCacheControl(t *testing.T) {
	app := scApp(t, nil)
	for _, target := range []string{"/files/logo.png", "/files/css/app.css", "/logo.png"} {
		w := scGet(app, target)
		if w.Code != http.StatusOK {
			t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: GET "+target))
		}
		expectCacheControl(t, w, "no-cache", "F1: a served file sends no-cache: "+target)
	}
}

func TestServe_DefaultCacheControl_Index(t *testing.T) {
	app := scApp(t, nil)
	w := scGet(app, "/docs/")
	if w.Code != http.StatusOK || w.Body.String() != "<p>home</p>" {
		t.Fatal(test.DiffMessage(w.Body.String(), "<p>home</p>", "fixture: the index.html of a directory"))
	}
	expectCacheControl(t, w, "no-cache", "F1: the index.html of a directory sends no-cache")
}

func TestServe_DefaultCacheControl_NotModified(t *testing.T) {
	app := scApp(t, nil)
	first := scGet(app, "/files/logo.png")
	w := scGet(app, "/files/logo.png", "If-Modified-Since", first.Header().Get("Last-Modified"))
	if w.Code != http.StatusNotModified {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotModified, "fixture: a conditional request"))
	}
	expectCacheControl(t, w, "no-cache", "F1: a 304 keeps no-cache")
}

func TestServe_DefaultCacheControl_PartialContent(t *testing.T) {
	app := scApp(t, nil)
	w := scGet(app, "/files/logo.png", "Range", "bytes=1-2")
	if w.Code != http.StatusPartialContent {
		t.Fatal(test.DiffMessage(w.Code, http.StatusPartialContent, "fixture: a range request"))
	}
	expectCacheControl(t, w, "no-cache", "F1: a 206 sends no-cache")
}

func TestServe_DefaultCacheControl_Head(t *testing.T) {
	app := scApp(t, nil)
	w := createRequest(app, http.MethodHead, "/files/logo.png")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: HEAD of a file"))
	}
	expectCacheControl(t, w, "no-cache", "F1: HEAD sends the same Cache-Control as GET")
}

func TestServe_HandlerCacheControlKept(t *testing.T) {
	app := scApp(t, nil)
	w := scGet(app, "/assets/logo.png")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: the handler-marked route serves the file"))
	}
	expectCacheControl(t, w, scImmutable, "F2: a Cache-Control set by the handler is kept")
}

func TestServe_MiddlewareCacheControlKept(t *testing.T) {
	app := scApp(t, func(app *App) {
		app.BindGlobalMiddlewares(scMiddleware{Value: "public, max-age=60"})
	})
	w := scGet(app, "/files/logo.png")
	expectCacheControl(t, w, "public, max-age=60", "F2: a Cache-Control set by a middleware is kept")
}

func TestServe_GuardCacheControlKept(t *testing.T) {
	app := scApp(t, func(app *App) {
		app.BindGlobalGuards(scGuard{Value: "private, max-age=5"})
	})
	w := scGet(app, "/files/logo.png")
	expectCacheControl(t, w, "private, max-age=5", "F2: a Cache-Control set by a guard is kept")
}

func TestServe_ErrorDropsCacheControl(t *testing.T) {
	app := scApp(t, func(app *App) {
		app.BindGlobalMiddlewares(scMiddleware{Value: scImmutable})
	})
	cases := map[string]int{
		"/files/missing.txt":          http.StatusNotFound,
		"/files/.env":                 http.StatusNotFound,
		"/files/css/nope.css":         http.StatusNotFound,
		"/missing.png":                http.StatusNotFound,
		"/files/a/../../files/secret": http.StatusBadRequest,
	}
	for target, code := range cases {
		w := scGet(app, target)
		if w.Code != code {
			t.Errorf("fixture: GET %s answered %d, want %d", target, w.Code, code)
			continue
		}
		expectCacheControl(t, w, "", "F3: an error the SERVE route answers carries no Cache-Control: "+target)
	}
}

func TestServe_ErrorDropsHandlerCacheControl(t *testing.T) {
	app := scApp(t, nil)
	w := scGet(app, "/assets/missing.png")
	if w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "fixture: a missing asset"))
	}
	expectCacheControl(t, w, "", "F3: the immutable the handler set never lands on an error")
}

func TestServe_NoRouteKeepsAnotherLayersCacheControl(t *testing.T) {
	app := scApp(t, func(app *App) {
		app.BindGlobalMiddlewares(scMiddleware{Value: "no-store"})
	})
	w := scGet(app, "/nowhere")
	if w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "fixture: no route"))
	}
	if got := w.Header().Get("Cache-Control"); got == "no-cache" {
		t.Error(test.DiffMessage(got, "<not the static default>", "F1: the static default applies to SERVE routes only"))
	}
}

func TestServe_DefaultCacheControl_DiscardWriter(t *testing.T) {
	app := scApp(t, nil)
	r := httptest.NewRequest(http.MethodGet, "/files/logo.png", nil)
	w := &benchDiscardWriter{header: http.Header{}}
	app.ServeHTTP(w, r)
	if got := w.header.Values("Cache-Control"); len(got) != 1 || got[0] != "no-cache" {
		t.Fatal(test.DiffMessage(got, []string{"no-cache"}, "F1: Cache-Control on a discard writer"))
	}
}
