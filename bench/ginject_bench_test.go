package bench

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func BenchmarkGinject(b *testing.B) { benchmarkHandler(b, NewGinject()) }

func BenchmarkGinjectPage(b *testing.B) {
	app := newPageApp(true)
	for _, s := range []struct{ name, path string }{{"JSONList", "/json"}, {"PageList", "/bookings"}} {
		b.Run(s.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, s.path, nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				app.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("status %d", w.Code)
				}
			}
		})
	}
}

func BenchmarkGinjectPageIdleMemory(b *testing.B) {
	for b.Loop() {
		before := heapAfterGC()
		withoutRender := newPageApp(false)
		mid := heapAfterGC()
		withRender := newPageApp(true)
		after := heapAfterGC()
		b.ReportMetric(float64(mid-before), "B-app")
		b.ReportMetric(float64(after-mid), "B-app+render")
		runtime.KeepAlive(withoutRender)
		runtime.KeepAlive(withRender)
	}
}
