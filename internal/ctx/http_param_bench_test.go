package ctx

import "testing"

func BenchmarkHTTPContext_Param(b *testing.B) {
	c := newTestHTTPContext()
	keys := map[string][]int{"id": {0}, "name": {1}}
	values := []string{"42", "joe"}
	var n int
	b.ReportAllocs()
	for b.Loop() {
		HTTPSetParams(c, keys, values)
		n += len(c.Param())
		HTTPReset(c)
	}
	if n == 0 {
		b.Fatal("Param should not be empty")
	}
}

func BenchmarkParam_Bind(b *testing.B) {
	p := Param{"id": {"42"}}
	b.ReportAllocs()
	for b.Loop() {
		p.Bind(paramBindDTO{})
	}
}

func BenchmarkParam_BindValue(b *testing.B) {
	p := Param{"id": {"42"}}
	b.ReportAllocs()
	for b.Loop() {
		p.BindValue(paramBindDTO{})
	}
}
