package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type bodyProbeController struct {
	common.HTTP
}

func (ctl bodyProbeController) NewController() Controller { return ctl }
func (ctl bodyProbeController) READ_probe(c *ctx.HTTPContext) string {
	switch c.Request.Body {
	case nil:
		return "nil"
	case http.NoBody:
		return "nobody"
	}
	return "wrapped"
}

func bodyProbe(t *testing.T, mutate func(*http.Request)) string {
	t.Helper()
	app := New()
	app.Create(ModuleBuilder().Controllers(bodyProbeController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w.Body.String()
}

func TestBodyLimit_NoBodyNotWrapped(t *testing.T) {
	if got := bodyProbe(t, nil); got != "nobody" {
		t.Error(test.DiffMessage(got, "nobody", "bug 12, B1: a request with http.NoBody is not wrapped"))
	}
}

func TestBodyLimit_NilBodyNotWrapped(t *testing.T) {
	got := bodyProbe(t, func(r *http.Request) { r.Body = nil })
	if got != "nil" {
		t.Error(test.DiffMessage(got, "nil", "B1: a request with a nil body is not wrapped"))
	}
}

func TestBodyLimit_BodyIsWrapped(t *testing.T) {
	got := bodyProbe(t, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("data")) })
	if got != "wrapped" {
		t.Error(test.DiffMessage(got, "wrapped", "B1: a request with a body is capped"))
	}
}

func TestBodyLimit_DeclaredOverLimit413(t *testing.T) {
	app := newBodyLimitApp(t, 64)
	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if r.ContentLength != int64(len(payload)) {
		t.Fatal(test.DiffMessage(r.ContentLength, len(payload), "fixture: the request declares its length"))
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "B1: a declared length over the limit answers 413"))
	}
}

func TestBodyLimit_ChunkedOver413(t *testing.T) {
	app := newBodyLimitApp(t, 64)
	payload := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(payload)))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "B1: a chunked body over the limit answers 413"))
	}
}

func TestBodyLimit_BodyAtTheLimitIsServed(t *testing.T) {
	payload := `{"k":"ok"}`
	app := newBodyLimitApp(t, int64(len(payload)))
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B1: a body of exactly the limit is served"))
	}
}
