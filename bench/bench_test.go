package bench

import (
	"bytes"
	"net/http"
	"testing"
)

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(code int)        { w.code = code }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

type bodyReader struct {
	*bytes.Reader
}

func (bodyReader) Close() error { return nil }

func benchmarkHandler(b *testing.B, handler http.Handler) {
	for _, s := range Scenarios {
		b.Run(s.Name, func(b *testing.B) {
			r := s.Request()
			payload := []byte(s.Body)
			reader := bodyReader{bytes.NewReader(payload)}
			w := &discardWriter{header: http.Header{}}

			b.ReportAllocs()
			for b.Loop() {
				reader.Reset(payload)
				r.Body = reader
				r.ContentLength = int64(len(payload))
				clear(w.header)
				w.code = 0
				handler.ServeHTTP(w, r)
			}
		})
	}
}

func BenchmarkGinject(b *testing.B) { benchmarkHandler(b, NewGinject()) }
