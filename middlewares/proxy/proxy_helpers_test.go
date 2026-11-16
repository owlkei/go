package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

var trustedPrivate = Proxy{TrustedProxies: []string{"10.0.0.0/8"}}

func noop() {}

type failBody struct{ t *testing.T }

func (b failBody) Read([]byte) (int, error) {
	b.t.Error("G3: the middleware read the request body")
	return 0, fmt.Errorf("body must not be read")
}

type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(int)             {}
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func scoped(remote string, headers ...string) *http.Request {
	ctx, _ := tracecontext.NewRequest(context.Background(), http.Header{}, time.Now())
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	r.RemoteAddr = remote
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func run(t testing.TB, p Proxy, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	called := false
	w := httptest.NewRecorder()
	p.NewMiddleware().Use(r, w, func() { called = true })
	if !called {
		t.Error(test.DiffMessage(called, true, "P12: proxy always calls next"))
	}
	return w
}

func resolve(t testing.TB, p Proxy, remote string, headers ...string) *http.Request {
	t.Helper()
	r := scoped(remote, headers...)
	run(t, p, r)
	return r
}

func ipFrom(t testing.TB, p Proxy, remote string, headers ...string) netip.Addr {
	t.Helper()
	return ClientIP(resolve(t, p, remote, headers...))
}

func addr(s string) netip.Addr {
	if s == "" {
		return netip.Addr{}
	}
	return netip.MustParseAddr(s)
}

func expectIP(t *testing.T, got netip.Addr, want, desc string) {
	t.Helper()
	if got != addr(want) {
		t.Error(test.DiffMessage(got, addr(want), desc))
	}
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
