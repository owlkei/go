package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestServe_NonStringResultIs404(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/number")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S2: a handler result that is not a string answers 404"))
	}
	if w.Body.String() != wantNotFoundJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S2: the 404 body is N1's"))
	}
}

func TestServe_ExistingFile(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/logo.png")

	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "a SERVE route serves an existing file with 200"))
	}
	if w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "a SERVE route writes the file contents"))
	}
}

func TestServe_MissingFileIs404(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/missing.png")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S2: a missing file answers 404"))
	}
	if w.Body.String() != wantNotFoundJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S2: the 404 body is N1's"))
	}
}

func TestServe_WildcardServesFileUnderDirectory(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/files/css/app.css")

	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "a wildcard SERVE route serves a file found under the directory"))
	}
	if w.Body.String() != "body{}" {
		t.Error(test.DiffMessage(w.Body.String(), "body{}", "a wildcard SERVE route writes the resolved file's contents"))
	}
}

func TestServe_TraversalIs400(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(filepath.Dir(serveRoot), "files", "secret"), "do-not-leak")

	w := serveGet(app, "/files/a/../../files/secret")

	if w.Code != http.StatusBadRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusBadRequest, "S2: a resolved path outside the served directory answers 400"))
	}
	if w.Body.String() != wantInvalidURLJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantInvalidURLJSON, "S2: the 400 body is fixed"))
	}
	if strings.Contains(w.Body.String(), "do-not-leak") {
		t.Error(test.DiffMessage(w.Body.String(), "<no leaked file contents>", "S2: the escaped file is never read"))
	}
}

func TestServe_DirectoryWithoutIndexIs404(t *testing.T) {
	app := newServeApp(t)

	for _, target := range []string{"/files/", "/files/css/", "/docs/"} {
		w := serveGet(app, target)

		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S1: a directory without index.html answers 404 for "+target))
		}
		if w.Body.String() != wantNotFoundJSON {
			t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S1: the 404 body is N1's for "+target))
		}
	}
}

func TestServe_NeverListsDirectory(t *testing.T) {
	app := newServeApp(t)

	for _, target := range []string{"/files/", "/files/css/", "/docs/"} {
		w := serveGet(app, target)
		body := w.Body.String()

		for _, leaked := range []string{".env", "logo.png", "app.css", "<a href", "<pre>"} {
			if strings.Contains(body, leaked) {
				t.Error(test.DiffMessage(body, "<no directory listing>", "S1: "+target+" must not list "+leaked))
			}
		}
	}
}

func TestServe_DirectoryWithIndex(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, "sub", "index.html"), "<h1>home</h1>")

	w := serveGet(app, "/files/sub/")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "S1: a directory with index.html answers that file"))
	}
	if w.Body.String() != "<h1>home</h1>" {
		t.Error(test.DiffMessage(w.Body.String(), "<h1>home</h1>", "S1: the body is the index.html contents"))
	}
}

func TestServe_DirectoryWithIndexRedirectsToSlash(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, "sub", "index.html"), "<h1>home</h1>")

	w := serveGet(app, "/files/sub")
	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "S1: a directory URL without the slash redirects as http.ServeFile does"))
	}
	if got := w.Header().Get("Location"); got != "sub/" {
		t.Error(test.DiffMessage(got, "sub/", "S1: the redirect points at the slash-terminated URL"))
	}
}

func TestApp_FilterRethrowKeepsCode(t *testing.T) {
	rethrowCalls = 0
	app := New()
	app.Create(NewModuleBuilder().Controllers(rethrowController{}).Build())

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

func TestServe_NameAnyServesSubtree(t *testing.T) {
	app := svApp(t)

	cases := []struct{ target, body string }{
		{"/static/a.txt", "alpha"},
		{"/static/sub/x.txt", "nested"},
	}
	for _, c := range cases {
		w := createGet(app, c.target)
		if w.Code != http.StatusOK {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "V3: status of GET "+c.target))
		}
		if w.Body.String() != c.body {
			t.Error(test.DiffMessage(w.Body.String(), c.body, "V3: body of GET "+c.target))
		}
	}
}

func TestServe_NameAnyDoesNotMatchPrefixSibling(t *testing.T) {
	app := svApp(t)

	for _, target := range []string{"/staticzzz", "/staticzzz/a.txt", "/other/a.txt"} {
		if w := createGet(app, target); w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "V3: GET "+target+" is not under /static/"))
		}
	}
}

func TestServe_NameAnyMissingFileIs404(t *testing.T) {
	app := svApp(t)

	if w := createGet(app, "/static/missing.txt"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "V3: a missing file under /static/ is 404"))
	}
}

func TestServe_NameAnyTraversalIsRefused(t *testing.T) {
	app := svApp(t)
	secret := filepath.Join(filepath.Dir(svRoot), "secret.txt")
	svWrite(t, secret, "secret")

	w := createGet(app, "/static/..%2fsecret.txt")
	if w.Code == http.StatusOK || w.Body.String() == "secret" {
		t.Error(test.DiffMessage(w.Code, "a refusal", "V3: a traversal never leaves the served directory"))
	}
}

func TestServe_NameAnyConflictsWithAnyOfName(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(svNameAnyController{}, svAnyOfController{}).Build())
	})

	want := "route conflict:"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "V4: SERVE_static_ANY and SERVE_ANY_OF_static are the same route"))
	}
}

func TestServeHTTP_NotFoundAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodGet, "/does-not-exist"); got > 10 {
		t.Error(test.DiffMessage(got, "<= 10", "P7: a 404 writes shared bytes (today 23 allocations, about 5 expected)"))
	}
}

func TestServeHTTP_MethodNotAllowedAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodDelete, "/bench"); got > 10 {
		t.Error(test.DiffMessage(got, "<= 10", "P7: a 405 writes shared bytes (about 5 expected)"))
	}
}

func TestServeHTTP_StaticRouteAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodGet, "/bench"); got > 7 {
		t.Error(test.DiffMessage(got, "<= 7", "P1, P3: a static route without body or middleware (today 9 allocations, 6 expected)"))
	}
}

func TestServeHTTP_ConcurrentAllPaths_NoRace(t *testing.T) {
	app := New()
	app.SetLogger(&mockLogger{})
	app.Create(NewModuleBuilder().Controllers(ccController{}).Build())

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

func TestMultipart_TempFilesRemovedAfterRequest(t *testing.T) {
	for _, route := range []string{"/ok", "/boom", "/abort"} {
		dir := t.TempDir()
		t.Setenv("TMPDIR", dir)
		app := New()
		app.SetLogger(quietLogger{})
		app.maxRequestBodyBytes.Store(64 << 20)
		app.Create(NewModuleBuilder().Controllers(multipartController{}).Build())

		body, contentType := largeMultipart(t)
		r := httptest.NewRequest(http.MethodPost, route, body)
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		serveRecovering(app, w, r)

		if route == "/ok" && w.Code != http.StatusCreated {
			t.Error(test.DiffMessage(w.Code, http.StatusCreated, "M1: the upload itself succeeds"))
		}
		if got := multipartTempFiles(t, dir); got != 0 {
			t.Error(test.DiffMessage(got, 0, "M1: multipart temp files are removed after the request ("+route+")"))
		}
	}
}

func TestNoRoute_404FixedBody(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/nowhere")

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N1: a path without a route answers the fixed 404")
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "N1: Content-Type"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "N1: X-Request-Id is set"))
	}
}

func TestNoRoute_404DoesNotEchoThePath(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/<script>alert(1)</script>")

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "bug 7, N1: the body does not depend on the request")
}

func TestNoRoute_LongPathSameBody(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/"+strings.Repeat("a", 100000))

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "bug 7, N1: a 100,000-character path gets the same body")
	if w.Body.Len() != 54 {
		t.Error(test.DiffMessage(w.Body.Len(), 54, "bug 7, N1: the body is 54 bytes"))
	}
}

func TestNoRoute_404ForEveryMethod(t *testing.T) {
	app := lcApp(t)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		w := lcServe(app, method, "/nowhere")
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "N1: "+method+" to a path with no route under any method"))
		}
	}
}

func TestNoRoute_405WithAllow(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/ok")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "bug 8, N2: a path with a route for another method answers 405")
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Error(test.DiffMessage(got, "GET, HEAD, POST", "N2: Allow lists the methods that have a route, HEAD with GET"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "N2: Content-Type"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "N2: X-Request-Id is set"))
	}
}

func TestNoRoute_405ForOptionsAndUnknownMethod(t *testing.T) {
	app := lcApp(t)
	for _, method := range []string{http.MethodOptions, "BREW"} {
		w := lcServe(app, method, "/ok")

		lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: "+method+" gets the 405")
		if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
			t.Error(test.DiffMessage(got, "GET, HEAD, POST", "N2: Allow for "+method))
		}
	}
}

func TestNoRoute_AllowOrder(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodTrace, "/bare")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: TRACE on a path with a route for every DSL method")
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Error(test.DiffMessage(got, "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", "N2: Allow follows the fixed order, not the order of registration"))
	}
}

func TestNoRoute_AllowWithoutGetHasNoHead(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/postonly")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: GET on a POST-only path")
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Error(test.DiffMessage(got, "POST", "N2: HEAD is listed exactly when GET is"))
	}
}

func TestNoRoute_HeadIn405Allow(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/readonly")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: DELETE on a GET-only path")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, N3: HEAD is listed with GET"))
	}
}

func TestNoRoute_HeadOnPostOnlyPathIs405(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodHead, "/postonly")

	if w.Code != http.StatusMethodNotAllowed {
		t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "N3: HEAD is served only by a GET route"))
	}
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Error(test.DiffMessage(got, "POST", "N2: Allow for a HEAD request"))
	}
}

func TestNoRoute_HeadOnUnknownPathIs404(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodHead, "/nowhere")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "N1: HEAD to a path with no route"))
	}
}

func TestNoRoute_HeadUsesGetRoute(t *testing.T) {
	app := lcApp(t)
	var method string
	lcSetHandler(func(c *ctx.HTTPContext) any {
		method = c.Method
		return "H"
	})

	get := lcServe(app, http.MethodGet, "/ok")
	getLog := lcAll()
	lcClearLog()
	head := lcServe(app, http.MethodHead, "/ok")
	headLog := lcAll()

	if head.Code != get.Code || head.Code != http.StatusOK {
		t.Error(test.DiffMessage(head.Code, get.Code, "bug 9, N3: HEAD answers the status of GET"))
	}
	if head.Header().Get("Content-Type") != get.Header().Get("Content-Type") || head.Header().Get("Content-Type") == "" {
		t.Error(test.DiffMessage(head.Header().Get("Content-Type"), get.Header().Get("Content-Type"), "N3: HEAD answers the headers of GET"))
	}
	if head.Body.String() != "H>m2>m1>g2>g1" {
		t.Error(test.DiffMessage(head.Body.String(), "H>m2>m1>g2>g1", "N3: through ServeHTTP on a recorder the body is what the handler wrote"))
	}
	if method != http.MethodHead {
		t.Error(test.DiffMessage(method, http.MethodHead, "N3: c.Method stays HEAD"))
	}
	lcExpectEntries(t, headLog, getLog, "N3: HEAD goes through the whole lifecycle of the GET route")
}

func TestNoRoute_HeadGoesThroughGuards(t *testing.T) {
	app := lcApp(t)
	lcSetGuard("g2", func(*ctx.HTTPContext) bool { return false })
	w := lcServe(app, http.MethodHead, "/ok")

	lcExpectResponse(t, w, http.StatusForbidden, "F[mf2]Access denied", "N3: a guard that refuses GET refuses HEAD")
	lcExpectEntries(t, lcEntries("handler"), nil, "N3: the handler does not run")
}

func TestNoRoute_PathOverLimit(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/"+strings.Repeat("a", 4097))
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N4: a path longer than 4,096 bytes matches nothing")

	w = lcServe(app, http.MethodGet, strings.Repeat("/a", 65))
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N4: a path with more than 64 segments matches nothing")
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

func TestServe_DotRootServesFilesUnderWorkingDirectory(t *testing.T) {
	file, err := os.CreateTemp(".", "serve_dot_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	if _, err := file.WriteString("DOT-ROOT-CONTENT"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	hidden, err := os.CreateTemp(".", ".serve_dot_hidden_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(hidden.Name()) })
	if _, err := hidden.WriteString("DOT-ROOT-SECRET"); err != nil {
		t.Fatal(err)
	}
	_ = hidden.Close()

	app := New()
	app.Create(NewModuleBuilder().Controllers(serveDotController{}).Build())

	w := serveGet(app, "/dot/"+strings.TrimPrefix(file.Name(), "./"))
	if w.Code != http.StatusOK || w.Body.String() != "DOT-ROOT-CONTENT" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "DOT-ROOT-CONTENT"}, "L9: a static route whose handler returns \".\" serves files under the working directory"))
	}

	w = serveGet(app, "/dot/"+strings.TrimPrefix(hidden.Name(), "./"))
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "DOT-ROOT-SECRET") {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "L9: a dotfile under the working directory still answers 404"))
	}

	w = serveGet(app, "/dot/../go.mod")
	if w.Code == http.StatusOK {
		t.Error(test.DiffMessage(w.Code, "not 200", "L9: a path leaving the working directory is refused"))
	}
}

func TestServe_DotfilesAre404(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, ".git", "config"), "SECRET=git")
	writeServeFile(t, filepath.Join(serveRoot, "css", ".secret"), "SECRET=css")
	writeServeFile(t, filepath.Join(serveRoot, ".hidden", "index.html"), "SECRET=index")
	for _, target := range []string{"/files/.env", "/files/%2eenv", "/files/.git/config", "/files/css/.secret", "/files/.hidden/", "/files/.hidden/index.html", "/files/.git/"} {
		w := serveGet(app, target)
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "a dotfile or dot directory under a served directory answers 404: "+target))
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Body.String(), "<no file contents>", "a dotfile is never read: "+target))
		}
	}
}

func TestServe_WellKnownIsServed(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, ".well-known", "security.txt"), "Contact: x")
	w := serveGet(app, "/files/.well-known/security.txt")
	if w.Code != http.StatusOK || w.Body.String() != "Contact: x" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, ".well-known (RFC 8615) stays served"))
	}
}

func TestServe_SymlinkOutOfRootIs404(t *testing.T) {
	app := newServeApp(t)
	outside := filepath.Dir(serveRoot)
	writeServeFile(t, filepath.Join(outside, "outside.txt"), "SECRET=file")
	writeServeFile(t, filepath.Join(outside, "outdir", "f.txt"), "SECRET=dir")
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(serveRoot, "link.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := os.Symlink(filepath.Join(outside, "outdir"), filepath.Join(serveRoot, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside.txt", filepath.Join(serveRoot, "css", "rel.txt")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/files/link.txt", "/files/linkdir/f.txt", "/files/css/rel.txt"} {
		w := serveGet(app, target)
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "a symlink leaving the served directory answers 404: "+target))
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Body.String(), "<no file contents>", "a file outside the served directory is never read: "+target))
		}
	}
}

func TestServe_SymlinkInsideRootIsServed(t *testing.T) {
	app := newServeApp(t)
	if err := os.Symlink("logo.png", filepath.Join(serveRoot, "alias.png")); err != nil {
		t.Skip("no symlinks:", err)
	}
	w := serveGet(app, "/files/alias.png")
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "a symlink that stays inside the served directory is served"))
	}
}

func TestServe_RangeAndConditionalStillWork(t *testing.T) {
	app := newServeApp(t)
	r := httptest.NewRequest(http.MethodGet, "/files/logo.png", nil)
	r.Header.Set("Range", "bytes=1-2")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "el" {
		t.Error(test.DiffMessage(w.Code, http.StatusPartialContent, "ranges are served"))
	}
	first := serveGet(app, "/files/logo.png")
	r = httptest.NewRequest(http.MethodGet, "/files/logo.png", nil)
	r.Header.Set("If-Modified-Since", first.Header().Get("Last-Modified"))
	w = httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Error(test.DiffMessage(w.Code, http.StatusNotModified, "conditional requests are answered"))
	}
}

func TestServe_NameAnyDotfileAndSymlinkAre404(t *testing.T) {
	app := svApp(t)
	svWrite(t, filepath.Join(svRoot, ".env"), "SECRET=1")
	outside := filepath.Join(filepath.Dir(svRoot), "outside.txt")
	svWrite(t, outside, "SECRET=2")
	if err := os.Symlink(outside, filepath.Join(svRoot, "out.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	for _, target := range []string{"/static/.env", "/static/out.txt"} {
		w := createGet(app, target)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "SERVE_<x>_ANY hides dotfiles and symlinks out of the root: "+target))
		}
	}
}

func TestVersioning_ExactThenNeutral(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "2"), http.StatusOK, "v2", "V1: the route registered for the request's version")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "1"), http.StatusOK, "v1", "V1: another version of the same path")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/neutral", "X-Version", "7"), http.StatusOK, "neutral", "V1: with no route for the version, the neutral one")
}

func TestVersioning_UnknownVersionIs404(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "7"), http.StatusNotFound, wantNotFoundJSON, "V1: no route for the version and no neutral one")
}

func TestVersioning_NeutralAnswersWithoutVersion(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodGet, "/neutral")

	lcExpectResponse(t, w, http.StatusOK, "neutral", "bug 11, V2: a neutral route answers a request without a version")
}

func TestVersioning_NeutralAnswersWithDefaultVersion(t *testing.T) {
	app := verApp(t, "1")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/neutral"), http.StatusOK, "neutral", "V2: a neutral route answers with a default version")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver"), http.StatusOK, "v1", "V1: the default version selects the exact route")
}

func TestVersioning_UnversionedWithoutVersionIs410(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodGet, "/plain")

	lcExpectResponse(t, w, http.StatusGone, wantGoneJSON, "V3: a request without a version to a route registered without one answers 410")
	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "V3: the global middlewares run, no module layer and no filter")
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "V3: Content-Type"))
	}
}

func TestVersioning_UnversionedMiddlewareStopsThe410(t *testing.T) {
	app := verApp(t, "")
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ func()) {
		_, _ = w.Write([]byte("mine"))
	})
	w := lcServe(app, http.MethodGet, "/plain")

	lcExpectResponse(t, w, http.StatusOK, "mine", "V3, L10: a middleware that does not call next() replaces the 410")
}

func TestVersioning_UnversionedWithVersionIs404(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/plain", "X-Version", "1"), http.StatusNotFound, wantNotFoundJSON, "V3: with a version, a route registered without one is not found")
}

func TestVersioning_405UsesTheRequestVersion(t *testing.T) {
	app := verApp(t, "")

	w := lcServe(app, http.MethodDelete, "/ver", "X-Version", "1")
	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2, V1: DELETE on a versioned path answers 405 for that version")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, V1: Allow lists the methods of the request's version"))
	}

	lcExpectResponse(t, lcServe(app, http.MethodDelete, "/ver", "X-Version", "9"), http.StatusNotFound, wantNotFoundJSON, "N2, V1: a version with no route at all is a 404, not a 405")
}

func TestVersioning_405ForANeutralRoute(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodDelete, "/neutral", "X-Version", "9")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2, V1: the neutral route counts for the Allow of any version")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, V1: Allow lists the neutral route's methods"))
	}
}

func TestVersioning_HeadUsesTheVersionedGetRoute(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodHead, "/ver", "X-Version", "2")

	lcExpectResponse(t, w, http.StatusOK, "v2", "N3, V1: HEAD resolves the version like GET")
}

func TestErrorAfterResponse_Sampled(t *testing.T) {
	clock := installStackSeams(t)
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		c.Text("first")
		panic("late")
	})
	w := serveFresh(app, "/bare")
	serveFresh(app, "/bare")
	lcExpectResponse(t, w, http.StatusOK, "first", "S7: the response stays")
	entries := logger.named("ErrorAfterResponse")
	if len(entries) != 2 || !hasKey(entries[0].args, "stack") || hasKey(entries[1].args, "stack") {
		t.Fatal(test.DiffMessage(stackCount(entries), 1, "S7: ErrorAfterResponse is sampled like UnhandledPanic"))
	}
	want := []string{"trace_id", "request", "method", "path", "code", "panic", "stack"}
	if got := keysOf(entries[0].args); !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "S7: ErrorAfterResponse goes through From and no longer passes id"))
	}
	if len(clock.armed()) != 1 {
		t.Error(test.DiffMessage(len(clock.armed()), 1, "S5: the suppressed ErrorAfterResponse arms the timer"))
	}
}

func TestHTTP_AnyAfterParamServes(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(httpAnyAfterParamController{}).Build())

	w := createGet(app, "/files/a/b/c")
	if w.Code != 200 || w.Body.String() != "name=a" {
		t.Error(test.DiffMessage(w.Body.String(), "name=a", "B20: GET /files/a/b/c reaches READ_files_BY_name_ANY with name = a"))
	}
}
