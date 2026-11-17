package ctx

import (
	"net/http/httptest"
	"testing"
)

func BenchmarkHTTPContext_Query(b *testing.B) {
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	r := httptest.NewRequest("GET", "/?foo=bar&baz=qux", nil)
	var n int
	b.ReportAllocs()
	for b.Loop() {
		HTTPInit(c, w, r)
		n += len(c.Query())
		HTTPReset(c)
	}
	if n == 0 {
		b.Fatal("Query should not be empty")
	}
}

func BenchmarkQuery_Bind(b *testing.B) {
	q := Query{"name": {"joe"}, "age": {"30"}}
	b.ReportAllocs()
	for b.Loop() {
		q.Bind(queryBindDTO{})
	}
}

func BenchmarkQuery_BindValue(b *testing.B) {
	q := Query{"name": {"joe"}, "age": {"30"}}
	b.ReportAllocs()
	for b.Loop() {
		q.BindValue(queryBindDTO{})
	}
}
