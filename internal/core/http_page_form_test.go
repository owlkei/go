package core

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/middlewares/csrf"
)

const (
	pfURLEncoded = "application/x-www-form-urlencoded"
	pfHostile    = `"><script>alert(1)</script>`
)

type pfBooking struct {
	Name   string `bind:"name"`
	Email  string `bind:"email"`
	Errors map[string]string
}

func (b pfBooking) Transform(f ctx.Form, _ common.ArgumentMetadata) any {
	bound, fls := f.Bind(b)
	dto := bound.(pfBooking)
	dto.Errors = map[string]string{}
	for _, fl := range fls {
		if !fl.IsValue() {
			dto.Errors[fl.Tag()] = fl.Tag() + " is required"
		}
	}
	if dto.Errors["name"] == "" && strings.TrimSpace(dto.Name) == "" {
		dto.Errors["name"] = "name is required"
	}
	if dto.Errors["email"] == "" && !strings.Contains(dto.Email, "@") {
		dto.Errors["email"] = "email must contain @"
	}
	return dto
}

type pfPages struct{ common.Page }

func (c pfPages) NewController() Controller {
	c.Prefix("bookings")
	return c
}

func (c pfPages) READ_new() common.View {
	return common.View{Name: "form", Data: pfBooking{Errors: map[string]string{}}}
}

func (c pfPages) READ_goto(h *ctx.HTTPContext) common.View {
	h.Redirect("/bookings")
	return common.View{Name: "marker"}
}

func (c pfPages) CREATE_new(h *ctx.HTTPContext, form pfBooking) common.View {
	if len(form.Errors) > 0 {
		h.Status(http.StatusUnprocessableEntity)
		return common.View{Name: "form", Data: form}
	}
	h.Redirect("/bookings")
	return common.View{Name: "marker"}
}

func (c pfPages) CREATE_viaparam(redirect ctx.Redirect, form pfBooking) common.View {
	if len(form.Errors) > 0 {
		return common.View{Name: "form", Data: form}
	}
	redirect("/bookings")
	return common.View{Name: "marker"}
}

func (c pfPages) CREATE_explicit(h *ctx.HTTPContext, _ pfBooking) common.View {
	h.Status(http.StatusTemporaryRedirect)
	h.Redirect("/bookings")
	return common.View{Name: "marker"}
}

func (c pfPages) CREATE_found(h *ctx.HTTPContext, _ pfBooking) common.View {
	h.Status(http.StatusFound)
	h.Redirect("/bookings")
	return common.View{Name: "marker"}
}

func (c pfPages) CREATE_echo(form pfBooking) common.View {
	return common.View{Name: "echo", Data: form}
}

type pfAPI struct{ common.HTTP }

func (c pfAPI) NewController() Controller {
	c.Prefix("api")
	return c
}

func (c pfAPI) READ_goto(h *ctx.HTTPContext) string {
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_new(h *ctx.HTTPContext, _ pfBooking) string {
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_explicit(h *ctx.HTTPContext, _ pfBooking) string {
	h.Status(http.StatusTemporaryRedirect)
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_echo(form pfBooking) ctx.Map {
	return ctx.Map{"name": form.Name, "email": form.Email}
}

func pfFiles() map[string]string {
	return map[string]string{
		"layout.html": `<html><body>{{template "content" .}}</body></html>`,
		"form.html": `<form method="post" action="/bookings/new">` +
			`<input name="name" value="{{.Name}}">{{with .Errors.name}}<p class="err">{{.}}</p>{{end}}` +
			`<input name="email" value="{{.Email}}">{{with .Errors.email}}<p class="err">{{.}}</p>{{end}}</form>`,
		"echo.html":   `name={{.Name}};email={{.Email}}`,
		"marker.html": `MARKER`,
	}
}

func pfApp(t *testing.T, setup func(*App)) (*App, *lcRecordingLogger) {
	return peApp(t, pfFiles(), setup, pfPages{}, pfAPI{})
}

func pfPost(app *App, target, contentType, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func pfMultipart(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), buf.String()
}

func pfEncode(fields map[string]string) string {
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	return v.Encode()
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
			t.Error(test.DiffMessage(page.Code, http.StatusOK, "R6, R25: a POST page answers 200 ("+name+")"))
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
			t.Error(test.DiffMessage(w.Body.String(), "no body from the returned View", "R27: the View is ignored ("+name+")"))
		}
	}

	w := peDo(app, http.MethodGet, "/bookings/goto")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R27, D4: a GET page redirects with 303 too"))
	}
	if strings.Contains(w.Body.String(), "MARKER") {
		t.Error(test.DiffMessage(w.Body.String(), "no View body", "R27: the View is ignored on GET"))
	}

	w = pfPost(app, "/bookings/viaparam", pfURLEncoded, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "name is required") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R6, R27: the invalid branch of the same handler is a 200 page"))
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
	app, _ := pfApp(t, nil)

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

	app.maxRequestBodyBytes.Store(64)
	w = pfPost(app, "/bookings/new", pfURLEncoded, "name="+strings.Repeat("a", 4096))
	peExpectHTML(t, "R28: a body over MaxRequestBodyBytes", w, http.StatusRequestEntityTooLarge)
	if body := w.Body.String(); !strings.Contains(body, "Request body too large") || strings.Contains(body, "http: request body too large") {
		t.Error(test.DiffMessage(body, "Request body too large", "R28, R24: the framework message"))
	}
}

func TestPageForm_CSRF(t *testing.T) {
	app, _ := pfApp(t, func(app *App) { app.BindGlobalMiddlewares(csrf.CSRF{}) })
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
