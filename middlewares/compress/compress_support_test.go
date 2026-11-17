package compress

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/httphook"
	"github.com/dangduoc08/ginject/internal/test"
)

type terminal struct{ ID int }

var (
	terminals    sync.Map
	nextTerminal atomic.Int64
)

func (t terminal) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	fn, _ := terminals.Load(t.ID)
	fn.(func(http.ResponseWriter, *http.Request))(w, r)
}

func terminalMiddleware(fn func(http.ResponseWriter, *http.Request)) terminal {
	id := int(nextTerminal.Add(1))
	terminals.Store(id, fn)
	return terminal{ID: id}
}

func terminalApp(t testing.TB, c Compress, fn func(http.ResponseWriter, *http.Request)) *core.App {
	t.Helper()
	return newApp(t, c, terminalMiddleware(fn))
}

type varyBefore struct{ Value string }

func (v varyBefore) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Add("Vary", v.Value)
	next()
}

func payload(n int) []byte {
	unit := []byte(`{"id":1234,"name":"owlky","tags":["alpha","beta","gamma"],"ok":true},`)
	out := make([]byte, 0, n+len(unit))
	for len(out) < n {
		out = append(out, unit...)
	}
	return out[:n]
}

func gzipRequest(method string, headers ...string) *http.Request {
	return request(method, "/any", append([]string{"Accept-Encoding", "gzip"}, headers...)...)
}

func writeJSON(body []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func writeType(contentType string, body []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}
}

func resultOf(t testing.TB, w *httptest.ResponseRecorder) (*http.Response, []byte) {
	t.Helper()
	res := w.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

func gunzip(t testing.TB, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Z6: the body is not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("Z6: the gzip stream is broken: %v", err)
	}
	return out
}

func expectGzip(t *testing.T, w *httptest.ResponseRecorder, want []byte, desc string) {
	t.Helper()
	res, body := resultOf(t, w)
	if got := res.Header.Get("Content-Encoding"); got != "gzip" {
		t.Error(test.DiffMessage(got, "gzip", desc+": Content-Encoding"))
		return
	}
	if got := res.Header.Values("Content-Length"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "Z6: "+desc+": Content-Length is removed"))
	}
	if !bytes.Equal(gunzip(t, body), want) {
		t.Error(test.DiffMessage(len(gunzip(t, body)), len(want), "Z6: "+desc+": the body decodes to the bytes written"))
	}
}

func expectSmaller(t *testing.T, w *httptest.ResponseRecorder, original int, desc string) {
	t.Helper()
	_, body := resultOf(t, w)
	if len(body) >= original {
		t.Error(test.DiffMessage(len(body), fmt.Sprintf("< %d", original), "Z6: "+desc+": the compressed body is smaller"))
	}
}

func expectPlain(t *testing.T, w *httptest.ResponseRecorder, want []byte, desc string) {
	t.Helper()
	res, body := resultOf(t, w)
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Error(test.DiffMessage(got, "", desc+": Content-Encoding"))
	}
	if !bytes.Equal(body, want) {
		t.Error(test.DiffMessage(len(body), len(want), desc+": the body is sent as written"))
	}
}

func hasVaryToken(h http.Header, token string) bool {
	for _, value := range h.Values("Vary") {
		start := 0
		for i := 0; i <= len(value); i++ {
			if i == len(value) || value[i] == ',' {
				piece := value[start:i]
				for len(piece) > 0 && piece[0] == ' ' {
					piece = piece[1:]
				}
				for len(piece) > 0 && piece[len(piece)-1] == ' ' {
					piece = piece[:len(piece)-1]
				}
				if piece == token {
					return true
				}
				start = i + 1
			}
		}
	}
	return false
}

type exchange struct {
	inner http.ResponseWriter
	hooks []httphook.Hook
}

func newExchange(w http.ResponseWriter) *exchange { return &exchange{inner: w} }

func (e *exchange) Header() http.Header         { return e.inner.Header() }
func (e *exchange) Write(p []byte) (int, error) { return e.inner.Write(p) }
func (e *exchange) WriteHeader(code int)        { e.inner.WriteHeader(code) }
func (e *exchange) Flush() {
	if f, ok := e.inner.(http.Flusher); ok {
		f.Flush()
	}
}
func (e *exchange) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := e.inner.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{e.inner}, r)
}
func (e *exchange) Inner() http.ResponseWriter     { return e.inner }
func (e *exchange) SetInner(w http.ResponseWriter) { e.inner = w }
func (e *exchange) OnDone(h httphook.Hook)         { e.hooks = append(e.hooks, h) }
func (e *exchange) done() {
	hooks := e.hooks
	e.hooks = nil
	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i].Done(time.Millisecond)
	}
}

var _ common.MiddlewareFn = Compress{}
