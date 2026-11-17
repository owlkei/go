package ctx

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestHTTPContext_FormURLEncoded(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader("foo=bar"))
	c.Request.Header.Set("Content-Type", applicationXWWWFormUrlencoded)

	f := c.Form()
	if f.Get("foo") != "bar" {
		t.Error(test.DiffMessage(f.Get("foo"), "bar", "Form should parse a urlencoded body"))
	}
}

func TestHTTPContext_FormMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", &buf)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())

	f := c.Form()
	if f.Get("foo") != "bar" {
		t.Error(test.DiffMessage(f.Get("foo"), "bar", "Form should parse a multipart body"))
	}
}

func TestHTTPContext_FormNoContentType(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)

	f := c.Form()
	if len(f) != 0 {
		t.Error(test.DiffMessage(f, Form{}, "Form should be empty when the content type is neither urlencoded nor multipart"))
	}
}

func TestHTTPContext_FormIsCached(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader("foo=bar"))
	c.Request.Header.Set("Content-Type", applicationXWWWFormUrlencoded)

	f1 := c.Form()
	f1.Set("foo", "mutated")
	f2 := c.Form()

	if f2.Get("foo") != "mutated" {
		t.Error(test.DiffMessage(f2.Get("foo"), "mutated", "Form should be cached across calls on the same context"))
	}
}

func TestForm_GetMissingKey(t *testing.T) {
	f := Form{}
	if f.Get("missing") != "" {
		t.Error(test.DiffMessage(f.Get("missing"), "", "Get on missing key should return empty string"))
	}
}

func TestForm_SetAddDelHas(t *testing.T) {
	f := Form{}
	f.Set("a", "1")
	if f.Get("a") != "1" {
		t.Error(test.DiffMessage(f.Get("a"), "1", "Set should store the value"))
	}

	f.Add("a", "2")
	if got := f["a"]; len(got) != 2 || got[1] != "2" {
		t.Error(test.DiffMessage(got, []string{"1", "2"}, "Add should append to the existing values"))
	}

	if !f.Has("a") {
		t.Error(test.DiffMessage(f.Has("a"), true, "Has should be true after Set"))
	}

	f.Del("a")
	if f.Has("a") {
		t.Error(test.DiffMessage(f.Has("a"), false, "Has should be false after Del"))
	}
}

type formBindDTO struct {
	Name string `bind:"name"`
}

func TestForm_Bind(t *testing.T) {
	f := Form{"name": {"joe"}}
	result, fls := f.Bind(formBindDTO{})

	dto, ok := result.(formBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, formBindDTO{}, "Bind should return a formBindDTO"))
	}
	if dto.Name != "joe" {
		t.Error(test.DiffMessage(dto.Name, "joe", "Bind should populate fields from form values"))
	}
	if len(fls) != 1 {
		t.Error(test.DiffMessage(len(fls), 1, "Bind should report a FieldLevel per bound field"))
	}
}

func multipartFieldBody(t *testing.T, name, value string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(name, value); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func newFormContext(contentType string, body io.Reader) *HTTPContext {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", body)
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

func TestHTTPContext_FormContentTypeCaseInsensitive(t *testing.T) {
	for _, ct := range []string{
		"Application/X-WWW-Form-Urlencoded",
		"APPLICATION/X-WWW-FORM-URLENCODED; charset=UTF-8",
		"application/x-www-form-urlencoded ; charset=utf-8",
		"  application/x-www-form-urlencoded ",
	} {
		c := newFormContext(ct, strings.NewReader("foo=bar"))
		if got := c.Form().Get("foo"); got != "bar" {
			t.Error(test.DiffMessage(got, "bar", "B10: Content-Type "+ct+" should be parsed as a form"))
		}
	}

	buf, ct := multipartFieldBody(t, "foo", "bar")
	ct = strings.Replace(ct, "multipart/form-data", "Multipart/Form-Data", 1)
	c := newFormContext(ct, buf)
	if got := c.Form().Get("foo"); got != "bar" {
		t.Error(test.DiffMessage(got, "bar", "B10: Content-Type "+ct+" should be parsed as a multipart form"))
	}
}

func TestHTTPContext_FormContentTypeParameterIsNotMediaType(t *testing.T) {
	for _, ct := range []string{"text/plain; note=application/x-www-form-urlencoded", "application/json; x=multipart/form-data", "application/x-www-form-urlencodedx"} {
		c := newFormContext(ct, strings.NewReader("foo=bar"))
		var f Form
		if !noPanic(t, "B10 Form with Content-Type "+ct, func() { f = c.Form() }) {
			continue
		}
		if f == nil || len(f) != 0 {
			t.Error(test.DiffMessage(f, Form{}, "B10: Content-Type "+ct+" is not a form, so the Form should be empty and not nil"))
		}
	}
}

func TestHTTPContext_FormMalformedIsBadRequest(t *testing.T) {
	c := newFormContext(applicationXWWWFormUrlencoded, strings.NewReader("a=%zz"))

	exc, err := panicException(t, func() { c.Form() })

	if exc.Code() != http.StatusBadRequest || exc.Message() != "Invalid form body" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Invalid form body"}, "B11: a malformed form should be a bad request"))
	}
	var escape url.EscapeError
	if !errors.As(err, &escape) {
		t.Error(test.DiffMessage(err, "<url.EscapeError in the chain>", "B11: errors.As should reach the cause"))
	}
}

func TestHTTPContext_FormTooLargeIs413(t *testing.T) {
	bodies := map[string]func() *HTTPContext{
		"urlencoded": func() *HTTPContext {
			return newFormContext(applicationXWWWFormUrlencoded, strings.NewReader("a="+strings.Repeat("x", 200)))
		},
		"multipart": func() *HTTPContext {
			buf, ct := multipartFieldBody(t, "a", strings.Repeat("x", 200))
			return newFormContext(ct, buf)
		},
	}
	for name, build := range bodies {
		c := build()
		c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 20)

		exc, err := panicException(t, func() { c.Form() })

		if exc.Code() != http.StatusRequestEntityTooLarge {
			t.Error(test.DiffMessage(exc.Code(), http.StatusRequestEntityTooLarge, "B11: a "+name+" form over the limit should panic with code 413"))
		}
		var tooLarge *http.MaxBytesError
		if !errors.As(err, &tooLarge) {
			t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "B11: errors.As should reach the limit error for a "+name+" form"))
		}
	}
}

func TestHTTPContext_FormBrokenMultipartIsBadRequest(t *testing.T) {
	bodies := map[string]string{
		"garbage":   "garbage",
		"bad part":  "--xyz\r\nbad",
		"truncated": "--xyz\r\nContent-Disposition: form-data; name=\"a\"\r\n\r\nvalue",
	}
	for name, payload := range bodies {
		c := newFormContext("multipart/form-data; boundary=xyz", strings.NewReader(payload))

		exc, _ := panicException(t, func() { c.Form() })

		if exc.Code() != http.StatusBadRequest || exc.Message() != "Invalid form body" {
			t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Invalid form body"}, "B11: a broken multipart body ("+name+") should be a bad request"))
		}
	}
}

func TestHTTPContext_FormMissingBoundaryIsBadRequest(t *testing.T) {
	c := newFormContext("multipart/form-data", strings.NewReader("--xyz--"))
	exc, _ := panicException(t, func() { c.Form() })
	if exc.Code() != http.StatusBadRequest {
		t.Error(test.DiffMessage(exc.Code(), http.StatusBadRequest, "B11: a multipart form without a boundary should be a bad request"))
	}
}

func TestHTTPContext_FormNeverNil(t *testing.T) {
	requests := map[string]*HTTPContext{
		"no content type": newFormContext("", nil),
		"json":            newFormContext(applicationJSON, strings.NewReader(`{"a":1}`)),
		"text":            newFormContext("text/plain", strings.NewReader("a=1")),
	}
	for name, c := range requests {
		var f Form
		if !noPanic(t, "B12 Form of a "+name+" request", func() { f = c.Form() }) {
			continue
		}
		if f == nil {
			t.Error(test.DiffMessage(f, Form{}, "B12: Form should never be nil ("+name+")"))
			continue
		}
		noPanic(t, "B12 Set on the Form of a "+name+" request", func() { f.Set("k", "v") })
	}
}

func TestHTTPContext_FormNoContentTypeIsCached(t *testing.T) {
	c := newFormContext("", nil)

	var f2 Form
	noPanic(t, "B12 Set on the Form without a form content type", func() {
		f1 := c.Form()
		f1.Set("foo", "mutated")
		f2 = c.Form()
	})

	if f2.Get("foo") != "mutated" {
		t.Error(test.DiffMessage(f2.Get("foo"), "mutated", "B12: Form should be computed once, even without a form content type"))
	}
}

func TestForm_BindValueFromRequest(t *testing.T) {
	c := newFormContext(applicationXWWWFormUrlencoded, strings.NewReader("name=joe"))
	got := c.Form().BindValue(formBindDTO{})
	if got.(formBindDTO).Name != "joe" {
		t.Error(test.DiffMessage(got, formBindDTO{Name: "joe"}, "BindValue should bind the parsed form"))
	}
}
