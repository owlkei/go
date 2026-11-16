package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

type unrelatedKey struct{}

type headerRecorder struct {
	mu      sync.Mutex
	headers []http.Header
}

func (h *headerRecorder) handler(status func(attempt int) int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.headers = append(h.headers, r.Header.Clone())
		attempt := len(h.headers)
		h.mu.Unlock()
		code := http.StatusOK
		if status != nil {
			code = status(attempt)
		}
		w.WriteHeader(code)
	})
}

func (h *headerRecorder) all() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]http.Header(nil), h.headers...)
}

func traceServer(status func(attempt int) int) (*httptest.Server, *headerRecorder) {
	rec := &headerRecorder{}
	return httptest.NewServer(rec.handler(status)), rec
}

func requestScope(pairs ...string) (context.Context, *tracecontext.Scope) {
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Add(pairs[i], pairs[i+1])
	}
	return tracecontext.NewRequest(context.Background(), h, time.Now())
}

func onlyHeaders(t *testing.T, rec *headerRecorder) http.Header {
	t.Helper()
	all := rec.all()
	if len(all) != 1 {
		t.Fatal(test.DiffMessage(len(all), 1, "the server saw one request"))
	}
	return all[0]
}

func TestSend_ForwardsTraceparent(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	ctx, scope := requestScope()

	if _, err := newTestClient(srv).Get("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	got := onlyHeaders(t, rec)
	values := got.Values("Traceparent")
	if len(values) != 1 || values[0] != scope.Traceparent() {
		t.Fatal(test.DiffMessage(values, []string{scope.Traceparent()}, "C1: the request carries the scope's traceparent"))
	}
	traceID, parentID, _, ok := tracecontext.ParseTraceparent(values[0])
	if !ok || traceID != scope.TraceID || parentID != scope.Request {
		t.Error(test.DiffMessage([]any{traceID, parentID, ok}, []any{scope.TraceID, scope.Request, true}, "C1: the trace ID, and our request ID as the parent"))
	}
}

func TestSend_ForwardsMessageScopeAsParent(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	connCtx, _ := requestScope()
	ctx, scope := tracecontext.NewMessage(connCtx, time.Now())

	if _, err := newTestClient(srv).Post("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	_, parentID, _, ok := tracecontext.ParseTraceparent(onlyHeaders(t, rec).Get("Traceparent"))
	if !ok || parentID != scope.Message {
		t.Error(test.DiffMessage(parentID, scope.Message, "C1: a WS message is the parent of its calls"))
	}
}

func TestSend_ForwardsDerivedContext(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	ctx, scope := requestScope()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	if _, err := newTestClient(srv).Get("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	if got := onlyHeaders(t, rec).Get("Traceparent"); got != scope.Traceparent() {
		t.Error(test.DiffMessage(got, scope.Traceparent(), "C1: a context derived from the scope's still forwards it"))
	}
}

func TestSend_ForwardsContinuedTraceFlags(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	ctx, scope := requestScope("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	if _, err := newTestClient(srv).Get("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	got := onlyHeaders(t, rec).Get("Traceparent")
	if got != scope.Traceparent() || !strings.HasPrefix(got, "00-4bf92f3577b34da6a3ce929d0e0e4736-") || !strings.HasSuffix(got, "-01") {
		t.Error(test.DiffMessage(got, scope.Traceparent(), "C1: the continued trace and its flags are forwarded"))
	}
}

func TestSend_CallerHeaderWins(t *testing.T) {
	const mine = "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-00"
	for name, configure := range map[string]func(c *httpClient, rb RequestBuilder) RequestBuilder{
		"Header on the builder": func(c *httpClient, rb RequestBuilder) RequestBuilder {
			return rb.Header("Traceparent", mine)
		},
		"lower-case Header on the builder": func(c *httpClient, rb RequestBuilder) RequestBuilder {
			return rb.Header("traceparent", mine)
		},
		"Headers on the builder": func(c *httpClient, rb RequestBuilder) RequestBuilder {
			return rb.Headers(map[string]string{"Traceparent": mine})
		},
		"SetHeader on the client": func(c *httpClient, rb RequestBuilder) RequestBuilder {
			c.SetHeader("Traceparent", mine)
			return rb
		},
	} {
		srv, rec := traceServer(nil)
		ctx, _ := requestScope()
		c := newTestClient(srv)
		if _, err := configure(c, c.Get("/")).Context(ctx).Send(); err != nil {
			srv.Close()
			t.Fatal(err)
		}
		values := onlyHeaders(t, rec).Values("Traceparent")
		srv.Close()
		if len(values) != 1 || values[0] != mine {
			t.Error(test.DiffMessage(values, []string{mine}, "C2: a caller's traceparent is not overwritten ("+name+")"))
		}
	}
}

func TestSend_NoScopeNoHeader(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	c := newTestClient(srv)

	if _, err := c.Get("/").Context(context.Background()).Send(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("/").Send(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("/").Context(context.WithValue(context.Background(), unrelatedKey{}, 1)).Send(); err != nil {
		t.Fatal(err)
	}
	for i, h := range rec.all() {
		if values := h.Values("Traceparent"); len(values) != 0 {
			t.Error(test.DiffMessage(values, []string(nil), "C2: no scope in the context, no header (request "+string(rune('1'+i))+")"))
		}
	}
	if len(rec.all()) != 3 {
		t.Error(test.DiffMessage(len(rec.all()), 3, "the server saw every request"))
	}
}

func TestSend_RetriesKeepTraceparent(t *testing.T) {
	srv, rec := traceServer(func(attempt int) int {
		if attempt < 3 {
			return http.StatusInternalServerError
		}
		return http.StatusOK
	})
	defer srv.Close()
	ctx, scope := requestScope()

	resp, err := newTestClient(srv).Get("/").Context(ctx).Retry(3).RetryBackoff(time.Millisecond, 5*time.Millisecond).Send()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatal(test.DiffMessage(resp.StatusCode, http.StatusOK, "the third attempt succeeds"))
	}
	all := rec.all()
	if len(all) != 3 {
		t.Fatal(test.DiffMessage(len(all), 3, "three attempts"))
	}
	for i, h := range all {
		if got := h.Get("Traceparent"); got != scope.Traceparent() {
			t.Error(test.DiffMessage(got, scope.Traceparent(), "C2: attempt "+string(rune('1'+i))+" carries the same traceparent"))
		}
	}
}

func TestSend_BeforeRequestHookCanDelete(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	ctx, _ := requestScope()
	c := newTestClient(srv)
	var seen string
	c.OnBeforeRequest(func(req *http.Request) error {
		seen = req.Header.Get("Traceparent")
		req.Header.Del("Traceparent")
		return nil
	})

	if _, err := c.Get("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	if seen == "" {
		t.Error(test.DiffMessage(seen, "<the traceparent>", "C2: OnBeforeRequest hooks run after the header is set"))
	}
	if values := onlyHeaders(t, rec).Values("Traceparent"); len(values) != 0 {
		t.Error(test.DiffMessage(values, []string(nil), "C2: a hook may delete the header"))
	}
}

func TestSend_DoesNotForwardRequestID(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	ctx, scope := requestScope(
		"X-Request-Id", "upstream-request-1",
		"Tracestate", "vendor=opaque",
		"Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)

	if _, err := newTestClient(srv).Get("/").Context(ctx).Send(); err != nil {
		t.Fatal(err)
	}
	got := onlyHeaders(t, rec)
	if values := got.Values("X-Request-Id"); len(values) != 0 {
		t.Error(test.DiffMessage(values, []string(nil), "C3: X-Request-Id is not forwarded"))
	}
	if values := got.Values("Tracestate"); len(values) != 0 {
		t.Error(test.DiffMessage(values, []string(nil), "C3: tracestate is not forwarded"))
	}
	if got.Get("Traceparent") != scope.Traceparent() {
		t.Error(test.DiffMessage(got.Get("Traceparent"), scope.Traceparent(), "C1: traceparent is"))
	}
}

func TestSend_ConcurrentRequestsForwardTheirOwnScope(t *testing.T) {
	srv, rec := traceServer(nil)
	defer srv.Close()
	c := newTestClient(srv)

	const goroutines = 16
	want := make(map[string]struct{}, goroutines)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range goroutines {
		ctx, scope := requestScope()
		mu.Lock()
		want[scope.Traceparent()] = struct{}{}
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get("/").Context(ctx).Send(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got := map[string]struct{}{}
	for _, h := range rec.all() {
		got[h.Get("Traceparent")] = struct{}{}
	}
	if len(got) != goroutines {
		t.Fatal(test.DiffMessage(len(got), goroutines, "every request forwards a distinct traceparent"))
	}
	for value := range got {
		if _, ok := want[value]; !ok {
			t.Error(test.DiffMessage(value, "<one of the scopes' traceparents>", "C1: no mix-up between concurrent requests"))
		}
	}
}
