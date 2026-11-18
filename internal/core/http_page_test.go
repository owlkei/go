package core

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
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
	"github.com/dangduoc08/ginject/internal/versioning"
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
	return agg.Pipe(agg.Transform(func(data any) any {
		view, ok := data.(common.View)
		if !ok {
			return data
		}
		view.Data = ctx.Map{"Word": "intercepted"}
		return view
	}))
}

type pgStringInterceptor struct{}

func (pgStringInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(any) any { return "replaced" }))
}

type pgShortInterceptor struct{}

func (pgShortInterceptor) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any {
	return common.View{Name: "short", Data: ctx.Map{"Word": "short-circuit"}}
}

type pgMiddleware struct{}

func (pgMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("X-Page-Middleware", "ran")
	next()
}

type pgPages struct{ common.Page }

func (c pgPages) NewController() Controller { return c }

func (c pgPages) READ_view(h *ctx.HTTPContext) common.View {
	return common.View{Name: h.Query().Get("n"), Data: ctx.Map{"X": h.Query().Get("x")}}
}

func (c pgPages) CREATE_view(h *ctx.HTTPContext) common.View {
	return common.View{Name: h.Query().Get("n"), Data: ctx.Map{"X": h.Query().Get("x")}}
}

func (c pgPages) READ_nodata() common.View { return common.View{Name: "nodata"} }

func (c pgPages) READ_rows() common.View {
	return common.View{Name: "rows", Data: pgRows{Rows: make([]string, 1000)}}
}

func (c pgPages) READ_big(h *ctx.HTTPContext) common.View {
	n, _ := strconv.Atoi(h.Query().Get("n"))
	return common.View{Name: "big", Data: pgBig{Rows: make([]int, n)}}
}

func (c pgPages) READ_statusctx(h *ctx.HTTPContext) common.View {
	h.Status(http.StatusUnprocessableEntity)
	return common.View{Name: "nodata"}
}

func (c pgPages) CREATE_statusctx(h *ctx.HTTPContext) common.View {
	h.Status(http.StatusUnprocessableEntity)
	return common.View{Name: "nodata"}
}

func (c pgPages) READ_statusresult() (int, common.View) {
	return http.StatusUnprocessableEntity, common.View{Name: "nodata"}
}

func (c pgPages) CREATE_statusresult() (int, common.View) {
	return http.StatusUnprocessableEntity, common.View{Name: "nodata"}
}

func (c pgPages) READ_cached(h *ctx.HTTPContext) common.View {
	h.ResponseWriter.Header().Set("Cache-Control", "max-age=60")
	return common.View{Name: "nodata"}
}

func (c pgPages) CREATE_redirect(h *ctx.HTTPContext) common.View {
	h.Redirect("/elsewhere")
	return common.View{Name: "marker"}
}

func (c pgPages) READ_direct(h *ctx.HTTPContext) common.View {
	h.Text("direct write")
	return common.View{Name: "marker"}
}

func (c pgPages) READ_dict(h *ctx.HTTPContext) common.View {
	return common.View{Name: "dict", Data: ctx.Map{"Value": h.Query().Get("v")}}
}

type pgVersioned struct{ common.Page }

func (c pgVersioned) NewController() Controller { return c }

func (c pgVersioned) READ_ver_VERSION_1() common.View { return common.View{Name: "ver1"} }

func (c pgVersioned) READ_ver_VERSION_2() common.View { return common.View{Name: "ver2"} }

type pgPrefixed struct{ common.Page }

func (c pgPrefixed) NewController() Controller {
	c.Prefix("site")
	return c
}

func (c pgPrefixed) READ_home() common.View { return common.View{Name: "nodata"} }

func (c pgPrefixed) CREATE_home() common.View { return common.View{Name: "nodata"} }

type pgLayered struct {
	common.Page
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

func (c pgLayered) READ_layered(d pgDTO) common.View {
	return common.View{Name: "layered", Data: ctx.Map{"Word": d.Word}}
}

type pgReplaced struct {
	common.Page
	common.Interceptor
}

func (c pgReplaced) NewController() Controller {
	c.BindInterceptor(pgStringInterceptor{})
	return c
}

func (c pgReplaced) READ_replaced() common.View { return common.View{Name: "nodata"} }

type pgShort struct {
	common.Page
	common.Interceptor
}

func (c pgShort) NewController() Controller {
	c.BindInterceptor(pgShortInterceptor{})
	return c
}

func (c pgShort) READ_short() common.View {
	pgHandlerCalls.Add(1)
	return common.View{Name: "nodata"}
}

func pgFiles(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
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

func pgView(name, x string) string {
	return "/view?n=" + url.QueryEscape(name) + "&x=" + url.QueryEscape(x)
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
	files := pgFiles(map[string]string{"nodata.html": "NODATA", "ver1.html": "V1", "ver2.html": "V2"})
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	inner := NewModuleBuilder().Controllers(pgPrefixed{}, pgPages{}).Build()
	inner.Prefix("v9")
	mustCreate(t, app, NewModuleBuilder().Imports(inner).Build())

	w := createGet(app, "/v9/site/home")
	if w.Code != http.StatusOK || w.Body.String() != "NODATA" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R1: GET under module prefix and controller Prefix, "+w.Body.String()))
	}
	w = createRequest(app, http.MethodPost, "/v9/site/home")
	if w.Code != http.StatusOK || w.Body.String() != "NODATA" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R1, R6: POST handler of a page controller answers 200, "+w.Body.String()))
	}
	if w := createGet(app, "/site/home"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "R1: the module prefix applies"))
	}
}

func TestPage_Versions(t *testing.T) {
	files := pgFiles(map[string]string{"ver1.html": "V1", "ver2.html": "V2"})
	app := New()
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	mustCreate(t, app, NewModuleBuilder().Controllers(pgVersioned{}).Build())
	if w := createRequest(app, http.MethodGet, "/ver", "X-Version", "2"); w.Body.String() != "V2" {
		t.Error(test.DiffMessage(w.Body.String(), "V2", "R1: the version of a page route"))
	}
	if w := createRequest(app, http.MethodGet, "/ver", "X-Version", "1"); w.Body.String() != "V1" {
		t.Error(test.DiffMessage(w.Body.String(), "V1", "R1: another version of a page route"))
	}
}

func TestPage_LayersBind(t *testing.T) {
	files := pgFiles(map[string]string{"layered.html": "word={{.Word}}"})
	app, _ := pgNew(t, files, pgLayered{})

	if w := createRequest(app, http.MethodGet, "/layered?w=a"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "R1: a guard returning false refuses a page route"))
	}
	w := createRequest(app, http.MethodGet, "/layered?w=a", "X-Allow", "yes")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R1: the guard lets the request through"))
	}
	if w.Header().Get("X-Page-Middleware") != "ran" {
		t.Error(test.DiffMessage(w.Header().Get("X-Page-Middleware"), "ran", "R1: a controller middleware runs on a page route"))
	}
	if want := "word=intercepted"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R1, R13: the interceptor changed the View's data"))
	}

	files2 := pgFiles(map[string]string{"layered.html": "word={{.Word}}"})
	piped, _ := pgNew(t, files2, pgPipeOnly{})
	if w := createGet(piped, "/piped?w=a"); w.Body.String() != "word=piped-a" {
		t.Error(test.DiffMessage(w.Body.String(), "word=piped-a", "R1: a pipe on a page route"))
	}
}

type pgPipeOnly struct{ common.Page }

func (c pgPipeOnly) NewController() Controller { return c }

func (c pgPipeOnly) READ_piped(d pgDTO) common.View {
	return common.View{Name: "layered", Data: ctx.Map{"Word": d.Word}}
}

func TestPage_RendersInsideLayout(t *testing.T) {
	files := pgFiles(map[string]string{
		"layout.html":        `<html><body>{{template "content" .}}</body></html>`,
		"bookings/list.html": `<ul><li>{{.X}}</li></ul>`,
	})
	app, _ := pgNew(t, files, pgPages{})
	w := createGet(app, pgView("bookings/list", "ab"))
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
		"public/info.html":       `info`,
		"admin/partials/x.html":  `ignored-here`,
		"admin/deep/.hidden.txt": `ignored`,
	})
	app, _ := pgNew(t, files, pgPages{})
	cases := map[string]string{
		"home":             "[root:home]",
		"admin/dash":       "[admin:dash]",
		"admin/deep/inner": "[admin:inner]",
		"public/info":      "[root:info]",
	}
	for name, want := range cases {
		if got := createGet(app, pgView(name, "")).Body.String(); got != want {
			t.Error(test.DiffMessage(got, want, "R5: the nearest layout of "+name))
		}
	}
}

func TestPage_NoLayout(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"plain.html": "just {{.X}}"}), pgPages{})
	w := createGet(app, pgView("plain", "me"))
	if w.Code != http.StatusOK || w.Body.String() != "just me" {
		t.Error(test.DiffMessage(w.Body.String(), "just me", "R5: without a layout the page is rendered alone"))
	}
	files := pgFiles(map[string]string{"nodata.html": "no data here"})
	app, _ = pgNew(t, files, pgPages{})
	if w := createGet(app, "/nodata"); w.Code != http.StatusOK || w.Body.String() != "no data here" {
		t.Error(test.DiffMessage(w.Body.String(), "no data here", "R5: a View with nil Data renders"))
	}
}

func TestPage_BlockOverride(t *testing.T) {
	files := pgFiles(map[string]string{
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
	app, _ := pgNew(t, pgFiles(map[string]string{"nodata.html": "héllo"}), pgPages{})
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
	app, _ := pgNew(t, pgFiles(map[string]string{"nodata.html": "x"}), pgPages{})
	if got := createGet(app, "/cached").Header().Get("Cache-Control"); got != "max-age=60" {
		t.Error(test.DiffMessage(got, "max-age=60", "R5: a Cache-Control set by the handler is kept"))
	}
}

func TestPage_DefaultStatus200(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"x.html": "x"}), pgPages{})
	if w := createGet(app, pgView("x", "")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R6: GET page status"))
	}
	if w := createRequest(app, http.MethodPost, pgView("x", "")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R6: POST page status is 200, not 201"))
	}
}

func TestPage_StatusFromContext(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"nodata.html": "form"}), pgPages{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := createRequest(app, method, "/statusctx")
		if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "form" {
			t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "R6: c.Status on "+method))
		}
	}
}

func TestPage_StatusFromResult(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"nodata.html": "form"}), pgPages{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := createRequest(app, method, "/statusresult")
		if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "form" {
			t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "R6: (int, View) on "+method))
		}
	}
}

func TestPage_EscapesByContext(t *testing.T) {
	files := pgFiles(map[string]string{
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

func TestPage_ViewIgnoredAfterResponse(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"marker.html": "VIEWMARKER"}), pgPages{})
	w := createRequest(app, http.MethodPost, "/redirect")
	if w.Code < 300 || w.Code > 399 || w.Header().Get("Location") != "/elsewhere" {
		t.Error(test.DiffMessage(w.Code, "a 3xx to /elsewhere", "R8: the redirect stands"))
	}
	if strings.Contains(w.Body.String(), "VIEWMARKER") {
		t.Error(test.DiffMessage(w.Body.String(), "no view body", "R8: the View after Redirect is ignored"))
	}
	w = createGet(app, "/direct")
	if w.Body.String() != "direct write" || w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Body.String(), "direct write", "R8: the View after a direct write is ignored"))
	}
}

func TestPage_UnknownViewIs500(t *testing.T) {
	counted := &pgCountFS{inner: pgFiles(map[string]string{
		"layout.html":         `{{template "content" .}}`,
		"partials/field.html": `field`,
		"error.html":          `ERRORPAGE {{.Code}} {{.Message}}`,
		"real.html":           `real`,
	})}
	app, logger := pgNew(t, counted, pgPages{})
	counted.opens.Store(0)

	names := []string{"nope", "", "error", "partials/field", "layout", "../x", "real.html", "/real", "partials"}
	for _, name := range names {
		w := createGet(app, pgView(name, ""))
		pgExpectHTMLFixed500(t, "R9: view "+strconv.Quote(name), w)
		if body := w.Body.String(); !strings.Contains(body, "ERRORPAGE 500 Internal Server Error") {
			t.Error(test.DiffMessage(body, "the error page with the fixed 500 data", "R9, R24: an unknown view answers the error page, never the error template as a 200 page"))
		}
	}
	if n := counted.opens.Load(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "R9: a request never opens a file"))
	}
	entries := logger.named("RenderFailed")
	if len(entries) != len(names) {
		t.Fatal(test.DiffMessage(len(entries), len(names), "R9: one RenderFailed per bad view"))
	}
	if entries[0].level != "error" {
		t.Error(test.DiffMessage(entries[0].level, "error", "R9: RenderFailed is an Error"))
	}
	if got := entries[0].field("template"); got != "nope" {
		t.Error(test.DiffMessage(got, "nope", "R9: the template field"))
	}
	if entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(nil, "a cause", "R9: the cause field"))
	}
}

func TestPage_ExecErrorSendsNoPartialPage(t *testing.T) {
	files := pgFiles(map[string]string{
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
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R10: one RenderFailed"))
	}
}

func TestPage_OversizedPageIs500(t *testing.T) {
	files := pgFiles(map[string]string{
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
	files := pgFiles(map[string]string{
		"assets/app.css": "body{}",
		"x.html":         `{{if .X}}<link href="{{asset "missing.css"}}">{{end}}ok`,
	})
	app, logger := pgNew(t, files, pgPages{})
	if w := createGet(app, pgView("x", "")); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Body.String(), "ok", "R12: the unused branch renders"))
	}
	w := createGet(app, pgView("x", "show"))
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
	files := pgFiles(map[string]string{
		"assets/css/app.css": css,
		"x.html":             `{{asset "css/app.css"}}`,
	})
	app, _ := pgNew(t, files, pgPages{})
	if got := createGet(app, pgView("x", "")).Body.String(); got != want {
		t.Error(test.DiffMessage(got, want, "D6: asset returns the path with the first 8 bytes of the SHA-256"))
	}

	reload := New()
	reload.EnableRender(&RenderOptions{FS: files, Dir: ".", IsReload: true})
	mustCreate(t, reload, NewModuleBuilder().Controllers(pgPages{}).Build())
	if got, want := createGet(reload, pgView("x", "")).Body.String(), "/assets/css/app.css"; got != want {
		t.Error(test.DiffMessage(got, want, "D6: with IsReload asset adds no version"))
	}
}

func TestPage_DictFunction(t *testing.T) {
	files := pgFiles(map[string]string{
		"partials/field.html": `<label>{{.Name}}={{.Value}}</label>`,
		"dict.html":           `{{template "partials/field" (dict "Name" "email" "Value" .Value)}}`,
	})
	app, _ := pgNew(t, files, pgPages{})
	w := createGet(app, "/dict?v=a%3Cb")
	if want := "<label>email=a&lt;b</label>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "D6: dict passes several values to a partial, escaped"))
	}

	bad := pgFiles(map[string]string{
		"partials/field.html": `x`,
		"dict.html":           `{{template "partials/field" (dict "Name" "email" "Value")}}`,
	})
	app, logger := pgNew(t, bad, pgPages{})
	w = createGet(app, "/dict")
	pgExpectHTMLFixed500(t, "D6: odd dict arguments", w)
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R10: a failed dict is RenderFailed"))
	}

	badKey := pgFiles(map[string]string{
		"partials/field.html": `x`,
		"dict.html":           `{{template "partials/field" (dict 1 "a")}}`,
	})
	app, _ = pgNew(t, badKey, pgPages{})
	w = createGet(app, "/dict")
	pgExpectHTMLFixed500(t, "D6: non-string dict key", w)
}

func TestPage_InterceptorResult(t *testing.T) {
	files := pgFiles(map[string]string{"nodata.html": "never", "short.html": "word={{.Word}}"})
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(pgReplaced{}, pgShort{}).Build())

	w := createGet(app, "/replaced")
	pgExpectHTMLFixed500(t, "R13: a post-interceptor replaced the View by a string", w)
	if strings.Contains(w.Body.String(), "replaced") && !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Error(test.DiffMessage(w.Body.String(), "fixed 500", "R13: the string is not sent"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R13: RenderFailed is logged"))
	}

	pgHandlerCalls.Store(0)
	w = createGet(app, "/short")
	if w.Code != http.StatusOK || w.Body.String() != "word=short-circuit" {
		t.Error(test.DiffMessage(w.Body.String(), "word=short-circuit", "R13: an interceptor returning a View renders it"))
	}
	if n := pgHandlerCalls.Load(); n != 0 {
		t.Error(test.DiffMessage(n, 0, "R13: the handler was not called"))
	}
}

func TestPage_Head(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"nodata.html": "body text"}), pgPages{})
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
