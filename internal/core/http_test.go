package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/internal/tracecontext"
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

func TestServe_TraversalIsCleanedTo404(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(filepath.Dir(serveRoot), "files", "secret"), "do-not-leak")

	w := serveGet(app, "/files/a/../../files/secret")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "N3: the path is cleaned to /files/secret, which is not below the served directory: 404"))
	}
	if w.Body.String() != wantNotFoundJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "N3: the fixed 404"))
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
	app := New(nil)
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
		New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"testdata"}}}).Create(NewModuleBuilder().Controllers(svNameAnyController{}, svAnyOfController{}).Build())
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
	app := New(&AppOptions{Logger: &mockLogger{}})
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
		app := New(&AppOptions{Logger: quietLogger{}, HTTP: &HTTPOptions{MaxBodyBytes: 64 * MiB}})
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
		"/files/a/../../files/secret": http.StatusNotFound,
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

func TestServe_DotRootIsRefused(t *testing.T) {
	file, err := os.CreateTemp(".", "serve_dot_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	if _, err := file.WriteString("DOT-ROOT-CONTENT"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	logger := &lcRecordingLogger{}
	app := New(&AppOptions{Logger: logger, HTTP: &HTTPOptions{ServeDirs: []string{"testdata"}}})
	app.Create(NewModuleBuilder().Controllers(serveDotController{}).Build())

	w := serveGet(app, "/dot/"+strings.TrimPrefix(file.Name(), "./"))
	if w.Code != http.StatusInternalServerError || w.Body.String() != wantInternalErrorJSON || strings.Contains(w.Body.String(), "DOT-ROOT-CONTENT") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusInternalServerError, wantInternalErrorJSON}, "D1, D4: a handler that returns \".\" is refused, the working directory cannot be served"))
	}
	if len(logger.named("ServeRootRefused")) != 1 {
		t.Error(test.DiffMessage(len(logger.named("ServeRootRefused")), 1, "D4: the refusal is logged"))
	}
	msExpectPanic(t, "D1", "is the working directory or above it: serve a subdirectory", func() { New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"."}}}) })
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
	app := New(nil)
	mustCreate(t, app, NewModuleBuilder().Controllers(httpAnyAfterParamController{}).Build())

	w := createGet(app, "/files/a/b/c")
	if w.Code != 200 || w.Body.String() != "name=a" {
		t.Error(test.DiffMessage(w.Body.String(), "name=a", "B20: GET /files/a/b/c reaches READ_files_BY_name_ANY with name = a"))
	}
}

type preController struct{ common.HTTP }

func (c preController) NewController() Controller { return c }

func (c preController) READ_hello() string { return "hello" }

func (c preController) READ_big() string { return strings.Repeat("a", 4096) }

func (c preController) CREATE_echo(body ctx.Body) any { return body }

func preApp(t *testing.T, opts *AppOptions, mws ...common.MiddlewareFn) *App {
	t.Helper()
	if opts == nil {
		opts = &AppOptions{}
	}
	opts.Logger = &mockLogger{}
	app := New(opts)
	if len(mws) > 0 {
		app.BindGlobalMiddlewares(mws...)
	}
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	return app
}

func preDo(app *App, method, target string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

type preSpy struct {
	mu       sync.Mutex
	ip       string
	frame    string
	called   int
	override func(r *http.Request, w http.ResponseWriter, next ctx.Next)
}

func (s *preSpy) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	s.mu.Lock()
	s.called++
	s.ip = tracecontext.ClientIP(r).String()
	s.frame = w.Header().Get("X-Frame-Options")
	s.mu.Unlock()
	if s.override != nil {
		s.override(r, w, next)
		return
	}
	next()
}

func TestPreRoute_FixedOrder(t *testing.T) {
	spy := &preSpy{}
	app := preApp(t, &AppOptions{
		Proxy:    &ProxyOptions{TrustedProxies: []string{"192.0.2.1"}},
		Headers:  &HeadersOptions{IsEnabled: true},
		LoadShed: &LoadShedOptions{IsEnabled: true},
		Compress: &CompressOptions{IsEnabled: true, MinSize: 1},
	}, spy)

	w := preDo(app, http.MethodGet, "/big", map[string]string{"X-Forwarded-For": "203.0.113.9", "Accept-Encoding": "gzip"})

	if spy.ip != "203.0.113.9" {
		t.Error(test.DiffMessage(spy.ip, "203.0.113.9", "P1: proxy ran before the user middleware"))
	}
	if spy.frame != "DENY" {
		t.Error(test.DiffMessage(spy.frame, "DENY", "P1: headers ran before the user middleware"))
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error(test.DiffMessage(w.Header().Get("Content-Encoding"), "gzip", "P1: compress wraps the pipeline"))
	}
}

func TestPreRoute_FixedOrderIgnoresBindingOrder(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}, Headers: &HeadersOptions{IsEnabled: true}})
	w := preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com"})
	if w.Code != http.StatusMisdirectedRequest || w.Header().Get("X-Frame-Options") != "" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("X-Frame-Options")}, []any{421, ""}, "P1: hostcheck refuses before the headers stage runs"))
	}
	w = preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "a.com"})
	if w.Code != http.StatusOK || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("X-Frame-Options")}, []any{200, "DENY"}, "P1: headers runs after hostcheck passes"))
	}
}

func TestPreRoute_UserMiddlewareRefusalCarriesHeaders(t *testing.T) {
	spy := &preSpy{override: func(_ *http.Request, w http.ResponseWriter, _ ctx.Next) { w.WriteHeader(http.StatusTeapot) }}
	app := preApp(t, &AppOptions{Headers: &HeadersOptions{IsEnabled: true}}, spy)
	w := preDo(app, http.MethodGet, "/hello", nil)
	if w.Code != http.StatusTeapot || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("X-Frame-Options")}, []any{418, "DENY"}, "P7: a user middleware's own refusal carries the security headers"))
	}
}

func TestPreRoute_CompressCompressesUserMiddlewareOutput(t *testing.T) {
	spy := &preSpy{override: func(_ *http.Request, w http.ResponseWriter, _ ctx.Next) {
		_, _ = w.Write([]byte(strings.Repeat("a", 4096)))
	}}
	app := preApp(t, &AppOptions{Compress: &CompressOptions{IsEnabled: true, MinSize: 1}}, spy)
	w := preDo(app, http.MethodGet, "/hello", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error(test.DiffMessage(w.Header().Get("Content-Encoding"), "gzip", "P7: compress covers what user middlewares write"))
	}
}

func TestPreRoute_NoneConfiguredNoStage(t *testing.T) {
	app := preApp(t, nil)
	w := preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "anything.example", "Accept-Encoding": "gzip"})
	if w.Code != http.StatusOK || w.Header().Get("X-Frame-Options") != "" || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error(test.DiffMessage(w.Header(), "no stage header", "P1: nothing configured, no stage runs"))
	}
}

func TestPreRoute_RefusalBeats404(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}})
	w := preDo(app, http.MethodGet, "/nothing-here", map[string]string{"Host": "b.com"})
	if w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, 421, "P2: a stage's refusal comes before the 404"))
	}
}

func TestPreRoute_RefusalBeats405(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}})
	w := preDo(app, http.MethodDelete, "/hello", map[string]string{"Host": "b.com"})
	if w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, 421, "P2: a stage's refusal comes before the 405"))
	}
}

func TestPreRoute_RefusalBeatsGate503(t *testing.T) {
	release := make(chan struct{})
	factory := func(c context.Context) (*Module, error) {
		<-release
		return lfNamed("storage", NewModuleBuilder().Providers(lfSQL{})), nil
	}
	user := lfNamed("user", NewModuleBuilder().AsyncImports(factory).Controllers(lfSQLCtl{}))
	app := New(&AppOptions{Hosts: []string{"a.com"}, Logger: &mockLogger{}})
	lfRelease(t, app, release)
	lfCreate(t, app, NewModuleBuilder().Imports(user).Build())

	w := preDo(app, http.MethodGet, "/lfsql/hello", map[string]string{"Host": "b.com"})
	if w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, 421, "P2: a stage's refusal comes before the async gate's 503"))
	}
	w = preDo(app, http.MethodGet, "/lfsql/hello", map[string]string{"Host": "a.com"})
	if w.Code != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(w.Code, 503, "P2: an allowed request still meets the gate"))
	}
	close(release)
}

func TestPreRoute_RefusalIsJSONOrHTML(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}})
	w := preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com"})
	if !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "application/json", "P3: a refusal is the fixed early JSON error"))
	}
	w = preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com", "Accept": "text/html"})
	if !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "application/json", "P3: without Render the refusal stays JSON"))
	}
}

func TestPreRoute_LoadShedFromOptions(t *testing.T) {
	srvReset()
	defer srvCur.open()
	app := New(&AppOptions{Logger: &mockLogger{}, LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}})
	app.Create(NewModuleBuilder().Controllers(srvController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()

	res := srvFetchAsync(srvClient(), server.URL+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	r := srvFetch(srvClient(), http.MethodGet, server.URL+"/ok", "", false)
	if r.err != nil || r.status != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage([]any{r.status, r.err}, 503, "P3: LoadShed refuses over MaxInFlight"))
	}
	srvCur.open()
	<-res
	r = srvFetch(srvClient(), http.MethodGet, server.URL+"/ok", "", false)
	if r.status != http.StatusOK {
		t.Error(test.DiffMessage(r.status, 200, "P3: LoadShed passes under the cap"))
	}
}

func TestPreRoute_ProxyFromOptions(t *testing.T) {
	spy := &preSpy{}
	app := preApp(t, &AppOptions{Proxy: &ProxyOptions{TrustedProxies: []string{"192.0.2.1"}}}, spy)
	preDo(app, http.MethodGet, "/hello", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	if spy.ip != "203.0.113.9" {
		t.Error(test.DiffMessage(spy.ip, "203.0.113.9", "P3: a trusted peer's X-Forwarded-For is the client"))
	}
	app = preApp(t, &AppOptions{Proxy: &ProxyOptions{TrustedProxies: []string{"10.0.0.1"}}}, spy)
	preDo(app, http.MethodGet, "/hello", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	if spy.ip != "192.0.2.1" {
		t.Error(test.DiffMessage(spy.ip, "192.0.2.1", "P3: an untrusted peer is the client"))
	}
}

func TestPreRoute_HostCheckFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}})
	if w := preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "a.com"}); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, 200, "P3: a listed host passes"))
	}
	if w := preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com"}); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, 421, "P3: another host gets 421"))
	}
}

func TestPreRoute_HTTPSRedirectFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{HTTPSRedirect: &HTTPSRedirectOptions{IsEnabled: true}})
	w := preDo(app, http.MethodGet, "/hello?x=1", map[string]string{"Host": "a.com"})
	if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != "https://a.com/hello?x=1" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{308, "https://a.com/hello?x=1"}, "P3: plain HTTP is redirected"))
	}
	r := httptest.NewRequest(http.MethodGet, "https://a.com/hello", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Error(test.DiffMessage(rec.Code, 200, "P3: HTTPS passes"))
	}
}

func TestPreRoute_HeadersFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{Headers: &HeadersOptions{IsEnabled: true, FrameOptions: "SAMEORIGIN", ReferrerPolicy: "-"}})
	w := preDo(app, http.MethodGet, "/hello", nil)
	if w.Header().Get("X-Frame-Options") != "SAMEORIGIN" || w.Header().Get("Content-Security-Policy") == "" {
		t.Error(test.DiffMessage(w.Header(), "SAMEORIGIN and a CSP", "P3: headers from options"))
	}
	if _, ok := w.Header()["Referrer-Policy"]; ok {
		t.Error(test.DiffMessage(w.Header().Get("Referrer-Policy"), "omitted", `P3: "-" omits a header`))
	}
}

func TestPreRoute_CORSFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{Origins: []string{"https://a.com"}, CORS: &CORSOptions{IsEnabled: true}})
	w := preDo(app, http.MethodOptions, "/hello", map[string]string{"Origin": "https://a.com", "Access-Control-Request-Method": "GET"})
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://a.com" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Access-Control-Allow-Origin")}, []any{204, "https://a.com"}, "P3: a listed origin's preflight"))
	}
	w = preDo(app, http.MethodOptions, "/hello", map[string]string{"Origin": "https://evil.com", "Access-Control-Request-Method": "GET"})
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error(test.DiffMessage(w.Header().Get("Access-Control-Allow-Origin"), "", "P3: another origin gets no CORS header"))
	}
}

func TestPreRoute_CSRFFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{Origins: []string{"https://a.com"}, CSRF: &CSRFOptions{IsEnabled: true}})
	if w := preDo(app, http.MethodPost, "/echo", map[string]string{"Origin": "https://evil.com", "Content-Type": "application/json", "Host": "a.com"}); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, 403, "P3: a cross-site POST is refused"))
	}
	if w := preDo(app, http.MethodPost, "/echo", map[string]string{"Origin": "https://a.com", "Content-Type": "application/json", "Host": "other.com"}); w.Code == http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, "not 403", "P3: a listed origin passes"))
	}
	if w := preDo(app, http.MethodGet, "/hello", map[string]string{"Origin": "https://evil.com"}); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, 200, "P3: GET is safe"))
	}
}

func TestPreRoute_CompressFromOptions(t *testing.T) {
	app := preApp(t, &AppOptions{Compress: &CompressOptions{IsEnabled: true, MinSize: 1 * KiB}})
	w := preDo(app, http.MethodGet, "/big", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Error(test.DiffMessage(w.Header().Get("Content-Encoding"), "gzip", "P3: a body over MinSize is compressed"))
	}
	w = preDo(app, http.MethodGet, "/hello", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "" {
		t.Error(test.DiffMessage(w.Header().Get("Content-Encoding"), "", "P3: a body under MinSize is not"))
	}
}

func TestOrigins_SharedByCORSAndCSRF(t *testing.T) {
	app := preApp(t, &AppOptions{Origins: []string{"https://a.com"}, CORS: &CORSOptions{IsEnabled: true}, CSRF: &CSRFOptions{IsEnabled: true}})
	w := preDo(app, http.MethodOptions, "/echo", map[string]string{"Origin": "https://a.com", "Access-Control-Request-Method": "POST"})
	if w.Header().Get("Access-Control-Allow-Origin") != "https://a.com" {
		t.Error(test.DiffMessage(w.Header().Get("Access-Control-Allow-Origin"), "https://a.com", "P4: CORS reads Origins"))
	}
	if w := preDo(app, http.MethodPost, "/echo", map[string]string{"Origin": "https://a.com", "Host": "other.com", "Content-Type": "application/json"}); w.Code == http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, "not 403", "P4: CSRF reads the same Origins"))
	}
}

func TestCSRF_WildcardOrigin(t *testing.T) {
	app := preApp(t, &AppOptions{Origins: []string{"https://*.a.com"}, CSRF: &CSRFOptions{IsEnabled: true}})
	if w := preDo(app, http.MethodPost, "/echo", map[string]string{"Origin": "https://app.a.com", "Host": "api.a.com:81", "Content-Type": "application/json"}); w.Code == http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, "not 403", "P4: a wildcard entry passes CSRF"))
	}
	if w := preDo(app, http.MethodPost, "/echo", map[string]string{"Origin": "https://evil.com", "Host": "api.a.com:81", "Content-Type": "application/json"}); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, 403, "P4: another origin is refused"))
	}
}

func TestCSRF_ExactOriginEqualsTrustedOrigin(t *testing.T) {
	cop := http.NewCrossOriginProtection()
	if err := cop.AddTrustedOrigin("https://a.com"); err != nil {
		t.Fatal(err)
	}
	app := preApp(t, &AppOptions{Origins: []string{"https://a.com"}, CSRF: &CSRFOptions{IsEnabled: true}})
	origins := []string{"", "https://a.com", "https://b.com", "null", "http://a.com", "https://a.com:444"}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodGet} {
		for _, origin := range origins {
			r := httptest.NewRequest(method, "/echo", nil)
			r.Host = "site.com"
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			want := cop.Check(r) != nil
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			got := w.Code == http.StatusForbidden
			if got != want {
				t.Error(test.DiffMessage(got, want, "P4: "+method+" Origin "+origin+" equals AddTrustedOrigin"))
			}
		}
	}
}

func TestServeHTTP_MaxBodyBytesInOwnServer(t *testing.T) {
	app := preApp(t, &AppOptions{HTTP: &HTTPOptions{MaxBodyBytes: 1 * KiB}})
	server := httptest.NewServer(app)
	defer server.Close()
	post := func(n int) int {
		body := `{"k":"` + strings.Repeat("a", n) + `"}`
		resp, err := http.Post(server.URL+"/echo", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	if got := post(2048); got != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(got, 413, "S5: 2KiB over a 1KiB limit"))
	}
	if got := post(900); got != http.StatusCreated {
		t.Error(test.DiffMessage(got, 201, "S5: a body under the limit passes"))
	}
}

func TestServeHTTP_MaxBodyBytesDefault(t *testing.T) {
	app := preApp(t, nil)
	body := `{"k":"` + strings.Repeat("a", 10*1024*1024) + `"}`
	w := optServe(app, http.MethodPost, "/echo", body, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, 413, "S5: 0 means 10MiB"))
	}
}

func TestServeHTTP_MaxBodyBytesNegativeNoLimit(t *testing.T) {
	app := preApp(t, &AppOptions{HTTP: &HTTPOptions{MaxBodyBytes: -1}})
	body := `{"k":"` + strings.Repeat("a", 11*1024*1024) + `"}`
	w := optServe(app, http.MethodPost, "/echo", body, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, 201, "S5: negative means no limit"))
	}
}

func TestPreRoute_ConcurrentRequests(t *testing.T) {
	app := preApp(t, &AppOptions{Hosts: []string{"a.com"}, Headers: &HeadersOptions{IsEnabled: true}, LoadShed: &LoadShedOptions{IsEnabled: true}})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "a.com"})
				preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com"})
			}
		}()
	}
	wg.Wait()
}

func preTraceApp(t *testing.T, opts *AppOptions, mws ...common.MiddlewareFn) (*App, *traceRecorder) {
	t.Helper()
	if opts == nil {
		opts = &AppOptions{}
	}
	opts.Logger = &mockLogger{}
	app := New(opts)
	if len(mws) > 0 {
		app.BindGlobalMiddlewares(mws...)
	}
	rec := newTraceRecorder(app)
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	return app, rec
}

func preEventNames(events []trace.Event, stage string) []string {
	var out []string
	for _, e := range events {
		if e.Stage == stage {
			out = append(out, e.Name)
		}
	}
	return out
}

func TestTrace_PreRouteStageConstant(t *testing.T) {
	if trace.StagePreRoute != "pre-route" {
		t.Error(test.DiffMessage(trace.StagePreRoute, "pre-route", "T1: the stage constant"))
	}
}

func TestTrace_PreRouteEvents(t *testing.T) {
	app, rec := preTraceApp(t, &AppOptions{
		Origins:       []string{"https://a.com"},
		LoadShed:      &LoadShedOptions{IsEnabled: true},
		Proxy:         &ProxyOptions{TrustedHops: 1},
		Hosts:         []string{"example.com"},
		HTTPSRedirect: &HTTPSRedirectOptions{IsEnabled: true},
		Headers:       &HeadersOptions{IsEnabled: true},
		CORS:          &CORSOptions{IsEnabled: true},
		CSRF:          &CSRFOptions{IsEnabled: true},
		Compress:      &CompressOptions{IsEnabled: true},
	})
	r := httptest.NewRequest(http.MethodGet, "https://example.com/hello", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	app.ServeHTTP(httptest.NewRecorder(), r)
	rec.waitComplete(t, "/hello")

	events := traceStages(rec.all(), trace.StagePreRoute)
	got := preEventNames(events, trace.StagePreRoute)
	want := []string{"loadshed", "proxy", "hostcheck", "httpsredirect", "headers", "cors", "csrf", "compress"}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "T1: one event per stage, in order"))
	}
	for _, e := range events {
		if e.Transport != trace.TransportHTTP || e.Code != 0 {
			t.Error(test.DiffMessage([]any{e.Transport, e.Code}, []any{"HTTP", 0}, "T1: Transport HTTP, Code 0 when the stage passes ("+e.Name+")"))
		}
	}
}

func TestTrace_PreRouteOffStageEmitsNothing(t *testing.T) {
	app, rec := preTraceApp(t, &AppOptions{Headers: &HeadersOptions{IsEnabled: true}})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hello", nil))
	rec.waitComplete(t, "/hello")
	if got := preEventNames(rec.all(), trace.StagePreRoute); !slices.Equal(got, []string{"headers"}) {
		t.Error(test.DiffMessage(got, []string{"headers"}, "T4: a stage that is off emits nothing"))
	}
}

func TestTrace_RefusedRequestEndsAtStage(t *testing.T) {
	type row struct {
		name   string
		opts   *AppOptions
		method string
		target string
		header map[string]string
		code   int
	}
	rows := []row{
		{"hostcheck", &AppOptions{Hosts: []string{"a.com"}}, http.MethodGet, "http://b.com/hello", nil, 421},
		{"httpsredirect", &AppOptions{HTTPSRedirect: &HTTPSRedirectOptions{IsEnabled: true}}, http.MethodGet, "http://a.com/hello", nil, 308},
		{"cors", &AppOptions{Origins: []string{"https://a.com"}, CORS: &CORSOptions{IsEnabled: true}}, http.MethodOptions, "http://a.com/hello", map[string]string{"Origin": "https://a.com", "Access-Control-Request-Method": "GET"}, 204},
		{"csrf", &AppOptions{Origins: []string{"https://a.com"}, CSRF: &CSRFOptions{IsEnabled: true}}, http.MethodPost, "http://a.com/echo", map[string]string{"Origin": "https://evil.com"}, 403},
		{"hostcheck", &AppOptions{Hosts: []string{"a.com"}}, http.MethodGet, "http://a.com/hello", map[string]string{"Host": "bad host"}, 421},
		{"hostcheck", &AppOptions{Hosts: []string{"a.com"}}, http.MethodGet, "http://a.com/hello", map[string]string{"Host": strings.Repeat("a", 256)}, 400},
	}
	for _, c := range rows {
		spy := &preSpy{}
		app, rec := preTraceApp(t, c.opts, spy)
		r := httptest.NewRequest(c.method, c.target, nil)
		for k, v := range c.header {
			if k == "Host" {
				r.Host = v
				continue
			}
			r.Header.Set(k, v)
		}
		app.ServeHTTP(httptest.NewRecorder(), r)
		rec.waitComplete(t, r.URL.Path)

		events := rec.all()
		var stage *trace.Event
		for i := range events {
			if events[i].Stage == trace.StagePreRoute && events[i].Name == c.name {
				stage = &events[i]
			}
		}
		if stage == nil || stage.Code != c.code {
			t.Error(test.DiffMessage(stage, "a pre-route event with code "+strconv.Itoa(c.code), "T1/T2: "+c.name+" "+strconv.Itoa(c.code)))
		}
		last := events[len(events)-1]
		beforeLast := events[len(events)-2]
		if last.Stage != trace.StageComplete || beforeLast.Stage != trace.StagePreRoute {
			t.Error(test.DiffMessage([]string{beforeLast.Stage, last.Stage}, []string{"pre-route", "complete"}, "T2: the trace ends at the stage, then complete ("+c.name+")"))
		}
		if spy.called != 0 || len(preEventNames(events, trace.StageMiddleware))+len(preEventNames(events, trace.StageHandler))+len(preEventNames(events, trace.StageGuard)) != 0 {
			t.Error(test.DiffMessage(events, "no later event", "T2: nothing runs after a refusing stage ("+c.name+")"))
		}
	}
}

func TestTrace_LoadShedRefusalCode(t *testing.T) {
	srvReset()
	defer srvCur.open()
	app := New(&AppOptions{Logger: &mockLogger{}, LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}})
	rec := newTraceRecorder(app)
	app.Create(NewModuleBuilder().Controllers(srvController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()

	res := srvFetchAsync(srvClient(), server.URL+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	srvFetch(srvClient(), http.MethodGet, server.URL+"/ok", "", false)
	srvCur.open()
	<-res

	rec.waitComplete(t, "/ok")
	found := false
	for _, e := range traceStages(rec.all(), trace.StagePreRoute) {
		if e.Name == "loadshed" && e.Code == http.StatusServiceUnavailable {
			found = true
		}
	}
	if !found {
		t.Error(test.DiffMessage(rec.all(), "a loadshed event with code 503", "T1: a refusal's code"))
	}
}

func TestTrace_LoadShedDurationIncludesWait(t *testing.T) {
	srvReset()
	defer srvCur.open()
	app := New(&AppOptions{Logger: &mockLogger{}, LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: 4}})
	rec := newTraceRecorder(app)
	app.Create(NewModuleBuilder().Controllers(srvController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()

	res := srvFetchAsync(srvClient(), server.URL+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	waiter := srvFetchAsync(srvClient(), server.URL+"/ok")
	srvCur.open()
	<-res
	<-waiter

	rec.waitComplete(t, "/ok")
	var longest time.Duration
	for _, e := range traceStages(rec.all(), trace.StagePreRoute) {
		longest = max(longest, e.Duration)
	}
	if longest <= 0 {
		t.Error(test.DiffMessage(longest, ">0", "T1: loadshed's duration includes the queue wait"))
	}
}

func TestTrace_UserMiddlewareStillMiddleware(t *testing.T) {
	app, rec := preTraceApp(t, &AppOptions{Headers: &HeadersOptions{IsEnabled: true}}, &preSpy{})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hello", nil))
	rec.waitComplete(t, "/hello")
	events := rec.all()
	if got := preEventNames(events, trace.StageMiddleware); len(got) != 1 || strings.Contains(got[0], "elmet") || strings.Contains(got[0], "headers") {
		t.Error(test.DiffMessage(got, "only the user middleware", "T3: StageMiddleware is for user middlewares only"))
	}
}

func TestTrace_NoListenerNoEvent(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, Headers: &HeadersOptions{IsEnabled: true}})
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	if app.tracer.Enabled() {
		t.Fatal(test.DiffMessage(true, false, "T4: no listener, tracer off"))
	}
	w := preDo(app, http.MethodGet, "/hello", nil)
	if w.Code != http.StatusOK || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error(test.DiffMessage(w.Code, 200, "T4: the stage still works untraced"))
	}
}

func TestDevtool_PreRouteSnapshot(t *testing.T) {
	app := New(&AppOptions{
		Logger: &mockLogger{}, IsDevtool: true,
		Hosts:   []string{"a.com"},
		Headers: &HeadersOptions{IsEnabled: true},
	})
	app.BindGlobalMiddlewares(&preSpy{})
	app.Create(NewModuleBuilder().Controllers(preController{}).Build())
	preDo(app, http.MethodGet, "/hello", map[string]string{"Host": "b.com"})

	encoded, err := json.Marshal(app.devtool.GetConfigurationResponse)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	rows, _ := doc["pre_route"].([]any)
	var names []string
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			names = append(names, fmt.Sprint(m["name"]))
		}
	}
	if !slices.Equal(names, []string{"hostcheck", "headers"}) {
		t.Error(test.DiffMessage(names, []string{"hostcheck", "headers"}, "P8: pre_route lists the stages in order with their Snapshot"))
	}
	if strings.Contains(string(encoded), `"hostcheck"`) && strings.Count(string(encoded), `"hostcheck"`) > 1 {
		t.Error(test.DiffMessage(string(encoded), "hostcheck only in pre_route", "P8: middlewares no longer lists the stages"))
	}
}

func TestPreRoute_LoadShedReleasedOnEveryOutcome(t *testing.T) {
	spy := &preSpy{override: func(r *http.Request, w http.ResponseWriter, next ctx.Next) {
		switch r.Header.Get("X-Mode") {
		case "panic":
			panic("boom")
		case "refuse":
			panic(exception.ForbiddenException("refused by a later middleware"))
		case "abort":
			panic(http.ErrAbortHandler)
		case "stop":
			w.WriteHeader(http.StatusTeapot)
		default:
			next()
		}
	}}
	app := preApp(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}}, spy)
	check := func(desc string) {
		t.Helper()
		w := preDo(app, http.MethodGet, "/hello", nil)
		if w.Code != http.StatusOK || w.Body.String() != "hello" {
			t.Fatal(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{200, "hello"}, "L4: the slot was released after "+desc))
		}
	}
	check("nothing happened")
	check("the first check")

	preDo(app, http.MethodGet, "/hello", map[string]string{"X-Mode": "panic"})
	check("a handler panic")
	preDo(app, http.MethodGet, "/hello", map[string]string{"X-Mode": "refuse"})
	check("a refusal by a later middleware")
	preDo(app, http.MethodGet, "/hello", map[string]string{"X-Mode": "stop"})
	check("a middleware that answered itself")
	if w := preDo(app, http.MethodGet, "/missing", nil); w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "fixture: no route"))
	}
	check("a 404")
	func() {
		defer func() { _ = recover() }()
		preDo(app, http.MethodGet, "/hello", map[string]string{"X-Mode": "abort"})
	}()
	check("http.ErrAbortHandler")
}

func TestPreRoute_LoadShedBurst(t *testing.T) {
	const capacity = 2
	var current, peak atomic.Int64
	spy := &preSpy{override: func(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
		now := current.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		for i := 0; i < 5; i++ {
			runtime.Gosched()
		}
		current.Add(-1)
		next()
	}}
	app := preApp(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: capacity}}, spy)
	var wg sync.WaitGroup
	var ok, busy, other atomic.Int64
	for i := 0; i < 50*capacity; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := preDo(app, http.MethodGet, "/hello", nil)
			switch {
			case w.Code == http.StatusOK && w.Body.String() == "hello":
				ok.Add(1)
			case w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "Server is busy"):
				busy.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if other.Load() != 0 {
		t.Error(test.DiffMessage(other.Load(), int64(0), "L3: every response is 2xx or 503"))
	}
	if peak.Load() > capacity {
		t.Error(test.DiffMessage(peak.Load(), "<= 2", "L1: the count in flight never exceeds the maximum"))
	}
	if ok.Load() == 0 {
		t.Error(test.DiffMessage(ok.Load(), "> 0", "L2: the server keeps serving during the burst"))
	}
	if current.Load() != 0 {
		t.Error(test.DiffMessage(current.Load(), int64(0), "L4: the count in flight returns to 0"))
	}
	if w := preDo(app, http.MethodGet, "/hello", nil); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "L4: after the burst every slot is free"))
	}
}

func TestPreRoute_LoadShedRefusalIsHTTPLayerResponse(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	spy := &preSpy{override: func(r *http.Request, _ http.ResponseWriter, next ctx.Next) {
		if r.Header.Get("X-Hold") != "" {
			entered <- struct{}{}
			<-release
		}
		next()
	}}
	app := preApp(t, &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}}, spy)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- preDo(app, http.MethodGet, "/hello", map[string]string{"X-Hold": "1"}) }()
	<-entered
	w := preDo(app, http.MethodGet, "/hello", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(w.Code, http.StatusServiceUnavailable, "G1: a request over the limit: status"))
	}
	if want := `{"code":503,"error":"Service Unavailable","message":"Server is busy"}`; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "G1: a request over the limit: body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "G1: a request over the limit: Content-Type"))
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "L3: Retry-After: 1"))
	}
	close(release)
	if held := <-done; held.Code != http.StatusOK {
		t.Error(test.DiffMessage(held.Code, http.StatusOK, "the held request completes"))
	}
}

type preWhoController struct{ common.HTTP }

func (c preWhoController) NewController() Controller { return c }

func (c preWhoController) READ_who(hc *ctx.HTTPContext) string {
	return tracecontext.ClientIP(hc.Request).String() + "|" + preScheme(hc.Request) + "|" + tracecontext.Host(hc.Request)
}

func preScheme(r *http.Request) string {
	if tracecontext.IsHTTPS(r) {
		return "https"
	}
	return "http"
}

func preWho(app *App, remoteAddr string, header map[string]string) string {
	r := httptest.NewRequest(http.MethodGet, "/who", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w.Body.String()
}

func TestPreRoute_ProxyHandlerReadsClient(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, Proxy: &ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}})
	app.Create(NewModuleBuilder().Controllers(preWhoController{}).Build())

	got := preWho(app, "10.0.0.7:5000", map[string]string{
		"X-Forwarded-For":   "198.51.100.20, 10.0.0.2",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "app.example.com",
	})
	if got != "198.51.100.20|https|app.example.com" {
		t.Error(test.DiffMessage(got, "198.51.100.20|https|app.example.com", "P4, P8, P9: a handler reads the resolved client through the app"))
	}
	got = preWho(app, "203.0.113.5:5000", map[string]string{"X-Forwarded-For": "198.51.100.20"})
	if got != "203.0.113.5|http|example.com" {
		t.Error(test.DiffMessage(got, "203.0.113.5|http|example.com", "P3: an untrusted peer resolves to the connection through the app"))
	}
}

func TestPreRoute_ProxyResolutionDoesNotLeakToNextRequest(t *testing.T) {
	app := New(&AppOptions{Logger: &mockLogger{}, Proxy: &ProxyOptions{TrustedProxies: []string{"10.0.0.0/8"}}})
	app.Create(NewModuleBuilder().Controllers(preWhoController{}).Build())

	preWho(app, "10.0.0.7:5000", map[string]string{"X-Forwarded-For": "198.51.100.20", "X-Forwarded-Proto": "https"})
	if got := preWho(app, "10.0.0.7:5000", nil); got != "10.0.0.7|http|example.com" {
		t.Error(test.DiffMessage(got, "10.0.0.7|http|example.com", "P12: a request without headers resolves to the peer, nothing is left from the previous one"))
	}
}

func TestRedirect_SameOrigin(t *testing.T) {
	app, logger := secRedirectApp(t)
	for _, route := range []struct {
		path string
		want int
	}{{"/land", http.StatusSeeOther}, {"/go", http.StatusMovedPermanently}} {
		for _, target := range []string{"/", "/bookings", "/a?next=/b#top", "/a?u=https://x.test"} {
			w := createGet(app, route.path+"?to="+url.QueryEscape(target))
			if w.Code != route.want || w.Header().Get("Location") != target {
				t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{route.want, target}, "R1, R3: accepted "+target+" on "+route.path))
			}
		}
		before := len(logger.named("RedirectRefused"))
		refused := []string{"", "https://evil.test", "//evil.test", "/\\evil.test", "\\\\evil.test", "/a\\b", "javascript:alert(1)", "evil.test", "bookings", "/a\r\nSet-Cookie: x=1", "/\t/evil.test"}
		for i, target := range refused {
			w := createGet(app, route.path+"?to="+url.QueryEscape(target))
			if w.Code != route.want || w.Header().Get("Location") != "/" || w.Header().Get("Set-Cookie") != "" {
				t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{route.want, "/"}, "R4: refused "+strconv.Quote(target)+" on "+route.path))
			}
			if got := len(logger.named("RedirectRefused")); got != before+i+1 {
				t.Error(test.DiffMessage(got, before+i+1, "R4: one RedirectRefused line per refusal: "+strconv.Quote(target)))
			}
		}
	}
	refusals := logger.named("RedirectRefused")
	if len(refusals) == 0 {
		t.Fatal(test.DiffMessage(0, "RedirectRefused lines", "R4: refusals are logged"))
	}
	entry := refusals[0]
	if entry.level != "warn" || lcText(entry.field("method")) != "GET" || lcText(entry.field("path")) != "/land" || lcText(entry.field("target")) != "" ||
		lcText(entry.field("note")) != "c.Redirect accepts a path of this site; another site needs c.RedirectExternal" {
		t.Error(test.DiffMessage(entry, "Warn with method, path, target and note", "R4: the line"))
	}

	long := "https://evil.test/" + strings.Repeat("a", 400)
	createGet(app, "/go?to="+url.QueryEscape(long))
	last := logger.named("RedirectRefused")
	if len(last) == 0 {
		t.Fatal(test.DiffMessage(0, "RedirectRefused lines", "R4: the long target is logged"))
	}
	if got := lcText(last[len(last)-1].field("target")); got != long[:256] {
		t.Error(test.DiffMessage(len(got), 256, "R4: the target is cut at 256 bytes"))
	}
}

func TestRedirect_External(t *testing.T) {
	app, logger := secRedirectApp(t)
	for _, target := range []string{"https://example.com", "http://example.com/cb?x=1", "HTTPS://Example.com/P", "/local"} {
		if w := createGet(app, "/external?to="+url.QueryEscape(target)); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != target {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusMovedPermanently, target}, "R2: accepted "+target))
		}
	}
	for _, target := range []string{"javascript:alert(1)", "data:text/html,x", "//evil.test", "https://user@evil.test", "https:///path"} {
		if w := createGet(app, "/external?to="+url.QueryEscape(target)); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusMovedPermanently, "/"}, "R2, R4: refused "+target))
		}
	}
	entries := logger.named("RedirectRefused")
	if len(entries) != 5 || lcText(entries[0].field("note")) != "c.RedirectExternal accepts a path or an http(s) URL" {
		t.Error(test.DiffMessage(len(entries), 5, "R4: the RedirectExternal note"))
	}
}

func TestRedirect_Status(t *testing.T) {
	app, _ := secRedirectApp(t)
	if w := createGet(app, "/land?to=/x"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R3: a page route redirects with 303"))
	}
	if w := createGet(app, "/go?to=/x"); w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "R3: an API route redirects with 301"))
	}
	status := New(nil)
	mustCreate(t, status, NewModuleBuilder().Controllers(secStatusRedirect{}).Build())
	if w := createGet(status, "/found"); w.Code != http.StatusFound || w.Header().Get("Location") != "/x" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusFound, "/x"}, "R3: c.Status(302) is kept"))
	}
	if w := createGet(status, "/found?bad=1"); w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusFound, "/"}, "R4: a refused target keeps the status"))
	}
}

func TestRedirect_InjectedFunc(t *testing.T) {
	app, logger := secRedirectApp(t)
	if w := createGet(app, "/injected?to=/ok"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/ok" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusMovedPermanently, "/ok"}, "R1: the injected owlkei.Redirect accepts a path"))
	}
	for _, target := range []string{"https://evil.test", "//evil.test", "evil.test"} {
		if w := createGet(app, "/injected?to="+url.QueryEscape(target)); w.Header().Get("Location") != "/" {
			t.Error(test.DiffMessage(w.Header().Get("Location"), "/", "R1: the injected func follows R1: "+target))
		}
	}
	if len(logger.named("RedirectRefused")) != 3 {
		t.Error(test.DiffMessage(len(logger.named("RedirectRefused")), 3, "R4: the injected func logs its refusals"))
	}
}

func TestRedirect_NoHeaderInjection(t *testing.T) {
	app, _ := secRedirectApp(t)
	srv := httptest.NewServer(app)
	defer srv.Close()
	resp, _ := secRawGet(t, srv.Listener.Addr().String(), "/go?to=%2Fa%0d%0aSet-Cookie:%20x=1")
	if resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("Location") != "/" {
		t.Error(test.DiffMessage([]any{resp.Header.Get("Set-Cookie"), resp.Header.Get("Location")}, []any{"", "/"}, "R4, H1: a CR LF target never reaches a header"))
	}
	if len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Error(test.DiffMessage(resp.Header, "no Set-Cookie", "H1: no injected header"))
	}
}
