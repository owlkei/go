package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

const (
	puRobots   = "User-agent: *\nDisallow: /private\n"
	puSecurity = "Contact: mailto:security@example.com\n"
	puRawHTML  = `<h1>{{.NotParsed}}</h1><script>alert(1)</script>`
	puSVG      = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
)

type puAPI struct{ common.HTTP }

func (c puAPI) NewController() Controller { return c }
func (c puAPI) READ_ping() string         { return "pong" }

type puRobotsRoute struct{ common.HTTP }

func (c puRobotsRoute) NewController() Controller { return c }
func (c puRobotsRoute) READ_robots_txt_FILE() string {
	return "mine"
}

type puAbout struct{ common.HTTP }

func (c puAbout) NewController() Controller { return c }
func (c puAbout) RENDER_about()             {}

func puFiles() fstest.MapFS {
	return pgFiles(map[string]string{
		"public/robots.txt":               puRobots,
		"public/.well-known/security.txt": puSecurity,
		"public/docs/index.html":          puRawHTML,
		"public/img/logo.svg":             puSVG,
		"public/.env":                     "SECRET=1",
		"public/.git/config":              "SECRET=2",
		"public/a/.hidden/x.txt":          "SECRET=3",
		"secret.txt":                      "OUTSIDE",
	})
}

func puApp(t *testing.T, files fstest.MapFS, setup func(*App), controllers ...Controller) *App {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{puAPI{}}
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

func TestPublic_ServesAtRoot(t *testing.T) {
	app := puApp(t, puFiles(), nil)

	cases := map[string]struct{ typ, body string }{
		"/robots.txt":               {"text/plain", puRobots},
		"/.well-known/security.txt": {"text/plain", puSecurity},
		"/docs/index.html":          {"text/html", puRawHTML},
		"/img/logo.svg":             {"image/svg+xml", puSVG},
	}
	for path, want := range cases {
		w := createGet(app, path)
		if w.Code != http.StatusOK || w.Body.String() != want.body {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, want.body}, "P22: GET "+path+" (an .html file is sent as it is, never parsed)"))
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, want.typ) {
			t.Error(test.DiffMessage(ct, want.typ, "P24: the content type of "+path))
		}
		h := createRequest(app, http.MethodHead, path)
		if h.Code != http.StatusOK || h.Body.Len() != 0 || h.Header().Get("Content-Type") != w.Header().Get("Content-Type") || h.Header().Get("ETag") != w.Header().Get("ETag") {
			t.Error(test.DiffMessage([]any{h.Code, h.Body.Len()}, []any{http.StatusOK, 0}, "P22: HEAD "+path))
		}
	}
	if w := createGet(app, "/ping"); w.Body.String() != "pong" {
		t.Error(test.DiffMessage(w.Body.String(), "pong", "P23: the controllers' routes still answer"))
	}
}

func TestPublic_Refuses(t *testing.T) {
	app := puApp(t, puFiles(), nil)

	for _, path := range []string{
		"/.env", "/.git/config", "/a/.hidden/x.txt",
		"/docs", "/docs/", "/img", "/img/", "/.well-known", "/.well-known/",
		"/public/robots.txt", "/missing.txt", "/secret.txt",
		"/..%2fsecret.txt", "/%2e%2e/secret.txt", "/docs/..%2f..%2fsecret.txt", "/%2e%2e%2fsecret.txt",
	} {
		w := createRequest(app, http.MethodGet, path)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "OUTSIDE") {
			t.Error(test.DiffMessage(w.Code, "a refusal", "P22: "+path))
		}
		if w.Code != http.StatusNotFound {
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Error(test.DiffMessage(ct, "application/json", "P24, R17: the refusal is JSON without Accept: "+path))
		}
		if h := createRequest(app, http.MethodGet, path, "Accept", "text/html"); h.Code == http.StatusOK || strings.Contains(h.Body.String(), "SECRET") || strings.Contains(h.Body.String(), "OUTSIDE") {
			t.Error(test.DiffMessage(h.Code, "a refusal", "P22: "+path+" with Accept text/html"))
		}
	}
	if w := createGet(app, "/"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P22: a directory has no index: public/docs/index.html is /docs/index.html only"))
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := createRequest(app, method, "/robots.txt")
		if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Header().Get("Allow"), "GET") {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Allow")}, []any{http.StatusMethodNotAllowed, "an Allow header with GET"}, "P22: "+method+" on a public file"))
		}
	}
}

func TestPublic_Headers(t *testing.T) {
	app := puApp(t, puFiles(), nil)
	w := createGet(app, "/robots.txt")
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Error(test.DiffMessage(got, "no-cache", "P24: Cache-Control, the URL is fixed"))
	}
	want := `"` + assetHash(puRobots) + `"`
	if got := w.Header().Get("ETag"); got != want {
		t.Error(test.DiffMessage(got, want, "P24: the ETag is the quoted first 8 bytes of the SHA-256"))
	}
}

func TestPublic_ETag304(t *testing.T) {
	app := puApp(t, puFiles(), nil)
	etag := `"` + assetHash(puRobots) + `"`

	w := createRequest(app, http.MethodGet, "/robots.txt", "If-None-Match", etag)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.Len()}, []any{http.StatusNotModified, 0}, "P24: a matching If-None-Match"))
	}
	w = createRequest(app, http.MethodGet, "/robots.txt", "If-None-Match", `"0000000000000000"`)
	if w.Code != http.StatusOK || w.Body.String() != puRobots {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P24: another ETag is served in full"))
	}
}

func TestPublic_Reload(t *testing.T) {
	swap := &rlSwapFS{}
	swap.set(fstest.MapFS{"public/robots.txt": {Data: []byte("v1")}})
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: swap, Dir: ".", IsReload: true})
	mustCreate(t, app, NewModuleBuilder().Controllers(puAPI{}).Build())

	w := createGet(app, "/robots.txt")
	if w.Body.String() != "v1" || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") != "" {
		t.Error(test.DiffMessage([]any{w.Body.String(), w.Header().Get("Cache-Control"), w.Header().Get("ETag")}, []any{"v1", "no-cache", ""}, "P24: IsReload sends no-cache and no ETag"))
	}
	swap.set(fstest.MapFS{
		"public/robots.txt": {Data: []byte("version two"), ModTime: time.Now()},
		"public/new.txt":    {Data: []byte("added")},
	})
	if got := createGet(app, "/robots.txt").Body.String(); got != "version two" {
		t.Error(test.DiffMessage(got, "version two", "P24: the content is read on each request"))
	}
	if w := createGet(app, "/new.txt"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P24: a file added after Create answers 404 until the restart"))
	}
}

func TestPublic_NoDirNoRoute(t *testing.T) {
	files := pgFiles(map[string]string{"layout.html": `{{template "content" .}}`})
	app := puApp(t, files, nil)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		w := createRequest(app, method, "/robots.txt")
		if w.Code != http.StatusNotFound || w.Header().Get("Allow") != "" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Allow")}, []any{http.StatusNotFound, ""}, "P25: no public directory, no route: "+method))
		}
	}
	free := New()
	free.EnableRender(&RenderOptions{FS: files, Dir: "."})
	if text, panicked := createPanicText(func() {
		free.Create(NewModuleBuilder().Controllers(puRobotsRoute{}).Build())
	}); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P25: without a public directory a user robots.txt route is free"))
	}
}

func TestPublic_GlobalLayersApply(t *testing.T) {
	app := puApp(t, puFiles(), func(app *App) {
		app.BindGlobalMiddlewares(assetMarkMiddleware{})
		app.BindGlobalGuards(assetDenyGuard{})
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "User-agent") {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P24: a global guard refuses a public file"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Error(test.DiffMessage(ct, "application/json", "P24, R17: the refusal is JSON"))
	}
	if w.Header().Get("X-Global-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Header().Get("X-Global-Middleware"), "ran", "P24: a global middleware runs for public files"))
	}
	ok := createRequest(app, http.MethodGet, "/robots.txt", "X-Allow", "yes")
	if ok.Code != http.StatusOK || ok.Body.String() != puRobots {
		t.Error(test.DiffMessage(ok.Code, http.StatusOK, "P24: the guard lets it through"))
	}
}

func TestPublic_RouteConflict(t *testing.T) {
	app := New()
	app.EnableRender(&RenderOptions{FS: puFiles(), Dir: "."})
	msExpectPanic(t, "P23", "route conflict: public/robots.txt method is conflicted with READ_robots_txt_FILE method", func() {
		app.Create(NewModuleBuilder().Controllers(puRobotsRoute{}).Build())
	})

	page := New()
	page.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"about.html": "ABOUT", "public/about": "FILE"}), Dir: "."})
	msExpectPanic(t, "P23", "route conflict: public/about method is conflicted with RENDER_about method", func() {
		page.Create(NewModuleBuilder().Controllers(puAbout{}).Build())
	})
}

func TestPublic_VersionNeutral(t *testing.T) {
	app := puApp(t, puFiles(), func(app *App) {
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	}, puVersioned{})

	for name, headers := range map[string][]string{"a version": {"X-Version", "2"}, "another version": {"X-Version", "9"}, "no version": nil} {
		w := createRequest(app, http.MethodGet, "/robots.txt", headers...)
		if w.Code != http.StatusOK || w.Body.String() != puRobots {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, puRobots}, "P23: public files are version-neutral: "+name))
		}
	}
}

type puVersioned struct{ common.HTTP }

func (c puVersioned) NewController() Controller   { return c }
func (c puVersioned) READ_ping_VERSION_1() string { return "pong" }
