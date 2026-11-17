package throttler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/memorycache"
)

func benchGuard(b *testing.B, strategy Strategy) activator {
	return compile(b, Throttler{
		Limit:    1 << 40,
		TTL:      time.Minute,
		Strategy: strategy,
		KeyFunc:  func(*ctx.HTTPContext) string { return "benchkey" },
		Backend:  memorycache.NewMemoryCache(nil),
	})
}

func benchCtx() *ctx.HTTPContext {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	c := ctx.NewHTTPContext()
	c.Request = req
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func BenchmarkCanActivate(b *testing.B) {
	for _, s := range allStrategies {
		b.Run(s.name, func(b *testing.B) {
			g := benchGuard(b, s.strategy)
			c := benchCtx()
			for b.Loop() {
				g.CanActivate(c)
			}
		})
	}
}

func BenchmarkCanActivate_Parallel(b *testing.B) {
	for _, s := range allStrategies {
		b.Run(s.name, func(b *testing.B) {
			g := benchGuard(b, s.strategy)
			b.RunParallel(func(pb *testing.PB) {
				c := benchCtx()
				for pb.Next() {
					g.CanActivate(c)
				}
			})
		})
	}
}

func BenchmarkCanActivate_DefaultKey(b *testing.B) {
	g := compile(b, Throttler{Limit: 1 << 40})
	c := benchCtx()
	for b.Loop() {
		g.CanActivate(c)
	}
}
