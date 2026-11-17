package core

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/httphook"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
)

type hookFunc func(time.Duration)

func (f hookFunc) Done(elapsed time.Duration) { f(elapsed) }

type spyWriter struct {
	header   http.Header
	codes    []int
	written  []byte
	flushed  int
	readFrom int
	hijacked int
}

func newSpyWriter() *spyWriter { return &spyWriter{header: http.Header{}} }

func (s *spyWriter) Header() http.Header { return s.header }
func (s *spyWriter) WriteHeader(code int) {
	s.codes = append(s.codes, code)
}
func (s *spyWriter) Write(p []byte) (int, error) {
	s.written = append(s.written, p...)
	return len(p), nil
}
func (s *spyWriter) Flush() { s.flushed++ }
func (s *spyWriter) ReadFrom(r io.Reader) (int64, error) {
	s.readFrom++
	return io.Copy(io.Discard, r)
}
func (s *spyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	s.hijacked++
	return nil, nil, nil
}

type tagWriter struct{ http.ResponseWriter }

func (w tagWriter) Write(p []byte) (int, error) {
	w.Header().Set("X-Wrapped", "1")
	return w.ResponseWriter.Write(p)
}

func hkRegister(name string, onDone func(time.Duration)) {
	lcSetMiddleware(name, func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		hw := httphook.Of(w)
		if hw == nil {
			lcLog("nohook:" + name)
			next()
			return
		}
		hw.OnDone(hookFunc(onDone))
		next()
	})
}

func hkServe(app *App, rec *httptest.ResponseRecorder, method, target string) {
	app.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
}

func TestResponseWriter_SetInner_ForwardsEveryMethod(t *testing.T) {
	base := httptest.NewRecorder()
	var w responseWriter
	w.reset(base)

	var hw httphook.Writer = &w
	if hw.Inner() != http.ResponseWriter(base) {
		t.Error(test.DiffMessage(hw.Inner(), base, "K1: before SetInner, Inner is the writer the response goes to"))
	}

	spy := newSpyWriter()
	hw.SetInner(spy)

	w.Header().Set("X-Probe", "1")
	if spy.header.Get("X-Probe") != "1" || base.Header().Get("X-Probe") != "" {
		t.Error(test.DiffMessage(spy.header, "X-Probe on the inner writer's header", "K1: Header goes to the inner writer"))
	}
	w.WriteHeader(http.StatusCreated)
	if len(spy.codes) != 1 || spy.codes[0] != http.StatusCreated {
		t.Error(test.DiffMessage(spy.codes, []int{http.StatusCreated}, "K1: WriteHeader goes to the inner writer"))
	}
	if w.status() != http.StatusCreated {
		t.Error(test.DiffMessage(w.status(), http.StatusCreated, "K1: status() is the code the layer passed to WriteHeader"))
	}
	if _, err := w.Write([]byte("abc")); err != nil || string(spy.written) != "abc" {
		t.Error(test.DiffMessage(string(spy.written), "abc", "K1: Write goes to the inner writer"))
	}
	w.Flush()
	if spy.flushed != 1 {
		t.Error(test.DiffMessage(spy.flushed, 1, "K1: Flush goes to the inner writer"))
	}
	if _, err := w.ReadFrom(strings.NewReader("data")); err != nil || spy.readFrom != 1 {
		t.Error(test.DiffMessage(spy.readFrom, 1, "K1: ReadFrom goes to the inner writer"))
	}
	if _, _, err := w.Hijack(); err != nil || spy.hijacked != 1 {
		t.Error(test.DiffMessage(spy.hijacked, 1, "K1: Hijack goes to the inner writer"))
	}
	if w.Unwrap() != http.ResponseWriter(spy) {
		t.Error(test.DiffMessage(w.Unwrap(), spy, "K1: Unwrap returns the inner writer"))
	}
	if base.Body.Len() != 0 || base.Flushed {
		t.Error(test.DiffMessage(base.Body.Len(), 0, "K1: nothing reaches the original writer once an inner writer is set"))
	}
}

func TestResponseWriter_SetInner_ReadFromWithoutReaderFromFallsBack(t *testing.T) {
	base := httptest.NewRecorder()
	var w responseWriter
	w.reset(base)
	w.SetInner(tagWriter{base})
	if _, err := w.ReadFrom(strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if base.Body.String() != "hello" || base.Header().Get("X-Wrapped") != "1" {
		t.Error(test.DiffMessage(base.Body.String(), "hello", "K1: ReadFrom with an inner writer that has no ReaderFrom copies through its Write"))
	}
}

func TestHooks_RunOnceAfterLifecycle(t *testing.T) {
	app := lcApp(t)
	var calls int
	rec := httptest.NewRecorder()
	var bodyAtHook string
	hkRegister("g1", func(elapsed time.Duration) {
		calls++
		bodyAtHook = rec.Body.String()
		lcLog("hook")
		if elapsed <= 0 {
			t.Error(test.DiffMessage(elapsed, "> 0", "K2: the hook gets the time since the request started"))
		}
	})
	hkServe(app, rec, http.MethodGet, "/ok")
	if calls != 1 {
		t.Error(test.DiffMessage(calls, 1, "K2: a hook runs exactly once per exchange"))
	}
	if !strings.HasPrefix(bodyAtHook, "H") {
		t.Error(test.DiffMessage(bodyAtHook, "<the response body>", "K2a: the hook runs after the response was written"))
	}
	all := lcAll()
	if len(all) == 0 || all[len(all)-1] != "hook" {
		t.Error(test.DiffMessage(all, "[... handler hook]", "K2a: the hook runs after the request lifecycle returned"))
	}
	lcBefore(t, "handler", "hook", "K2a: the handler ran before the hook")
}

func TestHooks_RunInReverseOrder(t *testing.T) {
	app := lcApp(t)
	hkRegister("g1", func(time.Duration) { lcLog("hook:g1") })
	hkRegister("g2", func(time.Duration) { lcLog("hook:g2") })
	lcServe(app, http.MethodGet, "/ok")
	lcExpectEntries(t, lcEntries("hook:"), []string{"hook:g2", "hook:g1"}, "K2: last registered first")
}

func TestHooks_RunOnMiddlewarePanic(t *testing.T) {
	app := lcApp(t)
	rec := httptest.NewRecorder()
	var codeAtHook int
	var runs int
	lcSetMiddleware("g2", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		httphook.Of(w).OnDone(hookFunc(func(time.Duration) {
			runs++
			codeAtHook = rec.Code
		}))
		panic(exception.BadRequestException("refused"))
	})
	hkServe(app, rec, http.MethodGet, "/ok")
	if runs != 1 {
		t.Error(test.DiffMessage(runs, 1, "K2a: the hook runs when a middleware refuses the request"))
	}
	if codeAtHook != http.StatusBadRequest {
		t.Error(test.DiffMessage(codeAtHook, http.StatusBadRequest, "K2a: the error response is written before the hook runs"))
	}
}

func TestHooks_RunOnNoRoute(t *testing.T) {
	app := lcApp(t)
	rec := httptest.NewRecorder()
	var runs, codeAtHook int
	hkRegister("g1", func(time.Duration) {
		runs++
		codeAtHook = rec.Code
	})
	hkServe(app, rec, http.MethodGet, "/missing")
	if runs != 1 || codeAtHook != http.StatusNotFound {
		t.Error(test.DiffMessage([]int{runs, codeAtHook}, []int{1, http.StatusNotFound}, "K2a: the hook runs for a request with no route, after the 404"))
	}
}

func TestHooks_RunOnAbortHandler(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(http.ErrAbortHandler))
	var runs int
	hkRegister("g1", func(time.Duration) { runs++; lcLog("hook") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		lcServe(app, http.MethodGet, "/ok")
	}()
	if recovered != http.ErrAbortHandler {
		t.Error(test.DiffMessage(recovered, http.ErrAbortHandler, "K2c: http.ErrAbortHandler is still re-panicked"))
	}
	if runs != 1 {
		t.Error(test.DiffMessage(runs, 1, "K2c: the hook runs before http.ErrAbortHandler is re-panicked"))
	}
}

func TestHooks_RunAtWSHandshake(t *testing.T) {
	lcReset(t)
	app := New()
	app.SetLogger(&mockLogger{})
	app.EnableWS(&WSConfig{Path: "/ws"}, lcMiddleware{Name: "w1"})
	app.Create(ModuleBuilder().Controllers(lcController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()
	var runs atomic.Int32
	hkRegister("w1", func(time.Duration) { runs.Add(1) })

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if runs.Load() != 1 {
		t.Error(test.DiffMessage(runs.Load(), int32(1), "K2b: the hook runs when an accepted handshake ends"))
	}
}

func TestHooks_RunAtRefusedWSHandshake(t *testing.T) {
	lcReset(t)
	app := New()
	app.SetLogger(&mockLogger{})
	app.EnableWS(&WSConfig{Path: "/ws"}, lcMiddleware{Name: "w1"})
	app.Create(ModuleBuilder().Controllers(lcController{}).Build())
	server := httptest.NewServer(app)
	defer server.Close()
	var runs atomic.Int32
	lcSetMiddleware("w1", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		httphook.Of(w).OnDone(hookFunc(func(time.Duration) { runs.Add(1) }))
		panic(exception.ForbiddenException("no"))
	})

	if conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL); err == nil {
		_ = conn.Close()
		t.Fatal(test.DiffMessage(nil, "a refused handshake", "fixture: the middleware refuses the handshake"))
	}
	if runs.Load() != 1 {
		t.Error(test.DiffMessage(runs.Load(), int32(1), "K2b: the hook runs when a refused handshake ends"))
	}
}

func TestHooks_ClearedForNextRequest(t *testing.T) {
	app := lcApp(t)
	var runs int
	lcSetMiddleware("g1", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		hw := httphook.Of(w)
		hw.OnDone(hookFunc(func(time.Duration) { runs++ }))
		hw.SetInner(tagWriter{hw.Inner()})
		next()
	})
	first := lcServe(app, http.MethodGet, "/ok")
	if first.Header().Get("X-Wrapped") != "1" || runs != 1 {
		t.Fatal(test.DiffMessage([]any{first.Header().Get("X-Wrapped"), runs}, []any{"1", 1}, "fixture: the first request wrapped its writer and ran its hook"))
	}

	second := httptest.NewRecorder()
	var inner http.ResponseWriter
	lcSetMiddleware("g1", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		inner = httphook.Of(w).Inner()
		next()
	})
	hkServe(app, second, http.MethodGet, "/ok")
	if runs != 1 {
		t.Error(test.DiffMessage(runs, 1, "K3: the next request on the pooled exchange has no hook"))
	}
	if inner != http.ResponseWriter(second) {
		t.Error(test.DiffMessage(inner, second, "K3: the next request's inner writer is its own http.ResponseWriter"))
	}
	if second.Header().Get("X-Wrapped") != "" {
		t.Error(test.DiffMessage(second.Header().Get("X-Wrapped"), "", "K3: the writer wrapped by the first request is gone"))
	}
}

func TestHooks_BeforeCompleteEvent(t *testing.T) {
	app := lcNewApp(t, lcConfig{setup: func(app *App) {
		app.tracer.Listen(func(e trace.Event) {
			if e.Stage == trace.StageComplete {
				lcLog("complete")
			}
		})
	}})
	hkRegister("g1", func(time.Duration) { lcLog("hook") })
	lcServe(app, http.MethodGet, "/ok")
	lcBefore(t, "hook", "complete", "K2a: hooks run before the complete trace event")
}

func TestHooks_NonFrameworkWriterIsNil(t *testing.T) {
	if httphook.Of(httptest.NewRecorder()) != nil {
		t.Error(test.DiffMessage("a Writer", nil, "K5: a recorder is not the framework's writer"))
	}
	app := lcApp(t)
	lcSetMiddleware("g1", func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		if httphook.Of(w) == nil {
			lcLog("nil")
		}
		next()
	})
	lcServe(app, http.MethodGet, "/ok")
	if len(lcEntries("nil")) != 0 {
		t.Error(test.DiffMessage(lcEntries("nil"), []string{}, "K5: the writer every middleware receives is the framework's"))
	}
}

func TestServeHTTP_NoHook_NoAllocation(t *testing.T) {
	plain := benchApp(benchPlainController{})
	probing := New()
	probing.SetLogger(&mockLogger{})
	probing.BindGlobalMiddlewares(hookProbeMiddleware{})
	probing.Create(ModuleBuilder().Controllers(benchPlainController{}).Build())
	bare := New()
	bare.SetLogger(&mockLogger{})
	bare.BindGlobalMiddlewares(passMiddleware{})
	bare.Create(ModuleBuilder().Controllers(benchPlainController{}).Build())

	withProbe := allocsOfServe(probing, http.MethodGet, "/bench")
	without := allocsOfServe(bare, http.MethodGet, "/bench")
	if withProbe > without {
		t.Error(test.DiffMessage(withProbe, without, "K4: asking for the hook writer and registering none adds no allocation"))
	}
	if got := allocsOfServe(plain, http.MethodGet, "/bench"); got > 7 {
		t.Error(test.DiffMessage(got, "<= 7", "K4: with no hook registered the request path adds no allocation"))
	}
}

type hookProbeMiddleware struct{}

func (hookProbeMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	_ = httphook.Of(w)
	next()
}

type passMiddleware struct{}

func (passMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }

func TestServeHTTP_HookRegistered_StillServes(t *testing.T) {
	app := lcApp(t)
	hkRegister("g1", func(time.Duration) {})
	w := lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "H") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "K2: registering a hook does not change the response"))
	}
}
