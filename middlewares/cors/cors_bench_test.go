package cors

import (
	"net/http"
	"testing"
)

func benchCORS(b *testing.B, r *http.Request) {
	b.Helper()
	mw := CORS{AllowOrigins: []string{appOrigin, "https://*.example.com"}, ExposeHeaders: []string{"X-Total"}}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		mw.Use(r, w, noop)
	}
}

func BenchmarkCORS_NoOrigin(b *testing.B) {
	benchCORS(b, corsRequest(http.MethodGet, "/ok", ""))
}

func BenchmarkCORS_ActualListedOrigin(b *testing.B) {
	benchCORS(b, corsRequest(http.MethodGet, "/ok", appOrigin))
}

func BenchmarkCORS_ActualWildcardOrigin(b *testing.B) {
	benchCORS(b, corsRequest(http.MethodGet, "/ok", "https://a.b.example.com"))
}

func BenchmarkCORS_OriginNotAllowed(b *testing.B) {
	benchCORS(b, corsRequest(http.MethodGet, "/ok", "https://evil.example"))
}

func BenchmarkCORS_Preflight(b *testing.B) {
	benchCORS(b, preflight(appOrigin))
}
