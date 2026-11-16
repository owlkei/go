package trace

import (
	"context"
	"net/http"
	"testing"
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

func BenchmarkTransport_NoScope(b *testing.B) {
	benchTransport(b, context.Background(), "")
}

func BenchmarkTransport_ExistingHeader(b *testing.B) {
	c, _ := scopeContext(b)
	benchTransport(b, c, "00-"+transportTraceID+"-"+transportParent+"-01")
}

func BenchmarkTransport_Scope(b *testing.B) {
	c, _ := scopeContext(b)
	benchTransport(b, c, "")
}
