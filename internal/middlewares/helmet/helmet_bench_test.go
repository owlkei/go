package helmet_test

import (
	"net/http"
	"testing"
)

func BenchmarkHelmet_HTTP(b *testing.B) {
	benchHelmet(b, request(http.MethodGet, "/ok"))
}

func BenchmarkHelmet_HTTPS(b *testing.B) {
	benchHelmet(b, secure("/ok"))
}
