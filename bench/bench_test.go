package bench

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/valyala/fasthttp"
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

func BenchmarkNetHTTP(b *testing.B) { benchmarkHandler(b, NewNetHTTP()) }
func BenchmarkGin(b *testing.B)     { benchmarkHandler(b, NewGin()) }
func BenchmarkEcho(b *testing.B)    { benchmarkHandler(b, NewEcho()) }
func BenchmarkChi(b *testing.B)     { benchmarkHandler(b, NewChi()) }

func BenchmarkFiber(b *testing.B) {
	handler := NewFiber().Handler()
	for _, s := range Scenarios {
		b.Run(s.Name, func(b *testing.B) {
			var request fasthttp.Request
			request.Header.SetMethod(s.Method)
			request.SetRequestURI(s.Path)
			if s.ContentType != "" {
				request.Header.SetContentType(s.ContentType)
			}
			if s.Auth {
				request.Header.Set(authHeader, authValue)
			}
			request.SetBodyString(s.Body)
			var fctx fasthttp.RequestCtx

			b.ReportAllocs()
			for b.Loop() {
				fctx.Init(&request, nil, nil)
				fctx.Response.Reset()
				handler(&fctx)
			}
		})
	}
}
