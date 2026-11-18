package core

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type paBookings struct{ common.HTTP }

func (c paBookings) NewController() Controller { return c }

func (c paBookings) RENDER_new_OF_bookings() {}

func (c paBookings) RENDER_bookings_BY_id(ctx.Param) {}

const (
	paLayoutCSS = "l{color:#111}"
	paNewCSS    = "n{color:#222}"
	paNewJS     = "console.log('new')"
	paIDCSS     = "i{color:#333}"
)

func paFiles() map[string]string {
	return map[string]string{
		"layout.html":          `<head>{{pageAssets}}</head>{{template "content" .}}`,
		"layout.css":           paLayoutCSS,
		"bookings/new.html":    `NEW`,
		"bookings/new.css":     paNewCSS,
		"bookings/new.form.js": paNewJS,
		"bookings/{id}.html":   `ONE`,
		"bookings/{id}.css":    paIDCSS,
		"partials/field.html":  `FIELD`,
		"secret.txt":           `OUTSIDE`,
	}
}

func paApp(t *testing.T, files map[string]string, setup func(*App), controllers ...Controller) *App {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{paBookings{}}
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

func TestPageAssets_Tags(t *testing.T) {
	app := paApp(t, paFiles(), nil)

	want := `<head><link rel="stylesheet" href="/assets/_pages/layout.css?v=` + assetHash(paLayoutCSS) + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/bookings/new.css?v=` + assetHash(paNewCSS) + `">` + "\n" +
		`<script src="/assets/_pages/bookings/new.form.js?v=` + assetHash(paNewJS) + `" defer></script></head>NEW`
	if got := createGet(app, "/bookings/new").Body.String(); got != want {
		t.Error(test.DiffMessage(got, want, "P29: the layout's files, then the page's"))
	}
}

func TestPageAssets_EncodedNames(t *testing.T) {
	app := paApp(t, paFiles(), nil)

	want := `<head><link rel="stylesheet" href="/assets/_pages/layout.css?v=` + assetHash(paLayoutCSS) + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/bookings/%7Bid%7D.css?v=` + assetHash(paIDCSS) + `"></head>ONE`
	if got := createGet(app, "/bookings/7").Body.String(); got != want {
		t.Error(test.DiffMessage(got, want, "P29: {id}.css is %7Bid%7D.css"))
	}
	for _, target := range []string{"/assets/_pages/bookings/%7Bid%7D.css", "/assets/_pages/bookings/{id}.css"} {
		if w := createGet(app, target); w.Code != http.StatusOK || w.Body.String() != paIDCSS {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, paIDCSS}, "P33: GET "+target))
		}
	}
}

func TestPageAssets_ErrorPageSet(t *testing.T) {
	files := paFiles()
	files["404.html"] = `E{{.Code}}`
	files["404.css"] = "e{}"
	app := paApp(t, files, nil)

	w := createRequest(app, http.MethodGet, "/nowhere", "Accept", "text/html")
	want := `<head><link rel="stylesheet" href="/assets/_pages/layout.css?v=` + assetHash(paLayoutCSS) + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/404.css?v=` + assetHash("e{}") + `"></head>E404`
	if w.Code != http.StatusNotFound || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P28, P29: an error page links its own files after the layout's"))
	}
}

func TestPageAssets_NoNonce(t *testing.T) {
	app := paApp(t, paFiles(), nil)
	before := app.http.render.NonceDraws()
	w := createGet(app, "/bookings/new")
	if w.Code != http.StatusOK || app.http.render.NonceDraws() != before || len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(app.http.render.NonceDraws(), before, "P31: a page with {{pageAssets}} and no {{nonce}} draws no nonce"))
	}
}

func TestPageAssets_Served(t *testing.T) {
	app := paApp(t, paFiles(), nil)
	hash := assetHash(paNewCSS)

	w := createGet(app, "/assets/_pages/bookings/new.css?v="+hash)
	if w.Code != http.StatusOK || w.Body.String() != paNewCSS || !strings.Contains(w.Header().Get("Content-Type"), "text/css") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, paNewCSS}, "P33: the page's CSS"))
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Error(test.DiffMessage(got, "public, max-age=31536000, immutable", "P33: immutable for a matching ?v="))
	}
	if got := w.Header().Get("ETag"); got != `"`+hash+`"` {
		t.Error(test.DiffMessage(got, `"`+hash+`"`, "P33: the ETag"))
	}

	w = createGet(app, "/assets/_pages/bookings/new.css")
	if got := w.Header().Get("Cache-Control"); got != "no-cache" || w.Body.String() != paNewCSS {
		t.Error(test.DiffMessage(got, "no-cache", "P33: without ?v= it is no-cache"))
	}
	w = createGet(app, "/assets/_pages/bookings/new.css?v=0000000000000000")
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Error(test.DiffMessage(got, "no-cache", "P33: a wrong ?v= is no-cache"))
	}

	w = createRequest(app, http.MethodGet, "/assets/_pages/bookings/new.css", "If-None-Match", `"`+hash+`"`)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Code, http.StatusNotModified, "P33: 304"))
	}

	js := createGet(app, "/assets/_pages/bookings/new.form.js?v="+assetHash(paNewJS))
	if js.Code != http.StatusOK || js.Body.String() != paNewJS || !strings.Contains(js.Header().Get("Content-Type"), "javascript") {
		t.Error(test.DiffMessage([]any{js.Code, js.Header().Get("Content-Type")}, []any{http.StatusOK, "javascript"}, "P33: the page's JS"))
	}
	head := createRequest(app, http.MethodHead, "/assets/_pages/layout.css")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Error(test.DiffMessage(head.Code, http.StatusOK, "P33: HEAD"))
	}
}

func TestPageAssets_OnlyOwnedFiles(t *testing.T) {
	files := paFiles()
	files["assets/app.css"] = "a{}"
	files["bookings/.hidden.css"] = "h{}"
	app := paApp(t, files, nil)

	for _, target := range []string{
		"/assets/_pages/", "/assets/_pages", "/assets/_pages/bookings", "/assets/_pages/bookings/",
		"/assets/_pages/bookings/new.html", "/assets/_pages/layout.html", "/assets/_pages/bookings/%7Bid%7D.html",
		"/assets/_pages/partials/field.html", "/assets/_pages/secret.txt", "/assets/_pages/bookings/.hidden.css",
		"/assets/_pages/bookings/missing.css", "/assets/_pages/assets/app.css", "/assets/_pages/app.css",
		"/assets/_pages/..%2fsecret.txt", "/assets/_pages/%2e%2e/secret.txt", "/assets/_pages/bookings/..%2f..%2fsecret.txt",
	} {
		w := createRequest(app, http.MethodGet, target, "Accept", "text/html")
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "NEW") || strings.Contains(w.Body.String(), "OUTSIDE") || strings.Contains(w.Body.String(), "FIELD") {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusNotFound, "no file content"}, "P33: "+target))
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Error(test.DiffMessage(ct, "application/json", "P33, R17: the refusal is JSON: "+target))
		}
	}
	if w := createGet(app, "/assets/app.css"); w.Code != http.StatusOK || w.Body.String() != "a{}" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P33: the ordinary asset route still serves assets/"))
	}
}

func TestPageAssets_RouteWithoutAssetsDir(t *testing.T) {
	app := paApp(t, paFiles(), nil)
	if w := createGet(app, "/assets/_pages/layout.css"); w.Code != http.StatusOK || w.Body.String() != paLayoutCSS {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P33: page files alone create the asset route"))
	}
	if w := createGet(app, "/assets/app.css"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P33: and /assets/ has no other file"))
	}

	plain := paApp(t, map[string]string{"bookings/new.html": "NEW", "bookings/{id}.html": "ONE"}, nil)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		w := createRequest(plain, method, "/assets/_pages/layout.css")
		if w.Code != http.StatusNotFound || w.Header().Get("Allow") != "" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Allow")}, []any{http.StatusNotFound, ""}, "R39: without an assets directory or page files there is no route: "+method))
		}
	}
	free := New()
	free.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"bookings/new.html": "NEW", "bookings/{id}.html": "ONE"}), Dir: "."})
	if text, panicked := createPanicText(func() {
		free.Create(NewModuleBuilder().Controllers(paBookings{}, assetUserServe{}).Build())
	}); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R39: without assets and page files a user /assets/* route is free"))
	}
}

func TestPageAssets_RouteConflictWithUserAssetsRoute(t *testing.T) {
	app := New()
	app.EnableRender(&RenderOptions{FS: pgFiles(paFiles()), Dir: "."})
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(paBookings{}, assetUserServe{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict:") {
		t.Error(test.DiffMessage(text, "route conflict: …", "R39: page files create the /assets/* route, which conflicts with a user route"))
	}
}

func TestPageAssets_GlobalLayersApply(t *testing.T) {
	app := paApp(t, paFiles(), func(app *App) {
		app.BindGlobalMiddlewares(assetMarkMiddleware{})
		app.BindGlobalGuards(assetDenyGuard{})
	})
	w := createGet(app, "/assets/_pages/layout.css")
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), paLayoutCSS) || w.Header().Get("X-Global-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "R40: a global guard covers the page files too"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Error(test.DiffMessage(ct, "application/json", "R17: the refusal is JSON"))
	}
}

func TestPageAssets_Reload(t *testing.T) {
	swap := &rlSwapFS{}
	swap.set(fstest.MapFS{
		"layout.html": {Data: []byte(`{{pageAssets}}{{template "content" .}}`)},
		"x.html":      {Data: []byte(`X`)},
		"x.css":       {Data: []byte(`a{}`)},
	})
	app, _ := rlApp(t, swap, true, rlX{})

	if got, want := createGet(app, "/x").Body.String(), `<link rel="stylesheet" href="/assets/_pages/x.css">X`; got != want {
		t.Error(test.DiffMessage(got, want, "P29: with IsReload the tag has no ?v="))
	}
	w := createGet(app, "/assets/_pages/x.css?v="+assetHash("a{}"))
	if got := w.Header().Get("Cache-Control"); got != "no-cache" || w.Header().Get("ETag") != "" || w.Body.String() != "a{}" {
		t.Error(test.DiffMessage([]any{got, w.Header().Get("ETag")}, []any{"no-cache", ""}, "P33: IsReload sends no-cache and no ETag, even with the right v"))
	}
	swap.set(fstest.MapFS{
		"layout.html": {Data: []byte(`{{pageAssets}}{{template "content" .}}`)},
		"x.html":      {Data: []byte(`X`)},
		"x.css":       {Data: []byte(`a{color:red}`), ModTime: time.Now()},
		"x.js":        {Data: []byte(`1`), ModTime: time.Now()},
	})
	if got := createGet(app, "/assets/_pages/x.css").Body.String(); got != "a{color:red}" {
		t.Error(test.DiffMessage(got, "a{color:red}", "P33: a changed file is read on each request"))
	}
	if got, want := createGet(app, "/x").Body.String(), `<link rel="stylesheet" href="/assets/_pages/x.css">`+"\n"+`<script src="/assets/_pages/x.js" defer></script>X`; got != want {
		t.Error(test.DiffMessage(got, want, "P29: the tags follow the reload"))
	}
}

func TestPageAssets_ReloadConcurrent(t *testing.T) {
	state := func(css string, at time.Time) fstest.MapFS {
		return fstest.MapFS{
			"layout.html": {Data: []byte(`{{pageAssets}}{{template "content" .}}`)},
			"x.html":      {Data: []byte(`X`)},
			"x.css":       {Data: []byte(css), ModTime: at},
		}
	}
	swap := &rlSwapFS{}
	swap.set(state("a{}", rlEpoch))
	app, _ := rlApp(t, swap, true, rlX{})

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				swap.set(state("bb{}", rlEpoch.Add(time.Duration(i)*time.Second)))
			} else {
				swap.set(state("a{}", rlEpoch.Add(time.Duration(i)*time.Second)))
			}
		}
	}()

	const page = `<link rel="stylesheet" href="/assets/_pages/x.css">X`
	var workers sync.WaitGroup
	var mu sync.Mutex
	var bad []string
	for g := 0; g < 16; g++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 40 {
				p := createGet(app, "/x")
				c := createGet(app, "/assets/_pages/x.css")
				if p.Code != http.StatusOK || p.Body.String() != page || c.Code != http.StatusOK || (c.Body.String() != "a{}" && c.Body.String() != "bb{}") {
					mu.Lock()
					bad = append(bad, p.Body.String()+" | "+c.Body.String())
					mu.Unlock()
				}
			}
		}()
	}
	workers.Wait()
	close(stop)
	<-done
	if len(bad) != 0 {
		t.Error(test.DiffMessage(bad[0], "one whole state", "P29, P33: concurrent requests under IsReload see a whole state"))
	}
}
