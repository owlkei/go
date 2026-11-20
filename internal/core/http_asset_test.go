package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"go.owlkei.com/owlkei/internal/test"
)

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

func TestAsset_RouteConflict(t *testing.T) {
	app := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"testdata"}}, Render: &RenderOptions{FS: assetFiles(), Dir: "."}})
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(assetPages{}, assetUserServe{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict:") {
		t.Error(test.DiffMessage(text, "route conflict: …", "R39: a user /assets/* route conflicts with the asset route"))
	}
}

func TestAsset_NoDirNoRoute(t *testing.T) {
	files := pgFiles(map[string]string{"x.html": "X"})
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: files, Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(assetPlain{}).Build())

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		w := createRequest(app, method, "/assets/app.css")
		if w.Code != http.StatusNotFound || w.Header().Get("Allow") != "" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Allow")}, []any{http.StatusNotFound, ""}, "R39: no assets directory, no route: "+method))
		}
	}

	other := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"testdata"}}, Render: &RenderOptions{FS: files, Dir: "."}})
	text, panicked := createPanicText(func() {
		other.Create(NewModuleBuilder().Controllers(assetPlain{}, assetUserServe{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "R39: without an assets directory a user /assets/* route is free"))
	}
}

func TestAsset_SharedNoGuardRuns(t *testing.T) {
	log := &secGuardLog{}
	app := assetApp(t, assetFiles(), func(app *App) {
		app.BindGlobalMiddlewares(assetMarkMiddleware{})
		app.BindGlobalGuards(secLogGuard{Name: "global", Log: log, Mode: "deny"})
	})

	w := createGet(app, "/assets/css/app.css")
	if w.Code != http.StatusOK || w.Body.String() != assetCSS {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, assetCSS}, "G4: a refusing global guard does not cover a shared asset"))
	}
	if w.Header().Get("X-Global-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Header().Get("X-Global-Middleware"), "ran", "G4: a global middleware still runs for the asset route"))
	}
	if len(log.names) != 0 {
		t.Error(test.DiffMessage(log.names, []string(nil), "G4: no guard runs for a shared asset"))
	}
}

func secPageAsset(app *App, path string, allow bool, query string) *httptest.ResponseRecorder {
	headers := []string{"Accept", "text/html"}
	if allow {
		headers = append(headers, "X-Allow", "yes")
	}
	return createRequest(app, http.MethodGet, "/assets/_pages/"+path+query, headers...)
}

func TestAsset_GuardedPageIsPrivate(t *testing.T) {
	app := secPagesApp(t, nil)
	w := secPageAsset(app, "dash.css", true, "")
	if w.Code != http.StatusOK || w.Body.String() != "DASH-CSS" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Cache-Control")}, []any{http.StatusOK, "private, no-cache"}, "C3: a guarded page's file is private"))
	}
	w = secPageAsset(app, "dash.css", true, "?v="+assetHash("DASH-CSS"))
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Error(test.DiffMessage(got, "private, max-age=31536000, immutable", "C3: a guarded page's file with ?v="))
	}
}

func TestAsset_SharedStaysPublic(t *testing.T) {
	app := secPagesApp(t, nil)
	for target, want := range map[string]string{
		"/assets/css/app.css":                                "no-cache",
		"/assets/css/app.css?v=" + assetHash(assetCSS):       "public, max-age=31536000, immutable",
		"/robots.txt":                                        "no-cache",
		"/assets/_pages/free.css":                            "no-cache",
		"/assets/_pages/free.css?v=" + assetHash("FREE-CSS"): "public, max-age=31536000, immutable",
		"/assets/_pages/layout.css":                          "no-cache",
	} {
		if got := createGet(app, target).Header().Get("Cache-Control"); got != want {
			t.Error(test.DiffMessage(got, want, "C3: "+target+" keeps today's value"))
		}
	}
}

func TestAsset_PageRunsPageGuards(t *testing.T) {
	app := secPagesApp(t, &secLogGuard{Name: "global", Log: secPagesLog})
	createRequest(app, http.MethodGet, "/dash", "X-Allow", "yes")
	want := secPagesLog.take()
	if len(want) != 3 || want[0] != "global" || want[1] != "controller" || want[2] != "route" {
		t.Fatal(test.DiffMessage(want, []string{"global", "controller", "route"}, "G5: the page's own guard order"))
	}
	w := secPageAsset(app, "dash.css", true, "")
	if w.Code != http.StatusOK || w.Body.String() != "DASH-CSS" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "DASH-CSS"}, "G5: the guards let the file through"))
	}
	if got := secPagesLog.take(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Error(test.DiffMessage(got, want, "G5: a page file runs its page's guards in that route's order"))
	}
	secPageAsset(app, "open.css", true, "")
	if got := secPagesLog.take(); len(got) != 2 || got[0] != "global" || got[1] != "controller" {
		t.Error(test.DiffMessage(got, []string{"global", "controller"}, "G5: a page without a route-bound guard"))
	}
}

func TestAsset_PageGuardRefusalIs404(t *testing.T) {
	for _, mode := range []string{"", "deny", "unauthorized"} {
		app := secPagesApp(t, &secLogGuard{Name: "g", Log: secPagesLog, Mode: mode})
		w := secPageAsset(app, "free.css", false, "")
		if w.Code != http.StatusNotFound || w.Body.String() != wantNotFoundJSON || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || strings.Contains(w.Body.String(), "FREE-CSS") {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusNotFound, wantNotFoundJSON}, "G6: a refusal is the fixed 404 JSON (mode "+mode+")"))
		}
		if page := createRequest(app, http.MethodGet, "/free"); mode == "" && page.Code != http.StatusForbidden {
			t.Error(test.DiffMessage(page.Code, http.StatusForbidden, "G6: the page itself still answers 403"))
		}
	}
	boom := secPagesApp(t, &secLogGuard{Name: "g", Log: secPagesLog, Mode: "boom"})
	if w := secPageAsset(boom, "free.css", true, ""); w.Code != http.StatusInternalServerError || w.Body.String() != wantInternalErrorJSON {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusInternalServerError, wantInternalErrorJSON}, "G6: another panic answers 500"))
	}
}

func TestAsset_LayoutAndErrorFilesOpen(t *testing.T) {
	app := secPagesApp(t, &secLogGuard{Name: "g", Log: secPagesLog, Mode: "deny"})
	for path, body := range map[string]string{"layout.css": "LAYOUT-CSS", "404.css": "NF-CSS"} {
		if w := secPageAsset(app, path, false, ""); w.Code != http.StatusOK || w.Body.String() != body {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, body}, "G5: a shared page file is open: "+path))
		}
	}
	if got := secPagesLog.take(); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string(nil), "G5: no guard runs for them"))
	}
}

func TestAsset_PageGuardSeesNoParams(t *testing.T) {
	app := secPagesApp(t, &secLogGuard{Name: "global", Log: secPagesLog})
	secPageAsset(app, "item/{id}.css", true, "")
	secPagesLog.mu.Lock()
	defer secPagesLog.mu.Unlock()
	if len(secPagesLog.paths) == 0 || secPagesLog.paths[len(secPagesLog.paths)-1] != "/assets/_pages/item/{id}.css" || secPagesLog.params[len(secPagesLog.params)-1] != 0 {
		t.Error(test.DiffMessage(secPagesLog.paths, []string{"/assets/_pages/item/{id}.css"}, "G6: the guard sees the asset URL and no parameter"))
	}
}

func TestAsset_UnknownPageFileRunsNoGuard(t *testing.T) {
	app := secPagesApp(t, &secLogGuard{Name: "g", Log: secPagesLog})
	w := secPageAsset(app, "nope.css", true, "")
	if w.Code != http.StatusNotFound || w.Body.String() != wantNotFoundJSON || len(secPagesLog.take()) != 0 {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "G8: an unknown page file is the fixed 404 and runs no guard"))
	}
}

func TestAsset_PageBehindGate(t *testing.T) {
	log := &secGuardLog{}
	files := map[string]string{"gated.html": "{{pageAssets}}G", "gated.css": "G-CSS"}
	app, _, gated, _, release := lfGateAppHeld(t, files, func(a *App) { a.BindGlobalGuards(secLogGuard{Name: "g", Log: log}) })

	w := secPageAsset(app, "gated.css", true, "")
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != lfWant503JSON || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusServiceUnavailable, lfWant503JSON}, "G7: pending answers the API 503"))
	}
	if seconds, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || seconds <= 0 {
		t.Error(test.DiffMessage(w.Header().Get("Retry-After"), "a positive integer", "G7: Retry-After while pending"))
	}
	if len(log.names) != 0 {
		t.Error(test.DiffMessage(log.names, []string(nil), "G7: no guard runs behind a gate that is not live"))
	}

	close(release)
	lfWait(t, app.container.settled(gated), "the gated module to be ready")
	if w := secPageAsset(app, "gated.css", false, ""); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "G7: live, the live route's guards run: a refusal is 404"))
	}
	if w := secPageAsset(app, "gated.css", true, ""); w.Code != http.StatusOK || w.Body.String() != "G-CSS" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "G-CSS"}, "G7: ready"))
	}

	failed, failedSQL, _ := lfGateApp(t, files, nil)
	failed.container.settle(failedSQL, errors.New("lf-down"))
	w = secPageAsset(failed, "gated.css", true, "")
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != lfWant503JSON || len(w.Header().Values("Retry-After")) != 0 {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Values("Retry-After")}, []any{http.StatusServiceUnavailable, []string(nil)}, "G7: failed answers 503 without Retry-After"))
	}
}

func TestAsset_PageReloadOwner(t *testing.T) {
	files := func(extra map[string]string) fstest.MapFS {
		m := secPagesFiles()
		for k, v := range extra {
			m[k] = v
		}
		return pgFiles(m)
	}
	swap := &rlSwapFS{}
	swap.set(files(nil))
	secPagesLog.take()
	app, _ := rlApp(t, swap, true, secGuardedPages{}, secOpenPages{})
	swap.set(files(map[string]string{"dash.js": "DASH-JS"}))

	if w := secPageAsset(app, "dash.js", false, ""); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "DASH-JS") {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "G5: with IsReload a new file of a guarded page is guarded"))
	}
	if got := secPagesLog.take(); len(got) != 1 || got[0] != "controller" {
		t.Error(test.DiffMessage(got, []string{"controller"}, "G5: the owner is read from the current template set; the first refusal stops the chain"))
	}
	if w := secPageAsset(app, "dash.js", true, ""); w.Code != http.StatusOK || w.Body.String() != "DASH-JS" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "DASH-JS"}, "G5: and served when the guards allow"))
	}
}
