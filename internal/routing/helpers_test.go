package routing

import (
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/test"
)

func randomSegment(rnd *rand.Rand, n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rnd.Intn(len(charset))]
	}
	return string(b)
}

func randomRoute(rnd *rand.Rand) string {
	return fmt.Sprintf("/%s/%s/{id}/%s", randomSegment(rnd, 8), randomSegment(rnd, 8), randomSegment(rnd, 8))
}

func buildBenchRouter(n int) (*Router, []string) {
	rnd := rand.New(rand.NewSource(42))
	r := NewRouter()
	routes := make([]string, n)
	handler := ctx.HTTPHandler(func(c *ctx.HTTPContext) {})
	for i := 0; i < n; i++ {
		routes[i] = randomRoute(rnd)
		r.Add(http.MethodGet, routes[i], "", handler)
	}
	return r, routes
}

func noop(c *ctx.HTTPContext) {}

func addGET(r *Router, routes ...string) *Router {
	for _, route := range routes {
		r.Add(http.MethodGet, route, "", noop)
	}
	return r
}

func patternOf(item *RouterItem) string {
	if item == nil {
		return ""
	}
	return item.Pattern
}

func getPattern(route string) string {
	return MethodRouteVersionToPattern(http.MethodGet, route, "")
}

func sameParams(a, b []string) bool {
	return len(a) == len(b) && (len(a) == 0 || slices.Equal(a, b))
}

func panicText(f func()) (string, bool) {
	var text string
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				text = fmt.Sprint(r)
			}
		}()
		f()
	}()
	return text, panicked
}

func expectRoutePanic(t *testing.T, rule, route, reason string, f func()) {
	t.Helper()
	text, panicked := panicText(f)
	if !panicked {
		t.Error(test.DiffMessage(panicked, true, rule+": "+route+" must panic"))
		return
	}
	if !strings.Contains(text, "invalid route: ") {
		t.Error(test.DiffMessage(text, "invalid route: ...", rule+": panic category of "+route))
	}
	if !strings.Contains(text, route) {
		t.Error(test.DiffMessage(text, route, rule+": panic names the route"))
	}
	if !strings.Contains(text, reason) {
		t.Error(test.DiffMessage(text, reason, rule+": panic reason of "+route))
	}
}

func invariantRoutes() []string {
	return []string{
		"/a/{x}",
		"/a/{x}/b/{y}",
		"/{x}/{y}/{z}",
		"/a/b",
		"/a/*",
		"/*/b/{x}",
		"/all/{x}/1",
		"/a/*.html",
		"/{x}",
		"/b/{x}/*",
	}
}

func randomPath(rnd *rand.Rand, tokens []string, maxSegments int) string {
	n := rnd.Intn(maxSegments + 1)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte('/')
		b.WriteString(tokens[rnd.Intn(len(tokens))])
	}
	if rnd.Intn(4) == 0 {
		b.WriteByte('/')
	}
	return b.String()
}

func staticEqualsRoutes() []string {
	return []string{
		"/", "/users/all", "/users/{id}", "/users/{id}/friends", "/a/b/c", "/a/*", "/a/b/*.html",
		"/x/{y}/z", "/b", "/*/b", "/files/in*.html",
	}
}
