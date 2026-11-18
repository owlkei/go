package compress

import (
	"net/http"
	"testing"
)

func BenchmarkCompress_JSON2K(b *testing.B) {
	benchCompress(b, Compress{}, payload(2<<10), gzipRequest(http.MethodGet))
}

func BenchmarkCompress_JSON16K(b *testing.B) {
	benchCompress(b, Compress{}, payload(16<<10), gzipRequest(http.MethodGet))
}

func BenchmarkCompress_JSON256K(b *testing.B) {
	benchCompress(b, Compress{}, payload(256<<10), gzipRequest(http.MethodGet))
}

func BenchmarkCompress_NotCandidate(b *testing.B) {
	benchCompress(b, Compress{}, payload(16<<10), request(http.MethodGet, "/any"))
}

func BenchmarkCompress_TooSmall(b *testing.B) {
	benchCompress(b, Compress{}, payload(512), gzipRequest(http.MethodGet))
}

func BenchmarkCompress_NoWriterFree(b *testing.B) {
	body := payload(16 << 10)
	mw := Compress{}.NewMiddleware()
	held := make([]*exchange, 8)
	for i := range held {
		held[i] = newExchange(&outputCounter{header: http.Header{}})
		mw.Use(gzipRequest(http.MethodGet), held[i], func() {
			held[i].Header()["Content-Type"] = jsonType
			_, _ = held[i].Write(body)
		})
	}
	w := &outputCounter{header: http.Header{}}
	r := gzipRequest(http.MethodGet)
	b.ReportAllocs()
	cpu := processCPU()
	for b.Loop() {
		clear(w.header)
		ex := newExchange(w)
		mw.Use(r, ex, func() {
			ex.Header()["Content-Type"] = jsonType
			_, _ = ex.Write(body)
		})
		ex.done()
	}
	b.ReportMetric(float64(w.bytes)/float64(len(body))/float64(b.N), "bytes-out/byte-in")
	b.ReportMetric(float64(processCPU()-cpu)/float64(b.N), "cpu-ns/op")
	for _, ex := range held {
		ex.done()
	}
}
