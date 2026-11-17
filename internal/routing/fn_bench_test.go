package routing

import (
	"net/http"
	"testing"
)

func BenchmarkPatternToMethodRouteVersion(b *testing.B) {
	pattern := MethodRouteVersionToPattern(http.MethodGet, "/users/{userId}/friends/{friendId}", "v2")
	b.ReportAllocs()
	for b.Loop() {
		PatternToMethodRouteVersion(pattern)
	}
}

func BenchmarkParseToParamKey(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseToParamKey("/users/{a}/friends/{b}/schools/{c}/subjects/{d}/")
	}
}

func BenchmarkMethodRouteVersionToPattern(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		MethodRouteVersionToPattern(http.MethodGet, "/users/{userId}/all", "v2")
	}
}
