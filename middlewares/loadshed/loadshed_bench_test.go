package loadshed

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkLoadShed_Admitted_Fixed(b *testing.B) {
	benchAdmit(b, LoadShed{MaxInFlight: 1_000_000})
}

func BenchmarkLoadShed_Admitted_Adaptive(b *testing.B) {
	benchAdmit(b, LoadShed{})
}

func BenchmarkLoadShed_Admitted_Parallel(b *testing.B) {
	mw := LoadShed{}.NewMiddleware()
	r := request(http.MethodGet, "/ok")
	b.ReportAllocs()
	var total atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		ex := newExchange(&discardWriter{header: http.Header{}})
		var n int64
		for pb.Next() {
			mw.Use(r, ex, noop)
			ex.finish(time.Millisecond)
			n++
		}
		total.Add(n)
	})
}

func BenchmarkLoadShed_Shed(b *testing.B) {
	mw := LoadShed{MaxInFlight: 1, MaxQueue: -1}.NewMiddleware()
	holder := newExchange(&discardWriter{header: http.Header{}})
	mw.Use(request(http.MethodGet, "/ok"), holder, noop)
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodGet, "/ok")
	var refused int64
	b.ReportAllocs()
	for b.Loop() {
		func() {
			defer func() {
				if recover() != nil {
					refused++
				}
			}()
			mw.Use(r, ex, noop)
		}()
	}
	b.ReportMetric(float64(refused)/float64(b.N), "refused/op")
	holder.finish(time.Millisecond)
}

func BenchmarkLoadShed_App_Admitted(b *testing.B) {
	app := newApp(b, LoadShed{})
	r := request(http.MethodGet, "/ok")
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		app.ServeHTTP(w, r)
	}
}

func BenchmarkLoadShed_Admitted_AdaptiveBody(b *testing.B) {
	step := bodyAdmission()
	b.ReportAllocs()
	for b.Loop() {
		step()
	}
}
