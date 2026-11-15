package routing

import (
	"fmt"
	"math/rand"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

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

func TestRouter_Match_Root(t *testing.T) {
	r := addGET(NewRouter(), "/")
	for _, path := range []string{"/", "", "//"} {
		item, params := r.Match(http.MethodGet, path, "", nil)
		if patternOf(item) != getPattern("/") || len(params) != 0 {
			t.Error(test.DiffMessage(patternOf(item), getPattern("/"), "R1: root route matches "+path))
		}
	}
}

func TestRouter_Match_RootRegisteredEmpty(t *testing.T) {
	r := addGET(NewRouter(), "")
	for _, path := range []string{"/", ""} {
		item, _ := r.Match(http.MethodGet, path, "", nil)
		if item == nil {
			t.Error(test.DiffMessage(item, "an item", "R1: route registered as \"\" matches "+path))
		}
	}
}

func TestRouter_Match_MissWithRootRoute(t *testing.T) {
	r := addGET(NewRouter(), "/")
	for _, path := range []string{"/nope/at/all", "/users", "/nope"} {
		if item, params := r.Match(http.MethodGet, path, "", nil); item != nil || len(params) != 0 {
			t.Error(test.DiffMessage(patternOf(item), "", "R2: "+path+" is a miss"))
		}
	}
}

func TestRouter_Match_KeepsSpaces(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}")
	item, params := r.Match(http.MethodGet, "/users/a b", "", nil)
	if item == nil || !slices.Equal(params, []string{"a b"}) {
		t.Error(test.DiffMessage(params, []string{"a b"}, "R3: spaces stay in values"))
	}
}

func TestRouter_Match_KeepsStars(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}")
	item, params := r.Match(http.MethodGet, "/users/a**b", "", nil)
	if item == nil || !slices.Equal(params, []string{"a**b"}) {
		t.Error(test.DiffMessage(params, []string{"a**b"}, "R3: stars stay in values"))
	}
}

func TestRouter_Match_SpaceInStaticIsMiss(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}")
	if item, _ := r.Match(http.MethodGet, "/us ers/1", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R3: request bytes are not rewritten"))
	}
}

func TestRouter_Match_LiteralDollarParam(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}", "/users/{userId}/friends/{friendId}")
	item, params := r.Match(http.MethodGet, "/users/$", "", nil)
	if item == nil || !slices.Equal(params, []string{"$"}) {
		t.Error(test.DiffMessage(params, []string{"$"}, "R4: a literal $ is a value"))
	}
}

func TestRouter_Match_LiteralDollarDeep(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}", "/users/{userId}/friends/{friendId}")
	item, params := r.Match(http.MethodGet, "/users/$/friends/123", "", nil)
	if item == nil || !slices.Equal(params, []string{"$", "123"}) {
		t.Error(test.DiffMessage(params, []string{"$", "123"}, "R4: a literal $ keeps parameters aligned"))
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

func TestRouter_Match_ParamCountInvariant(t *testing.T) {
	r := addGET(NewRouter(), invariantRoutes()...)
	rnd := rand.New(rand.NewSource(1))
	tokens := []string{"a", "b", "all", "$", "*", "1"}
	for i := 0; i < 5000; i++ {
		p := randomPath(rnd, tokens, 5)
		item, params := r.Match(http.MethodGet, p, "", nil)
		if item == nil {
			continue
		}
		if len(params) != len(item.ParamNames) {
			t.Error(test.DiffMessage(len(params), len(item.ParamNames), "R5: "+p+" matched "+item.Pattern))
		}
	}
}

func TestRouter_Match_ParamCountInvariantAppends(t *testing.T) {
	r := addGET(NewRouter(), "/a/{x}/b/{y}")
	input := []string{"keep"}
	item, params := r.Match(http.MethodGet, "/a/1/b/2", "", input)
	if item == nil || len(params)-len(input) != len(item.ParamNames) {
		t.Error(test.DiffMessage(len(params)-len(input), 2, "R5: values added to a non-empty input"))
	}
}

func TestRouter_Match_TrailingSlash(t *testing.T) {
	r := addGET(NewRouter(), "/users/all", "/users/{id}")
	for _, p := range []string{"/users/all", "/users/all/"} {
		if item, _ := r.Match(http.MethodGet, p, "", nil); patternOf(item) != getPattern("/users/all") {
			t.Error(test.DiffMessage(patternOf(item), getPattern("/users/all"), "R6: "+p))
		}
	}
	item, params := r.Match(http.MethodGet, "/users/7/", "", nil)
	if item == nil || !slices.Equal(params, []string{"7"}) {
		t.Error(test.DiffMessage(params, []string{"7"}, "R6: trailing slash after a parameter"))
	}
}

func TestRouter_Match_RepeatedSlashes(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}")
	item, params := r.Match(http.MethodGet, "//users///7//", "", nil)
	if item == nil || !slices.Equal(params, []string{"7"}) {
		t.Error(test.DiffMessage(params, []string{"7"}, "R6: repeated slashes"))
	}
}

func TestRouter_Match_DotSegments(t *testing.T) {
	r := addGET(NewRouter(), "/f/{id}", "/g/x")
	item, params := r.Match(http.MethodGet, "/g/../f/1", "", nil)
	if item == nil || item.Pattern != getPattern("/f/{id}") || !slices.Equal(params, []string{"1"}) {
		t.Error(test.DiffMessage(patternOf(item), getPattern("/f/{id}"), "R6: /g/../f/1"))
	}
	item, _ = r.Match(http.MethodGet, "/g/./x", "", nil)
	if patternOf(item) != getPattern("/g/x") {
		t.Error(test.DiffMessage(patternOf(item), getPattern("/g/x"), "R6: /g/./x"))
	}
}

func TestRouter_Match_DotDotAboveRoot(t *testing.T) {
	r := addGET(NewRouter(), "/f/{id}", "/")
	item, params := r.Match(http.MethodGet, "/../../f/1", "", nil)
	if item == nil || item.Pattern != getPattern("/f/{id}") || !slices.Equal(params, []string{"1"}) {
		t.Error(test.DiffMessage(patternOf(item), getPattern("/f/{id}"), "R6: .. above the root stays at the root"))
	}
	item, _ = r.Match(http.MethodGet, "/..", "", nil)
	if patternOf(item) != getPattern("/") {
		t.Error(test.DiffMessage(patternOf(item), getPattern("/"), "R6: /.. is the root"))
	}
}

func TestRouter_Match_FastEqualsCleaned(t *testing.T) {
	r := addGET(NewRouter(), "/", "/a/b", "/a/{x}", "/{x}/b", "/a/*", "/{x}/{y}/{z}", "/a b/{x}", "/*.html")
	rnd := rand.New(rand.NewSource(2))
	tokens := []string{"a", "b", "/", ".", "$", "*", " ", "..", "x.html"}
	for i := 0; i < 10000; i++ {
		var b strings.Builder
		for j, n := 0, rnd.Intn(8); j < n; j++ {
			b.WriteString(tokens[rnd.Intn(len(tokens))])
		}
		p := b.String()
		gotItem, gotParams := r.Match(http.MethodGet, p, "", nil)
		wantItem, wantParams := r.Match(http.MethodGet, path.Clean("/"+p), "", nil)
		if gotItem != wantItem || !sameParams(gotParams, wantParams) {
			t.Error(test.DiffMessage(patternOf(gotItem), patternOf(wantItem), fmt.Sprintf("R6: Match(%q) equals Match(Clean)", p)))
			return
		}
	}
}

func TestRouter_Match_MethodAware(t *testing.T) {
	r := NewRouter()
	r.Add(http.MethodGet, "/items/all", "", noop)
	r.Add(http.MethodPost, "/items/{id}", "", noop)
	item, params := r.Match(http.MethodPost, "/items/all", "", nil)
	if item == nil || item.Method != http.MethodPost || !slices.Equal(params, []string{"all"}) {
		t.Error(test.DiffMessage(params, []string{"all"}, "R7: POST /items/all reaches the POST route"))
	}
	item, _ = r.Match(http.MethodGet, "/items/all", "", nil)
	if item == nil || item.Method != http.MethodGet {
		t.Error(test.DiffMessage(patternOf(item), "GET item", "R7: GET /items/all"))
	}
	if item, _ := r.Match(http.MethodGet, "/items/7", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: GET has no parameter route"))
	}
}

func TestRouter_Match_VersionAware(t *testing.T) {
	r := NewRouter()
	r.Add(http.MethodGet, "/a/all", "1", noop)
	r.Add(http.MethodGet, "/a/{id}", "2", noop)
	item, params := r.Match(http.MethodGet, "/a/all", "2", nil)
	if item == nil || item.Version != "2" || !slices.Equal(params, []string{"all"}) {
		t.Error(test.DiffMessage(params, []string{"all"}, "R7: version 2 owns /a/{id}"))
	}
	item, params = r.Match(http.MethodGet, "/a/all", "1", nil)
	if item == nil || item.Version != "1" || len(params) != 0 {
		t.Error(test.DiffMessage(patternOf(item), "version 1 item", "R7: version 1 owns /a/all"))
	}
}

func TestRouter_Match_UnknownMethod(t *testing.T) {
	r := addGET(NewRouter(), "/a")
	if item, _ := r.Match(http.MethodDelete, "/a", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: DELETE was never registered"))
	}
	if item, _ := r.Match("BREW", "/a", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: unknown method"))
	}
}

func TestRouter_Match_UnknownVersion(t *testing.T) {
	r := addGET(NewRouter(), "/a")
	if item, _ := r.Match(http.MethodGet, "/a", "v9", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: unknown version"))
	}
}

func TestRouterMatchSamePathDifferentMethodAndVersion(t *testing.T) {
	r := NewRouter()
	r.Add(http.MethodGet, "/users/{id}", "", noop)
	r.Add(http.MethodPost, "/users/{id}", "", noop)
	r.Add(http.MethodGet, "/users/{id}", "v2", noop)

	item, params := r.Match(http.MethodGet, "/users/123/", "", nil)
	if item == nil || len(item.Handlers) != 1 || !slices.Equal(params, []string{"123"}) {
		t.Error(test.DiffMessage(item, "an item", "R7: GET, no version should match"))
	}
	if item, _ = r.Match(http.MethodPost, "/users/123/", "", nil); item == nil || len(item.Handlers) != 1 {
		t.Error(test.DiffMessage(item, "an item", "R7: POST, no version should match"))
	}
	if item, _ = r.Match(http.MethodGet, "/users/123/", "v2", nil); item == nil || len(item.Handlers) != 1 {
		t.Error(test.DiffMessage(item, "an item", "R7: GET, v2 should match"))
	}
	if item, _ = r.Match(http.MethodDelete, "/users/123/", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: DELETE was never registered"))
	}
	if item, _ = r.Match(http.MethodPost, "/users/123/", "v2", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R7: POST + v2 was never registered"))
	}
}

func TestRouter_Match_PathTooLong(t *testing.T) {
	r := addGET(NewRouter(), "/*")
	atLimit := "/" + strings.Repeat("a", MaxPathLength-1)
	if len(atLimit) != MaxPathLength {
		t.Fatal("setup")
	}
	if item, _ := r.Match(http.MethodGet, atLimit, "", nil); item == nil {
		t.Error(test.DiffMessage(item, "an item", "R8: 4096 bytes match"))
	}
	if item, _ := r.Match(http.MethodGet, atLimit+"a", "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R8: 4097 bytes miss"))
	}
}

func TestRouter_Match_TooManySegments(t *testing.T) {
	r := addGET(NewRouter(), "/*")
	if item, _ := r.Match(http.MethodGet, strings.Repeat("/a", MaxPathSegments), "", nil); item == nil {
		t.Error(test.DiffMessage(item, "an item", "R8: 64 segments match"))
	}
	if item, _ := r.Match(http.MethodGet, strings.Repeat("/a", MaxPathSegments+1), "", nil); item != nil {
		t.Error(test.DiffMessage(item.Pattern, "", "R8: 65 segments miss"))
	}
}

func TestRouter_Add_PanicsOnPartialParam(t *testing.T) {
	reason := "parameter must be a whole segment like {name}"
	expectRoutePanic(t, "R9", "/files/{name}.json", reason, func() {
		NewRouter().Add(http.MethodGet, "/files/{name}.json", "", noop)
	})
	expectRoutePanic(t, "R9", "/files/pre{name}", reason, func() {
		NewRouter().Add(http.MethodGet, "/files/pre{name}", "", noop)
	})
	expectRoutePanic(t, "R9", "/a/{b/c}", reason, func() {
		NewRouter().Add(http.MethodGet, "/a/{b/c}", "", noop)
	})
	expectRoutePanic(t, "R9", "/a/{x", reason, func() {
		NewRouter().Add(http.MethodGet, "/a/{x", "", noop)
	})
	expectRoutePanic(t, "R9", "/a/x}", reason, func() {
		NewRouter().Add(http.MethodGet, "/a/x}", "", noop)
	})
}

func TestRouter_Add_PanicsOnEmptyParam(t *testing.T) {
	expectRoutePanic(t, "R9", "/a/{}", "parameter must be a whole segment like {name}", func() {
		NewRouter().Add(http.MethodGet, "/a/{}", "", noop)
	})
}

func TestRouter_Add_PanicsOnTwoParamsInSegment(t *testing.T) {
	expectRoutePanic(t, "R9", "/a/{x}{y}", "parameter must be a whole segment like {name}", func() {
		NewRouter().Add(http.MethodGet, "/a/{x}{y}", "", noop)
	})
}

func TestRouter_Add_PanicsOnTooManySegments(t *testing.T) {
	route := strings.Repeat("/a", MaxPathSegments+1)
	expectRoutePanic(t, "R9", route, "more than 64 segments", func() {
		NewRouter().Add(http.MethodGet, route, "", noop)
	})
	if _, panicked := panicText(func() {
		NewRouter().Add(http.MethodGet, strings.Repeat("/a", MaxPathSegments), "", noop)
	}); panicked {
		t.Error(test.DiffMessage(panicked, false, "R9: 64 segments are accepted"))
	}
}

func TestRouter_Add_AllowsStarInParamName(t *testing.T) {
	r := NewRouter()
	if _, panicked := panicText(func() { r.Add(http.MethodGet, "/f/{feed*Id}", "", noop) }); panicked {
		t.Error(test.DiffMessage(panicked, false, "R9: {feed*Id} is valid"))
	}
	item, params := r.Match(http.MethodGet, "/f/1", "", nil)
	if item == nil || !slices.Equal(params, []string{"1"}) || !slices.Equal(item.ParamNames, []string{"feed*Id"}) {
		t.Error(test.DiffMessage(params, []string{"1"}, "R9: {feed*Id} captures"))
	}
}

func TestRouter_For_PanicsOnPartialParam(t *testing.T) {
	expectRoutePanic(t, "R9", "/files/{name}.json", "parameter must be a whole segment like {name}", func() {
		NewRouter().For([]string{http.MethodGet}, "/files/{name}.json", "")(noop)
	})
}

func TestRouter_AddInjectableHandler_PanicsOnPartialParam(t *testing.T) {
	expectRoutePanic(t, "R9", "/files/{name}.json", "parameter must be a whole segment like {name}", func() {
		NewRouter().AddInjectableHandler(http.MethodGet, "/files/{name}.json", "", func() {})
	})
}

func TestRouter_Item_Pattern(t *testing.T) {
	r := NewRouter()
	r.Add(http.MethodPost, "/users/{id}", "v2", noop)
	item, _ := r.Match(http.MethodPost, "/users/1", "v2", nil)
	want := MethodRouteVersionToPattern(http.MethodPost, "/users/{id}", "v2")
	if item == nil || item.Pattern != want || item.Method != http.MethodPost || item.Version != "v2" {
		t.Error(test.DiffMessage(patternOf(item), want, "R10: Pattern, Method, Version"))
	}
}

func TestRouter_Item_ParamKeysAndNames(t *testing.T) {
	r := addGET(NewRouter(), "/s/{a}/t/{b}/{a}", "/plain")
	item, params := r.Match(http.MethodGet, "/s/1/t/2/3", "", nil)
	if item == nil {
		t.Fatal("R10: no item")
	}
	if !reflect.DeepEqual(item.ParamKeys, map[string][]int{"a": {0, 2}, "b": {1}}) {
		t.Error(test.DiffMessage(item.ParamKeys, map[string][]int{"a": {0, 2}, "b": {1}}, "R10: ParamKeys"))
	}
	if !slices.Equal(item.ParamNames, []string{"a", "b", "a"}) {
		t.Error(test.DiffMessage(item.ParamNames, []string{"a", "b", "a"}, "R10: ParamNames"))
	}
	if !slices.Equal(params, []string{"1", "2", "3"}) {
		t.Error(test.DiffMessage(params, []string{"1", "2", "3"}, "R10: values in path order"))
	}
	plain, _ := r.Match(http.MethodGet, "/plain", "", nil)
	if plain == nil || plain.ParamNames != nil {
		t.Error(test.DiffMessage(plain, "an item with nil ParamNames", "R10: no parameters"))
	}
}

func TestRouter_Item_IndexDense(t *testing.T) {
	r := NewRouter()
	r.Add(http.MethodGet, "/a", "", noop)
	r.Add(http.MethodPost, "/a", "", noop)
	r.Add(http.MethodGet, "/b", "", noop)
	r.Add(http.MethodGet, "/a", "v2", noop)
	r.Add(http.MethodGet, "/a", "", noop)
	r.Use(noop)
	r.For([]string{http.MethodGet}, "/b", "")(noop)

	want := map[string]int{"/a|GET|": 0, "/a|POST|": 1, "/b|GET|": 2, "/a|GET|v2": 3}
	for key, index := range want {
		parts := strings.Split(key, "|")
		item, _ := r.Match(parts[1], parts[0], parts[2], nil)
		if item == nil || item.Index != index {
			got := -1
			if item != nil {
				got = item.Index
			}
			t.Error(test.DiffMessage(got, index, "R10: Index of "+key))
		}
	}
}

func TestRouter_Item_PointerStableAfterUse(t *testing.T) {
	r := addGET(NewRouter(), "/a")
	before, _ := r.Match(http.MethodGet, "/a", "", nil)
	r.Use(noop)
	r.For([]string{http.MethodGet}, "/a", "")(noop)
	r.Add(http.MethodGet, "/a", "", noop)
	after, _ := r.Match(http.MethodGet, "/a", "", nil)
	if before == nil || before != after {
		t.Error(test.DiffMessage(after, before, "R10: the item pointer is stable"))
	}
	if after != nil && len(after.Handlers) != 3 {
		t.Error(test.DiffMessage(len(after.Handlers), 3, "R10: the stable item sees later handlers"))
	}
}

func TestAddInjectableHandler(t *testing.T) {
	r := NewRouter()
	handler := func() {}
	r.AddInjectableHandler(http.MethodGet, "/users/{userId}", "", handler)

	pattern := MethodRouteVersionToPattern(http.MethodGet, "/users/{userId}", "")
	if _, ok := r.InjectableHandlers[pattern]; !ok {
		t.Error(test.DiffMessage(ok, true, "R10: injectable handler is stored under its pattern"))
	}

	item, _ := r.Match(http.MethodGet, "/users/123/", "", nil)
	if item == nil {
		t.Error(test.DiffMessage(item, "an item", "R10: AddInjectableHandler also registers the route"))
	}
}

func TestAddInjectableHandler_SetsInjectable(t *testing.T) {
	r := NewRouter()
	handler := func() {}
	r.AddInjectableHandler(http.MethodGet, "/users/{userId}", "", handler)
	r.Add(http.MethodGet, "/plain", "", noop)

	item, _ := r.Match(http.MethodGet, "/users/1", "", nil)
	if item == nil || item.Injectable == nil ||
		reflect.ValueOf(item.Injectable).Pointer() != reflect.ValueOf(handler).Pointer() {
		t.Error(test.DiffMessage(item, "an item with Injectable set", "R10: Injectable"))
	}
	if item != nil && reflect.ValueOf(r.InjectableHandlers[item.Pattern]).Pointer() != reflect.ValueOf(handler).Pointer() {
		t.Error(test.DiffMessage(r.InjectableHandlers[item.Pattern], handler, "R10: InjectableHandlers keeps the same value"))
	}
	plain, _ := r.Match(http.MethodGet, "/plain", "", nil)
	if plain == nil || plain.Injectable != nil {
		t.Error(test.DiffMessage(plain, "an item without Injectable", "R10: Injectable is nil for Add"))
	}
}

func TestAddInjectableHandlerPanicsOnNil(t *testing.T) {
	text, panicked := panicText(func() {
		NewRouter().AddInjectableHandler(http.MethodGet, "/x", "", nil)
	})
	if !panicked || !strings.Contains(text, "invalid handler: ") {
		t.Error(test.DiffMessage(text, "invalid handler: ...", "R10: nil handler panics"))
	}
}

func TestAddInjectableHandlerPanicsOnNonFunc(t *testing.T) {
	text, panicked := panicText(func() {
		NewRouter().AddInjectableHandler(http.MethodGet, "/x", "", "not a func")
	})
	if !panicked || !strings.Contains(text, "invalid handler: ") {
		t.Error(test.DiffMessage(text, "invalid handler: ...", "R10: non-function handler panics"))
	}
}

func TestRouterMiddleware(t *testing.T) {
	var calls []int
	tag := func(n int) ctx.HTTPHandler {
		return func(c *ctx.HTTPContext) { calls = append(calls, n) }
	}
	run := func(item *RouterItem) []int {
		calls = nil
		c := ctx.NewHTTPContext()
		for _, handler := range item.Handlers {
			handler(c)
		}
		return calls
	}
	expect := func(rule string, item *RouterItem, want []int) {
		t.Helper()
		if item == nil {
			t.Error(test.DiffMessage(item, want, rule+": item exists"))
			return
		}
		if got := run(item); !slices.Equal(got, want) {
			t.Error(test.DiffMessage(got, want, rule+": handler order"))
		}
	}

	r0 := NewRouter()
	r0.Use(tag(1))
	r0.For(HTTPMethods, "/test0", "")(tag(1))
	for _, method := range HTTPMethods {
		r0.Add(method, "/test0", "", tag(1))
	}
	item, _ := r0.Match(http.MethodHead, "/test0/", "", nil)
	expect("R11 router 0", item, []int{1, 1, 1})

	r1 := NewRouter()
	r1.Use(tag(1))
	r1.Use(tag(2))
	for _, method := range HTTPMethods {
		r1.Add(method, "/test1", "", tag(4))
	}
	r1.For(HTTPMethods, "/test1", "")(tag(3))
	r1.Use(tag(1))
	item, _ = r1.Match(http.MethodPatch, "/test1/", "", nil)
	expect("R11 router 1", item, []int{1, 2, 4, 3, 1})

	r2 := NewRouter()
	r2.For(HTTPMethods, "/test2/{param}", "")(tag(1))
	r2.Use(tag(2))
	for _, method := range HTTPMethods {
		r2.Add(method, "/test2/{param}", "", tag(3))
	}
	r2.For(HTTPMethods, "/test2/{param}", "")(tag(4))
	item, _ = r2.Match(http.MethodOptions, "/test2/123/", "", nil)
	expect("R11 router 2", item, []int{1, 2, 3, 4})
}

func TestRouterMiddleware_AddReplacesMainHandler(t *testing.T) {
	var calls []int
	tag := func(n int) ctx.HTTPHandler {
		return func(c *ctx.HTTPContext) { calls = append(calls, n) }
	}
	r := NewRouter()
	r.Use(tag(1))
	r.Add(http.MethodGet, "/a", "", tag(2))
	r.For([]string{http.MethodGet}, "/a", "")(tag(3))
	r.Add(http.MethodGet, "/a", "", tag(4))
	item, _ := r.Match(http.MethodGet, "/a", "", nil)
	if item == nil {
		t.Fatal("R11: no item")
	}
	c := ctx.NewHTTPContext()
	for _, handler := range item.Handlers {
		handler(c)
	}
	if want := []int{1, 3, 4}; !slices.Equal(calls, want) {
		t.Error(test.DiffMessage(calls, want, "R11: a second Add replaces the first and moves to the end"))
	}
}

func TestRouterMatch(t *testing.T) {
	cases := map[string]string{
		"/lv1/ping/":                             "/lv1/ping",
		"/lv1/642e948adef44c303cdd2df3/":         "/lv1/{id1}",
		"/lv1/foo/bar/":                          "/lv1/*",
		"/lv1/":                                  "/lv1/*",
		"/lv1/lv2/pong/":                         "/lv1/lv2/pong",
		"/lv1/lv2/642e951525714c1ec609b338/":     "/lv1/lv2/{id2}",
		"/lv1/lv2/foo/bar/":                      "/lv1/lv2/*",
		"/lv1/lv2/file/index.html/":              "lv1/lv2/file/in*.html",
		"/lv1/lv2/file/in.html/":                 "lv1/lv2/file/in*.html",
		"/lv1/lv2/file/image.png/":               "lv1/lv2/file/image.*",
		"/lv1/lv2/lv3/peng/":                     "/lv1/lv2/lv3/peng",
		"/lv1/lv2/lv3/642e95c4fbb2ad847ca96840/": "/lv1/lv2/lv3/{id3}",
		"/lv1/lv2/lv3/foo/bar/":                  "/lv1/lv2/lv3/*",
		"/lv1/lv2/lv3/":                          "/lv1/lv2/lv3/*",
		"/lv1/lv2/lv3/file/index.html/":          "lv1/lv2/lv3/file/in*.html",
		"/lv1/lv2/lv3/file/in.html/":             "lv1/lv2/lv3/file/in*.html",
		"/lv1/lv2/lv3/file/image.jpeg/":          "lv1/lv2/lv3/file/image.*",
		"/api/feeds/{feedApiId}/next/files/index.html/endpoint/any/things/after/": "/*/feeds/{feed*Id}/*/files/*.html/*/",
		"/users/633b0aa5d7fc3578b655b9bd/friends/633b0af45f4fe7d45b00fba5/":       "/users/{userId}/friends/{friendId}",
	}

	r := NewRouter()
	for _, route := range cases {
		for _, method := range HTTPMethods {
			r.Add(method, route, "", nil)
		}
	}

	for requestedRoute, expectedRoute := range cases {
		expected := MethodRouteVersionToPattern(http.MethodPost, expectedRoute, "")
		item, _ := r.Match(http.MethodPost, requestedRoute, "", nil)
		if actual := patternOf(item); actual != expected {
			t.Error(test.DiffMessage(actual, expected, "existing: "+requestedRoute+" should match "+expectedRoute))
		}
	}
}

func staticEqualsRoutes() []string {
	return []string{
		"/", "/users/all", "/users/{id}", "/users/{id}/friends", "/a/b/c", "/a/*", "/a/b/*.html",
		"/x/{y}/z", "/b", "/*/b", "/files/in*.html",
	}
}

func TestRouter_Match_StaticEqualsTrie(t *testing.T) {
	r := NewRouter()
	for _, route := range staticEqualsRoutes() {
		r.Add(http.MethodGet, route, "", noop)
		r.Add(http.MethodPost, route, "v2", noop)
	}
	check := func(p, version string) {
		t.Helper()
		gotItem, gotParams := r.Match(http.MethodGet, p, version, nil)
		wantItem, wantParams := r.matchTrie(http.MethodGet, p, version, nil)
		if gotItem != wantItem || !sameParams(gotParams, wantParams) {
			t.Error(test.DiffMessage(patternOf(gotItem), patternOf(wantItem), fmt.Sprintf("R12: Match(%q) equals matchTrie", p)))
		}
	}
	for _, route := range []string{"/", "/users/all", "/a/b/c", "/b"} {
		check(route, "")
		check(route+"/", "")
		check(route, "v2")
	}
	rnd := rand.New(rand.NewSource(3))
	tokens := []string{"a", "b", "c", "all", "users", "1", "x", "z", "files", "index.html", "in.html", "friends"}
	for i := 0; i < 10000; i++ {
		check(randomPath(rnd, tokens, 5), "")
	}
}

func TestRouter_Match_StaticNoAlloc(t *testing.T) {
	r := addGET(NewRouter(), "/users/all", "/users/{id}", "/a/b/c")
	for _, p := range []string{"/users/all", "/users/all/"} {
		allocs := testing.AllocsPerRun(100, func() {
			r.Match(http.MethodGet, p, "", nil)
		})
		if allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "R13: allocations of a static match "+p))
		}
	}
}

func TestRouter_Match_MissNoAlloc(t *testing.T) {
	r := addGET(NewRouter(), "/", "/users/all", "/users/{id}")
	for _, p := range []string{"/notexist/route/", "/users", "/users/1/2/3"} {
		allocs := testing.AllocsPerRun(100, func() {
			r.Match(http.MethodGet, p, "", nil)
		})
		if allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "R13: allocations of a miss "+p))
		}
	}
}

func TestRouter_Match_ConcurrentNoRace(t *testing.T) {
	r := addGET(NewRouter(), "/users/{id}", "/users/all", "/a/*", "/files/*.html")
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				item, params := r.Match(http.MethodGet, "/users/42", "", nil)
				if item == nil || !slices.Equal(params, []string{"42"}) {
					t.Error(test.DiffMessage(params, []string{"42"}, "concurrent: parameter route"))
					return
				}
				if item, _ := r.Match(http.MethodGet, "/users/all", "", nil); item == nil {
					t.Error(test.DiffMessage(item, "an item", "concurrent: static route"))
					return
				}
				if item, _ := r.Match(http.MethodGet, "/nope", "", nil); item != nil {
					t.Error(test.DiffMessage(item.Pattern, "", "concurrent: miss"))
					return
				}
			}
		}()
	}
	wg.Wait()
}
