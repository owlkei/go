package core

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type multipartController struct {
	common.HTTP
}

func (c multipartController) NewController() Controller { return c }

func (c multipartController) CREATE_ok(h *ctx.HTTPContext) string {
	h.Form()
	return "ok"
}

func (c multipartController) CREATE_boom(h *ctx.HTTPContext) string {
	h.Form()
	panic("boom after parsing the form")
}

func (c multipartController) CREATE_abort(h *ctx.HTTPContext) string {
	h.Form()
	panic(http.ErrAbortHandler)
}

type quietLogger struct{}

func (quietLogger) Debug(string, ...any) {}
func (quietLogger) Info(string, ...any)  {}
func (quietLogger) Warn(string, ...any)  {}
func (quietLogger) Error(string, ...any) {}
func (quietLogger) Fatal(string, ...any) {}

func largeMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(part, zeroReader{}, 33<<20); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func multipartTempFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestMultipart_TempFilesRemovedAfterRequest(t *testing.T) {
	for _, route := range []string{"/ok", "/boom", "/abort"} {
		dir := t.TempDir()
		t.Setenv("TMPDIR", dir)
		app := New()
		app.SetLogger(quietLogger{})
		app.maxRequestBodyBytes.Store(64 << 20)
		app.Create(ModuleBuilder().Controllers(multipartController{}).Build())

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

// serveRecovering serves r and swallows http.ErrAbortHandler, which core panics again for
// net/http to abort the connection.
func serveRecovering(app *App, w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
			panic(rec)
		}
	}()
	app.ServeHTTP(w, r)
}
