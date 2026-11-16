package loadshed

import (
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func benchLoad(b *testing.B, kind string, overload float64) {
	b.Helper()
	procs := runtime.GOMAXPROCS(0)
	var factor atomic.Int64
	factor.Store(1)
	w := newWorkload(kind, procs, &factor)
	for b.Loop() {
		mw := LoadShed{}.NewMiddleware()
		o := runLoad(b, mw, w.work, overload*w.capacity, 30*time.Second, overload < 1, 9)
		limits := slices.Clone(o.limits)
		slices.Sort(limits)
		if len(limits) > 0 {
			b.ReportMetric(float64(limits[0]), "limit-min")
			b.ReportMetric(float64(limits[len(limits)/2]), "limit-median")
			b.ReportMetric(float64(limits[len(limits)-1]), "limit-max")
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		b.ReportMetric(float64(o.p99())/float64(time.Millisecond), "p99-ms")
		b.ReportMetric(float64(w.noLoad)/float64(time.Millisecond), "no-load-ms")
		b.ReportMetric(float64(stats.HeapInuse)/(1<<20), "heap-MiB")
		b.ReportMetric(float64(o.served), "status-2xx")
		b.ReportMetric(float64(o.refused), "status-503")
		b.ReportMetric(o.throughput()/w.capacity, "throughput/capacity")
	}
}

func BenchmarkLoadShed_L12_CPUBound_5x(b *testing.B)        { benchLoad(b, "cpu", 5) }
func BenchmarkLoadShed_L12_CPUBoundNoYield_5x(b *testing.B) { benchLoad(b, "cpu-noyield", 5) }
func BenchmarkLoadShed_L12_CPUBound_0_7x(b *testing.B)      { benchLoad(b, "cpu", 0.7) }
func BenchmarkLoadShed_L12_IOBound_5x(b *testing.B)         { benchLoad(b, "io", 5) }
func BenchmarkLoadShed_L12_IOBound_0_7x(b *testing.B)       { benchLoad(b, "io", 0.7) }
