package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkProxy_UntrustedPeer(b *testing.B) {
	mw := trustedPrivate.NewMiddleware()
	r := scoped("198.51.100.9:1234", "X-Forwarded-For", "1.1.1.1")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkProxy_TrustedPeer_3Entries_4CIDRs(b *testing.B) {
	mw := Proxy{TrustedProxies: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fd00::/8"}}.NewMiddleware()
	r := scoped("10.0.0.1:80", "X-Forwarded-For", "8.8.8.8, 10.0.0.3, 10.0.0.2", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "app.example.com")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkProxy_Hops(b *testing.B) {
	mw := Proxy{TrustedHops: 2}.NewMiddleware()
	r := scoped("198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkProxy_Forwarded(b *testing.B) {
	mw := Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded}.NewMiddleware()
	r := scoped("10.0.0.1:80", "Forwarded", "for=8.8.8.8;proto=https, for=10.0.0.2")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkClientIP_Resolved(b *testing.B) {
	r := resolve(b, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "8.8.8.8")
	b.ReportAllocs()
	for b.Loop() {
		_ = clientIP(r)
	}
}

func BenchmarkClientIP_Unresolved(b *testing.B) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:4711"
	b.ReportAllocs()
	for b.Loop() {
		_ = clientIP(r)
	}
}
