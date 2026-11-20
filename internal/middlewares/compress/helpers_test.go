package compress_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/compress"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/httphook"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/test"
)

type outputCounter struct {
	header http.Header
	bytes  int64
}

func (w *outputCounter) Header() http.Header { return w.header }

func (w *outputCounter) WriteHeader(int) {}

func (w *outputCounter) Write(p []byte) (int, error) {
	w.bytes += int64(len(p))
	return len(p), nil
}

func benchCompress(b *testing.B, c Compress, body []byte, r *http.Request) {
	b.Helper()
	app := terminalApp(b, c, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = jsonType
		_, _ = w.Write(body)
	})
	w := &outputCounter{header: http.Header{}}
	b.ReportAllocs()
	cpu := processCPU()
	for b.Loop() {
		clear(w.header)
		app.ServeHTTP(w, r)
	}
	b.ReportMetric(float64(w.bytes)/float64(len(body))/float64(b.N), "bytes-out/byte-in")
	b.ReportMetric(float64(processCPU()-cpu)/float64(b.N), "cpu-ns/op")
}

type okController struct{ common.HTTP }

func (c okController) NewController() core.Controller { return c }

func (okController) READ_ok() string { return "ok" }

func (okController) CREATE_ok() string { return "ok" }

func (okController) UPDATE_ok() string { return "ok" }

func (okController) MODIFY_ok() string { return "ok" }

func (okController) DELETE_ok() string { return "ok" }

func (okController) PREFLIGHT_ok() string { return "options-route" }

type silentLogger struct{}

func (silentLogger) Debug(string, ...any) {}

func (silentLogger) Info(string, ...any) {}

func (silentLogger) Warn(string, ...any) {}

func (silentLogger) Error(string, ...any) {}

func (silentLogger) Fatal(string, ...any) {}

type probeMiddleware struct{ Name string }

type probed struct {
	request *http.Request
	context any
}

var probes sync.Map

func (p probeMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	probes.Store(p.Name, probed{request: r, context: r.Context()})
	next()
}

func probeOf(name string) probed {
	v, _ := probes.Load(name)
	p, _ := v.(probed)
	return p
}

type failBody struct{ t *testing.T }

func (b failBody) Read([]byte) (int, error) {
	b.t.Error("G3: the middleware read the request body")
	return 0, fmt.Errorf("body must not be read")
}

func (failBody) Close() error { return nil }

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header { return w.header }

func (w *discardWriter) WriteHeader(code int) { w.code = code }

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newApp(t testing.TB, mws ...common.MiddlewareFn) *core.App {
	t.Helper()
	app := core.New(&core.AppOptions{Logger: silentLogger{}})
	app.BindGlobalMiddlewares(mws...)
	app.Create(owlkei.ModuleBuilder().Controllers(okController{}).Build())
	return app
}

func serve(app *core.App, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func request(method, target string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func panicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			text, panicked = fmt.Sprint(r), true
		}
	}()
	fn()
	return "", false
}

func field(s inspect.Snapshot, key string) any {
	for _, f := range s.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}

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
	unit := []byte(`{"id":1234,"name":"owlkei","tags":["alpha","beta","gamma"],"ok":true},`)
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

func (e *exchange) Header() http.Header { return e.inner.Header() }

func (e *exchange) Write(p []byte) (int, error) { return e.inner.Write(p) }

func (e *exchange) WriteHeader(code int) { e.inner.WriteHeader(code) }

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

func (e *exchange) Inner() http.ResponseWriter { return e.inner }

func (e *exchange) SetInner(w http.ResponseWriter) { e.inner = w }

func (e *exchange) OnDone(h httphook.Hook) { e.hooks = append(e.hooks, h) }

func (e *exchange) done() {
	hooks := e.hooks
	e.hooks = nil
	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i].Done(time.Millisecond)
	}
}

var _ common.MiddlewareFn = Compress{}

type bigController struct{ common.HTTP }

func (c bigController) NewController() core.Controller { return c }

func (bigController) READ_big() map[string]string {
	return map[string]string{"data": strings.Repeat("ok-ok ", 1000)}
}

type readerFromWriter struct {
	header   http.Header
	body     bytes.Buffer
	code     int
	readFrom int
}

func (w *readerFromWriter) Header() http.Header { return w.header }

func (w *readerFromWriter) WriteHeader(code int) { w.code = code }

func (w *readerFromWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

func (w *readerFromWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom++
	return io.Copy(&w.body, r)
}

type fileController struct{ common.HTTP }

var fileRoot string

func (c fileController) NewController() core.Controller { return c }

func (c fileController) SERVE_ANY_OF_files() string { return fileRoot }

var jsonType = []string{"application/json"}
