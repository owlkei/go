package tracecontext

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/test"
)

func TestClientIP_UnresolvedUsesRemoteAddr(t *testing.T) {
	r, _ := scopedRequest(t, "203.0.113.9:4711")
	if got, want := ClientIP(r), netip.MustParseAddr("203.0.113.9"); got != want {
		t.Error(test.DiffMessage(got, want, "P2: unresolved, the client is the peer of RemoteAddr"))
	}
}

func TestClientIP_WithoutScopeUsesRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.1:80"
	if got, want := ClientIP(r), netip.MustParseAddr("198.51.100.1"); got != want {
		t.Error(test.DiffMessage(got, want, "P2: a request with no scope still resolves from RemoteAddr"))
	}
}

func TestClientIP_RemoteAddrForms(t *testing.T) {
	cases := []struct {
		remote string
		want   netip.Addr
	}{
		{"[2001:db8::1]:443", netip.MustParseAddr("2001:db8::1")},
		{"[::ffff:192.0.2.7]:80", netip.MustParseAddr("192.0.2.7")},
		{"[fe80::1%eth0]:80", netip.MustParseAddr("fe80::1")},
		{"", netip.Addr{}},
		{"not-an-address", netip.Addr{}},
		{"192.0.2.7", netip.Addr{}},
		{"@", netip.Addr{}},
		{"example.com:80", netip.Addr{}},
	}
	for _, c := range cases {
		r, _ := scopedRequest(t, c.remote)
		got := ClientIP(r)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "P2: RemoteAddr "+c.remote))
		}
		if got.Zone() != "" || got.Is4In6() {
			t.Error(test.DiffMessage(got, "<no zone, unmapped>", "P2: zone removed and IPv4-mapped unmapped: "+c.remote))
		}
	}
}

func TestClientIP_ResolvedScopeWins(t *testing.T) {
	r, scope := scopedRequest(t, "10.0.0.1:1")
	scope.ClientIP = netip.MustParseAddr("198.51.100.77")
	scope.IsClientSet = true
	if got, want := ClientIP(r), scope.ClientIP; got != want {
		t.Error(test.DiffMessage(got, want, "P12: a request proxy resolved reads the scope"))
	}
}

func TestClientIP_UnsetScopeIgnoresFields(t *testing.T) {
	r, scope := scopedRequest(t, "10.0.0.1:1")
	scope.ClientIP = netip.MustParseAddr("198.51.100.77")
	if got, want := ClientIP(r), netip.MustParseAddr("10.0.0.1"); got != want {
		t.Error(test.DiffMessage(got, want, "P2: IsClientSet false means proxy did not resolve this request"))
	}
}

func TestIsHTTPS_ConnectionAndScope(t *testing.T) {
	r, scope := scopedRequest(t, "10.0.0.1:1")
	if IsHTTPS(r) {
		t.Error(test.DiffMessage(true, false, "P2: plain connection is http"))
	}
	r.TLS = &tls.ConnectionState{}
	if !IsHTTPS(r) {
		t.Error(test.DiffMessage(false, true, "P2: r.TLS != nil is https"))
	}
	r.TLS = nil
	scope.IsClientHTTPS = true
	if IsHTTPS(r) {
		t.Error(test.DiffMessage(true, false, "P2: IsClientHTTPS is ignored while IsClientSet is false"))
	}
	scope.IsClientSet = true
	if !IsHTTPS(r) {
		t.Error(test.DiffMessage(false, true, "P8: a resolved https scheme is read from the scope"))
	}
	scope.IsClientHTTPS = false
	r.TLS = &tls.ConnectionState{}
	if IsHTTPS(r) {
		t.Error(test.DiffMessage(true, false, "P8: a resolved scope decides, not the connection"))
	}
}

func TestIsHTTPS_WithoutScope(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if IsHTTPS(r) {
		t.Error(test.DiffMessage(true, false, "P2: no scope, no TLS"))
	}
	r.TLS = &tls.ConnectionState{}
	if !IsHTTPS(r) {
		t.Error(test.DiffMessage(false, true, "P2: no scope, TLS"))
	}
}

func TestHost_ConnectionAndScope(t *testing.T) {
	r, scope := scopedRequest(t, "10.0.0.1:1")
	r.Host = "internal.local:8080"
	if got := Host(r); got != "internal.local:8080" {
		t.Error(test.DiffMessage(got, "internal.local:8080", "P2: unresolved, the host is r.Host"))
	}
	scope.ClientHost = "app.example.com"
	if got := Host(r); got != "internal.local:8080" {
		t.Error(test.DiffMessage(got, "internal.local:8080", "P2: ClientHost is ignored while IsClientSet is false"))
	}
	scope.IsClientSet = true
	if got := Host(r); got != "app.example.com" {
		t.Error(test.DiffMessage(got, "app.example.com", "P9: a resolved host is read from the scope"))
	}
	scope.ClientHost = ""
	if got := Host(r); got != "internal.local:8080" {
		t.Error(test.DiffMessage(got, "internal.local:8080", "ClientHost empty means r.Host"))
	}
}

func TestHost_WithoutScope(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "example.org"
	if got := Host(r); got != "example.org" {
		t.Error(test.DiffMessage(got, "example.org", "P2: no scope"))
	}
}

func TestClientFields_ZeroedWithNewRequestScope(t *testing.T) {
	_, scope := NewRequest(context.Background(), http.Header{}, time.Now())
	if scope.IsClientSet || scope.IsClientHTTPS || scope.ClientHost != "" || scope.ClientIP.IsValid() {
		t.Error(test.DiffMessage(*scope, "<client fields zero>", "a new request scope starts with no resolved client"))
	}
}

func TestClientIP_Allocations(t *testing.T) {
	r, _ := scopedRequest(t, "203.0.113.9:4711")
	if got := testing.AllocsPerRun(200, func() { _ = ClientIP(r) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P2: reading the client IP does not allocate"))
	}
	if got := testing.AllocsPerRun(200, func() { _ = IsHTTPS(r); _ = Host(r) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P2: reading scheme and host does not allocate"))
	}
}
