package httpsredirect

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func BenchmarkHTTPSRedirect_HTTPS(b *testing.B) {
	mw := HTTPSRedirect{}.NewMiddleware()
	r := request(http.MethodGet, "/ok")
	r.TLS = &tls.ConnectionState{}
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkHTTPSRedirect_Redirect(b *testing.B) {
	mw := HTTPSRedirect{}.NewMiddleware()
	r := plainRequest(http.MethodGet, "example.com:8080", "/a/b?x=1&y=2")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		mw.Use(r, w, noop)
	}
}
