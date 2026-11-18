package core

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

const (
	assetCSS = "body{color:#123}"
	assetJS  = "console.log(1)"
	assetSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
)

type assetPages struct{ common.HTTP }

func (c assetPages) NewController() Controller { return c }

func (c assetPages) RENDER_list_OF_bookings() {}

func (c assetPages) RENDER_page() {}

type assetPlain struct{ common.HTTP }

func (c assetPlain) NewController() Controller { return c }
func (c assetPlain) RENDER_x()                 {}

func assetFiles() fstest.MapFS {
	return pgFiles(map[string]string{
		"layout.html":              `<html>{{template "content" .}}</html>`,
		"page.html":                `{{asset "css/app.css"}}`,
		"bookings/list.html":       `LIST-TEMPLATE`,
		"partials/field.html":      `FIELD-PARTIAL`,
		"assets/css/app.css":       assetCSS,
		"assets/js/app.js":         assetJS,
		"assets/img/logo.svg":      assetSVG,
		"assets/.env":              `SECRET=1`,
		"assets/.hidden/x.txt":     `HIDDEN`,
		"assets/.well-known/t.txt": `WELL-KNOWN`,
		"secret.txt":               `OUTSIDE`,
	})
}

func assetHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

func assetApp(t *testing.T, files fstest.MapFS, setup func(*App)) *App {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(assetPages{}).Build())
	return app
}

func TestAsset_Serves(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)

	for path, want := range map[string]struct{ typ, body string }{
		"/assets/css/app.css":  {"text/css", assetCSS},
		"/assets/js/app.js":    {"javascript", assetJS},
		"/assets/img/logo.svg": {"image/svg+xml", assetSVG},
	} {
		w := createGet(app, path)
		if w.Code != http.StatusOK || w.Body.String() != want.body {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, want.body}, "R36: GET "+path))
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, want.typ) {
			t.Error(test.DiffMessage(ct, want.typ, "R36: the content type of "+path))
		}
		h := createRequest(app, http.MethodHead, path)
		if h.Code != http.StatusOK || h.Body.Len() != 0 || h.Header().Get("Content-Type") != w.Header().Get("Content-Type") {
			t.Error(test.DiffMessage([]any{h.Code, h.Body.Len()}, []any{http.StatusOK, 0}, "R36: HEAD "+path))
		}
	}

	if w := createGet(app, "/assets/.well-known/t.txt"); w.Code != http.StatusOK || w.Body.String() != "WELL-KNOWN" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "WELL-KNOWN"}, "R36: .well-known is served"))
	}
}

func TestAsset_Refuses(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)

	for _, path := range []string{
		"/assets/", "/assets/css", "/assets/css/", "/assets/missing.css",
		"/assets/.env", "/assets/.hidden/x.txt", "/assets/css/.env",
		"/assets/layout.html", "/assets/bookings/list.html", "/assets/partials/field.html", "/assets/_pages/bookings/list.html", "/assets/_pages/layout.html",
	} {
		w := createRequest(app, http.MethodGet, path, "Accept", "text/html")
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "R36: "+path+" is a 404"))
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Error(test.DiffMessage(ct, "application/json", "R36, R17: the refusal is JSON whatever Accept says: "+path))
		}
		if strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "HIDDEN") || strings.Contains(w.Body.String(), "TEMPLATE") {
			t.Error(test.DiffMessage(w.Body.String(), "no file content", "R36: nothing leaks from "+path))
		}
	}

	for _, path := range []string{
		"/assets/..%2fsecret.txt", "/assets/%2e%2e/secret.txt", "/assets/..%2flayout.html",
		"/assets/css/..%2f..%2fsecret.txt", "/assets/%2e%2e%2fsecret.txt",
	} {
		w := createGet(app, path)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "OUTSIDE") || strings.Contains(w.Body.String(), "LAYOUT") {
			t.Error(test.DiffMessage(w.Code, "a refusal", "R36: a path with .. is refused: "+path))
		}
	}
}

func TestAsset_ImmutableWithHash(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)
	hash := assetHash(assetCSS)

	w := createGet(app, "/assets/css/app.css?v="+hash)
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Error(test.DiffMessage(got, "public, max-age=31536000, immutable", "R37: the right v is immutable"))
	}
	if got := w.Header().Get("ETag"); got != `"`+hash+`"` {
		t.Error(test.DiffMessage(got, `"`+hash+`"`, "R37: the ETag is the quoted hash"))
	}
	if got := createRequest(app, http.MethodHead, "/assets/css/app.css?v="+hash).Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Error(test.DiffMessage(got, "public, max-age=31536000, immutable", "R37: HEAD is immutable too"))
	}
}

func TestAsset_NoCacheWithoutHash(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)
	hash := assetHash(assetCSS)

	for _, target := range []string{
		"/assets/css/app.css", "/assets/css/app.css?v=", "/assets/css/app.css?v=0000000000000000",
		"/assets/css/app.css?v=" + assetHash(assetJS), "/assets/css/app.css?v=" + strings.ToUpper(hash),
		"/assets/css/app.css?v=" + hash + "0", "/assets/css/app.css?x=" + hash,
	} {
		w := createGet(app, target)
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Error(test.DiffMessage(got, "no-cache", "R37: "+target))
		}
		if got := w.Header().Get("ETag"); got != `"`+hash+`"` {
			t.Error(test.DiffMessage(got, `"`+hash+`"`, "R37: the ETag is sent anyway: "+target))
		}
		if w.Code != http.StatusOK || w.Body.String() != assetCSS {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "R37: the file is served: "+target))
		}
	}
}

func TestAsset_ETag304(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)
	hash := assetHash(assetCSS)

	for _, target := range []string{"/assets/css/app.css", "/assets/css/app.css?v=" + hash} {
		w := createRequest(app, http.MethodGet, target, "If-None-Match", `"`+hash+`"`)
		if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.Len()}, []any{http.StatusNotModified, 0}, "R37: If-None-Match with the ETag: "+target))
		}
	}
	if w := createRequest(app, http.MethodGet, "/assets/css/app.css", "If-None-Match", `"0000000000000000"`); w.Code != http.StatusOK || w.Body.String() != assetCSS {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R37: another ETag is served in full"))
	}
}

func TestAsset_ETagMatchesAssetFunction(t *testing.T) {
	app := assetApp(t, assetFiles(), nil)

	link := strings.TrimSuffix(strings.TrimPrefix(createGet(app, "/page").Body.String(), "<html>"), "</html>")
	if !strings.HasPrefix(link, "/assets/") {
		t.Fatal(test.DiffMessage(link, "/assets/css/app.css?v=…", "R37: {{asset}} writes the URL of the asset"))
	}
	w := createGet(app, link)
	if got := w.Header().Get("Cache-Control"); w.Code != http.StatusOK || got != "public, max-age=31536000, immutable" {
		t.Error(test.DiffMessage(got, "public, max-age=31536000, immutable", "R37: the URL {{asset}} writes is immutable: "+link))
	}
}

func TestAsset_Reload(t *testing.T) {
	swap := &rlSwapFS{}
	swap.set(fstest.MapFS{
		"assets/app.css": {Data: []byte("a{}")},
		"page.html":      {Data: []byte(`{{asset "app.css"}}`)},
	})
	app, _ := rlApp(t, swap, true, rlPageOnly{})

	w := createGet(app, "/assets/app.css?v="+assetHash("a{}"))
	if got := w.Header().Get("Cache-Control"); got != "no-cache" || w.Header().Get("ETag") != "" {
		t.Error(test.DiffMessage([]any{got, w.Header().Get("ETag")}, []any{"no-cache", ""}, "R38: no-cache and no ETag, even with the right v"))
	}
	if w.Body.String() != "a{}" {
		t.Error(test.DiffMessage(w.Body.String(), "a{}", "R38: the file"))
	}
	if got := createGet(app, "/page").Body.String(); got != "/assets/app.css" {
		t.Error(test.DiffMessage(got, "/assets/app.css", "R38: {{asset}} adds no ?v="))
	}

	swap.set(fstest.MapFS{
		"assets/app.css": {Data: []byte("a{color:red}")},
		"assets/new.css": {Data: []byte("n{}")},
		"page.html":      {Data: []byte(`{{asset "app.css"}}`)},
	})
	if got := createGet(app, "/assets/app.css").Body.String(); got != "a{color:red}" {
		t.Error(test.DiffMessage(got, "a{color:red}", "R38: a changed file is read on each request"))
	}
	if w := createGet(app, "/assets/new.css"); w.Code != http.StatusOK || w.Body.String() != "n{}" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R38: a file added after Create is served"))
	}
	if w := createRequest(app, http.MethodGet, "/assets/app.css", "If-None-Match", `"`+assetHash("a{}")+`"`); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R38: no 304 without ETags"))
	}
}

type assetUserServe struct{ common.HTTP }

func (c assetUserServe) NewController() Controller   { return c }
func (c assetUserServe) SERVE_ANY_OF_assets() string { return "." }

func TestAsset_RouteConflict(t *testing.T) {
	app := New()
	app.EnableRender(&RenderOptions{FS: assetFiles(), Dir: "."})
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(assetPages{}, assetUserServe{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict:") {
		t.Error(test.DiffMessage(text, "route conflict: …", "R39: a user /assets/* route conflicts with the asset route"))
	}
}

func TestAsset_NoDirNoRoute(t *testing.T) {
	files := pgFiles(map[string]string{"x.html": "X"})
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(assetPlain{}).Build())

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		w := createRequest(app, method, "/assets/app.css")
		if w.Code != http.StatusNotFound || w.Header().Get("Allow") != "" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Allow")}, []any{http.StatusNotFound, ""}, "R39: no assets directory, no route: "+method))
		}
	}

	other := New()
	other.EnableRender(&RenderOptions{FS: files, Dir: "."})
	text, panicked := createPanicText(func() {
		other.Create(NewModuleBuilder().Controllers(assetPlain{}, assetUserServe{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "R39: without an assets directory a user /assets/* route is free"))
	}
}

type assetMarkMiddleware struct{}

func (assetMarkMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("X-Global-Middleware", "ran")
	next()
}

type assetDenyGuard struct{}

func (assetDenyGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Allow") == "yes"
}

func TestAsset_GlobalLayersApply(t *testing.T) {
	app := assetApp(t, assetFiles(), func(app *App) {
		app.BindGlobalMiddlewares(assetMarkMiddleware{})
		app.BindGlobalGuards(assetDenyGuard{})
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/css/app.css", nil))
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), assetCSS) {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "R40: a global guard refuses the asset"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Error(test.DiffMessage(ct, "application/json", "R17, R40: the refusal on the asset route is JSON"))
	}
	if w.Header().Get("X-Global-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Header().Get("X-Global-Middleware"), "ran", "R40: a global middleware runs for the asset route"))
	}

	allowed := createRequest(app, http.MethodGet, "/assets/css/app.css", "X-Allow", "yes")
	if allowed.Code != http.StatusOK || allowed.Body.String() != assetCSS || allowed.Header().Get("X-Global-Middleware") != "ran" {
		t.Error(test.DiffMessage([]any{allowed.Code, allowed.Body.String()}, []any{http.StatusOK, assetCSS}, "R40: the guard lets it through"))
	}
}
