package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/trace"
	"go.owlky.com/owlky/internal/versioning"
)

func TestPage_RoutesLikeHTTP(t *testing.T) {
	files := pgFiles(map[string]string{"v9/site/home.html": "HOME"})
	logger := &lcRecordingLogger{}
	app := New(&AppOptions{Logger: logger, Render: &RenderOptions{FS: files, Dir: "."}})
	inner := NewModuleBuilder().Controllers(pgPrefixed{}).Build()
	inner.Prefix("v9")
	mustCreate(t, app, NewModuleBuilder().Imports(inner).Build())

	w := createGet(app, "/v9/site/home")
	if w.Code != http.StatusOK || w.Body.String() != "HOME" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P1, P2: GET under module prefix and controller Prefix, "+w.Body.String()))
	}
	w = createRequest(app, http.MethodPost, "/v9/site/home")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/done" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: the SUBMIT_ of the page posts under the same prefixes"))
	}
	if w := createGet(app, "/site/home"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P1: the module prefix applies"))
	}
}

func TestPage_LayersBind(t *testing.T) {
	files := pgFiles(map[string]string{"layered.html": "word={{.Word}}"})
	app, _ := pgNew(t, files, pgLayered{})

	if w := createRequest(app, http.MethodGet, "/layered?w=a"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P1: a guard returning false refuses a page route"))
	}
	w := createRequest(app, http.MethodGet, "/layered?w=a", "X-Allow", "yes")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "P1: the guard lets the request through"))
	}
	if w.Header().Get("X-Page-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Header().Get("X-Page-Middleware"), "ran", "P1: a controller middleware runs on a page route"))
	}
	if want := "word=intercepted"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P1, P6: the interceptor's result is the template's data"))
	}

	piped, _ := pgNew(t, pgFiles(map[string]string{"piped.html": "word={{.Word}}"}), pgPipeOnly{})
	if w := createGet(piped, "/piped?w=a"); w.Body.String() != "word=piped-a" {
		t.Error(test.DiffMessage(w.Body.String(), "word=piped-a", "P1: a pipe on a page route"))
	}
}

func TestPage_RendersInsideLayout(t *testing.T) {
	files := pgFiles(map[string]string{
		"layout.html":        `<html><body>{{template "content" .}}</body></html>`,
		"bookings/list.html": `<ul><li>{{.X}}</li></ul>`,
	})
	app, _ := pgNew(t, files, pgBookings{})
	w := createGet(app, "/bookings/list?x=ab")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R5: status of a page"))
	}
	if want := "<html><body><ul><li>ab</li></ul></body></html>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R5: the page file is the content of the layout"))
	}
}

func TestPage_NearestLayoutWins(t *testing.T) {
	files := pgFiles(map[string]string{
		"layout.html":            `[root:{{template "content" .}}]`,
		"admin/layout.html":      `[admin:{{template "content" .}}]`,
		"home.html":              `home`,
		"admin/dash.html":        `dash`,
		"admin/deep/inner.html":  `inner`,
		"info.html":              `info`,
		"admin/deep/.hidden.txt": `ignored`,
	})
	app, _ := pgNew(t, files, pgNested{})
	cases := map[string]string{
		"/home":             "[root:home]",
		"/admin/dash":       "[admin:dash]",
		"/admin/deep/inner": "[admin:inner]",
		"/info":             "[root:info]",
	}
	for target, want := range cases {
		if got := createGet(app, target).Body.String(); got != want {
			t.Error(test.DiffMessage(got, want, "R5: the nearest layout of "+target))
		}
	}
}

func TestPage_NoLayout(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"plain.html": "just {{.X}}"}), pgPages{})
	w := createGet(app, pgView("plain", "me"))
	if w.Code != http.StatusOK || w.Body.String() != "just me" {
		t.Error(test.DiffMessage(w.Body.String(), "just me", "R5: without a layout the page is rendered alone"))
	}
	app, _ = pgNew(t, pgPageFiles(map[string]string{"nodata.html": "no data here"}), pgPages{})
	if w := createGet(app, "/nodata"); w.Code != http.StatusOK || w.Body.String() != "no data here" {
		t.Error(test.DiffMessage(w.Body.String(), "no data here", "P6: a handler without a result renders"))
	}
}

func TestPage_BlockOverride(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"layout.html": `<title>{{block "title" .}}Default{{end}}</title><main>{{template "content" .}}</main>`,
		"custom.html": `{{define "title"}}Custom {{.X}}{{end}}body-custom`,
		"plain.html":  `body-plain`,
	})
	app, _ := pgNew(t, files, pgPages{})
	if got, want := createGet(app, pgView("custom", "T")).Body.String(), "<title>Custom T</title><main>body-custom</main>"; got != want {
		t.Error(test.DiffMessage(got, want, "R5: a define replaces the layout's block"))
	}
	if got, want := createGet(app, pgView("plain", "")).Body.String(), "<title>Default</title><main>body-plain</main>"; got != want {
		t.Error(test.DiffMessage(got, want, "R5: a page without define keeps the block's default"))
	}
}

func TestPage_Headers(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"nodata.html": "héllo"}), pgPages{})
	w := createGet(app, "/nodata")
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/html; charset=utf-8", "R5: Content-Type"))
	}
	if got, want := w.Header().Get("Content-Length"), strconv.Itoa(len("héllo")); got != want {
		t.Error(test.DiffMessage(got, want, "R5: Content-Length"))
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Error(test.DiffMessage(got, "no-cache", "R5: Cache-Control default"))
	}
}

func TestPage_KeepsHandlerCacheControl(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(nil), pgPages{})
	if got := createGet(app, "/cached").Header().Get("Cache-Control"); got != "max-age=60" {
		t.Error(test.DiffMessage(got, "max-age=60", "R5: a Cache-Control set by the handler is kept"))
	}
}

func TestPage_DefaultStatus200(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(nil), pgPages{})
	if w := createGet(app, pgView("view", "")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P7: GET page status"))
	}
}

func TestPage_StatusFromContext(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"statusctx.html": "form"}), pgPages{})
	w := createGet(app, "/statusctx")
	if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "form" {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P7: c.Status on a RENDER_"))
	}
}

func TestPage_StatusFromResult(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"statusresult.html": "form {{.Word}}"}), pgPages{})
	w := createGet(app, "/statusresult")
	if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "form result" {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P7: (int, data) on a RENDER_, "+w.Body.String()))
	}
}

func TestPage_EscapesByContext(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"esc.html": `<p>{{.X}}</p><i title="{{.X}}"></i><a href="/q?v={{.X}}">l</a><script>var a = {{.X}};</script>`,
		"url.html": `<a href="{{.X}}">l</a>`,
	})
	app, _ := pgNew(t, files, pgPages{})
	hostile := `"><script>alert(1)</script>`
	body := createGet(app, pgView("esc", hostile)).Body.String()
	if strings.Contains(body, "<script>alert(1)") {
		t.Error(test.DiffMessage(body, "no raw script from data", "R7: hostile data stays inert"))
	}
	if !strings.Contains(body, "<p>&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;</p>") {
		t.Error(test.DiffMessage(body, "text node escaped", "R7: text context"))
	}
	if !strings.Contains(body, `title="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`) {
		t.Error(test.DiffMessage(body, "attribute escaped", "R7: attribute context"))
	}
	if !strings.Contains(body, `/q?v=%22%3e%3cscript%3ealert%281%29%3c%2fscript%3e`) {
		t.Error(test.DiffMessage(body, "URL escaped", "R7: URL context"))
	}
	if !strings.Contains(body, `<script>var a = "\"\u003e\u003cscript\u003ealert(1)\u003c/script\u003e";</script>`) {
		t.Error(test.DiffMessage(body, "script string escaped", "R7: script context"))
	}
	if got := createGet(app, pgView("url", "javascript:alert(1)")).Body.String(); strings.Contains(got, "javascript:") {
		t.Error(test.DiffMessage(got, "#ZgotmplZ", "R7: a javascript: URL is refused"))
	}
}

func TestPage_ResultIgnoredAfterResponse(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"redirect.html": "PAGEMARKER", "direct.html": "PAGEMARKER"}), pgPages{})
	w := createGet(app, "/redirect")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/elsewhere" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R8, P14: the redirect stands"))
	}
	if strings.Contains(w.Body.String(), "PAGEMARKER") {
		t.Error(test.DiffMessage(w.Body.String(), "no page body", "R8: the template after Redirect is not rendered"))
	}
	w = createGet(app, "/direct")
	if w.Body.String() != "direct write" || w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Body.String(), "direct write", "R8: the template after a direct write is not rendered"))
	}
}

func TestPage_RequestOpensNoFile(t *testing.T) {
	counted := &pgCountFS{inner: pgPageFiles(map[string]string{
		"layout.html":         `{{template "content" .}}`,
		"partials/field.html": `field`,
		"404.html":            `E{{.Code}}`,
		"view.html":           `real`,
	})}
	app, _ := pgNew(t, counted, pgPages{})
	counted.opens.Store(0)

	createGet(app, "/view")
	createRequest(app, http.MethodGet, "/missing", "Accept", "text/html")
	createGet(app, "/nodata")
	if n := counted.opens.Load(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "P2, P17: the templates are bound at Create; a request, a page or an error page, opens no file"))
	}
}

func TestPage_ExecErrorSendsNoPartialPage(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"layout.html": `<html>LAYOUTMARKER{{template "content" .}}</html>`,
		"rows.html":   `{{range .Rows}}<i>PAGEMARKER-0123456789</i>{{end}}{{.Nope}}`,
	})
	app, logger := pgNew(t, files, pgPages{})
	w := createGet(app, "/rows")
	pgExpectHTMLFixed500(t, "R10", w)
	for _, marker := range []string{"PAGEMARKER", "LAYOUTMARKER", "Nope"} {
		if strings.Contains(w.Body.String(), marker) {
			t.Error(test.DiffMessage(w.Body.String(), "no "+marker, "R10: no fragment of the failed page or its error"))
		}
	}
	entries := logger.named("RenderFailed")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "R10: one RenderFailed"))
	}
	if got := entries[0].field("template"); got != "rows" {
		t.Error(test.DiffMessage(got, "rows", "R10, R41: the template field is the template path without .html"))
	}
	if entries[0].level != "error" || entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(entries[0].level, "error with a cause", "R10: RenderFailed is an Error with its cause"))
	}
}

func TestPage_OversizedPageIs500(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"big.html": `{{range .Rows}}` + strings.Repeat("0123456789", 10) + `{{end}}`,
	})
	app, logger := pgNew(t, files, pgPages{})

	w := createGet(app, "/big?n=90000")
	pgExpectHTMLFixed500(t, "R11: 9,000,000 bytes", w)
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R11: the oversize page logs RenderFailed"))
	}
	if len(w.Body.String()) > 100000 {
		t.Error(test.DiffMessage(len(w.Body.String()), "a small error page", "R11: nothing of the oversized page is sent"))
	}

	w = createGet(app, "/big?n=80000")
	if w.Code != http.StatusOK || len(w.Body.String()) != 8000000 {
		t.Error(test.DiffMessage(len(w.Body.String()), 8000000, "R11: a page under 8 MiB renders"))
	}
}

func TestPage_UnknownAssetFails(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"assets/app.css": "body{}",
		"view.html":      `{{if .X}}<link href="{{asset "missing.css"}}">{{end}}ok`,
	})
	app, logger := pgNew(t, files, pgPages{})
	if w := createGet(app, pgView("view", "")); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Body.String(), "ok", "R12: the unused branch renders"))
	}
	w := createGet(app, pgView("view", "show"))
	pgExpectHTMLFixed500(t, "R12: an unknown asset", w)
	if strings.Contains(w.Body.String(), "missing.css") {
		t.Error(test.DiffMessage(w.Body.String(), "no asset name", "R12: the cause is not sent"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R12: RenderFailed is logged"))
	}
}

func TestPage_AssetFunction(t *testing.T) {
	css := "body{color:red}"
	sum := sha256.Sum256([]byte(css))
	want := "/assets/css/app.css?v=" + hex.EncodeToString(sum[:8])
	files := pgPageFiles(map[string]string{
		"assets/css/app.css": css,
		"view.html":          `{{asset "css/app.css"}}`,
	})
	app, _ := pgNew(t, files, pgPages{})
	if got := createGet(app, pgView("view", "")).Body.String(); got != want {
		t.Error(test.DiffMessage(got, want, "D6: asset returns the path with the first 8 bytes of the SHA-256"))
	}

	reload := New(&AppOptions{Render: &RenderOptions{FS: files, Dir: ".", IsReload: true}})
	mustCreate(t, reload, NewModuleBuilder().Controllers(pgPages{}).Build())
	if got, want := createGet(reload, pgView("view", "")).Body.String(), "/assets/css/app.css"; got != want {
		t.Error(test.DiffMessage(got, want, "D6: with IsReload asset adds no version"))
	}
}

func TestPage_DictFunction(t *testing.T) {
	files := pgPageFiles(map[string]string{
		"partials/field.html": `<label>{{.Name}}={{.Value}}</label>`,
		"dict.html":           `{{template "partials/field" (dict "Name" "email" "Value" .Value)}}`,
	})
	app, _ := pgNew(t, files, pgPages{})
	w := createGet(app, "/dict?v=a%3Cb")
	if want := "<label>email=a&lt;b</label>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "D6: dict passes several values to a partial, escaped"))
	}

	bad := pgPageFiles(map[string]string{
		"partials/field.html": `x`,
		"dict.html":           `{{template "partials/field" (dict "Name" "email" "Value")}}`,
	})
	app, logger := pgNew(t, bad, pgPages{})
	w = createGet(app, "/dict")
	pgExpectHTMLFixed500(t, "D6: odd dict arguments", w)
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R10: a failed dict is RenderFailed"))
	}

	badKey := pgPageFiles(map[string]string{
		"partials/field.html": `x`,
		"dict.html":           `{{template "partials/field" (dict 1 "a")}}`,
	})
	app, _ = pgNew(t, badKey, pgPages{})
	w = createGet(app, "/dict")
	pgExpectHTMLFixed500(t, "D6: non-string dict key", w)
}

func TestPage_InterceptorResult(t *testing.T) {
	files := pgFiles(map[string]string{"replaced.html": "data={{.}}", "short.html": "word={{.Word}}"})
	app, _ := pgNew(t, files, pgReplaced{}, pgShort{})

	w := createGet(app, "/replaced")
	if w.Code != http.StatusOK || w.Body.String() != "data=replaced" {
		t.Error(test.DiffMessage(w.Body.String(), "data=replaced", "P6: a post-interceptor's replacement, of any type, is the data"))
	}

	pgHandlerCalls.Store(0)
	w = createGet(app, "/short")
	if w.Code != http.StatusOK || w.Body.String() != "word=short-circuit" {
		t.Error(test.DiffMessage(w.Body.String(), "word=short-circuit", "P6: an interceptor that answers without the handler supplies the data"))
	}
	if n := pgHandlerCalls.Load(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "P6: the handler was not called"))
	}
}

func TestPage_Head(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"nodata.html": "body text"}), pgPages{})
	w := createRequest(app, http.MethodHead, "/nodata")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R15: HEAD status"))
	}
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "R15: HEAD has no body"))
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/html; charset=utf-8", "R15: HEAD headers are the GET headers"))
	}
	status := createRequest(app, http.MethodHead, "/statusctx")
	if status.Code != http.StatusUnprocessableEntity {
		t.Error(test.DiffMessage(status.Code, http.StatusUnprocessableEntity, "R15: HEAD answers the page's status"))
	}
}

func TestCreate_RenderWithoutRenderFSPanics(t *testing.T) {
	msExpectPanic(t, "P3", "invalid render: cpPageController.RENDER_x is a page route, set Render.FS in AppOptions",
		cpCreate(nil, cpPageController{}))
	msExpectPanic(t, "P3", "invalid render: cpRenderElsewhere.RENDER_x is a page route, set Render.FS in AppOptions",
		cpCreate(nil, cpRenderElsewhere{}))
}

func TestCreate_RenderResultMayBeAnyType(t *testing.T) {
	files := map[string]string{"x.html": "x", "s.html": "s", "n.html": "n", "i.html": "i", "e.html": "e"}
	if text, panicked := createPanicText(cpCreateWith(files, cpAnyResultController{})); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P6: a page handler may return any type or nothing; a SERVE handler on the same controller is accepted"))
	}
}

func TestCreate_RenderMissingTemplatePanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPageController.RENDER_x needs a template: x.html or x/index.html",
		cpCreateFiles(map[string]string{"layout.html": `{{template "content" .}}`}))
	msExpectPanic(t, "P4", "invalid render: cpBookingParam.RENDER_bookings_BY_id needs a template: bookings/{id}.html or bookings/{id}/index.html",
		cpCreateWith(map[string]string{"bookings/{slug}.html": "x"}, cpBookingParam{}))
	msExpectPanic(t, "P4", "invalid render: cpAdmin404.RENDER_404 needs a template: admin/404.html or admin/404/index.html",
		cpCreateWith(map[string]string{"layout.html": `{{template "content" .}}`}, cpAdmin404{}))
}

func TestCreate_RenderTwoTemplatesPanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPageController.RENDER_x has two templates: x.html and x/index.html",
		cpCreateFiles(map[string]string{"x.html": "a", "x/index.html": "b"}))
}

func TestCreate_RenderReservedDirPanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPartialsPrefix.RENDER_app can have no template: partials/ is reserved",
		cpCreateWith(map[string]string{"partials/app.html": "x"}, cpPartialsPrefix{}))
	msExpectPanic(t, "P4", "invalid render: cpAssetsPrefix.RENDER_app can have no template: assets/ is reserved",
		cpCreateWith(map[string]string{"assets/app.html": "x"}, cpAssetsPrefix{}))
	msExpectPanic(t, "P4", "invalid render: cpPublicPath.RENDER_x can have no template: public/ is reserved",
		cpCreateWith(map[string]string{"public/app.html": "x"}, cpPublicPath{}))
}

func TestCreate_PageRouteWordsPanic(t *testing.T) {
	msExpectPanic(t, "P5", "invalid route: ANY in RENDER_files_ANY: a page route has no wildcard",
		cpCreateWith(map[string]string{"files.html": "x"}, cpAny{}))
	msExpectPanic(t, "P5", "invalid route: VERSION in RENDER_x_VERSION_1: a page route has no version",
		cpCreateWith(map[string]string{"x.html": "x"}, cpVersion{}))
	msExpectPanic(t, "P5", "invalid route: FILE in RENDER_feed_xml_FILE: a page route has no file extension",
		cpCreateWith(map[string]string{"feed.xml.html": "x"}, cpFile{}))
	msExpectPanic(t, "P5", "invalid route: ANY in SUBMIT_x_ANY: a page route has no wildcard",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitAny{}))
	msExpectPanic(t, "P5", "invalid route: VERSION in SUBMIT_x_VERSION_1: a page route has no version",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitVersion{}))
	msExpectPanic(t, "P5", "invalid route: FILE in SUBMIT_x_FILE: a page route has no file extension",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitFile{}))
}

func TestCreate_PageRouteWildcardPathPanics(t *testing.T) {
	files := map[string]string{"x.html": "x", "docs/x.html": "x"}
	msExpectPanic(t, "P5", "invalid route: cpWildcardPath.RENDER_x is a page route and its path", cpCreateWith(files, cpWildcardPath{}))
	msExpectPanic(t, "P5", "holds a wildcard", cpCreateWith(files, cpWildcardPath{}))
	msExpectPanic(t, "P5", "invalid route: cpWildcardPrefix.RENDER_x is a page route and its path", cpCreateWith(files, cpWildcardPrefix{}))
}

func TestCreate_UnboundTemplatePanics(t *testing.T) {
	msExpectPanic(t, "P9", "invalid render: bookings/detial.html has no RENDER_ route: add one, move it to partials/, or delete it",
		cpCreateFiles(map[string]string{"x.html": "x", "bookings/detail.html": "d", "bookings/detial.html": "d"}))
	msExpectPanic(t, "P9", "invalid render: error.html has no RENDER_ route: add one, move it to partials/, or delete it",
		cpCreateFiles(map[string]string{"x.html": "x", "error.html": "{{.Code}}"}))
	msExpectPanic(t, "P9", "has no RENDER_ route",
		cpCreateFiles(map[string]string{"x.html": "x", "layout.html": `{{template "content" .}}`, "admin/layout.html": `{{template "content" .}}`, "admin/stray.html": "s"}))

	files := map[string]string{
		"x.html":             "x",
		"layout.html":        `{{template "content" .}}`,
		"partials/p.html":    "p",
		"404.html":           "e",
		"4xx.html":           "e",
		"assets/page.html":   "{{ not parsed",
		"public/index.html":  "{{ not parsed",
		"public/docs/a.html": "{{ not parsed",
		"assets/app.css":     "a{}",
	}
	if text, panicked := createPanicText(cpCreateFiles(files)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P9: layouts, partials, error pages and files under assets and public are not strays"))
	}
}

func TestCreate_RenderReservedNamePanics(t *testing.T) {
	msExpectPanic(t, "P20", "invalid render: cpR404.RENDER_404 resolves to 404.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"404.html": "x"}, cpR404{}))
	msExpectPanic(t, "P20", "invalid render: cpR5xx.RENDER_5xx resolves to 5xx.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"5xx.html": "x"}, cpR5xx{}))
	msExpectPanic(t, "P20", "invalid render: cpPath404.RENDER_x resolves to 404.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"404.html": "x"}, cpPath404{}))

	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"admin/404.html": "ADMIN-404"}), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(cpAdmin404{}).Build())
	if w := createGet(app, "/admin/404"); w.Code != http.StatusOK || w.Body.String() != "ADMIN-404" {
		t.Error(test.DiffMessage(w.Body.String(), "ADMIN-404", "P20: under a prefix the name is an ordinary page"))
	}

	prefixed := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"site/404.html": "SITE-404"}), Dir: "."}})
	module := NewModuleBuilder().Controllers(cpR404{}).Build()
	module.Prefix("site")
	mustCreate(t, prefixed, NewModuleBuilder().Imports(module).Build())
	if w := createGet(prefixed, "/site/404"); w.Code != http.StatusOK || w.Body.String() != "SITE-404" {
		t.Error(test.DiffMessage(w.Body.String(), "SITE-404", "P20: a module prefix removes the conflict"))
	}
}

func TestCreate_UnusableErrorNamePanics(t *testing.T) {
	for _, name := range []string{"200.html", "302.html", "3xx.html", "600.html"} {
		msExpectPanic(t, "P21", "invalid render: "+name+" is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html",
			cpCreateFiles(map[string]string{"x.html": "x", name: "x"}))
	}
}

func TestCreate_ReservedPagesDirPanics(t *testing.T) {
	msExpectPanic(t, "P34", "invalid render: assets/_pages is reserved for page CSS and JS",
		cpCreateFiles(map[string]string{"x.html": "x", "assets/_pages/x.css": "x"}))
	msExpectPanic(t, "P34", "invalid render: assets/_pages is reserved for page CSS and JS",
		cpCreateFiles(map[string]string{"x.html": "x", "assets/_pages": "x"}))
	msExpectPanic(t, "P34", "invalid render: x.html:",
		cpCreateFiles(map[string]string{"x.html": `{{asset "_pages/x.css"}}`, "x.css": "x"}))
}

func TestCreate_OrphanPageAssetPanics(t *testing.T) {
	const tail = "belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/"
	msExpectPanic(t, "P30", "invalid render: nw.css "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "nw.css": "x"}))
	msExpectPanic(t, "P30", "invalid render: partials/field.css "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "partials/field.html": "f", "partials/field.css": "x"}))
	msExpectPanic(t, "P30", "invalid render: sub/y.js "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "sub/y.js": "x"}))
}

func TestCreate_MissingPageAssetsCallPanics(t *testing.T) {
	msExpectPanic(t, "P32", "invalid render: x.html has x.css, but no template of the page calls {{pageAssets}}: add it to layout.html",
		cpCreateFiles(map[string]string{"layout.html": `{{template "content" .}}`, "x.html": "x", "x.css": "x"}))
	msExpectPanic(t, "P32", "invalid render: x.html has x.css, but no template of the page calls {{pageAssets}}: add it to the page",
		cpCreateFiles(map[string]string{"x.html": "x", "x.css": "x"}))

	partial := map[string]string{
		"layout.html":        `{{template "partials/head" .}}{{template "content" .}}`,
		"partials/head.html": `{{pageAssets}}`,
		"x.html":             "x",
		"x.css":              "x",
	}
	if text, panicked := createPanicText(cpCreateFiles(partial)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P32: the call inside a partial counts"))
	}
}

func TestCreate_TemplateParseErrorPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{"x.html": "{{.Open"}))
	msExpectPanic(t, "R4", "invalid render: sub/y.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/y.html": "{{if}}"}))
	msExpectPanic(t, "R4", "invalid render: layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "layout.html": "{{template \"content\" .}}{{end}}"}))
	msExpectPanic(t, "R4", "invalid render: sub/layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/layout.html": "{{range}}"}))
	msExpectPanic(t, "R4", "invalid render: partials/p.html:", cpCreateFiles(map[string]string{"x.html": "ok", "partials/p.html": "{{if .A}}open"}))
	msExpectPanic(t, "P18", "invalid render: 404.html:", cpCreateFiles(map[string]string{"x.html": "ok", "404.html": "{{.Code"}))
	msExpectPanic(t, "P18", "invalid render: 4xx.html:", cpCreateFiles(map[string]string{"x.html": "ok", "4xx.html": "{{.Code"}))
	msExpectPanic(t, "P18", "invalid render: 5xx.html:", cpCreateFiles(map[string]string{"x.html": "ok", "5xx.html": "<a href='{{.Code}}"}))
}

func TestCreate_MissingPartialPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{
		"x.html": `{{template "partials/nope" .}}`,
	}))
	msExpectPanic(t, "R4", "invalid render: sub/z.html:", cpCreateWith(map[string]string{
		"x.html":          `fine`,
		"layout.html":     `{{template "content" .}}`,
		"sub/z.html":      `{{template "partials/missing" .}}`,
		"partials/a.html": `a`,
	}, cpPageController{}, cpSubZ{}))
}

func TestCreate_EscapeErrorPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{
		"x.html": `<a href='{{.X}}`,
	}))
}

func TestCreate_IgnoresNonTemplateFilesAndHiddenPaths(t *testing.T) {
	files := map[string]string{
		"x.html":           "ok",
		"notes.txt":        "{{ broken",
		".secret.html":     "{{ broken",
		".git/config.html": "{{ broken",
		"assets/page.html": "{{ broken",
	}
	if text, panicked := createPanicText(cpCreateFiles(files)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R4: files that are no templates are never parsed"))
	}
}

func TestCreate_SubmitWithoutRenderPanics(t *testing.T) {
	files := map[string]string{"x.html": "x"}
	msExpectPanic(t, "P13", "invalid handler: cpSubmitOnly.SUBMIT_x needs cpSubmitOnly.RENDER_x: a SUBMIT_ posts to the page that shows its form",
		cpCreateWith(files, cpSubmitOnly{}))
	msExpectPanic(t, "P13", "invalid handler: cpSubmitOnly.SUBMIT_x needs cpSubmitOnly.RENDER_x: a SUBMIT_ posts to the page that shows its form",
		cpCreateWith(files, cpSubmitOnly{}, cpRenderElsewhere{}))
}

func TestCreate_SubmitPathPanics(t *testing.T) {
	files := map[string]string{"x.html": "x", "y.html": "y", "p/x.html": "x"}
	msExpectPanic(t, "P15", `invalid route: Path("/y") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_`,
		cpCreateWith(files, cpSubmitPath{}))
	msExpectPanic(t, "P15", `invalid route: Prefix("p") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_`,
		cpCreateWith(files, cpSubmitPrefix{}))
}

func TestSubmit_PathAndPrefixOfRenderMoveBoth(t *testing.T) {
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"p/moved.html": "FORM"}), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(cpSubmitMovesWithRender{}).Build())

	if w := createGet(app, "/p/moved"); w.Body.String() != "FORM" {
		t.Error(test.DiffMessage(w.Body.String(), "FORM", "P15: Path and Prefix on the RENDER_ move the page"))
	}
	if w := createRequest(app, http.MethodPost, "/p/moved"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P15: and the SUBMIT_ with it"))
	}
	if w := createRequest(app, http.MethodPost, "/x"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P15: the SUBMIT_ has no path of its own"))
	}
}

func TestDevtool_PageKind(t *testing.T) {
	app := dvrApp(t, map[string]string{"dvrpage.html": "A"}, dvrPage{}, dvrAPI{})

	kinds := dvrKinds(t, dvrDecode(t, app))
	var page, submit, api string
	for key := range kinds {
		switch {
		case strings.HasPrefix(key, "GET") && strings.Contains(key, "dvrpage"):
			page = key
		case strings.HasPrefix(key, "POST") && strings.Contains(key, "dvrpage"):
			submit = key
		case strings.Contains(key, "dvrapi"):
			api = key
		}
	}
	if page == "" || submit == "" || api == "" {
		t.Fatal(test.DiffMessage(kinds, "a RENDER_, a SUBMIT_ and an API route", "R42: all routes are in the snapshot"))
	}
	if kinds[page] != "page" {
		t.Error(test.DiffMessage(kinds[page], "page", "R42: a RENDER_ route has kind page"))
	}
	if kinds[submit] != "page" {
		t.Error(test.DiffMessage(kinds[submit], "page", "R42: a SUBMIT_ route has kind page"))
	}
	if kinds[api] != nil {
		t.Error(test.DiffMessage(kinds[api], nil, "R42: an API route has no kind"))
	}
}

func TestDevtool_NoRenderWithoutRenderFS(t *testing.T) {
	app := dvrApp(t, nil, dvrAPI{})

	snapshot := dvrDecode(t, app)
	if _, ok := snapshot["render"]; ok {
		t.Error(test.DiffMessage(snapshot["render"], "no render section", "R43: omitted without Render.FS"))
	}
	for route, kind := range dvrKinds(t, snapshot) {
		if kind != nil {
			t.Error(test.DiffMessage(kind, nil, "R42: no kind on an app without Render.FS: "+route))
		}
	}
}

func TestDevtool_RenderSnapshot(t *testing.T) {
	app := dvrApp(t, dvrSnapFiles(), dvrSnap{})

	for i := 0; i < 3; i++ {
		createGet(app, "/a")
	}
	if w := createGet(app, "/rows"); w.Code != http.StatusInternalServerError {
		t.Fatal(test.DiffMessage(w.Code, http.StatusInternalServerError, "R43: a failing render"))
	}

	snapshot := dvrDecode(t, app)
	render, ok := snapshot["render"].(map[string]any)
	if !ok {
		t.Fatal(test.DiffMessage(snapshot["render"], "a render section", "R43: present with Render.FS"))
	}
	if render["is_reload"] != nil && render["is_reload"] != false {
		t.Error(test.DiffMessage(render["is_reload"], false, "R43: is_reload"))
	}
	if _, old := render["error_template"]; old {
		t.Error(test.DiffMessage(render["error_template"], "no error_template key", "R43: error_template became error_templates"))
	}
	templates, _ := json.Marshal(render["error_templates"])
	if string(templates) != `["404.html","5xx.html"]` {
		t.Error(test.DiffMessage(string(templates), `["404.html","5xx.html"]`, "R43: error_templates are the file names"))
	}
	if render["assets"] != float64(2) {
		t.Error(test.DiffMessage(render["assets"], float64(2), "R43: assets counts the served files of assets/"))
	}
	if render["public"] != float64(2) {
		t.Error(test.DiffMessage(render["public"], float64(2), "R43: public counts the files of public/"))
	}

	pages, _ := render["pages"].([]any)
	type info struct {
		route, layout string
		assets        []string
	}
	got := map[string]info{}
	for _, p := range pages {
		entry, _ := p.(map[string]any)
		name, _ := entry["name"].(string)
		route, _ := entry["route"].(string)
		layout, _ := entry["layout"].(string)
		var assets []string
		list, _ := entry["assets"].([]any)
		for _, a := range list {
			assets = append(assets, a.(string))
		}
		got[name] = info{strings.Trim(route, "/"), layout, assets}
	}
	if len(got) != 5 {
		t.Fatal(test.DiffMessage(len(got), 5, "R41, R43: one entry per page route, named by its template path"))
	}
	want := map[string]info{
		"a":         {"a", "layout.html", []string{"layout.css", "a.css"}},
		"b/c":       {"b/c", "b/layout.html", nil},
		"nodata":    {"nodata", "layout.html", []string{"layout.css"}},
		"rows":      {"rows", "layout.html", []string{"layout.css"}},
		"user/{id}": {"user/{id}", "layout.html", []string{"layout.css"}},
	}
	for name, w := range want {
		g := got[name]
		if g.route != w.route || g.layout != w.layout || strings.Join(g.assets, ",") != strings.Join(w.assets, ",") {
			t.Error(test.DiffMessage(g, w, "R43: page "+name+" with its route, layout file and CSS and JS files"))
		}
	}
	partials, _ := json.Marshal(render["partials"])
	if string(partials) != `["partials/p","partials/q"]` {
		t.Error(test.DiffMessage(string(partials), `["partials/p","partials/q"]`, "R43: partials"))
	}
	renders, _ := render["renders"].(float64)
	if renders < 3 || render["failures"] != float64(1) {
		t.Error(test.DiffMessage([]any{render["renders"], render["failures"]}, []any{"3 or more", float64(1)}, "R43: counters"))
	}

	createGet(app, "/a")
	after, _ := dvrDecode(t, app)["render"].(map[string]any)
	if got, _ := after["renders"].(float64); got <= renders {
		t.Error(test.DiffMessage(got, "more than "+sprint(renders), "R43: renders counts live"))
	}
}

func TestDevtool_RenderSnapshotBuiltInError(t *testing.T) {
	app := dvrApp(t, map[string]string{"dvrpage.html": "A"}, dvrPage{})

	render, _ := dvrDecode(t, app)["render"].(map[string]any)
	if _, ok := render["error_templates"]; ok {
		t.Error(test.DiffMessage(render["error_templates"], "absent", "R43: error_templates is absent when only the built-in page exists"))
	}
	if render["assets"] != nil && render["assets"] != float64(0) {
		t.Error(test.DiffMessage(render["assets"], 0, "R43: no assets"))
	}
	if render["public"] != nil && render["public"] != float64(0) {
		t.Error(test.DiffMessage(render["public"], 0, "R43: no public files"))
	}
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
	free := New(&AppOptions{Render: &RenderOptions{FS: pgFiles(map[string]string{"bookings/new.html": "NEW", "bookings/{id}.html": "ONE"}), Dir: "."}})
	if text, panicked := createPanicText(func() {
		free.Create(NewModuleBuilder().Controllers(paBookings{}, assetUserServe{}).Build())
	}); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R39: without assets and page files a user /assets/* route is free"))
	}
}

func TestPageAssets_RouteConflictWithUserAssetsRoute(t *testing.T) {
	app := New(&AppOptions{Render: &RenderOptions{FS: pgFiles(paFiles()), Dir: "."}})
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

func TestPageForm_BindsLikeAPI(t *testing.T) {
	app, _ := pfApp(t, nil)
	fields := map[string]string{"name": "Ann & Bob", "email": "ann@example.com"}

	multipartType, multipartBody := pfMultipart(t, fields)
	bodies := map[string][2]string{
		"urlencoded": {pfURLEncoded, pfEncode(fields)},
		"multipart":  {multipartType, multipartBody},
	}
	for name, b := range bodies {
		api := pfPost(app, "/api/echo", b[0], b[1])
		var got map[string]string
		if err := json.Unmarshal(api.Body.Bytes(), &got); err != nil {
			t.Fatal(test.DiffMessage(api.Body.String(), "a JSON object", "R25: the API route answers "+name))
		}
		if got["name"] != "Ann & Bob" || got["email"] != "ann@example.com" {
			t.Error(test.DiffMessage(got, fields, "R25: the API route binds a "+name+" form"))
		}

		page := pfPost(app, "/bookings/echo", b[0], b[1])
		if page.Code != http.StatusOK {
			t.Error(test.DiffMessage(page.Code, http.StatusOK, "P14: a SUBMIT_ that sets 200 answers 200 ("+name+")"))
		}
		if want := "<html><body>name=Ann &amp; Bob;email=ann@example.com</body></html>"; page.Body.String() != want {
			t.Error(test.DiffMessage(page.Body.String(), want, "R25: the page route binds a "+name+" form with the same values, escaped"))
		}
	}
}

func TestPageForm_InvalidRerenders(t *testing.T) {
	app, _ := pfApp(t, nil)

	w := pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": "", "email": "nobody"}))
	peExpectHTML(t, "R26: invalid input", w, http.StatusUnprocessableEntity)
	body := w.Body.String()
	for _, want := range []string{"name is required", "email must contain @", `value="nobody"`, "<form"} {
		if !strings.Contains(body, want) {
			t.Error(test.DiffMessage(body, "a body holding "+want, "R26: the form comes back with its messages and the submitted values"))
		}
	}
	if !strings.HasPrefix(body, "<html><body>") {
		t.Error(test.DiffMessage(body, "the page inside its layout", "R26: the form is a page"))
	}

	w = pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": "Ann", "email": "nobody"}))
	if body := w.Body.String(); w.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `value="Ann"`) || strings.Contains(body, "name is required") {
		t.Error(test.DiffMessage(body, "the name kept and only the email message", "R26: a valid field keeps its value and has no message"))
	}

	w = pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": pfHostile, "email": "nobody"}))
	body = w.Body.String()
	if !strings.Contains(body, `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`) {
		t.Error(test.DiffMessage(body, `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`, "R26: a hostile name is escaped inside value"))
	}
	if strings.Contains(body, "<script>") {
		t.Error(test.DiffMessage(body, "no script element", "R26: the submitted value never becomes markup"))
	}

	multipartType, multipartBody := pfMultipart(t, map[string]string{"name": pfHostile, "email": "nobody"})
	w = pfPost(app, "/bookings/new", multipartType, multipartBody)
	if body := w.Body.String(); w.Code != http.StatusUnprocessableEntity || strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Error(test.DiffMessage(body, "the escaped value", "R26: a multipart form is re-rendered escaped"))
	}

	w = pfPost(app, "/bookings/new", pfURLEncoded, "")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "name is required") || !strings.Contains(w.Body.String(), "email is required") {
		t.Error(test.DiffMessage(w.Body.String(), "a 422 with a message per missing field", "R26: an empty body"))
	}
}

func TestPageForm_Redirect303(t *testing.T) {
	app, _ := pfApp(t, nil)
	valid := pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})

	for name, target := range map[string]string{"context method": "/bookings/new", "parameter": "/bookings/viaparam"} {
		w := pfPost(app, target, pfURLEncoded, valid)
		if w.Code != http.StatusSeeOther {
			t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R27: a redirect without a status on a page route ("+name+")"))
		}
		if got := w.Header().Get("Location"); got != "/bookings" {
			t.Error(test.DiffMessage(got, "/bookings", "R27: Location ("+name+")"))
		}
		if strings.Contains(w.Body.String(), "MARKER") {
			t.Error(test.DiffMessage(w.Body.String(), "no body from the page", "R27: the result is ignored ("+name+")"))
		}
	}

	w := peDo(app, http.MethodGet, "/bookings/goto")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R27, D4: a GET page redirects with 303 too"))
	}
	if strings.Contains(w.Body.String(), "MARKER") {
		t.Error(test.DiffMessage(w.Body.String(), "no page body", "R27: the page is not rendered on GET"))
	}

	w = pfPost(app, "/bookings/viaparam", pfURLEncoded, "")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "name is required") {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P14: the invalid branch of the same handler renders with 422"))
	}
}

func TestPageForm_ExplicitRedirectStatus(t *testing.T) {
	app, _ := pfApp(t, nil)
	valid := pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})

	w := pfPost(app, "/bookings/explicit", pfURLEncoded, valid)
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusTemporaryRedirect, "R27: an explicit 307 is kept"))
	}
	w = pfPost(app, "/bookings/found", pfURLEncoded, valid)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusFound, "R27: an explicit 302 is kept"))
	}
}

func TestRedirect_APIStill301(t *testing.T) {
	app, _ := pfApp(t, nil)
	valid := pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})

	w := pfPost(app, "/api/new", pfURLEncoded, valid)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "R27: a POST redirect on an API route is still 301"))
	}
	w = peDo(app, http.MethodGet, "/api/goto")
	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "R27: a GET redirect on an API route is still 301"))
	}
	w = pfPost(app, "/api/explicit", pfURLEncoded, valid)
	if w.Code != http.StatusTemporaryRedirect {
		t.Error(test.DiffMessage(w.Code, http.StatusTemporaryRedirect, "R27: an explicit status on an API route is kept"))
	}
}

func TestPageForm_BadBodyIsHTMLError(t *testing.T) {
	app, _ := peAppOptions(t, pfFiles(), &AppOptions{HTTP: &HTTPOptions{MaxBodyBytes: 64}}, nil, pfPages{}, pfAPI{})

	w := pfPost(app, "/bookings/new", pfURLEncoded, "name=%zz")
	peExpectHTML(t, "R28: a malformed urlencoded body", w, http.StatusBadRequest)
	if body := w.Body.String(); !strings.Contains(body, "Invalid form body") || strings.Contains(body, "invalid URL escape") || strings.Contains(body, "%zz") {
		t.Error(test.DiffMessage(body, "the message of R24 only", "R28: no cause text in the page"))
	}

	w = pfPost(app, "/bookings/new", "multipart/form-data; boundary=xyz", "garbage")
	peExpectHTML(t, "R28: a malformed multipart body", w, http.StatusBadRequest)
	if !strings.Contains(w.Body.String(), "Invalid form body") {
		t.Error(test.DiffMessage(w.Body.String(), "Invalid form body", "R28: the message"))
	}

	w = pfPost(app, "/bookings/new", pfURLEncoded, "name="+strings.Repeat("a", 4096))
	peExpectHTML(t, "R28: a body over HTTP.MaxBodyBytes", w, http.StatusRequestEntityTooLarge)
	if body := w.Body.String(); !strings.Contains(body, "Request body too large") || strings.Contains(body, "http: request body too large") {
		t.Error(test.DiffMessage(body, "Request body too large", "R28, R24: the framework message"))
	}
}

func TestPageForm_CSRF(t *testing.T) {
	app, _ := peAppOptions(t, pfFiles(), &AppOptions{CSRF: &CSRFOptions{IsEnabled: true}}, nil, pfPages{}, pfAPI{})
	valid := pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})

	w := pfPost(app, "/bookings/echo", pfURLEncoded, valid, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptHTML)
	peExpectHTML(t, "R29: a cross-origin form POST with Accept text/html", w, http.StatusForbidden)
	if !strings.Contains(w.Body.String(), "Cross-origin request refused") {
		t.Error(test.DiffMessage(w.Body.String(), "a page holding the refusal message", "R29: the message"))
	}
	if strings.Contains(w.Body.String(), "name=Ann") {
		t.Error(test.DiffMessage(w.Body.String(), "the handler never ran", "R29: the form is not processed"))
	}

	w = pfPost(app, "/bookings/echo", pfURLEncoded, valid, "Origin", "https://evil.test", "Accept", peAcceptHTML)
	peExpectHTML(t, "R29: a cross-origin Origin with Accept text/html", w, http.StatusForbidden)

	w = pfPost(app, "/bookings/echo", pfURLEncoded, valid, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptJSON)
	peExpectJSON(t, "R29: the same refusal with Accept application/json", w, http.StatusForbidden)

	w = pfPost(app, "/bookings/echo", pfURLEncoded, valid, "Sec-Fetch-Site", "same-origin", "Accept", peAcceptHTML)
	if want := "<html><body>name=Ann;email=ann@example.com</body></html>"; w.Code != http.StatusOK || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R29: a same-origin POST passes"))
	}
}

func TestRender_NoReloadIgnoresChanges(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("old", rlEpoch)}}
	app, _ := rlApp(t, site, false)

	site.set(fstest.MapFS{
		"a.html": rlFile("new and longer", rlEpoch.Add(time.Hour)),
		"b.html": rlFile("added", rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "old" {
		t.Error(test.DiffMessage(got, "old", "R14: without IsReload a changed file is never read"))
	}
	if w := createGet(app, "/b"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "R14: an added file has no route"))
	}
}

func TestRender_ReloadSeesChanges(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("old", rlEpoch),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	}}
	app, _ := rlApp(t, site, true, rlTwo{})
	if got := createGet(app, "/a").Body.String(); got != "[old]" {
		t.Fatal(test.DiffMessage(got, "[old]", "R14: the first render"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("new text", rlEpoch),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "[new text]" {
		t.Error(test.DiffMessage(got, "[new text]", "R14: a page whose size changed"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("NEW TEXT", rlEpoch.Add(time.Minute)),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "[NEW TEXT]" {
		t.Error(test.DiffMessage(got, "[NEW TEXT]", "R14: a page whose modification time changed"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("<p>{{template \"content\" .}}</p>", rlEpoch),
		"partials/field.html": rlFile("p2-longer", rlEpoch),
		"a.html":              rlFile("NEW TEXT", rlEpoch.Add(time.Minute)),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "<p>NEW TEXT</p>" {
		t.Error(test.DiffMessage(got, "<p>NEW TEXT</p>", "R14: a changed layout"))
	}
	if got := createGet(app, "/p").Body.String(); got != "<p>p2-longer</p>" {
		t.Error(test.DiffMessage(got, "<p>p2-longer</p>", "R14: a changed partial"))
	}
}

func TestRender_ReloadRebinds(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("old form", rlEpoch)}}
	app, _ := rlApp(t, site, true, rlX{})
	if got := createGet(app, "/x").Body.String(); got != "old form" {
		t.Fatal(test.DiffMessage(got, "old form", "P10: the first render"))
	}

	site.set(fstest.MapFS{"x/index.html": rlFile("directory form", rlEpoch)})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "directory form" {
		t.Error(test.DiffMessage(w.Body.String(), "directory form", "P10: x.html moved to x/index.html, the route is bound again"))
	}
}

func TestRender_ReloadMissingTemplateIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("here", rlEpoch)}}
	app, logger := rlApp(t, site, true, rlX{})

	site.set(fstest.MapFS{"other.txt": rlFile("nothing", rlEpoch)})
	w := createGet(app, "/x")
	pgExpectHTMLFixed500(t, "P10: the template of a route is gone", w)
	if strings.Contains(w.Body.String(), "x.html") {
		t.Error(test.DiffMessage(w.Body.String(), "no file name", "P10: the cause is never in the response"))
	}
	entries := logger.named("RenderReloadFailed")
	if len(entries) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one RenderReloadFailed", "P10: logged with file and cause"))
	}
	if entries[0].field("file") == nil || entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(entries[0], "file and cause fields", "P10: RenderReloadFailed fields"))
	}

	site.set(fstest.MapFS{"x.html": rlFile("x", rlEpoch), "x/index.html": rlFile("twice", rlEpoch)})
	pgExpectHTMLFixed500(t, "P10: both templates exist", createGet(app, "/x"))

	site.set(fstest.MapFS{"x.html": rlFile("restored", rlEpoch.Add(time.Minute))})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "restored" {
		t.Error(test.DiffMessage(w.Body.String(), "restored", "P10: the page answers once the file is back"))
	}
}

func TestRender_ReloadIgnoresNewUnboundFile(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("x", rlEpoch)}}
	app, logger := rlApp(t, site, true, rlX{})

	site.set(fstest.MapFS{
		"x.html":     rlFile("x", rlEpoch),
		"stray.html": rlFile("stray", rlEpoch),
		"orphan.css": rlFile("a{}", rlEpoch),
	})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "x" {
		t.Error(test.DiffMessage(w.Body.String(), "x", "P10: a file without a route is ignored until the restart"))
	}
	if w := createGet(app, "/stray"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P10: it has no route"))
	}
	if n := len(logger.named("RenderReloadFailed")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "P9, P30 are not repeated on reload"))
	}
}

func TestRender_ReloadLostPageAssetsCallIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{
		"layout.html": rlFile(`{{pageAssets}}{{template "content" .}}`, rlEpoch),
		"x.html":      rlFile("x", rlEpoch),
		"x.css":       rlFile("a{}", rlEpoch),
	}}
	app, _ := rlApp(t, site, true, rlX{})
	if got := createGet(app, "/x").Body.String(); got != `<link rel="stylesheet" href="/assets/_pages/x.css">x` {
		t.Fatal(test.DiffMessage(got, "a link without ?v=", "P29: IsReload tags have no version"))
	}
	site.set(fstest.MapFS{
		"layout.html": rlFile(`{{template "content" .}}`, rlEpoch.Add(time.Minute)),
		"x.html":      rlFile("x", rlEpoch),
		"x.css":       rlFile("a{}", rlEpoch),
	})
	pgExpectHTMLFixed500(t, "P10, P32: the layout lost its {{pageAssets}}", createGet(app, "/x"))
}

func TestRender_ReloadFailureIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("fine", rlEpoch)}}
	app, logger := rlApp(t, site, true)

	site.set(fstest.MapFS{"a.html": rlFile("{{.Broken SECRETDETAIL", rlEpoch.Add(time.Minute))})
	for range 2 {
		w := createGet(app, "/a")
		pgExpectHTMLFixed500(t, "R14: a reload that fails to parse", w)
		if strings.Contains(w.Body.String(), "SECRETDETAIL") || strings.Contains(w.Body.String(), "a.html") {
			t.Error(test.DiffMessage(w.Body.String(), "no parse error text", "R14: the error is never in the response"))
		}
	}
	entries := logger.named("RenderReloadFailed")
	if len(entries) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one RenderReloadFailed", "R14: the failure is logged"))
	}
	if entries[0].level != "error" {
		t.Error(test.DiffMessage(entries[0].level, "error", "R14: RenderReloadFailed is an Error"))
	}
	if got := entries[0].field("file"); got != "a.html" {
		t.Error(test.DiffMessage(got, "a.html", "R14: the file field"))
	}
	if entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(nil, "a cause", "R14: the cause field"))
	}

	site.set(fstest.MapFS{"a.html": rlFile("fixed again", rlEpoch.Add(2*time.Minute))})
	if w := createGet(app, "/a"); w.Code != http.StatusOK || w.Body.String() != "fixed again" {
		t.Error(test.DiffMessage(w.Body.String(), "fixed again", "R14: the page answers again once the file is fixed"))
	}
}

func TestRender_ReloadConcurrent(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("state-A", rlEpoch)}}
	app, _ := rlApp(t, site, true)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				site.set(fstest.MapFS{"a.html": rlFile("state-B-longer", rlEpoch.Add(time.Duration(i)*time.Second))})
			} else {
				site.set(fstest.MapFS{"a.html": rlFile("state-A", rlEpoch.Add(time.Duration(i)*time.Second))})
			}
		}
	}()

	var renderers sync.WaitGroup
	var mu sync.Mutex
	var bad []string
	for g := 0; g < 32; g++ {
		renderers.Add(1)
		go func() {
			defer renderers.Done()
			for range 40 {
				w := createGet(app, "/a")
				if w.Code != http.StatusOK || (w.Body.String() != "state-A" && w.Body.String() != "state-B-longer") {
					mu.Lock()
					bad = append(bad, w.Body.String())
					mu.Unlock()
				}
			}
		}()
	}
	renderers.Wait()
	close(stop)
	wg.Wait()
	if len(bad) != 0 {
		t.Error(test.DiffMessage(bad[0], "state-A or state-B-longer", "R14: every concurrent render sees one whole state"))
	}
}

func TestRender_IsGetPage(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"nodata.html": "NODATA"}), pgPages{})

	w := createGet(app, "/nodata")
	if w.Code != http.StatusOK || w.Body.String() != "NODATA" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P1: GET answers the template, "+w.Body.String()))
	}
	h := createRequest(app, http.MethodHead, "/nodata")
	if h.Code != http.StatusOK || h.Body.Len() != 0 {
		t.Error(test.DiffMessage(h.Code, http.StatusOK, "P1, R15: HEAD answers without a body"))
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := createRequest(app, method, "/nodata")
		if w.Code != http.StatusMethodNotAllowed {
			t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "P1: "+method+" on a page with no SUBMIT_ is 405"))
		}
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") || strings.Contains(allow, "POST") {
			t.Error(test.DiffMessage(allow, "GET without POST", "P1: Allow of "+method))
		}
	}
}

func TestRender_MixedController(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"dashboard.html": "hello {{.Who}}"}), rnMixed{})

	if w := createGet(app, "/dashboard"); w.Code != http.StatusOK || w.Body.String() != "hello me" {
		t.Error(test.DiffMessage(w.Body.String(), "hello me", "P1: a RENDER_ route in a controller that has a READ_ route"))
	}
	if w := createGet(app, "/api"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Error(test.DiffMessage(w.Body.String(), `{"ok":true}`, "P1: the READ_ route is JSON"))
	}
	page := createRequest(app, http.MethodGet, "/dashboard?k=boom", "Accept", "application/json")
	peExpectHTML(t, "R16: the page route of a mixed controller fails in HTML", page, http.StatusNotFound)
	api := createRequest(app, http.MethodGet, "/api?k=boom", "Accept", "text/html")
	peExpectJSON(t, "R17: the API route of the same controller fails in JSON", api, http.StatusNotFound)
}

func TestRender_TemplateByRoute(t *testing.T) {
	const who = `{{.Who}}`
	cases := []struct {
		note   string
		files  map[string]string
		build  func() *Module
		target string
		want   string
	}{
		{"root renders index.html", map[string]string{"index.html": who}, rnModule(rnRoot{}), "/", "root"},
		{"the .html form", map[string]string{"bookings.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings", "list"},
		{"the index.html form", map[string]string{"bookings/index.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings", "list"},
		{"a parameter file", map[string]string{"bookings/index.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings/42", "id=42"},
		{"a parameter directory", map[string]string{"bookings/index.html": who, "bookings/{id}/index.html": who}, rnModule(rnBookings{}), "/bookings/7", "id=7"},
		{"a controller prefix", map[string]string{"admin/users.html": who}, rnModule(rnAdminUsers{}), "/admin/users", "users"},
		{"a controller prefix, directory form", map[string]string{"admin/users/index.html": who}, rnModule(rnAdminUsers{}), "/admin/users", "users"},
		{"a module prefix", map[string]string{"api/v2/admin/users.html": who}, func() *Module {
			m := NewModuleBuilder().Controllers(rnAdminUsers{}).Build()
			m.Prefix("api/v2")
			return m
		}, "/api/v2/admin/users", "users"},
		{"a prefix alone, .html", map[string]string{"admin.html": who}, rnModule(rnAdminRoot{}), "/admin", "admin"},
		{"a prefix alone, index.html", map[string]string{"admin/index.html": who}, rnModule(rnAdminRoot{}), "/admin", "admin"},
		{"kebab case", map[string]string{"court-bookings.html": who}, rnModule(rnKebab{}), "/court-bookings", "court"},
		{"as written", map[string]string{"court_bookings.html": who}, func() *Module {
			return NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(rnKebab{}).Build()
		}, "/court_bookings", "court"},
		{"Path", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/exact/page", "other"},
		{"layout as the last segment", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/courts/layout", "lay"},
		{"index as the last segment", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/x/index", "idx"},
		{"assets.html at the root", map[string]string{"assets.html": who}, rnModule(rnAssetsName{}), "/assets", "assets"},
	}
	for _, tc := range cases {
		got := rnServe(t, tc.files, tc.build, tc.target)
		if got.code != http.StatusOK || got.body != tc.want {
			t.Error(test.DiffMessage([]any{got.code, got.body}, []any{http.StatusOK, tc.want}, "P2: "+tc.note))
		}
	}
}

func TestRender_TemplateByRouteIgnoresTheRequestPath(t *testing.T) {
	files := map[string]string{"bookings/index.html": `list`, "bookings/{id}.html": `id={{.Who}}`}
	app, _ := pgNew(t, pgFiles(files), rnBookings{})
	for _, target := range []string{"/bookings/%7Bid%7D", "/bookings/..%2findex", "/bookings/index"} {
		w := createGet(app, target)
		if w.Body.String() == "list" {
			t.Error(test.DiffMessage(w.Body.String(), "never the index template", "P2, Security: the request never picks the template: "+target))
		}
	}
}

func TestRender_DataIsResult(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	escaped := `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`
	cases := map[string]string{
		"/map":        `<p>` + escaped + `</p>`,
		"/structdata": `<p>` + escaped + `</p>`,
		"/ptr":        `<p>ptr</p>`,
		"/slice":      `<i>a</i><i>` + escaped + `</i>`,
		"/text":       `<p>plain ` + escaped + `</p>`,
		"/nilresult":  `nil`,
	}
	for target, want := range cases {
		w := createGet(app, target)
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Error(test.DiffMessage(w.Body.String(), want, "P6, R7: the handler's result is the data, escaped: "+target))
		}
	}
}

func TestRender_NoResult(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	if w := createGet(app, "/noresult"); w.Code != http.StatusOK || w.Body.String() != "nil" {
		t.Error(test.DiffMessage(w.Body.String(), "nil", "P6: a handler with no result renders with nil"))
	}
}

func TestRender_LastResultIsTheData(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	w := createGet(app, "/last")
	if w.Code != http.StatusAccepted || w.Body.String() != "<p>last</p>" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusAccepted, "<p>last</p>"}, "P6, P7: (int, data) renders the last result with the status"))
	}
}

func TestRender_VersionNeutral(t *testing.T) {
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"nodata.html": "PAGE"}), Dir: "."}, Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}})
	mustCreate(t, app, NewModuleBuilder().Controllers(pgNodataOnly{}).Build())

	for name, headers := range map[string][]string{
		"a version":       {"X-Version", "2"},
		"another version": {"X-Version", "9"},
		"no version":      nil,
	} {
		w := createRequest(app, http.MethodGet, "/nodata", headers...)
		if w.Code != http.StatusOK || w.Body.String() != "PAGE" {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "PAGE"}, "P8: a page route answers "+name))
		}
	}
}

func TestRender_EmptyDirectoryServesNothingWithoutRoutes(t *testing.T) {
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: fstest.MapFS{"layout.html": {Data: []byte(`{{template "content" .}}`)}}, Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(rnMixedAPIOnly{}).Build())
	if w := createGet(app, "/layout"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P2: a layout is no route"))
	}
}

func TestSubmit_PostsToRenderPath(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	w := pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": "", "email": "x"}))
	if w.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(w.Body.String(), "FORM name= errors=2") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "FORM name= errors=2"}, "P12: SUBMIT_new_OF_bookings posts to /bookings/new and renders its RENDER_'s template"))
	}
	w = pfPost(app, "/bookings/new", pfURLEncoded, sbValid())
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: a valid post redirects"))
	}

	w = pfPost(app, "/bookings/42", pfURLEncoded, pfEncode(map[string]string{"name": "Bo"}))
	if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "BOOKING 42 Bo" {
		t.Error(test.DiffMessage(w.Body.String(), "BOOKING 42 Bo", "P12: a parameter reaches the SUBMIT_ and the {id} template"))
	}

	w = pfPost(app, "/", pfURLEncoded, "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings/new" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: SUBMIT posts to the root of RENDER"))
	}
	if w := pfPost(app, "/new", pfURLEncoded, sbValid()); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P12: the SUBMIT_'s own words are not a path"))
	}
}

func TestSubmit_PostsUnderModulePrefix(t *testing.T) {
	inner := NewModuleBuilder().Controllers(sbPages{}).Build()
	inner.Prefix("shop")
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{
		"shop.html":               "SHOP",
		"shop/bookings/new.html":  `FORM {{.Name}}`,
		"shop/bookings/{id}.html": `BOOKING {{.Id}}`,
	}), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Imports(inner).Build())

	w := pfPost(app, "/shop/bookings/new", pfURLEncoded, sbValid())
	if w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: the post follows the module prefix of its page"))
	}
	if w := pfPost(app, "/bookings/new", pfURLEncoded, sbValid()); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P12: and only there"))
	}
}

func TestSubmit_Rerenders422(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	w := pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": pfHostile, "email": "nobody"}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P14: a SUBMIT_ that renders answers 422"))
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "P14: Content-Type"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Error(test.DiffMessage(cc, "no-cache", "P14: Cache-Control"))
	}
	if strings.Contains(w.Body.String(), "<script>") {
		t.Error(test.DiffMessage(w.Body.String(), "no raw script", "R26: submitted values are escaped"))
	}
}

func TestSubmit_Redirect303(t *testing.T) {
	app, _ := pfApp(t, nil)

	for name, target := range map[string]string{"context method": "/bookings/new", "parameter": "/bookings/viaparam"} {
		w := pfPost(app, target, pfURLEncoded, sbValid())
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusSeeOther, "/bookings"}, "P14: a redirect without a 3xx is 303 ("+name+")"))
		}
		if w.Body.Len() != 0 && strings.Contains(w.Body.String(), "<form") {
			t.Error(test.DiffMessage(w.Body.String(), "no page", "P14, R27: the result is ignored after a redirect ("+name+")"))
		}
	}
	if w := pfPost(app, "/bookings/explicit", pfURLEncoded, sbValid()); w.Code != http.StatusTemporaryRedirect {
		t.Error(test.DiffMessage(w.Code, http.StatusTemporaryRedirect, "P14: an explicit 3xx is kept"))
	}
}

func TestSubmit_StatusOverride(t *testing.T) {
	files := map[string]string{
		"ctx.html":    "CTX",
		"result.html": "RESULT {{.Word}}",
		"plain.html":  "PLAIN",
		"nodata.html": "NODATA",
	}
	app, _ := pgNew(t, pgFiles(files), sbStatus{})

	if w := pfPost(app, "/ctx", pfURLEncoded, ""); w.Code != http.StatusOK || w.Body.String() != "CTX" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "CTX"}, "P14: c.Status(200) replaces the default"))
	}
	if w := pfPost(app, "/result", pfURLEncoded, ""); w.Code != http.StatusCreated || w.Body.String() != "RESULT made" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusCreated, "RESULT made"}, "P14: the (int, data) form replaces the default"))
	}
	if w := pfPost(app, "/plain", pfURLEncoded, ""); w.Code != http.StatusUnprocessableEntity || w.Body.String() != "PLAIN" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "PLAIN"}, "P14: no result and no redirect renders with nil, 422"))
	}
	if w := pfPost(app, "/nodata", pfURLEncoded, ""); w.Code != http.StatusUnprocessableEntity {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P14: a nil result renders with 422"))
	}
	if w := createGet(app, "/ctx"); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P7: the RENDER_ stays 200"))
	}
}

func TestSubmit_GuardBoundToRenderOnly(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"only.html": "PAGE"}), sbGuardedRenderOnly{})

	if w := createGet(app, "/only"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P16: the guard bound to RENDER_only covers the GET"))
	}
	if w := createRequest(app, http.MethodPost, "/only"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P16: the guard does not run on the POST"))
	}
}

func TestSubmit_GuardBoundToAllCoversBoth(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"only.html": "PAGE"}), sbGuarded{})

	if w := createRequest(app, http.MethodPost, "/only"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P16: BindGuard(g) covers the SUBMIT_ too"))
	}
	if w := createRequest(app, http.MethodPost, "/only", "X-Allow", "yes"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P16: and lets it through when it allows"))
	}
}

func TestSubmit_OtherMethodIs405(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		w := createRequest(app, method, "/bookings/new", "Accept", "text/html")
		if w.Code != http.StatusMethodNotAllowed {
			t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "P16: "+method+" on a page"))
		}
		allow := w.Header().Get("Allow")
		if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
			t.Error(test.DiffMessage(allow, "GET and POST", "P16: Allow of "+method))
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "text/html", "R16: the 405 of a page is HTML"))
		}
	}
}

func TestSubmit_ErrorsAreHTML(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"fail.html": "x"}), sbFailing{})
	w := createRequest(app, http.MethodPost, "/fail", "Accept", "application/json")
	peExpectHTML(t, "R16: a SUBMIT_ is a page route, its error is HTML", w, http.StatusNotFound)
}

func TestSubmit_CreateStaysAnAPIRoute(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})
	w := createRequest(app, http.MethodPost, "/api")
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"created":true`) {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusCreated, `{"created":true}`}, "P16: CREATE_ stays an API route, 201 and JSON"))
	}
}

func TestRenderDir_DefaultIsWeb(t *testing.T) {
	files := fstest.MapFS{"web/x.html": {Data: []byte("FROMWEB")}, "x.html": {Data: []byte("FROMROOT")}}
	app, _ := rdApp(t, &RenderOptions{FS: files})
	if got := createGet(app, "/x").Body.String(); got != "FROMWEB" {
		t.Error(test.DiffMessage(got, "FROMWEB", "an empty Dir reads the web directory of FS"))
	}
}

func TestRenderDir_DotIsTheRoot(t *testing.T) {
	files := fstest.MapFS{"web/note.txt": {Data: []byte("FROMWEB")}, "x.html": {Data: []byte("FROMROOT")}}
	app, _ := rdApp(t, &RenderOptions{FS: files, Dir: "."})
	if got := createGet(app, "/x").Body.String(); got != "FROMROOT" {
		t.Error(test.DiffMessage(got, "FROMROOT", `Dir "." reads the root of FS`))
	}
}

func TestRenderDir_MissingDirectoryPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(&AppOptions{Render: &RenderOptions{FS: fstest.MapFS{"x.html": {Data: []byte("x")}}}})
	})
	want := `invalid render: RenderOptions.FS has no "web" directory: embed it with //go:embed web, or set Dir`
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "a missing directory says how to fix it"))
	}
}

func TestRenderDir_EmbeddedFSWithReloadReadsDisk(t *testing.T) {
	dir := filepath.Join("testdata", "web")
	app, logger := rdApp(t, &RenderOptions{FS: rdEmbedded, Dir: dir, IsReload: true})
	if got := createGet(app, "/x").Body.String(); !strings.HasPrefix(got, "EMBEDDED") {
		t.Fatal(test.DiffMessage(got, "EMBEDDED …", "the page renders from the directory"))
	}
	path := filepath.Join(dir, "x.html")
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path, old, 0o644) })
	if err := os.WriteFile(path, []byte("ONDISK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := createGet(app, "/x").Body.String(); got != "ONDISK" {
		t.Error(test.DiffMessage(got, "ONDISK", "with an embed.FS, IsReload reads the same directory from disk"))
	}
	entries := logger.named("RenderReloadOn")
	if len(entries) != 1 || entries[0].level != "warn" {
		t.Error(test.DiffMessage(entries, "one Warn RenderReloadOn", "reload mode is visible in the log at start"))
	}
}

func TestRenderDir_ReloadWithoutDiskDirectoryPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New(&AppOptions{Render: &RenderOptions{FS: rdEmbedded, Dir: "no-such-dir", IsReload: true}})
	})
	want := `invalid render: IsReload reads "no-such-dir" from disk, but there is no such directory under the working directory`
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "reload from a missing disk directory stops at start"))
	}
}

func TestRenderDir_NoReloadNoWarning(t *testing.T) {
	_, logger := rdApp(t, &RenderOptions{FS: fstest.MapFS{"web/x.html": {Data: []byte("x")}}})
	if n := len(logger.named("RenderReloadOn")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "no reload warning in production mode"))
	}
}

func TestTrace_RenderEvent(t *testing.T) {
	app, rec := traceRenderApp(t, map[string]string{"bookings/list.html": "LIST"}, true, pgBookings{})

	w := createGet(app, "/bookings/list")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R41: the page renders"))
	}
	events := rec.all()
	renders := traceStages(events, trace.StageRender)
	if len(renders) != 1 {
		t.Fatal(test.DiffMessage(len(renders), 1, "R41: one render event per render"))
	}
	e := renders[0]
	if e.Name != "bookings/list" || e.Transport != trace.TransportHTTP || e.Code == http.StatusInternalServerError || e.Duration < 0 {
		t.Error(test.DiffMessage([]any{e.Name, e.Transport}, []any{"bookings/list", trace.TransportHTTP}, "R41: Name and Transport"))
	}

	handler, render, complete := traceIndex(events, trace.StageHandler), traceIndex(events, trace.StageRender), traceIndex(events, trace.StageComplete)
	if handler < 0 || handler >= render || render >= complete {
		t.Error(test.DiffMessage([]int{handler, render, complete}, "handler < render < complete", "R41: the render event comes after handler and before complete"))
	}

	h := events[handler]
	if e.ID == "" || e.ID != h.ID || e.TraceID != h.TraceID || e.ParentID != e.ID {
		t.Error(test.DiffMessage([]any{e.ID, e.TraceID, e.ParentID}, []any{h.ID, h.TraceID, h.ID}, "R41: the trace ids of the request"))
	}
	if e.SpanID == "" || e.SpanID == h.SpanID {
		t.Error(test.DiffMessage(e.SpanID, "its own span id", "R41: a span id of its own"))
	}
}

func TestTrace_RenderEventOnFailure(t *testing.T) {
	app, rec := traceRenderApp(t, map[string]string{"rows.html": `{{template "partials/x" (dict "odd")}}`, "partials/x.html": "x"}, true)

	w := createGet(app, "/rows")
	if w.Code != http.StatusInternalServerError {
		t.Fatal(test.DiffMessage(w.Code, http.StatusInternalServerError, "R41: the render fails"))
	}
	events := rec.all()
	renders := traceStages(events, trace.StageRender)
	if len(renders) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one", "R41: a failed render emits its event"))
	}
	if renders[0].Name != "rows" || renders[0].Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage([]any{renders[0].Name, renders[0].Code}, []any{"rows", http.StatusInternalServerError}, "R41: Name and Code 500 of the failed render"))
	}
	if render, complete := traceIndex(events, trace.StageRender), traceIndex(events, trace.StageComplete); render > complete || traceIndex(events, trace.StageHandler) > render {
		t.Error(test.DiffMessage([]int{render, complete}, "render before complete", "R41: order on failure"))
	}
}

func TestTrace_RenderEventNamesTheErrorPage(t *testing.T) {
	custom, rec := traceRenderApp(t, map[string]string{"404.html": `<p>{{.Code}}</p>`}, true)
	custom.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	custom.ServeHTTP(httptest.NewRecorder(), r)
	renders := traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "404" || renders[0].Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(renders, "one render event named 404 (the JSON 404 renders nothing)", "R41: 404.html"))
	}

	class, rec := traceRenderApp(t, map[string]string{"4xx.html": `<p>{{.Code}}</p>`}, true)
	r = httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	class.ServeHTTP(httptest.NewRecorder(), r)
	renders = traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "4xx" {
		t.Error(test.DiffMessage(renders, "one render event named 4xx", "R41: 4xx.html"))
	}

	builtIn, rec := traceRenderApp(t, nil, true)
	r = httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	builtIn.ServeHTTP(httptest.NewRecorder(), r)
	renders = traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "error (built-in)" {
		t.Error(test.DiffMessage(renders, "one render event named error (built-in)", "R41: the built-in page"))
	}
}

func TestTrace_NoRenderEventWhenOff(t *testing.T) {
	app, _ := traceRenderApp(t, map[string]string{"bookings/list.html": "LIST", "rows.html": `{{template "partials/x" (dict "odd")}}`, "partials/x.html": "x"}, false, pgBookings{}, pgRowsOnly{})
	var seen int
	stop := app.tracer.Listen(func(trace.Event) { seen++ })
	stop()

	if w := createGet(app, "/bookings/list"); w.Body.String() != "LIST" {
		t.Error(test.DiffMessage(w.Body.String(), "LIST", "R41: the page still renders"))
	}
	if w := createGet(app, "/rows"); w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, "R41: a failure still answers"))
	}
	if seen != 0 {
		t.Error(test.DiffMessage(seen, 0, "R41: with tracing off no render event is emitted"))
	}
}

func TestTrace_RenderEventNamesTheTemplatePath(t *testing.T) {
	files := map[string]string{"bookings/index.html": "LIST", "bookings/{id}.html": "ONE {{.Who}}"}
	app, rec := traceRenderApp(t, files, true, rnBookings{})

	createGet(app, "/bookings")
	createGet(app, "/bookings/7")
	var names []string
	for _, e := range traceStages(rec.all(), trace.StageRender) {
		names = append(names, e.Name)
	}
	if len(names) != 2 || names[0] != "bookings/index" || names[1] != "bookings/{id}" {
		t.Error(test.DiffMessage(names, []string{"bookings/index", "bookings/{id}"}, "R41: Name is the template path without .html"))
	}
}

func TestPage_MultiParam_NestedTemplate(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	w := createGet(app, "/orgs/acme/projects/42")
	want := `<org><link rel="stylesheet" href="/assets/_pages/orgs/%7Borg_id%7D/projects/%7Bproject_id%7D.css?v=` + assetHash(mpProjectCSS) + `">PROJECT acme/42</org>`
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, want}, "MP1: two parameters pick orgs/{org_id}/projects/{project_id}.html in the nearest layout"))
	}
}

func TestPage_MultiParam_IndexForm(t *testing.T) {
	files := mpFiles()
	delete(files, "orgs/{org_id}/projects/{project_id}.html")
	delete(files, "orgs/{org_id}/projects/{project_id}.css")
	files["orgs/{org_id}/projects/{project_id}/index.html"] = `INDEX {{.Org}}/{{.Project}}`
	app := paApp(t, files, nil, mpOrgs{})

	if w := createGet(app, "/orgs/acme/projects/42"); w.Code != http.StatusOK || w.Body.String() != `<org>INDEX acme/42</org>` {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, `<org>INDEX acme/42</org>`}, "MP1: the index.html form of a nested parameter page"))
	}
}

func TestPage_MultiParam_And(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	if w := createGet(app, "/reports/2026/11"); w.Code != http.StatusOK || w.Body.String() != `<root>REPORT 2026-11</root>` {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, `<root>REPORT 2026-11</root>`}, "MP2: BY_year_AND_month picks reports/{year}/{month}.html"))
	}
}

func TestPage_MultiParam_LayoutScope(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	if w := createGet(app, "/orgs/acme"); w.Body.String() != `<root>ORG acme</root>` {
		t.Error(test.DiffMessage(w.Body.String(), `<root>ORG acme</root>`, "MP3: orgs/{org_id}.html sits in orgs/, so the root layout wraps it, not orgs/{org_id}/layout.html"))
	}
}

func TestPage_MultiParam_PageAssetServed(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	for _, target := range []string{
		"/assets/_pages/orgs/%7Borg_id%7D/projects/%7Bproject_id%7D.css",
		"/assets/_pages/orgs/{org_id}/projects/{project_id}.css",
	} {
		if w := createGet(app, target); w.Code != http.StatusOK || w.Body.String() != mpProjectCSS {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, mpProjectCSS}, "MP4: GET "+target))
		}
	}
	if w := createGet(app, "/assets/_pages/orgs/%7Borg_id%7D/projects/%7Bproject_id%7D.html"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "MP4: a template under a parameter directory is never served"))
	}
}

func TestPage_MultiParam_Submit(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	w := createRequest(app, http.MethodPost, "/orgs/acme/projects/42", "Content-Type", "application/x-www-form-urlencoded")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "PROJECT acme/42") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "PROJECT acme/42"}, "MP5: a SUBMIT_ with two parameters renders its RENDER_'s template with 422"))
	}
}

func TestPage_MultiParam_WrongParamNamePanics(t *testing.T) {
	files := map[string]string{"orgs/{org}/projects/{project_id}.html": "X"}
	msExpectPanic(t, "MP6", "needs a template: orgs/{org_id}/projects/{project_id}.html or orgs/{org_id}/projects/{project_id}/index.html",
		cpCreateWith(files, mpProjects{}))
}

func TestPage_MultiParam_TwoFormsPanics(t *testing.T) {
	files := map[string]string{"orgs/{org_id}.html": "A", "orgs/{org_id}/index.html": "B"}
	msExpectPanic(t, "MP6", "has two templates: orgs/{org_id}.html and orgs/{org_id}/index.html", cpCreateWith(files, mpOrg{}))
}

func TestPage_MultiParam_ErrorPage(t *testing.T) {
	app := paApp(t, mpFiles(), nil, mpOrgs{})

	w := createGet(app, "/orgs/acme/members/7")
	if w.Code != http.StatusNotFound || w.Body.String() != `<root>E404 No member 7 in acme</root>` {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusNotFound, `<root>E404 No member 7 in acme</root>`}, "MP7: a multi-parameter page's NotFoundException renders 404.html in the root layout"))
	}
}
