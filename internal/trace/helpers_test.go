package trace

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

func benchTransport(b *testing.B, c context.Context, header string) {
	rt := Transport(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }))
	r, err := http.NewRequestWithContext(c, http.MethodGet, "http://example.invalid/path", nil)
	if err != nil {
		b.Fatal(err)
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Authorization", "Bearer token")
	r.Header.Set("User-Agent", "bench")
	if header != "" {
		r.Header.Set("Traceparent", header)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = rt.RoundTrip(r)
	}
}

const (
	transportTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	transportParent  = "00f067aa0ba902b7"
)

var transportNow = time.UnixMilli(1763300000123)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type recordingBase struct {
	mu       sync.Mutex
	requests []*http.Request
	response *http.Response
	err      error
}

func (b *recordingBase) RoundTrip(r *http.Request) (*http.Response, error) {
	b.mu.Lock()
	b.requests = append(b.requests, r)
	b.mu.Unlock()
	return b.response, b.err
}

func (b *recordingBase) last(t testing.TB) *http.Request {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) == 0 {
		t.Fatal("T1: the base received no request")
	}
	return b.requests[len(b.requests)-1]
}

type closingBase struct {
	recordingBase
	closed atomic.Int64
}

func (b *closingBase) CloseIdleConnections() { b.closed.Add(1) }

func scopeContext(t testing.TB, pairs ...string) (context.Context, *tracecontext.Scope) {
	t.Helper()
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Add(pairs[i], pairs[i+1])
	}
	return tracecontext.NewRequest(context.Background(), h, transportNow)
}

func newRequest(t testing.TB, c context.Context) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(c, http.MethodGet, "http://example.invalid/path?q=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func expectSpan(t *testing.T, value string, s *tracecontext.Scope, desc string) {
	t.Helper()
	traceID, parentID, flags, ok := tracecontext.ParseTraceparent(value)
	if !ok {
		t.Fatal(test.DiffMessage(value, "<a valid traceparent>", desc+": the value parses"))
	}
	if traceID != s.TraceID {
		t.Error(test.DiffMessage(traceID, s.TraceID, desc+": the trace ID is the scope's"))
	}
	if parentID != s.SpanID() {
		t.Error(test.DiffMessage(parentID, s.SpanID(), desc+": the span is the request's"))
	}
	_, _, want, _ := tracecontext.ParseTraceparent(s.Traceparent())
	if flags != want {
		t.Error(test.DiffMessage(flags, want, desc+": the flags are the scope's"))
	}
}
