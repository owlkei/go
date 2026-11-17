package loadshed

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func benchAdmit(b *testing.B, c LoadShed) {
	b.Helper()
	mw := c.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodGet, "/ok")
	b.ReportAllocs()
	for b.Loop() {
		mw.Use(r, ex, noop)
		ex.finish(time.Millisecond)
	}
}

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

type twoReads struct{ reads int }

func (b *twoReads) Read(p []byte) (int, error) {
	b.reads++
	if b.reads == 1 {
		return copy(p, "body"), nil
	}
	return 0, io.EOF
}

func (*twoReads) Close() error { return nil }

func bodyAdmission() (step func()) {
	mw := LoadShed{}.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodPost, "/ok")
	body := &twoReads{}
	buf := make([]byte, 8)
	read := func() {
		_, _ = r.Body.Read(buf)
		_, _ = r.Body.Read(buf)
	}
	return func() {
		body.reads = 0
		r.Body = body
		mw.Use(r, ex, read)
		ex.finish(time.Millisecond)
	}
}

func BenchmarkLoadShed_Admitted_AdaptiveBody(b *testing.B) {
	step := bodyAdmission()
	b.ReportAllocs()
	for b.Loop() {
		step()
	}
}

func TestLoadShed_AdaptiveBodyAdmissionAllocatesAtMostOnce(t *testing.T) {
	step := bodyAdmission()
	if got := testing.AllocsPerRun(200, step); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "L8: an admitted request with a body costs one allocation, the wrapper"))
	}
}

func TestLoadShed_AdaptiveAdmissionWithoutBodyAllocatesNothing(t *testing.T) {
	mw := LoadShed{}.NewMiddleware()
	ex := newExchange(&discardWriter{header: http.Header{}})
	r := request(http.MethodGet, "/ok")
	step := func() {
		mw.Use(r, ex, noop)
		ex.finish(time.Millisecond)
	}
	if got := testing.AllocsPerRun(200, step); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "L8: a request without a body adds no allocation"))
	}
}
