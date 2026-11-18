package bench

import (
	"testing"

	"github.com/valyala/fasthttp"
)

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
