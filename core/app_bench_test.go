package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkServeHTTP(b *testing.B) {
	app := New()
	app.Create(ModuleBuilder().Build())

	r := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}
