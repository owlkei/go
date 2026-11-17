package ctx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkHTTPContext_FormURLEncoded(b *testing.B) {
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	var n int
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("POST", "/", strings.NewReader("foo=bar&baz=qux"))
		r.Header.Set("Content-Type", applicationXWWWFormUrlencoded)
		HTTPInit(c, w, r)
		n += len(c.Form())
		HTTPReset(c)
	}
	if n == 0 {
		b.Fatal("Form should not be empty")
	}
}

func BenchmarkForm_Bind(b *testing.B) {
	f := Form{"name": {"joe"}}
	b.ReportAllocs()
	for b.Loop() {
		f.Bind(formBindDTO{})
	}
}

func BenchmarkForm_BindValue(b *testing.B) {
	f := Form{"name": {"joe"}}
	b.ReportAllocs()
	for b.Loop() {
		f.BindValue(formBindDTO{})
	}
}
