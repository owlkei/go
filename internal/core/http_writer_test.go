package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/httphook"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/trace"
	"golang.org/x/net/websocket"
)

func TestResponseWriter_TracksWriteAndWriteHeader(t *testing.T) {
	var found bool
	var before, afterHeader, afterSecond int
	wrServe(t, httptest.NewRecorder(), func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		found = true
		before = rw.status()
		rw.WriteHeader(http.StatusNoContent)
		afterHeader = rw.status()
		rw.WriteHeader(http.StatusTeapot)
		afterSecond = rw.status()
		return nil
	})

	if !found {
		t.Fatal(test.DiffMessage("<other writer>", "*responseWriter", "E4: layers receive the tracking writer"))
	}
	if before != 0 {
		t.Error(test.DiffMessage(before, 0, "E4: status() is 0 before anything is written"))
	}
	if afterHeader != http.StatusNoContent {
		t.Error(test.DiffMessage(afterHeader, http.StatusNoContent, "E4: status() is the code of WriteHeader"))
	}
	if afterSecond != http.StatusNoContent {
		t.Error(test.DiffMessage(afterSecond, http.StatusNoContent, "E4: a second WriteHeader does not change the status sent"))
	}
}

func TestResponseWriter_WriteCountsAsStatus200(t *testing.T) {
	var after int
	var n int
	w := httptest.NewRecorder()
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		n, _ = rw.Write([]byte("abc"))
		after = rw.status()
		return nil
	})

	if after != http.StatusOK {
		t.Error(test.DiffMessage(after, http.StatusOK, "E4: a write without WriteHeader sends 200"))
	}
	if n != 3 {
		t.Error(test.DiffMessage(n, 3, "E4: Write returns the count of the underlying writer"))
	}
	if !strings.HasPrefix(w.Body.String(), "abc") {
		t.Error(test.DiffMessage(w.Body.String(), "abc...", "E4: Write reaches the underlying writer"))
	}
}

func TestResponseWriter_InformationalStatusDoesNotCount(t *testing.T) {
	var found bool
	var after int
	w := &lcStrictWriter{}
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		found = true
		rw.WriteHeader(http.StatusEarlyHints)
		after = rw.status()
		return nil
	})

	if !found {
		t.Fatal(test.DiffMessage("<other writer>", "*responseWriter", "E4: layers receive the tracking writer"))
	}
	if after != 0 {
		t.Error(test.DiffMessage(after, 0, "E4: a status under 200 is not a response"))
	}
	if len(w.infos) != 1 || w.infos[0] != http.StatusEarlyHints {
		t.Error(test.DiffMessage(w.infos, []int{http.StatusEarlyHints}, "E4: the informational status still reaches the underlying writer"))
	}
}

func TestResponseWriter_MiddlewareAndHandlerShareTheWriter(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	var fromMiddleware, fromHandler http.ResponseWriter
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, next ctx.Next) {
		fromMiddleware = w
		next()
	})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		fromHandler = c.ResponseWriter
		return "H"
	})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bare", nil))

	if _, ok := fromMiddleware.(*responseWriter); !ok {
		t.Fatal(test.DiffMessage(fromMiddleware, "*responseWriter", "E4: middlewares receive the tracking writer"))
	}
	if fromMiddleware != fromHandler {
		t.Error(test.DiffMessage(fromHandler, fromMiddleware, "E4: every layer receives the same writer"))
	}
}

func TestResponseWriter_UnwrapWorksWithResponseController(t *testing.T) {
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	deadline := time.Unix(1700000000, 0)
	var flushErr, deadlineErr error
	var unwrapped http.ResponseWriter
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		if rw, ok := c.ResponseWriter.(*responseWriter); ok {
			unwrapped = rw.Unwrap()
		}
		rc := http.NewResponseController(c.ResponseWriter)
		flushErr = rc.Flush()
		deadlineErr = rc.SetWriteDeadline(deadline)
		return nil
	})

	if unwrapped != http.ResponseWriter(underlying) {
		t.Error(test.DiffMessage(unwrapped, underlying, "Unwrap returns the server's writer"))
	}
	if flushErr != nil || underlying.flushes < 1 {
		t.Error(test.DiffMessage([]any{flushErr, underlying.flushes}, []any{nil, "at least 1"}, "http.ResponseController reaches Flush of the server's writer"))
	}
	if deadlineErr != nil || len(underlying.deadlines) != 1 || !underlying.deadlines[0].Equal(deadline) {
		t.Error(test.DiffMessage([]any{deadlineErr, underlying.deadlines}, []any{nil, []time.Time{deadline}}, "http.ResponseController reaches SetWriteDeadline of the server's writer"))
	}
}

func TestResponseWriter_FlushHijackReadFromForward(t *testing.T) {
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	var flusherOK, hijackerOK, readerFromOK bool
	var hijackErr error
	var copied int64
	var status int
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		flusher, ok := c.ResponseWriter.(http.Flusher)
		flusherOK = ok
		if ok {
			flusher.Flush()
		}
		hijacker, ok := c.ResponseWriter.(http.Hijacker)
		hijackerOK = ok
		if ok {
			_, _, hijackErr = hijacker.Hijack()
		}
		readerFrom, ok := c.ResponseWriter.(io.ReaderFrom)
		readerFromOK = ok
		if ok {
			copied, _ = readerFrom.ReadFrom(strings.NewReader("abc"))
		}
		if rw, ok := c.ResponseWriter.(*responseWriter); ok {
			status = rw.status()
		}
		return nil
	})

	if !flusherOK || !hijackerOK || !readerFromOK {
		t.Fatal(test.DiffMessage([]bool{flusherOK, hijackerOK, readerFromOK}, []bool{true, true, true}, "the writer implements http.Flusher, http.Hijacker and io.ReaderFrom"))
	}
	if underlying.flushes < 1 {
		t.Error(test.DiffMessage(underlying.flushes, "at least 1", "Flush forwards when the server supports it"))
	}
	if underlying.hijacks != 1 || hijackErr != errWriterHijack {
		t.Error(test.DiffMessage([]any{underlying.hijacks, hijackErr}, []any{1, errWriterHijack}, "Hijack forwards to the server's writer"))
	}
	if underlying.readFroms != 1 || copied != 3 {
		t.Error(test.DiffMessage([]any{underlying.readFroms, copied}, []any{1, int64(3)}, "ReadFrom forwards to the server's writer"))
	}
	if status != http.StatusOK {
		t.Error(test.DiffMessage(status, http.StatusOK, "E4: ReadFrom counts as a response"))
	}
	if !strings.HasPrefix(underlying.Body.String(), "abc") {
		t.Error(test.DiffMessage(underlying.Body.String(), "abc...", "ReadFrom writes the bytes"))
	}
}

func TestResponseWriter_HijackWithoutSupportReturnsAnError(t *testing.T) {
	var hijackerOK bool
	var err error
	wrServe(t, &lcStrictWriter{}, func(c *ctx.HTTPContext) any {
		hijacker, ok := c.ResponseWriter.(http.Hijacker)
		hijackerOK = ok
		if ok {
			_, _, err = hijacker.Hijack()
		}
		return nil
	})

	if !hijackerOK {
		t.Fatal(test.DiffMessage(false, true, "the writer implements http.Hijacker"))
	}
	if err == nil {
		t.Error(test.DiffMessage(err, "an error", "Hijack on a server writer that cannot hijack returns an error"))
	}
}

func TestResponseWriter_FlushWithoutSupportDoesNothing(t *testing.T) {
	var flusherOK bool
	wrServe(t, &lcStrictWriter{}, func(c *ctx.HTTPContext) any {
		if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
			flusherOK = true
			flusher.Flush()
		}
		return nil
	})

	if !flusherOK {
		t.Error(test.DiffMessage(false, true, "the writer implements http.Flusher and does not panic without support"))
	}
}

func TestResponseWriter_ServeFileKeepsReadFrom(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		http.ServeFile(c.ResponseWriter, c.Request, file)
		return nil
	})

	if underlying.readFroms < 1 {
		t.Error(test.DiffMessage(underlying.readFroms, "at least 1", "http.ServeFile reaches ReadFrom of the server's writer (sendfile)"))
	}
	if !strings.HasPrefix(underlying.Body.String(), "file body") {
		t.Error(test.DiffMessage(underlying.Body.String(), "file body...", "http.ServeFile writes the file"))
	}
}

func TestResponseWriter_ResetDropsLateWrites(t *testing.T) {
	var late http.ResponseWriter
	w := httptest.NewRecorder()
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		late = c.ResponseWriter
		return "H"
	})
	bodyBefore, codeBefore := w.Body.String(), w.Code

	var rec any
	func() {
		defer func() { rec = recover() }()
		_, _ = late.Write([]byte("late"))
		late.WriteHeader(http.StatusTeapot)
		if flusher, ok := late.(http.Flusher); ok {
			flusher.Flush()
		}
	}()

	if rec != nil {
		t.Error(test.DiffMessage(rec, nil, "a late write on an idle exchange does not panic"))
	}
	if w.Body.String() != bodyBefore || w.Code != codeBefore {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{codeBefore, bodyBefore}, "a late write on an idle exchange is dropped"))
	}
}

func TestResponseWriter_HooksEndTheResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	var w responseWriter
	w.reset(rec)
	pooled := httptest.NewRecorder()
	w.SetInner(pooled)
	w.OnDone(pooledHook{})
	start := time.Now()
	w.runHooks(&start)

	if _, err := w.Write([]byte("late")); !errors.Is(err, errResponseEnded) {
		t.Error(test.DiffMessage(err, errResponseEnded, "a write after the hooks ran is refused"))
	}
	w.WriteHeader(http.StatusTeapot)
	if pooled.Body.Len() != 0 || pooled.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Error(test.DiffMessage(pooled.Body.String(), "", "a late write reaches neither a writer a hook pooled nor the connection"))
	}
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
	app := New(&AppOptions{Logger: &mockLogger{}, WS: &WSOptions{Path: "/ws"}})
	app.BindGlobalMiddlewares(lcMiddleware{Name: "w1"})
	app.Create(NewModuleBuilder().Controllers(lcController{}).Build())
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
	app := New(&AppOptions{Logger: &mockLogger{}, WS: &WSOptions{Path: "/ws"}})
	app.BindGlobalMiddlewares(lcMiddleware{Name: "w1"})
	app.Create(NewModuleBuilder().Controllers(lcController{}).Build())
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
	probing := New(&AppOptions{Logger: &mockLogger{}})
	probing.BindGlobalMiddlewares(hookProbeMiddleware{})
	probing.Create(NewModuleBuilder().Controllers(benchPlainController{}).Build())
	bare := New(&AppOptions{Logger: &mockLogger{}})
	bare.BindGlobalMiddlewares(passMiddleware{})
	bare.Create(NewModuleBuilder().Controllers(benchPlainController{}).Build())

	withProbe := allocsOfServe(probing, http.MethodGet, "/bench")
	without := allocsOfServe(bare, http.MethodGet, "/bench")
	if withProbe > without {
		t.Error(test.DiffMessage(withProbe, without, "K4: asking for the hook writer and registering none adds no allocation"))
	}
	if got := allocsOfServe(plain, http.MethodGet, "/bench"); got > 7 {
		t.Error(test.DiffMessage(got, "<= 7", "K4: with no hook registered the request path adds no allocation"))
	}
}

func TestServeHTTP_HookRegistered_StillServes(t *testing.T) {
	app := lcApp(t)
	hkRegister("g1", func(time.Duration) {})
	w := lcServe(app, http.MethodGet, "/ok")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "H") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "K2: registering a hook does not change the response"))
	}
}

func TestNonce_WithoutHelmet(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	w := createGet(app, "/used")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R34: the page renders"))
	}
	nonceOf(t, w.Body.String())
	if csp := w.Header().Values("Content-Security-Policy"); len(csp) != 0 {
		t.Error(test.DiffMessage(csp, []string{}, "R34: no CSP header is added without helmet"))
	}
}

func TestNonce_PlainPageHasNone(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	w := createGet(app, "/plain")
	if w.Body.String() != "<p>plain</p>" || len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "<p>plain</p>", "R30: a page without {{nonce}} is unchanged"))
	}
}

func TestNonce_UniquePerResponse(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		seen[nonceOf(t, createGet(app, "/used").Body.String())] = true
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "R33: 1,000 responses give 1,000 nonces"))
	}
}

func TestNonce_NeverFromClient(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})
	submitted := strings.Repeat("f", 32)

	w := createRequest(app, http.MethodGet, "/used?x="+submitted, "X-Nonce", submitted, "Content-Security-Policy", "script-src 'nonce-"+submitted+"'")
	if got := nonceOf(t, w.Body.String()); got == submitted {
		t.Error(test.DiffMessage(got, "a server drawn value", "R33: a value submitted by a client never becomes a nonce"))
	}
}

func TestNonce_Concurrent(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				body := createGet(app, "/used").Body.String()
				found := nonceAttr.FindAllStringSubmatch(body, -1)
				if len(found) != 3 || found[0][1] != found[1][1] || found[1][1] != found[2][1] || strings.Count(body, found[0][1]) != 4 {
					t.Error(test.DiffMessage(body, "one nonce at every use", "R33: concurrent responses"))
					return
				}
				mu.Lock()
				seen[found[0][1]] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 640 {
		t.Error(test.DiffMessage(len(seen), 640, "R33: 64 goroutines never share a nonce"))
	}
}

func TestNonce_ErrorPageSet(t *testing.T) {
	files := pgFiles(map[string]string{
		"boom.html": `boom`,
		"404.html":  `<script nonce="{{nonce}}"></script><style nonce="{{nonce}}"></style><i data-n="{{nonce}}"></i><b>{{.Code}}</b>`,
	})
	app, _ := pgNew(t, files, nonceFailing{})

	w := createGet(app, "/boom")
	if w.Code != http.StatusNotFound || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "R30: the error page answers"))
	}
	body := w.Body.String()
	found := nonceAttr.FindAllStringSubmatch(body, -1)
	if len(found) != 3 || found[0][1] != found[1][1] || found[1][1] != found[2][1] || !nonceHex.MatchString(found[0][1]) {
		t.Error(test.DiffMessage(body, "one nonce at every use", "R30: an error page that uses {{nonce}} gets one"))
	}
	again := nonceAttr.FindStringSubmatch(createGet(app, "/boom").Body.String())
	if again == nil || again[1] == found[0][1] {
		t.Error(test.DiffMessage(again, "another nonce", "R33: the error page draws a nonce per response"))
	}
}
