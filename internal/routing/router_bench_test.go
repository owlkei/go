package routing

import (
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
)

func BenchmarkRouterAdd(b *testing.B) {
	rnd := rand.New(rand.NewSource(7))
	routes := make([]string, 1000)
	for i := range routes {
		routes[i] = randomRoute(rnd)
	}
	r := NewRouter()
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		if i == len(routes) {
			b.StopTimer()
			r = NewRouter()
			i = 0
			b.StartTimer()
		}
		r.Add(http.MethodGet, routes[i], "", nil)
		i++
	}
}

func BenchmarkRouterUse(b *testing.B) {
	noop := ctx.HTTPHandler(func(c *ctx.HTTPContext) {})
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r, _ := buildBenchRouter(100)
		b.StartTimer()
		r.Use(noop)
	}
}

func BenchmarkRouterMatch_Static(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	r.Add(http.MethodGet, "/users/all", "", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/users/all", "", nil)
	}
}

func BenchmarkRouterMatch_StaticTrailingSlash(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	r.Add(http.MethodGet, "/users/all", "", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/users/all/", "", nil)
	}
}

func BenchmarkRouterMatch_Param(b *testing.B) {
	r, routes := buildBenchRouter(1000)
	requestPath := strings.Replace(routes[len(routes)/2], "{id}", "123", 1)
	buf := make([]string, 0, 4)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, requestPath, "", buf[:0])
	}
}

func BenchmarkRouterMatch_ParamNoBuffer(b *testing.B) {
	r, routes := buildBenchRouter(1000)
	requestPath := strings.Replace(routes[len(routes)/2], "{id}", "123", 1)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, requestPath, "", nil)
	}
}

func BenchmarkRouterMatch_Wildcard(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	r.Add(http.MethodGet, "/assets/*", "", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/assets/css/site/main.css", "", nil)
	}
}

func BenchmarkRouterMatch_Glob(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	r.Add(http.MethodGet, "/files/in*.html", "", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/files/index.html", "", nil)
	}
}

func BenchmarkRouterMatch_NoMatch(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/notexist/route/", "", nil)
	}
}

func BenchmarkRouterMatch_DotSegments(b *testing.B) {
	r, _ := buildBenchRouter(1000)
	r.Add(http.MethodGet, "/users/all", "", nil)
	b.ReportAllocs()
	for b.Loop() {
		r.Match(http.MethodGet, "/users/x/../all", "", nil)
	}
}

func BenchmarkRouterMatch_Parallel(b *testing.B) {
	r, routes := buildBenchRouter(1000)
	requestPath := strings.Replace(routes[len(routes)/2], "{id}", "123", 1)
	var total atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		buf := make([]string, 0, 4)
		var count int64
		for pb.Next() {
			if item, _ := r.Match(http.MethodGet, requestPath, "", buf[:0]); item != nil {
				count++
			}
		}
		total.Add(count)
	})
}
