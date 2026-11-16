package hostcheck

import (
	"net/http"
	"testing"
)

func benchHost(b *testing.B, host string) {
	b.Helper()
	mw := HostCheck{Hosts: []string{"example.com", "*.example.org", "api.example.net:8443"}}.NewMiddleware()
	r := hostRequest(host)
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, w, noop)
	}
}

func BenchmarkHostCheck_Hit(b *testing.B)         { benchHost(b, "example.com") }
func BenchmarkHostCheck_HitWithPort(b *testing.B) { benchHost(b, "example.com:8080") }
func BenchmarkHostCheck_MixedCase(b *testing.B)   { benchHost(b, "EXAMPLE.com") }
func BenchmarkHostCheck_Wildcard(b *testing.B)    { benchHost(b, "a.b.example.org") }
