package ctx

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestClientText_BodyOverLimitMessage(t *testing.T) {
	c := newBodyContext(applicationJSON, `{"a":"`+strings.Repeat("x", 100)+`"}`)
	c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 10)

	exc, err := panicException(t, func() { c.Body() })

	if exc.Code() != http.StatusRequestEntityTooLarge || exc.Message() != "Request body too large" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusRequestEntityTooLarge, "Request body too large"}, "R24: a body over the limit sends the framework's own words"))
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "R24: the limit error stays as the cause"))
	}
	if exc.StatusText() != http.StatusText(http.StatusRequestEntityTooLarge) {
		t.Error(test.DiffMessage(exc.StatusText(), http.StatusText(http.StatusRequestEntityTooLarge), "R24: the status text"))
	}
}

func TestClientText_FormOverLimitMessage(t *testing.T) {
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

		if exc.Code() != http.StatusRequestEntityTooLarge || exc.Message() != "Request body too large" {
			t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusRequestEntityTooLarge, "Request body too large"}, "R24: a "+name+" form over the limit sends the framework's own words"))
		}
		var tooLarge *http.MaxBytesError
		if !errors.As(err, &tooLarge) {
			t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "R24: the limit error stays as the cause of a "+name+" form"))
		}
	}
}

func TestClientText_FileOverLimitMessage(t *testing.T) {
	c, _ := newUploadContext(t, "avatar", "photo.png", strings.Repeat("x", 500))
	c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 50)

	exc, _ := panicException(t, func() { c.File() })

	if exc.Code() != http.StatusRequestEntityTooLarge || exc.Message() != "Request body too large" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusRequestEntityTooLarge, "Request body too large"}, "R24: an upload over the limit sends the framework's own words"))
	}
}

func TestClientText_BrokenUploadMessage(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("avatar", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("hello-bytes"))
	_ = mw.Close()

	form, err := multipart.NewReader(&buf, mw.Boundary()).ReadForm(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := form.RemoveAll(); err != nil {
		t.Fatal(err)
	}

	exc, cause := panicException(t, func() { File(form.File).Bind(fileHandlerDTO{}) })

	if exc.Code() != http.StatusBadRequest || exc.Message() != "Invalid file upload" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Invalid file upload"}, "R24: a file that cannot be opened sends the framework's own words"))
	}
	if errors.Unwrap(exc) == nil || cause == nil {
		t.Error(test.DiffMessage(errors.Unwrap(exc), "<the open error>", "R24: the error stays as the cause"))
	}
}
