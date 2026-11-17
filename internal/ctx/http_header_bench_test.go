package ctx

import (
	"net/http/httptest"
	"testing"
)

func BenchmarkHTTPContext_Header(b *testing.B) {
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Foo", "bar")
	var n int
	b.ReportAllocs()
	for b.Loop() {
		HTTPInit(c, w, r)
		n += len(c.Header())
		HTTPReset(c)
	}
	if n == 0 {
		b.Fatal("Header should not be empty")
	}
}

func BenchmarkHeader_Bind(b *testing.B) {
	h := Header{}
	h.Set("Authorization", "Bearer token")
	b.ReportAllocs()
	for b.Loop() {
		h.Bind(headerBindDTO{})
	}
}

func BenchmarkHeader_BindValue(b *testing.B) {
	h := Header{}
	h.Set("Authorization", "Bearer token")
	b.ReportAllocs()
	for b.Loop() {
		h.BindValue(headerBindDTO{})
	}
}
