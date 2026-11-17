package core

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

type clientIPEvents struct {
	mu     sync.Mutex
	events []trace.Event
}

func (c *clientIPEvents) listen(e trace.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *clientIPEvents) all() []trace.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]trace.Event(nil), c.events...)
}

func (c *clientIPEvents) complete(t *testing.T) trace.Event {
	t.Helper()
	for _, e := range c.all() {
		if e.Stage == trace.StageComplete {
			return e
		}
	}
	t.Fatal(test.DiffMessage(c.all(), "a complete event", "fixture: the request emitted a complete event"))
	return trace.Event{}
}

func clientIPApp(t *testing.T, events *clientIPEvents, withProxy bool) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{setup: func(app *App) {
		app.tracer.Listen(events.listen)
		if withProxy {
			app.BindGlobalMiddlewares(proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}})
		}
	}})
}

func clientIPServe(app *App, remote string, headers ...string) {
	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.RemoteAddr = remote
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

func TestCompleteEvent_ClientIP_Direct(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "203.0.113.5:5555", "X-Forwarded-For", "198.51.100.1", "X-Real-IP", "198.51.100.2")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("203.0.113.5"); got != want {
		t.Error(test.DiffMessage(got, want, "A1: with no proxy the complete event carries the peer, whatever the headers say"))
	}
}

func TestCompleteEvent_ClientIP_BehindProxy(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, true)
	clientIPServe(app, "10.0.0.9:80", "X-Forwarded-For", "198.51.100.20")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("198.51.100.20"); got != want {
		t.Error(test.DiffMessage(got, want, "A1: behind a trusted proxy the complete event carries the client"))
	}
}

func TestCompleteEvent_ClientIP_UntrustedPeerBehindProxyMiddleware(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, true)
	clientIPServe(app, "203.0.113.5:5555", "X-Forwarded-For", "198.51.100.20")
	got := events.complete(t).ClientIP
	if want := netip.MustParseAddr("203.0.113.5"); got != want {
		t.Error(test.DiffMessage(got, want, "A1, P3: an untrusted peer's headers never reach the event"))
	}
}

func TestCompleteEvent_ClientIP_ZeroOnStageEvents(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "203.0.113.5:5555")
	for _, e := range events.all() {
		if e.Stage != trace.StageComplete && e.ClientIP.IsValid() {
			t.Error(test.DiffMessage(e.ClientIP, netip.Addr{}, "A1: only complete events carry the client IP, not "+e.Stage))
		}
	}
}

func TestCompleteEvent_ClientIP_UnparsableRemoteAddr(t *testing.T) {
	events := &clientIPEvents{}
	app := clientIPApp(t, events, false)
	clientIPServe(app, "not-an-address")
	if got := events.complete(t).ClientIP; got.IsValid() {
		t.Error(test.DiffMessage(got, netip.Addr{}, "A1: the zero Addr when RemoteAddr is not ip:port"))
	}
}
