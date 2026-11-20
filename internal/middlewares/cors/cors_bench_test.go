package cors_test

import (
	"net/http"
	"testing"
)

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
