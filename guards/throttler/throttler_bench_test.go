package throttler

import (
	"testing"
)

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
