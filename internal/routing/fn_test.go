package routing

import (
	"net/http"
	"reflect"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestPatternToMethodRouteVersion(t *testing.T) {
	cases := []struct {
		pattern     string
		wantMethod  string
		wantRoute   string
		wantVersion string
	}{
		{
			pattern:     "/users/$/||/[GET]/",
			wantMethod:  "GET",
			wantRoute:   "/users/$",
			wantVersion: "",
		},
		{
			pattern:     "/users/$/|v2|/[POST]/",
			wantMethod:  "POST",
			wantRoute:   "/users/$",
			wantVersion: "v2",
		},
		{
			pattern:     "/feeds/all/||/[DELETE]/",
			wantMethod:  "DELETE",
			wantRoute:   "/feeds/all",
			wantVersion: "",
		},
	}

	for _, c := range cases {
		method, route, version := PatternToMethodRouteVersion(c.pattern)
		if method != c.wantMethod {
			t.Error(test.DiffMessage(method, c.wantMethod, "PatternToMethodRouteVersion method"))
		}
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "PatternToMethodRouteVersion route"))
		}
		if version != c.wantVersion {
			t.Error(test.DiffMessage(version, c.wantVersion, "PatternToMethodRouteVersion version"))
		}
	}
}

func TestParseToParamKey(t *testing.T) {
	str, keys := ParseToParamKey("/users/{userId}/friends/{friendId}/")
	wantStr := "/users/$/friends/$/"
	if str != wantStr {
		t.Error(test.DiffMessage(str, wantStr, "ParseToParamKey str"))
	}
	if keys["userId"][0] != 0 {
		t.Error(test.DiffMessage(keys["userId"][0], 0, "ParseToParamKey userId index"))
	}
	if keys["friendId"][0] != 1 {
		t.Error(test.DiffMessage(keys["friendId"][0], 1, "ParseToParamKey friendId index"))
	}

	str2, keys2 := ParseToParamKey("/plain/route/")
	if str2 != "/plain/route/" {
		t.Error(test.DiffMessage(str2, "/plain/route/", "ParseToParamKey no params"))
	}
	if len(keys2) != 0 {
		t.Error(test.DiffMessage(len(keys2), 0, "ParseToParamKey no param keys"))
	}
}

func TestParseToParamKey_RepeatedName(t *testing.T) {
	str, keys := ParseToParamKey("/a/{x}/b/{y}/{x}/")
	if str != "/a/$/b/$/$/" {
		t.Error(test.DiffMessage(str, "/a/$/b/$/$/", "R16: parsed route"))
	}
	want := map[string][]int{"x": {0, 2}, "y": {1}}
	if !reflect.DeepEqual(keys, want) {
		t.Error(test.DiffMessage(keys, want, "R16: param keys"))
	}
}

func TestParseToParamKey_NoBrace(t *testing.T) {
	str, keys := ParseToParamKey("/a/b")
	if str != "/a/b" || keys != nil {
		t.Error(test.DiffMessage(keys, nil, "R16: no { returns the input and nil keys"))
	}
}

func TestMethodRouteVersionToPattern_RootSpellings(t *testing.T) {
	for _, version := range []string{"", "v2"} {
		a := MethodRouteVersionToPattern(http.MethodGet, "", version)
		b := MethodRouteVersionToPattern(http.MethodGet, "/", version)
		if a != b {
			t.Error(test.DiffMessage(a, b, "R14: root spellings agree for version "+version))
		}
	}
	if got := MethodRouteVersionToPattern(http.MethodGet, "", ""); got != "/||/[GET]/" {
		t.Error(test.DiffMessage(got, "/||/[GET]/", "R14: root pattern"))
	}
	if got := MethodRouteVersionToPattern(http.MethodGet, "", "v2"); got != "/|v2|/[GET]/" {
		t.Error(test.DiffMessage(got, "/|v2|/[GET]/", "R14: root pattern with version"))
	}
}

func TestPatternToMethodRouteVersion_Root(t *testing.T) {
	method, route, version := PatternToMethodRouteVersion(MethodRouteVersionToPattern(http.MethodGet, "", ""))
	if method != "GET" || route != "" || version != "" {
		t.Error(test.DiffMessage([]string{method, route, version}, []string{"GET", "", ""}, "R15: root"))
	}
	method, route, version = PatternToMethodRouteVersion("/||/[GET]/")
	if method != "GET" || route != "" || version != "" {
		t.Error(test.DiffMessage([]string{method, route, version}, []string{"GET", "", ""}, "R15: literal root pattern"))
	}
}

func TestPatternToMethodRouteVersion_RoundTrip(t *testing.T) {
	methods := []string{"GET", "POST", "SERVE", "LINK", "X"}
	routes := []struct{ route, want string }{
		{"/a/b", "/a/b"},
		{"/a/b/", "/a/b"},
		{"", ""},
		{"/", ""},
		{"/users/{id}/friends/{fid}", "/users/{id}/friends/{fid}"},
		{"/a/*.html", "/a/*.html"},
		{"/a/*", "/a/*"},
	}
	versions := []string{"", "v1", "1.2", "NEUTRAL"}
	for _, m := range methods {
		for _, r := range routes {
			for _, v := range versions {
				pattern := MethodRouteVersionToPattern(m, r.route, v)
				method, route, version := PatternToMethodRouteVersion(pattern)
				if method != m || route != r.want || version != v {
					t.Error(test.DiffMessage([]string{method, route, version}, []string{m, r.want, v}, "R15: round trip of "+pattern))
				}
			}
		}
	}
}

func TestPatternToMethodRouteVersion_UnknownMethod(t *testing.T) {
	var method, route, version string
	if _, panicked := panicText(func() {
		method, route, version = PatternToMethodRouteVersion(MethodRouteVersionToPattern("LINK", "/a", "v1"))
	}); panicked {
		t.Error(test.DiffMessage(panicked, false, "R15: a method outside HTTPMethods does not panic"))
	}
	if method != "LINK" || route != "/a" || version != "v1" {
		t.Error(test.DiffMessage([]string{method, route, version}, []string{"LINK", "/a", "v1"}, "R15: LINK"))
	}
}

func TestPatternToMethodRouteVersion_MethodLikeSegment(t *testing.T) {
	pattern := MethodRouteVersionToPattern(http.MethodGet, "/[POST]/x", "")
	method, route, version := PatternToMethodRouteVersion(pattern)
	if method != "GET" || route != "/[POST]/x" || version != "" {
		t.Error(test.DiffMessage([]string{method, route, version}, []string{"GET", "/[POST]/x", ""}, "R15: a segment such as [POST] is part of the route"))
	}
}

func TestPatternToMethodRouteVersion_NotAPattern(t *testing.T) {
	for _, s := range []string{"", "x", "/", "/a/b/", "[GET]/", "/a/|v|/[GET", "||", "/[GET]/", "////"} {
		var method, route, version string
		if _, panicked := panicText(func() { method, route, version = PatternToMethodRouteVersion(s) }); panicked {
			t.Error(test.DiffMessage(panicked, false, "R15: "+s+" must not panic"))
			continue
		}
		if method != "" || route != "" || version != "" {
			t.Error(test.DiffMessage([]string{method, route, version}, []string{"", "", ""}, "R15: "+s+" is not a pattern"))
		}
	}
}

func TestFromMethodtoPattern(t *testing.T) {
	got := toPattern(http.MethodGet, "[", "]")
	want := "[GET]"
	if got != want {
		t.Error(test.DiffMessage(got, want, "fromMethodtoPattern"))
	}
}

func TestFromVersiontoPattern(t *testing.T) {
	if got := toPattern("", "|", "|"); got != "||" {
		t.Error(test.DiffMessage(got, "||", "toPattern empty"))
	}
	if got := toPattern("v2", "|", "|"); got != "|v2|" {
		t.Error(test.DiffMessage(got, "|v2|", "toPattern v2"))
	}
}

func TestToPattern(t *testing.T) {
	cases := []struct {
		s, l, r, want string
	}{
		{"", "[", "]", "[]"},
		{"GET", "[", "]", "[GET]"},
		{"[GET]", "[", "]", "[GET]"},
		{"[GET", "[", "]", "[GET]"},
		{"GET]", "[", "]", "[GET]"},
		{"  GET  ", "[", "]", "[GET]"},
	}

	for _, c := range cases {
		got := toPattern(c.s, c.l, c.r)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "toPattern("+c.s+", "+c.l+", "+c.r+")"))
		}
	}
}

func TestMethodRouteVersionToPattern(t *testing.T) {
	cases := []struct {
		method, route, version, want string
	}{
		{http.MethodGet, "/users/{userId}", "", "/users/{userId}/||/[GET]/"},
		{http.MethodPost, "/users/{userId}", "v2", "/users/{userId}/|v2|/[POST]/"},
		{http.MethodDelete, "/feeds/all", "", "/feeds/all/||/[DELETE]/"},
		{"", "/feeds/all", "", "/feeds/all/||/[]/"},
	}

	for _, c := range cases {
		got := MethodRouteVersionToPattern(c.method, c.route, c.version)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "MethodRouteVersionToPattern("+c.method+", "+c.route+", "+c.version+")"))
		}
	}
}
