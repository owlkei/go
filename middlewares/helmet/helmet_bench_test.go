package helmet

import (
	"net/http"
	"testing"
)

func benchHelmet(b *testing.B, r *http.Request) {
	b.Helper()
	mw := Helmet{}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		mw.Use(r, w, noop)
	}
}

func BenchmarkHelmet_HTTP(b *testing.B) {
	benchHelmet(b, request(http.MethodGet, "/ok"))
}

func BenchmarkHelmet_HTTPS(b *testing.B) {
	benchHelmet(b, secure("/ok"))
}
