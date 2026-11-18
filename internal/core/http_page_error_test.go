package core

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
	"github.com/dangduoc08/ginject/middlewares/csrf"
	"github.com/dangduoc08/ginject/middlewares/hostcheck"
	"github.com/dangduoc08/ginject/middlewares/loadshed"
)

const (
	peFixed       = "Internal Server Error"
	peFixedJSON   = `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`
	peTextBody    = "{{.Code}}|{{.Status}}|{{.Message}}"
	peAcceptHTML  = "text/html"
	peAcceptJSON  = "application/json"
	peSecretWords = "hunter2"
)

func peBoom(kind string) {
	switch kind {
	case "notfound":
		panic(exception.NotFoundException("no such thing"))
	case "description":
		panic(exception.NotFoundException("no such thing", exception.ExceptionOptions{Description: "gone away"}))
	case "emptymessage":
		panic(exception.NotFoundException(""))
	case "cause":
		panic(exception.BadRequestException("bad input", errors.New("sql: password=hunter2")))
	case "causeoptions":
		panic(exception.BadRequestException("bad input", exception.ExceptionOptions{Description: "mine", Cause: errors.New("sql: password=hunter2")}))
	case "wrapped":
		panic(fmt.Errorf("service: %w", exception.ConflictException("taken", errors.New("pq: duplicate key hunter2"))))
	case "internal":
		panic(exception.InternalServerErrorException("db down: password=hunter2"))
	case "internalcause":
		panic(exception.InternalServerErrorException("db down", errors.New("dial tcp 10.0.0.5: refused hunter2")))
	case "fixedinternal":
		panic(exception.InternalServerErrorException("Internal Server Error"))
	case "outside":
		panic(exception.NewException("odd hunter2", 700))
	case "string":
		panic("secret string hunter2")
	case "error":
		panic(errors.New("secret error hunter2"))
	case "hostile":
		panic(exception.BadRequestException(`<b>"x"&</b>`))
	}
}

type pePage struct{ common.HTTP }

func (c pePage) NewController() Controller {
	c.Prefix("page")
	return c
}

func (c pePage) RENDER_boom(h *ctx.HTTPContext) ctx.Map {
	k := h.Query().Get("k")
	peBoom(k)
	return ctx.Map{"K": k}
}

func (c pePage) SUBMIT_boom(h *ctx.HTTPContext) { h.Redirect("/page/ok") }

func (c pePage) RENDER_ok() {}

type peAPI struct{ common.HTTP }

func (c peAPI) NewController() Controller {
	c.Prefix("api")
	return c
}

func (c peAPI) READ_boom(h *ctx.HTTPContext) string {
	peBoom(h.Query().Get("k"))
	return "ok"
}

func (c peAPI) CREATE_boom() string { return "ok" }

func (c peAPI) READ_ok() string { return "ok" }

type peGuard struct{}

func (peGuard) CanActivate(c *ctx.HTTPContext) bool {
	switch c.Request.Header.Get("X-Guard") {
	case "false":
		return false
	case "panic":
		panic(exception.ForbiddenException("guard refuses"))
	}
	return true
}

type peInterceptor struct{}

func (peInterceptor) Intercept(c *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	if c.Request.Header.Get("X-Intercept") == "panic" {
		panic(exception.ConflictException("interceptor refuses"))
	}
	return agg.Pipe()
}

type peFilter struct{}

func (peFilter) Catch(c *ctx.HTTPContext, e *exception.Exception) {
	switch c.Request.Header.Get("X-Filter") {
	case "refilter":
		panic(exception.GoneException("filter refuses"))
	case "handle":
		c.Status(http.StatusTeapot).Text("handled by the user filter")
		return
	}
	panic(e)
}

type peDTO struct{}

func (d peDTO) Transform(q ctx.Query, _ common.ArgumentMetadata) any {
	if q.Get("pipe") == "boom" {
		panic(exception.BadRequestException("pipe refuses"))
	}
	return d
}

type peLayeredPage struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (c peLayeredPage) NewController() Controller {
	c.Prefix("lp")
	c.BindGuard(peGuard{})
	c.BindInterceptor(peInterceptor{})
	c.BindExceptionFilter(peFilter{})
	return c
}

func (c peLayeredPage) RENDER_run(_ peDTO, h *ctx.HTTPContext) {
	if h.Query().Get("h") == "boom" {
		panic(exception.NotFoundException("handler refuses"))
	}
}

type peLayeredAPI struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (c peLayeredAPI) NewController() Controller {
	c.Prefix("la")
	c.BindGuard(peGuard{})
	c.BindInterceptor(peInterceptor{})
	c.BindExceptionFilter(peFilter{})
	return c
}

func (c peLayeredAPI) READ_run(_ peDTO, h *ctx.HTTPContext) string {
	if h.Query().Get("h") == "boom" {
		panic(exception.NotFoundException("handler refuses"))
	}
	return "ok"
}

type peMiddleware struct{}

func (peMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	switch r.Header.Get("X-Mw") {
	case "panic":
		panic(exception.ServiceUnavailableException("mw refuses"))
	case "retry":
		w.Header().Set("Retry-After", "7")
		panic(exception.ServiceUnavailableException("mw busy"))
	case "dirty":
		h := w.Header()
		h.Set("Content-Length", "5")
		h.Set("Content-Encoding", "gzip")
		h.Set("ETag", `"abc"`)
		h.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		h.Set("X-Custom", "kept")
		panic(exception.BadRequestException("dirty refuses"))
	}
	next()
}

func peDo(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			r.Host = headers[i+1]
			continue
		}
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func peApp(t *testing.T, files map[string]string, setup func(*App), controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	if files != nil {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	}
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func peIsHTML(w *httptest.ResponseRecorder) bool {
	return strings.HasPrefix(w.Header().Get("Content-Type"), "text/html")
}

func peIsJSON(w *httptest.ResponseRecorder) bool {
	return w.Header().Get("Content-Type") == "application/json" && strings.HasPrefix(w.Body.String(), "{")
}

func peExpectHTML(t *testing.T, rule string, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status"))
	}
	if !peIsHTML(w) {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "text/html; charset=utf-8", rule+": the error is HTML"))
	}
	if strings.HasPrefix(w.Body.String(), "{") {
		t.Error(test.DiffMessage(w.Body.String(), "an HTML page", rule+": no JSON body"))
	}
}

func peExpectJSON(t *testing.T, rule string, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status"))
	}
	if !peIsJSON(w) {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type")+" "+w.Body.String(), "application/json with a JSON body", rule+": the error is JSON"))
	}
}

func peLayerFiles(groups ...string) map[string]string {
	files := map[string]string{}
	for _, group := range groups {
		switch group {
		case "page":
			files["page/ok.html"] = "OK"
			files["page/boom.html"] = `{{if eq .K "render"}}{{index .K 99}}{{end}}OK`
		case "lp":
			files["lp/run.html"] = "OK"
		}
	}
	return files
}

func peFilesWith(extra map[string]string, groups ...string) map[string]string {
	files := peLayerFiles(groups...)
	for name, content := range extra {
		files[name] = content
	}
	return files
}

func TestPageError_RouteKindDecides(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("lp"), nil, peLayeredPage{}, peLayeredAPI{})
	triggers := []struct {
		name    string
		query   string
		headers []string
		code    int
	}{
		{"guard returns false", "", []string{"X-Guard", "false"}, http.StatusForbidden},
		{"guard panics", "", []string{"X-Guard", "panic"}, http.StatusForbidden},
		{"pipe panics", "?pipe=boom", nil, http.StatusBadRequest},
		{"interceptor panics", "", []string{"X-Intercept", "panic"}, http.StatusConflict},
		{"handler panics", "?h=boom", nil, http.StatusNotFound},
		{"filter re-panics", "?h=boom", []string{"X-Filter", "refilter"}, http.StatusGone},
	}
	for _, tr := range triggers {
		page := peDo(app, http.MethodGet, "/lp/run"+tr.query, append([]string{"Accept", peAcceptJSON}, tr.headers...)...)
		peExpectHTML(t, "R16: page route, "+tr.name+", Accept is ignored", page, tr.code)
		api := peDo(app, http.MethodGet, "/la/run"+tr.query, append([]string{"Accept", peAcceptHTML}, tr.headers...)...)
		peExpectJSON(t, "R17: API route, "+tr.name+", Accept is ignored", api, tr.code)
	}
	if w := peDo(app, http.MethodGet, "/lp/run", "Accept", peAcceptJSON); w.Code != http.StatusOK || w.Body.String() != "OK" {
		t.Error(test.DiffMessage(w.Body.String(), "OK", "R16: a request without a failure still renders"))
	}
}

func TestError_AcceptDecidesBeforeGuards(t *testing.T) {
	setup := func(app *App) {
		app.BindGlobalMiddlewares(peMiddleware{}, csrf.CSRF{}, hostcheck.HostCheck{Hosts: []string{"example.com"}})
	}
	app, _ := peApp(t, peLayerFiles("page"), setup, pePage{}, peAPI{})

	for _, route := range []string{"/page/ok", "/api/ok"} {
		peExpectHTML(t, "R18: a user middleware refusal on "+route+" with Accept text/html", peDo(app, http.MethodGet, route, "X-Mw", "panic", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
		peExpectJSON(t, "R18: a user middleware refusal on "+route+" with Accept application/json", peDo(app, http.MethodGet, route, "X-Mw", "panic", "Accept", peAcceptJSON), http.StatusServiceUnavailable)
		peExpectJSON(t, "R18: a user middleware refusal on "+route+" without Accept", peDo(app, http.MethodGet, route, "X-Mw", "panic"), http.StatusServiceUnavailable)
	}

	for _, route := range []string{"/page/boom", "/api/boom"} {
		peExpectHTML(t, "R18, R29: cross-origin POST to "+route+" with Accept text/html", peDo(app, http.MethodPost, route, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptHTML), http.StatusForbidden)
		peExpectJSON(t, "R18, R29: cross-origin POST to "+route+" with Accept application/json", peDo(app, http.MethodPost, route, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptJSON), http.StatusForbidden)
	}

	peExpectHTML(t, "R18: hostcheck 421 with Accept text/html", peDo(app, http.MethodGet, "/page/ok", "Host", "evil.test", "Accept", peAcceptHTML), http.StatusMisdirectedRequest)
	peExpectJSON(t, "R18: hostcheck 421 with Accept application/json", peDo(app, http.MethodGet, "/page/ok", "Host", "evil.test", "Accept", peAcceptJSON), http.StatusMisdirectedRequest)

	peExpectHTML(t, "R18: 404 with Accept text/html", peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptHTML), http.StatusNotFound)
	peExpectJSON(t, "R18: 404 with Accept application/json", peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptJSON), http.StatusNotFound)

	w := peDo(app, http.MethodPost, "/page/ok", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptHTML)
	peExpectHTML(t, "R18: 405 with Accept text/html", w, http.StatusMethodNotAllowed)
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Error(test.DiffMessage(w.Header().Get("Allow"), "an Allow header holding GET", "R18, R20: the Allow header of a 405 is kept in HTML"))
	}
	w = peDo(app, http.MethodPost, "/page/ok", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptJSON)
	peExpectJSON(t, "R18: 405 with Accept application/json", w, http.StatusMethodNotAllowed)
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Error(test.DiffMessage(w.Header().Get("Allow"), "an Allow header holding GET", "R18: the Allow header of a 405 is kept in JSON"))
	}

	if w := peDo(app, http.MethodPost, "/page/boom", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptHTML); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R29: a same-origin POST passes (the SUBMIT_ redirects)"))
	}
}

func TestError_AcceptDecidesBeforeGuardsGone(t *testing.T) {
	setup := func(app *App) {
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	}
	app, _ := peApp(t, peLayerFiles(), setup, peAPI{})
	peExpectHTML(t, "R18: 410 with Accept text/html", peDo(app, http.MethodGet, "/api/ok", "Accept", peAcceptHTML), http.StatusGone)
	peExpectJSON(t, "R18: 410 with Accept application/json", peDo(app, http.MethodGet, "/api/ok", "Accept", peAcceptJSON), http.StatusGone)
	peExpectJSON(t, "R18: 410 without Accept", peDo(app, http.MethodGet, "/api/ok"), http.StatusGone)
}

func TestError_AcceptDecidesBeforeGuardsLoadShed(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(loadshed.LoadShed{MaxInFlight: 1, MaxQueue: -1}) }
	peBlockStarted = make(chan struct{})
	peBlockRelease = make(chan struct{})
	app, _ := peApp(t, peLayerFiles(), setup, peBlocking{})
	done := make(chan struct{})
	go func() {
		peDo(app, http.MethodGet, "/block/hold")
		close(done)
	}()
	<-peBlockStarted

	peExpectHTML(t, "R18: loadshed 503 with Accept text/html", peDo(app, http.MethodGet, "/block/hold", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
	peExpectJSON(t, "R18: loadshed 503 with Accept application/json", peDo(app, http.MethodGet, "/block/hold", "Accept", peAcceptJSON), http.StatusServiceUnavailable)

	close(peBlockRelease)
	<-done
}

var (
	peBlockStarted chan struct{}
	peBlockRelease chan struct{}
)

type peBlocking struct{ common.HTTP }

func (c peBlocking) NewController() Controller {
	c.Prefix("block")
	return c
}

func (c peBlocking) READ_hold() string {
	select {
	case <-peBlockStarted:
	default:
		close(peBlockStarted)
	}
	<-peBlockRelease
	return "held"
}

func TestError_AcceptTable(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("page"), nil, pePage{})
	cases := []struct {
		name    string
		accept  []string
		isHTML  bool
		comment string
	}{
		{"text/html", []string{"text/html"}, true, "plain"},
		{"upper case", []string{"TEXT/HTML"}, true, "case-insensitive"},
		{"mixed case", []string{"Text/Html"}, true, "case-insensitive"},
		{"browser list", []string{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}, true, "a browser's value"},
		{"q-value is not read", []string{"text/html;q=0.9,*/*"}, true, "q-values are not read"},
		{"zero q-value is not read", []string{"application/json, text/html;q=0"}, true, "q-values are not read"},
		{"two lines", []string{"application/json", "text/html"}, true, "a value of any Accept line"},
		{"star", []string{"*/*"}, false, "*/* alone"},
		{"missing", nil, false, "no Accept"},
		{"empty", []string{""}, false, "empty Accept"},
		{"xhtml alone", []string{"application/xhtml+xml"}, false, "xhtml alone"},
		{"json", []string{"application/json"}, false, "json"},
		{"text plain", []string{"text/plain"}, false, "text/plain"},
	}
	for _, tc := range cases {
		var headers []string
		for _, v := range tc.accept {
			headers = append(headers, "Accept", v)
		}
		w := peDo(app, http.MethodGet, "/nowhere", headers...)
		if tc.isHTML {
			peExpectHTML(t, "R18: Accept "+tc.name+" ("+tc.comment+")", w, http.StatusNotFound)
		} else {
			peExpectJSON(t, "R18: Accept "+tc.name+" ("+tc.comment+")", w, http.StatusNotFound)
		}
	}
}

func TestError_VaryAccept(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("page"), nil, pePage{})
	for name, accept := range map[string]string{"html": peAcceptHTML, "json": peAcceptJSON, "none": ""} {
		var headers []string
		if accept != "" {
			headers = []string{"Accept", accept}
		}
		w := peDo(app, http.MethodGet, "/nowhere", headers...)
		if got := strings.ToLower(strings.Join(w.Header().Values("Vary"), ",")); !strings.Contains(got, "accept") {
			t.Error(test.DiffMessage(got, "a Vary header holding Accept", "R18: a response chosen by Accept carries Vary: Accept ("+name+")"))
		}
	}
}

func TestError_JSONWithoutEnableRender(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}, csrf.CSRF{}) }
	app, _ := peApp(t, nil, setup, peAPI{})

	w := peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptHTML)
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "R18: without EnableRender a 404 is JSON whatever Accept says")
	if got := w.Header().Values("Vary"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string(nil), "R18: without EnableRender the response is byte for byte as before, no Vary"))
	}
	peExpectJSON(t, "R18: a middleware refusal is JSON without EnableRender", peDo(app, http.MethodGet, "/api/ok", "X-Mw", "panic", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
	peExpectJSON(t, "R18: csrf is JSON without EnableRender", peDo(app, http.MethodPost, "/api/boom", "Sec-Fetch-Site", "cross-site", "Accept", peAcceptHTML), http.StatusForbidden)
	peExpectJSON(t, "R17: a handler exception is JSON without EnableRender", peDo(app, http.MethodGet, "/api/boom?k=notfound", "Accept", peAcceptHTML), http.StatusNotFound)
}

func TestPageError_CustomTemplate(t *testing.T) {
	files := peFilesWith(map[string]string{
		"layout.html": `<html>{{template "content" .}}</html>`,
		"4xx.html":    `[{{.Code}}|{{.Status}}|{{.Message}}|{{.RequestID}}|{{printf "%T" .Code}}]`,
	}, "lp")
	app, _ := peApp(t, files, nil, peLayeredPage{})
	w := peDo(app, http.MethodGet, "/lp/run", "X-Guard", "panic")
	want := "<html>[403|Forbidden|guard refuses|" + w.Header().Get("X-Request-Id") + "|int]</html>"
	if w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P17, P18: 4xx.html inside the root layout with Code, Status, Message and RequestID"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "a request id", "R20: the request id is on the page and in the header"))
	}
	peExpectHTML(t, "R20: custom template", w, http.StatusForbidden)
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/html; charset=utf-8", "R20: Content-Type"))
	}
}

func TestPageError_KeepsLayerHeaders(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	app, _ := peApp(t, peLayerFiles("page"), setup, pePage{})

	w := peDo(app, http.MethodGet, "/page/ok", "X-Mw", "retry", "Accept", peAcceptHTML)
	peExpectHTML(t, "R20: error after a layer set Retry-After", w, http.StatusServiceUnavailable)
	if got := w.Header().Get("Retry-After"); got != "7" {
		t.Error(test.DiffMessage(got, "7", "R20: Retry-After set by a layer is kept"))
	}

	w = peDo(app, http.MethodPost, "/page/ok", "Accept", peAcceptHTML)
	peExpectHTML(t, "R20: 405", w, http.StatusMethodNotAllowed)
	if got := w.Header().Get("Allow"); !strings.Contains(got, "GET") {
		t.Error(test.DiffMessage(got, "GET", "R20: Allow is kept"))
	}
}

func TestPageError_DropsBodyHeaders(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	app, _ := peApp(t, peLayerFiles("page"), setup, pePage{})
	w := peDo(app, http.MethodGet, "/page/ok", "X-Mw", "dirty", "Accept", peAcceptHTML)

	peExpectHTML(t, "R20: error after a layer set body headers", w, http.StatusBadRequest)
	for _, name := range []string{"Content-Length", "Content-Encoding", "ETag", "Last-Modified"} {
		if got := w.Header().Get(name); got != "" {
			t.Error(test.DiffMessage(got, "", "R20: "+name+" set earlier is removed"))
		}
	}
	if got := w.Header().Get("X-Custom"); got != "kept" {
		t.Error(test.DiffMessage(got, "kept", "R20: other headers are kept"))
	}
}

func TestPageError_BuiltIn(t *testing.T) {
	files := peFilesWith(map[string]string{"layout.html": `<html>LAYOUTMARK{{template "content" .}}</html>`}, "page")
	app, _ := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=hostile")

	peExpectHTML(t, "R21: built-in page", w, http.StatusBadRequest)
	body := w.Body.String()
	lower := strings.ToLower(body)
	if !strings.HasPrefix(lower, "<!doctype html>") || !strings.Contains(lower, "<html") || !strings.Contains(lower, "</html>") || !strings.Contains(lower, "<title") {
		t.Error(test.DiffMessage(body, "a complete HTML document", "R21: a complete document"))
	}
	for _, banned := range []string{"<style", "<script", "<link", "style=", "src=", "http://", "https://", "LAYOUTMARK"} {
		if strings.Contains(body, banned) {
			t.Error(test.DiffMessage(body, "no "+banned, "R21: no layout, inline style or script, or external resource"))
		}
	}
	for _, want := range []string{"400", "Bad Request"} {
		if !strings.Contains(body, want) {
			t.Error(test.DiffMessage(body, "a body holding "+want, "R21: the code and the status text"))
		}
	}
	if strings.Contains(body, "<b>") || !strings.Contains(body, "&lt;b&gt;") {
		t.Error(test.DiffMessage(body, "the message escaped", "R21: the message is escaped"))
	}
}

func TestPageError_FallsBackToBuiltIn(t *testing.T) {
	files := peFilesWith(map[string]string{"layout.html": `<html>LAYOUTMARK{{template "content" .}}</html>`, "404.html": `ERRORMARK{{index .Code 0}}`, "4xx.html": "NEXTFILE"}, "page")
	app, logger := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=notfound")
	peExpectHTML(t, "P19: 404.html fails", w, http.StatusNotFound)
	body := w.Body.String()
	if body == "" || strings.Contains(body, "ERRORMARK") || strings.Contains(body, "LAYOUTMARK") || strings.Contains(body, "NEXTFILE") || !strings.Contains(body, "no such thing") {
		t.Error(test.DiffMessage(body, "the built-in page with the same code", "P19: no half page and the next file is not tried"))
	}
	entries := logger.named("RenderFailed")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "P19: RenderFailed is logged"))
	}
	if got := entries[0].field("template"); got != "404" {
		t.Error(test.DiffMessage(got, "404", "P19: the template field names the error page"))
	}

	files = peFilesWith(map[string]string{"layout.html": `<html>{{index .Code 0}}{{template "content" .}}</html>`, "404.html": `ERRORMARK`}, "page")
	app, logger = peApp(t, files, nil, pePage{})
	w = peDo(app, http.MethodGet, "/page/boom?k=notfound")
	peExpectHTML(t, "P19: the layout fails with error data", w, http.StatusNotFound)
	if body := w.Body.String(); body == "" || strings.Contains(body, "ERRORMARK") || !strings.Contains(body, "no such thing") {
		t.Error(test.DiffMessage(body, "the built-in page with the same code", "P19: no half page when the layout fails"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "P19: RenderFailed is logged for the layout"))
	}
}

func TestPageError_UserFilterFirst(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": "BUILTIN {{.Code}}"}, "lp")
	app, _ := peApp(t, files, nil, peLayeredPage{})

	w := peDo(app, http.MethodGet, "/lp/run?h=boom", "X-Filter", "handle")
	if w.Code != http.StatusTeapot || w.Body.String() != "handled by the user filter" {
		t.Error(test.DiffMessage(w.Code, http.StatusTeapot, "R23: a user filter answers by itself, "+w.Body.String()))
	}

	w = peDo(app, http.MethodGet, "/lp/run?h=boom", "X-Filter", "refilter")
	peExpectHTML(t, "R23: an exception the filter re-panics ends as an HTML error", w, http.StatusGone)
	if w.Body.String() != "BUILTIN 410" {
		t.Error(test.DiffMessage(w.Body.String(), "BUILTIN 410", "R23: the new exception is rendered"))
	}

	w = peDo(app, http.MethodGet, "/lp/run?h=boom")
	if w.Code != http.StatusNotFound || w.Body.String() != "BUILTIN 404" {
		t.Error(test.DiffMessage(w.Body.String(), "BUILTIN 404", "R23: a filter that passes the exception on ends as an HTML error"))
	}
}

type peCase struct {
	kind    string
	code    int
	status  string
	message string
	logMsg  string
	level   string
}

func peCases() []peCase {
	return []peCase{
		{"notfound", 404, "Not Found", "no such thing", "", ""},
		{"description", 404, "gone away", "no such thing", "", ""},
		{"emptymessage", 404, "Not Found", "Unhandled exception has occurred", "", ""},
		{"cause", 400, "Bad Request", "bad input", "ExceptionCause", "debug"},
		{"causeoptions", 400, "Bad Request", "bad input", "ExceptionCause", "debug"},
		{"wrapped", 409, "Conflict", "taken", "ExceptionCause", "debug"},
		{"internal", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"internalcause", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"fixedinternal", 500, peFixed, peFixed, "", ""},
		{"outside", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"string", 500, peFixed, peFixed, "UnhandledPanic", "error"},
		{"error", 500, peFixed, peFixed, "UnhandledPanic", "error"},
	}
}

func peLogCount(l *lcRecordingLogger, msg string) int { return len(l.named(msg)) }

func TestClientText_Table(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": peTextBody, "5xx.html": peTextBody}, "page")
	app, logger := peApp(t, files, nil, pePage{}, peAPI{})

	for _, tc := range peCases() {
		for _, route := range []string{"/api/boom", "/page/boom"} {
			isHTML := strings.HasPrefix(route, "/page")
			before := map[string]int{}
			for _, m := range []string{"ExceptionCause", "InternalServerError", "UnhandledPanic"} {
				before[m] = peLogCount(logger, m)
			}
			w := peDo(app, http.MethodGet, route+"?k="+tc.kind)
			desc := "R24: " + tc.kind + " on " + route

			if isHTML {
				peExpectHTML(t, desc, w, tc.code)
				if want := fmt.Sprintf("%d|%s|%s", tc.code, tc.status, tc.message); w.Body.String() != want {
					t.Error(test.DiffMessage(w.Body.String(), want, desc+": .Code, .Status and .Message"))
				}
			} else {
				peExpectJSON(t, desc, w, tc.code)
				want := exception.NewException(tc.message, tc.code, tc.status).AppendJSON(nil)
				if w.Body.String() != string(want) {
					t.Error(test.DiffMessage(w.Body.String(), string(want), desc+": code, error and message"))
				}
			}
			if strings.Contains(w.Body.String(), peSecretWords) || strings.Contains(w.Body.String(), "10.0.0.5") || strings.Contains(w.Body.String(), "secret") {
				t.Error(test.DiffMessage(w.Body.String(), "no cause text", desc+": a cause never reaches the client"))
			}
			for _, m := range []string{"ExceptionCause", "InternalServerError", "UnhandledPanic"} {
				want := 0
				if m == tc.logMsg {
					want = 1
				}
				if got := peLogCount(logger, m) - before[m]; got != want {
					t.Error(test.DiffMessage(got, want, desc+": log events named "+m))
				}
			}
		}
	}
}

func TestClientText_LoggedFields(t *testing.T) {
	app, logger := peApp(t, nil, nil, peAPI{})

	peDo(app, http.MethodGet, "/api/boom?k=cause")
	entries := logger.named("ExceptionCause")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "R24: ExceptionCause is logged once"))
	}
	e := entries[0]
	if e.level != "debug" {
		t.Error(test.DiffMessage(e.level, "debug", "R24: ExceptionCause is a Debug event"))
	}
	for key, want := range map[string]any{"method": http.MethodGet, "path": "/api/boom", "code": 400} {
		if got := e.field(key); got != want {
			t.Error(test.DiffMessage(got, want, "R24: ExceptionCause field "+key))
		}
	}
	if got := lcText(e.field("cause")); !strings.Contains(got, peSecretWords) {
		t.Error(test.DiffMessage(got, "the cause's text", "R24: ExceptionCause carries the cause"))
	}

	peDo(app, http.MethodGet, "/api/boom?k=internalcause")
	entries = logger.named("InternalServerError")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "R24: InternalServerError is logged once"))
	}
	e = entries[0]
	if e.level != "error" {
		t.Error(test.DiffMessage(e.level, "error", "R24: InternalServerError is an Error event"))
	}
	for key, want := range map[string]any{"method": http.MethodGet, "path": "/api/boom", "message": "db down"} {
		if got := e.field(key); got != want {
			t.Error(test.DiffMessage(got, want, "R24: InternalServerError field "+key))
		}
	}
	if got := lcText(e.field("cause")); !strings.Contains(got, "10.0.0.5") {
		t.Error(test.DiffMessage(got, "the cause's text", "R24: InternalServerError carries the cause"))
	}
}

func TestClientText_RenderFailure(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": peTextBody, "5xx.html": peTextBody}, "page")
	app, logger := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=render")
	peExpectHTML(t, "R24: a render failure", w, http.StatusInternalServerError)
	if want := "500|Internal Server Error|Internal Server Error"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R24: a render failure sends the fixed text"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R24: RenderFailed is logged"))
	}
}

func TestClientText_JSONShapeUnchanged(t *testing.T) {
	app, _ := peApp(t, nil, nil, peAPI{})
	for kind, want := range map[string]string{
		"wrapped":  `{"code":409,"error":"Conflict","message":"taken"}`,
		"notfound": `{"code":404,"error":"Not Found","message":"no such thing"}`,
		"internal": peFixedJSON,
		"cause":    `{"code":400,"error":"Bad Request","message":"bad input"}`,
	} {
		w := peDo(app, http.MethodGet, "/api/boom?k="+kind)
		lcExpectResponse(t, w, map[string]int{"wrapped": 409, "notfound": 404, "internal": 500, "cause": 400}[kind], want, "R24: the JSON shape and key order are unchanged ("+kind+")")
	}
}

type peBodyAPI struct{ common.HTTP }

func (c peBodyAPI) NewController() Controller {
	c.Prefix("in")
	return c
}

func (c peBodyAPI) CREATE_json(b ctx.Body) any { return ctx.Map{"got": b["k"]} }

func (c peBodyAPI) CREATE_form(f ctx.Form) any { return ctx.Map{"got": f.Get("k")} }

type pePageIn struct{ common.HTTP }

func (c pePageIn) NewController() Controller {
	c.Prefix("pin")
	return c
}

func (c pePageIn) RENDER_json() {}

func (c pePageIn) RENDER_form() {}

func (c pePageIn) SUBMIT_json(b ctx.Body) ctx.Map { return ctx.Map{"got": b["k"]} }

func (c pePageIn) SUBMIT_form(f ctx.Form) ctx.Map { return ctx.Map{"got": f.Get("k")} }

func pePost(app *App, target, contentType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func TestClientText_FrameworkMessages(t *testing.T) {
	files := map[string]string{"4xx.html": peTextBody, "pin/json.html": "OK", "pin/form.html": "OK"}
	app, logger := peApp(t, files, nil, peBodyAPI{}, pePageIn{})
	app.maxRequestBodyBytes.Store(64)
	big := strings.Repeat("a", 4096)
	tooLarge := http.StatusText(http.StatusRequestEntityTooLarge)

	w := pePost(app, "/in/json", "application/json", `{"k":"`+big+`"}`)
	want := exception.NewException("Request body too large", 413, tooLarge).AppendJSON(nil)
	lcExpectResponse(t, w, http.StatusRequestEntityTooLarge, string(want), "R24: a body over the limit")

	w = pePost(app, "/in/form", "application/x-www-form-urlencoded", "k="+big)
	lcExpectResponse(t, w, http.StatusRequestEntityTooLarge, string(want), "R24: a urlencoded form over the limit")

	w = pePost(app, "/pin/json", "application/json", `{"k":"`+big+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.String() != fmt.Sprintf("413|%s|Request body too large", tooLarge) {
		t.Error(test.DiffMessage(w.Body.String(), fmt.Sprintf("413|%s|Request body too large", tooLarge), "R24, R28: the same text in HTML"))
	}

	for _, entry := range logger.named("ExceptionCause") {
		if got := lcText(entry.field("cause")); !strings.Contains(got, "request body too large") {
			t.Error(test.DiffMessage(got, "http: request body too large", "R24: the limit error is kept as the logged cause"))
		}
	}
	if n := len(logger.named("ExceptionCause")); n != 3 {
		t.Error(test.DiffMessage(n, 3, "R24: each refusal logs its cause"))
	}

	app.maxRequestBodyBytes.Store(10 << 20)

	w = pePost(app, "/in/form", "application/x-www-form-urlencoded", "k=%zz")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"Invalid form body"}`, "R24: a malformed form keeps its message and loses the cause text in error")

	w = pePost(app, "/in/json", "application/json", `{"k":`)
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"Invalid JSON body"}`, "R24: a malformed JSON body loses the cause text in error")

	w = pePost(app, "/pin/form", "application/x-www-form-urlencoded", "k=%zz")
	if w.Code != http.StatusBadRequest || w.Body.String() != "400|Bad Request|Invalid form body" {
		t.Error(test.DiffMessage(w.Body.String(), "400|Bad Request|Invalid form body", "R24, R28: the same text in HTML"))
	}
}

func TestPageError_HTMLErrorIsNotCached(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{
		"layout.html": `<script nonce="{{nonce}}"></script>{{template "content" .}}`,
		"404.html":    `E{{.Code}}`,
	}), pgPages{})
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-cache" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Cache-Control")}, []any{404, "no-cache"}, "an HTML error page, which may carry a nonce, is never stored by a shared cache"))
	}
}
