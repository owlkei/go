package csrf

import (
	"net/http"
	"testing"
)

func BenchmarkCSRF_SafeMethod(b *testing.B) {
	benchCSRF(b, csrfRequest(http.MethodGet, "", ""))
}

func BenchmarkCSRF_SecFetchSiteSameOrigin(b *testing.B) {
	benchCSRF(b, csrfRequest(http.MethodPost, "same-origin", ""))
}

func BenchmarkCSRF_OriginAgainstHost(b *testing.B) {
	benchCSRF(b, csrfRequest(http.MethodPost, "", "https://example.com"))
}

func BenchmarkCSRF_TrustedOrigin(b *testing.B) {
	benchCSRF(b, csrfRequest(http.MethodPost, "cross-site", "https://app.example.com"))
}
