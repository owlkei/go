package trace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

func TestTransport_ReturnsNonNil(t *testing.T) {
	if Transport(nil) == nil || Transport(&recordingBase{}) == nil {
		t.Error(test.DiffMessage(nil, "<a RoundTripper>", "T1: Transport returns a non-nil RoundTripper"))
	}
}

func TestTransport_NilBaseUsesDefaultTransport(t *testing.T) {
	rt := Transport(nil)
	previous := http.DefaultTransport
	base := &recordingBase{response: &http.Response{StatusCode: 204}}
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = previous })

	c, _ := scopeContext(t)
	res, err := rt.RoundTrip(newRequest(t, c))
	if err != nil || res != base.response {
		t.Fatal(test.DiffMessage([]any{res, err}, []any{base.response, nil}, "T1: a nil base calls the http.DefaultTransport current at each RoundTrip"))
	}
	base.last(t)
}

func TestTransport_ConcurrentUse(t *testing.T) {
	base := &recordingBase{response: &http.Response{StatusCode: 200}}
	rt := Transport(base)
	c, s := scopeContext(t)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				r := newRequest(t, c)
				if _, err := rt.RoundTrip(r); err != nil {
					t.Error(err)
					return
				}
				if r.Header.Get("Traceparent") != "" {
					t.Error("T4: the caller's request is never changed")
					return
				}
			}
		}()
	}
	wg.Wait()

	base.mu.Lock()
	defer base.mu.Unlock()
	if len(base.requests) != 16*200 {
		t.Fatal(test.DiffMessage(len(base.requests), 16*200, "T1: every call reaches the base"))
	}
	for _, r := range base.requests {
		expectSpan(t, r.Header.Get("Traceparent"), s, "T1")
	}
}

func TestTransport_NoScopeSameRequest(t *testing.T) {
	base := &recordingBase{}
	rt := Transport(base)
	r := newRequest(t, context.Background())
	_, _ = rt.RoundTrip(r)
	if base.last(t) != r {
		t.Error(test.DiffMessage(base.last(t), r, "T2: without a scope the base receives the same *http.Request"))
	}
	if _, ok := r.Header["Traceparent"]; ok {
		t.Error(test.DiffMessage(r.Header, "<no Traceparent>", "T2: nothing is added without a scope"))
	}
}

func TestTransport_NoScopeNoAllocation(t *testing.T) {
	rt := Transport(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }))
	r := newRequest(t, context.Background())
	if got := testing.AllocsPerRun(1000, func() { _, _ = rt.RoundTrip(r) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "T2: the wrapper allocates nothing without a scope"))
	}
}

func TestTransport_BackgroundContext(t *testing.T) {
	base := &recordingBase{}
	r := newRequest(t, context.Background())
	_, _ = Transport(base).RoundTrip(r)
	if base.last(t) != r {
		t.Error(test.DiffMessage(base.last(t), r, "T2: a background context passes through"))
	}
}

func TestTransport_AddsTraceparent(t *testing.T) {
	c, s := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	_, _ = Transport(base).RoundTrip(r)

	got := base.last(t)
	if got == r {
		t.Fatal(test.DiffMessage(got, "<a copy>", "T3: the base receives another request pointer"))
	}
	values := got.Header.Values("Traceparent")
	if len(values) != 1 {
		t.Fatal(test.DiffMessage(values, "<one value>", "T3: exactly one Traceparent value"))
	}
	if values[0] != s.Traceparent() {
		t.Error(test.DiffMessage(values[0], s.Traceparent(), "T3: the value names the request's span"))
	}
	expectSpan(t, values[0], s, "T3")
	if got.Method != r.Method || got.URL != r.URL || got.Context() != r.Context() || got.ContentLength != r.ContentLength {
		t.Error(test.DiffMessage(got, r, "T3: method, URL, context and length are the original's"))
	}
}

func TestTransport_SpanIsRequestID(t *testing.T) {
	c, s := scopeContext(t)
	base := &recordingBase{}
	rt := Transport(base)
	_, _ = rt.RoundTrip(newRequest(t, c))
	first := base.last(t).Header.Get("Traceparent")
	_, _ = rt.RoundTrip(newRequest(t, c))
	second := base.last(t).Header.Get("Traceparent")
	if first != second || !strings.Contains(first, "-"+s.Request+"-") {
		t.Error(test.DiffMessage([]string{first, second}, []string{s.Traceparent(), s.Traceparent()}, "T3, Q4: every call carries the request's span"))
	}
}

func TestTransport_FlagsOfContinuedTrace(t *testing.T) {
	for _, flags := range []string{"00", "01"} {
		c, s := scopeContext(t, "Traceparent", "00-"+transportTraceID+"-"+transportParent+"-"+flags)
		base := &recordingBase{}
		_, _ = Transport(base).RoundTrip(newRequest(t, c))
		value := base.last(t).Header.Get("Traceparent")
		if !strings.HasSuffix(value, "-"+flags) {
			t.Error(test.DiffMessage(value, "<ends with -"+flags+">", "T3: the flags are the incoming ones"))
		}
		expectSpan(t, value, s, "T3")
		if !strings.Contains(value, transportTraceID) {
			t.Error(test.DiffMessage(value, "<the continued trace ID>", "T3: the trace ID is the client's"))
		}
	}
}

func TestTransport_KeepsOtherHeaders(t *testing.T) {
	c, _ := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	r.Header.Set("Authorization", "Bearer x")
	r.Header.Add("Accept", "a")
	r.Header.Add("Accept", "b")
	_, _ = Transport(base).RoundTrip(r)

	got := base.last(t).Header
	if got.Get("Authorization") != "Bearer x" || strings.Join(got.Values("Accept"), ",") != "a,b" {
		t.Error(test.DiffMessage(got, "<the caller's headers plus Traceparent>", "T3: the caller's headers are kept"))
	}
	got.Set("Authorization", "changed")
	if r.Header.Get("Authorization") != "Bearer x" {
		t.Error(test.DiffMessage(r.Header.Get("Authorization"), "Bearer x", "T3: the forwarded header map is a deep copy"))
	}
}

func TestTransport_CallerRequestUnchanged(t *testing.T) {
	c, _ := scopeContext(t)
	r := newRequest(t, c)
	r.Header.Set("X-A", "1")
	before := r.Header.Clone()
	_, _ = Transport(&recordingBase{}).RoundTrip(r)
	if _, ok := r.Header["Traceparent"]; ok {
		t.Error(test.DiffMessage(r.Header, "<no Traceparent>", "T4: the caller's header has no Traceparent key"))
	}
	if len(r.Header) != len(before) || r.Header.Get("X-A") != "1" {
		t.Error(test.DiffMessage(r.Header, before, "T4: the caller's header keeps its entries"))
	}
}

func TestTransport_NilHeader(t *testing.T) {
	c, s := scopeContext(t)
	r := newRequest(t, c)
	r.Header = nil
	base := &recordingBase{}
	_, _ = Transport(base).RoundTrip(r)
	if r.Header != nil {
		t.Error(test.DiffMessage(r.Header, http.Header(nil), "T4: a nil header stays nil"))
	}
	if got := base.last(t).Header.Get("Traceparent"); got != s.Traceparent() {
		t.Error(test.DiffMessage(got, s.Traceparent(), "T3: a nil header still gets the value"))
	}
}

func TestTransport_ExistingHeaderWins(t *testing.T) {
	c, _ := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	value := "00-" + transportTraceID + "-" + transportParent + "-01"
	r.Header.Set("Traceparent", value)
	_, _ = Transport(base).RoundTrip(r)
	if base.last(t) != r || r.Header.Get("Traceparent") != value || len(r.Header.Values("Traceparent")) != 1 {
		t.Error(test.DiffMessage(r.Header, value, "T5: a Traceparent set by the caller wins and the same request is passed"))
	}
}

func TestTransport_InvalidExistingHeaderKept(t *testing.T) {
	c, _ := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	r.Header.Set("Traceparent", "garbage")
	_, _ = Transport(base).RoundTrip(r)
	if base.last(t) != r || base.last(t).Header.Get("Traceparent") != "garbage" {
		t.Error(test.DiffMessage(base.last(t).Header, "garbage", "T5: an invalid value is kept as it is"))
	}
}

func TestTransport_EmptyKeyOptsOut(t *testing.T) {
	c, _ := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	r.Header["Traceparent"] = nil
	_, _ = Transport(base).RoundTrip(r)
	got := base.last(t)
	if got != r {
		t.Fatal(test.DiffMessage(got, r, "T5: the key with no value opts out: same request"))
	}
	if len(got.Header["Traceparent"]) != 0 {
		t.Error(test.DiffMessage(got.Header["Traceparent"], []string(nil), "T5: nothing is added after an opt out"))
	}
}

func TestTransport_NonCanonicalKeyIgnored(t *testing.T) {
	c, s := scopeContext(t)
	base := &recordingBase{}
	r := newRequest(t, c)
	r.Header["traceparent"] = []string{"x"}
	_, _ = Transport(base).RoundTrip(r)
	if got := base.last(t).Header["Traceparent"]; len(got) != 1 || got[0] != s.Traceparent() {
		t.Error(test.DiffMessage(got, []string{s.Traceparent()}, "T5: only the canonical key is looked up"))
	}
}

func TestTransport_MessageScope(t *testing.T) {
	connCtx, _ := scopeContext(t, "Traceparent", "00-"+transportTraceID+"-"+transportParent+"-01")
	c, s := tracecontext.NewMessage(connCtx, transportNow)
	base := &recordingBase{}
	_, _ = Transport(base).RoundTrip(newRequest(t, c))
	value := base.last(t).Header.Get("Traceparent")
	expectSpan(t, value, s, "T6")
	if !strings.Contains(value, transportTraceID) {
		t.Error(test.DiffMessage(value, "<the connection's trace ID>", "T6: a message carries the connection's trace"))
	}
}

func TestTransport_DerivedContext(t *testing.T) {
	c, s := scopeContext(t)
	type key struct{}
	derived, cancel := context.WithTimeout(context.WithValue(c, key{}, 1), time.Minute)
	defer cancel()
	base := &recordingBase{}
	_, _ = Transport(base).RoundTrip(newRequest(t, derived))
	expectSpan(t, base.last(t).Header.Get("Traceparent"), s, "T6")
}

func TestTransport_ContextKeptAfterRequest(t *testing.T) {
	c, s := scopeContext(t)
	done := make(chan struct{})
	base := &recordingBase{}
	go func() {
		defer close(done)
		_, _ = Transport(base).RoundTrip(newRequest(t, c))
	}()
	<-done
	expectSpan(t, base.last(t).Header.Get("Traceparent"), s, "T6")
}

func TestTransport_AddsNothingElse(t *testing.T) {
	c, _ := scopeContext(t,
		"Traceparent", "00-"+transportTraceID+"-"+transportParent+"-01",
		"Tracestate", "vendor=1",
		"X-Request-Id", "incoming-id")
	base := &recordingBase{}
	_, _ = Transport(base).RoundTrip(newRequest(t, c))
	got := base.last(t).Header
	if len(got) != 1 || got.Get("Traceparent") == "" {
		t.Error(test.DiffMessage(got, "<only Traceparent>", "T7: no tracestate, no X-Request-Id"))
	}
}

func TestTransport_RedirectKeepsTraceSameSpan(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Traceparent"))
		mu.Unlock()
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/next", http.StatusFound)
		}
	}))
	defer server.Close()

	c, s := scopeContext(t)
	r, err := http.NewRequestWithContext(c, http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&http.Client{Transport: Transport(nil)}).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatal(test.DiffMessage(len(seen), 2, "T8: the server was called twice"))
	}
	for i, value := range seen {
		if value != s.Traceparent() {
			t.Error(test.DiffMessage(value, s.Traceparent(), "T8: hop "+string(rune('1'+i))+" carries the same trace and span"))
		}
	}
}

func TestTransport_ReturnsBaseResponseAndError(t *testing.T) {
	c, _ := scopeContext(t)
	res := &http.Response{StatusCode: 418, Body: io.NopCloser(strings.NewReader("x"))}
	failure := context.DeadlineExceeded
	got, err := Transport(&recordingBase{response: res, err: failure}).RoundTrip(newRequest(t, c))
	if got != res || err != failure {
		t.Error(test.DiffMessage([]any{got, err}, []any{res, failure}, "T9: the response and the error are the base's, unchanged"))
	}
}

func TestTransport_CloseIdleConnectionsForwarded(t *testing.T) {
	base := &closingBase{}
	closer, ok := Transport(base).(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatal(test.DiffMessage(false, true, "T10: the transport has CloseIdleConnections"))
	}
	closer.CloseIdleConnections()
	if base.closed.Load() != 1 {
		t.Error(test.DiffMessage(base.closed.Load(), int64(1), "T10: the call reaches the base"))
	}
}

func TestTransport_CloseIdleConnectionsDefaultTransport(t *testing.T) {
	rt := Transport(nil)
	previous := http.DefaultTransport
	base := &closingBase{}
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = previous })
	closer, ok := rt.(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatal(test.DiffMessage(false, true, "T10: the transport has CloseIdleConnections"))
	}
	closer.CloseIdleConnections()
	if base.closed.Load() != 1 {
		t.Error(test.DiffMessage(base.closed.Load(), int64(1), "T10: a nil base forwards to http.DefaultTransport"))
	}
}

func TestTransport_CloseIdleConnectionsBaseWithoutMethod(t *testing.T) {
	closer, ok := Transport(&recordingBase{}).(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatal(test.DiffMessage(false, true, "T10: the transport has CloseIdleConnections"))
	}
	closer.CloseIdleConnections()
}
