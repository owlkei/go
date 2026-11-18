package core

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type pgRows struct{ Rows []string }

type pgBig struct{ Rows []int }

type pgCountFS struct {
	inner fs.FS
	opens atomic.Int64
}

func (f *pgCountFS) Open(name string) (fs.File, error) {
	f.opens.Add(1)
	return f.inner.Open(name)
}

var pgHandlerCalls atomic.Int32

type pgDTO struct{ Word string }

func (pgDTO) Transform(q ctx.Query, _ common.ArgumentMetadata) any {
	return pgDTO{Word: "piped-" + q.Get("w")}
}

type pgGuard struct{}

func (pgGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Allow") == "yes"
}

type pgInterceptor struct{}

func (pgInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(any) any { return ctx.Map{"Word": "intercepted"} }))
}

type pgStringInterceptor struct{}

func (pgStringInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(any) any { return "replaced" }))
}

type pgShortInterceptor struct{}

func (pgShortInterceptor) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any {
	return ctx.Map{"Word": "short-circuit"}
}

type pgMiddleware struct{}

func (pgMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("X-Page-Middleware", "ran")
	next()
}

type pgPages struct{ common.HTTP }

func (c pgPages) NewController() Controller { return c }

func (c pgPages) RENDER_nodata() {}

func (c pgPages) RENDER_view(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_plain(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_custom(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_esc(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_url(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_rows() pgRows { return pgRows{Rows: make([]string, 1000)} }

func (c pgPages) RENDER_big(h *ctx.HTTPContext) pgBig {
	n, _ := strconv.Atoi(h.Query().Get("n"))
	return pgBig{Rows: make([]int, n)}
}

func (c pgPages) RENDER_statusctx(h *ctx.HTTPContext) { h.Status(http.StatusUnprocessableEntity) }

func (c pgPages) RENDER_statusresult() (int, ctx.Map) {
	return http.StatusUnprocessableEntity, ctx.Map{"Word": "result"}
}

func (c pgPages) RENDER_cached(h *ctx.HTTPContext) {
	h.ResponseWriter.Header().Set("Cache-Control", "max-age=60")
}

func (c pgPages) RENDER_redirect(h *ctx.HTTPContext) { h.Redirect("/elsewhere") }

func (c pgPages) RENDER_direct(h *ctx.HTTPContext) { h.Text("direct write") }

func (c pgPages) RENDER_dict(h *ctx.HTTPContext) ctx.Map {
	return ctx.Map{"Value": h.Query().Get("v")}
}

var pgRoutes = []string{
	"nodata", "view", "plain", "custom", "esc", "url", "rows", "big",
	"statusctx", "statusresult", "cached", "redirect", "direct", "dict",
}

type pgPrefixed struct{ common.HTTP }

func (c pgPrefixed) NewController() Controller {
	c.Prefix("site")
	return c
}

func (c pgPrefixed) RENDER_home() {}

func (c pgPrefixed) SUBMIT_home(h *ctx.HTTPContext) { h.Redirect("/done") }

type pgLayered struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.Middleware
}

func (c pgLayered) NewController() Controller {
	c.BindGuard(pgGuard{})
	c.BindInterceptor(pgInterceptor{})
	c.BindMiddleware(pgMiddleware{})
	return c
}

func (c pgLayered) RENDER_layered(d pgDTO) ctx.Map { return ctx.Map{"Word": d.Word} }

type pgPipeOnly struct{ common.HTTP }

func (c pgPipeOnly) NewController() Controller { return c }

func (c pgPipeOnly) RENDER_piped(d pgDTO) ctx.Map { return ctx.Map{"Word": d.Word} }

type pgReplaced struct {
	common.HTTP
	common.Interceptor
}

func (c pgReplaced) NewController() Controller {
	c.BindInterceptor(pgStringInterceptor{})
	return c
}

func (c pgReplaced) RENDER_replaced() ctx.Map { return ctx.Map{"Word": "handler"} }

type pgShort struct {
	common.HTTP
	common.Interceptor
}

func (c pgShort) NewController() Controller {
	c.BindInterceptor(pgShortInterceptor{})
	return c
}

func (c pgShort) RENDER_short() ctx.Map {
	pgHandlerCalls.Add(1)
	return ctx.Map{"Word": "handler"}
}

type pgBookings struct{ common.HTTP }

func (c pgBookings) NewController() Controller { return c }

func (c pgBookings) RENDER_list_OF_bookings(h *ctx.HTTPContext) ctx.Map {
	return ctx.Map{"X": h.Query().Get("x")}
}

type pgNested struct{ common.HTTP }

func (c pgNested) NewController() Controller {
	c.Path("/admin/dash", c.RENDER_dash)
	c.Path("/admin/deep/inner", c.RENDER_inner)
	c.Path("/info", c.RENDER_info)
	return c
}

func (c pgNested) RENDER_home()  {}
func (c pgNested) RENDER_dash()  {}
func (c pgNested) RENDER_inner() {}
func (c pgNested) RENDER_info()  {}

func pgFiles(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func pgPageFileMap(extra map[string]string) map[string]string {
	files := map[string]string{}
	for _, name := range pgRoutes {
		files[name+".html"] = "DEFAULT"
	}
	maps.Copy(files, extra)
	return files
}

func pgPageFiles(extra map[string]string) fstest.MapFS {
	return pgFiles(pgPageFileMap(extra))
}

func pgNew(t *testing.T, fsys fs.FS, controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: fsys, Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func pgView(route, x string) string {
	return "/" + route + "?x=" + url.QueryEscape(x)
}

func pgExpectHTMLFixed500(t *testing.T, rule string, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	if w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, rule+": status"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", rule+": the 500 is HTML"))
	}
	if !strings.Contains(body, "Internal Server Error") {
		t.Error(test.DiffMessage(body, "a body holding Internal Server Error", rule+": the fixed text"))
	}
}

func TestPage_RoutesLikeHTTP(t *testing.T) {
	files := pgFiles(map[string]string{"v9/site/home.html": "HOME"})
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
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

	reload := New()
	reload.EnableRender(&RenderOptions{FS: files, Dir: ".", IsReload: true})
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
