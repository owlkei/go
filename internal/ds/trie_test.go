package ds

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestMatchGlob_Prefix(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"image.png", "image.*", true},
		{"image.", "image.*", true},
		{"image", "image.*", false},
		{"x.image.png", "image.*", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Suffix(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"index.html", "*.html", true},
		{".html", "*.html", true},
		{"a.html.html", "*.html", true},
		{"index.png", "*.html", false},
		{"index.html.png", "*.html", false},
		{"aaaa", "*a", true},
		{"x.y.z", "*.z", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Middle(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"index.html", "in*.html", true},
		{"in.html", "in*.html", true},
		{"in.htm", "in*.html", false},
		{"xin.html", "in*.html", false},
		{"x.y.z", "x.*", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_TwoStars(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"abc", "a*b*c", true},
		{"aXbYc", "a*b*c", true},
		{"abcc", "a*b*c", true},
		{"acb", "a*b*c", false},
		{"ac", "a*b*c", false},
		{"abc", "**", true},
		{"a", "*", true},
		{"", "*", true},
		{"", "**", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Overlap(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"aba", "ab*ba", false},
		{"abba", "ab*ba", true},
		{"ababa", "ab*ba", true},
		{"ab", "ab*ba", false},
		{"foo", "foo", true},
		{"foobar", "foo", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestTrie_Find_Root(t *testing.T) {
	for _, pattern := range []string{"/", ""} {
		tr := newTrie(t, pattern)
		for _, path := range []string{"/", "", "//"} {
			value, _ := tr.Find(path, nil)
			if value == nil {
				t.Error(test.DiffMessage(value, pattern, "T1: root inserted as "+pattern+" must match "+path))
			}
		}
		if value, _ := tr.Find("/a", nil); value != nil {
			t.Error(test.DiffMessage(*value, nil, "T1: root must not match /a"))
		}
	}
}

func TestTrie_Find_EmptyTrie(t *testing.T) {
	tr := NewTrie[string]()
	for _, path := range []string{"/", "", "//", "/a", "/a/b"} {
		value, params := tr.Find(path, nil)
		if value != nil || len(params) != 0 {
			t.Error(test.DiffMessage(value, nil, "T1: empty trie must miss "+path))
		}
	}
}

func TestTrie_Find_Static(t *testing.T) {
	tr := newTrie(t, "/feeds/all")
	checkFind(t, tr, "/feeds/all", "/feeds/all", nil, "T2")
	checkFind(t, tr, "/feeds/all/", "/feeds/all", nil, "T2")
}

func TestTrie_Find_StaticPrefixIsMiss(t *testing.T) {
	tr := newTrie(t, "/feeds/all")
	checkFind(t, tr, "/feeds", "", nil, "T2")
	checkFind(t, tr, "/", "", nil, "T2")
}

func TestTrie_Find_StaticLongerIsMiss(t *testing.T) {
	tr := newTrie(t, "/feeds/all")
	checkFind(t, tr, "/feeds/all/x", "", nil, "T2")
}

func TestTrie_Find_Param(t *testing.T) {
	tr := newTrie(t, "/users/$")
	checkFind(t, tr, "/users/7", "/users/$", []string{"7"}, "T3")
	checkFind(t, tr, "/users", "", nil, "T3: a parameter needs a segment")
	checkFind(t, tr, "/users/7/x", "", nil, "T3: a parameter is one segment")
}

func TestTrie_Find_TwoParams(t *testing.T) {
	tr := newTrie(t, "/users/$/friends/$")
	checkFind(t, tr, "/users/7/friends/9", "/users/$/friends/$", []string{"7", "9"}, "T3")
	checkFind(t, tr, "/users/7/friends", "", nil, "T3")
}

func TestTrie_Find_LiteralDollarIsValue(t *testing.T) {
	tr := newTrie(t, "/users/$/friends/$")
	checkFind(t, tr, "/users/$/friends/*", "/users/$/friends/$", []string{"$", "*"}, "T4")
}

func TestTrie_Find_LiteralStarIsText(t *testing.T) {
	tr := newTrie(t, "/a/b")
	checkFind(t, tr, "/a/*", "", nil, "T4")
	checkFind(t, tr, "/a/$", "", nil, "T4")
}

func TestTrie_Find_WildcardMiddleOneSegment(t *testing.T) {
	tr := newTrie(t, "/*/feeds")
	checkFind(t, tr, "/x/feeds", "/*/feeds", nil, "T5")
	checkFind(t, tr, "/feeds", "", nil, "T5: zero segments")
	checkFind(t, tr, "/x/y/feeds", "", nil, "T5: two segments")
}

func TestTrie_Find_WildcardEndZeroSegments(t *testing.T) {
	tr := newTrie(t, "/lv1/*")
	for _, path := range []string{"/lv1", "/lv1/"} {
		checkFind(t, tr, path, "/lv1/*", nil, "T6")
	}
}

func TestTrie_Find_WildcardEndManySegments(t *testing.T) {
	tr := newTrie(t, "/lv1/*")
	for _, path := range []string{"/lv1/a", "/lv1/a/b/c"} {
		checkFind(t, tr, path, "/lv1/*", nil, "T6")
	}
	checkFind(t, tr, "/lv2/a", "", nil, "T6")
}

func TestTrie_Find_WildcardMiddleAndEnd(t *testing.T) {
	tr := newTrie(t, "/a/*/b", "/a/*")
	checkFind(t, tr, "/a/x/b", "/a/*/b", nil, "T6")
	checkFind(t, tr, "/a/x/c", "/a/*", nil, "T6")
	checkFind(t, tr, "/a/x/b/c", "/a/*", nil, "T6")
	checkFind(t, tr, "/a", "/a/*", nil, "T6")
}

func TestTrie_Find_Glob(t *testing.T) {
	tr := newTrie(t, "/files/in*.html", "/files/image.*", "/files/*.css")
	checkFind(t, tr, "/files/in.html", "/files/in*.html", nil, "T7")
	checkFind(t, tr, "/files/index.html", "/files/in*.html", nil, "T7")
	checkFind(t, tr, "/files/in.htm", "", nil, "T7")
	checkFind(t, tr, "/files/xin.html", "", nil, "T7")
	checkFind(t, tr, "/files/image.png", "/files/image.*", nil, "T7")
	checkFind(t, tr, "/files/site.css", "/files/*.css", nil, "T7")
	checkFind(t, tr, "/files/site.css/x", "", nil, "T7: a glob is one segment")
}

func TestTrie_Find_BacktracksStaticToParam(t *testing.T) {
	tr := newTrie(t, "/users/all/x", "/users/$/y")
	checkFind(t, tr, "/users/all/y", "/users/$/y", []string{"all"}, "T8")
	checkFind(t, tr, "/users/all/x", "/users/all/x", nil, "T8")
}

func TestTrie_Find_GlobBeforeWildcard(t *testing.T) {
	tr := newTrie(t, "/x/*", "/x/*.html")
	checkFind(t, tr, "/x/a.html", "/x/*.html", nil, "T8")
	checkFind(t, tr, "/x/a.css", "/x/*", nil, "T8")
}

func TestTrie_Find_GlobBeforeParam(t *testing.T) {
	tr := newTrie(t, "/f/$", "/f/*.html")
	checkFind(t, tr, "/f/a.html", "/f/*.html", nil, "T8")
	checkFind(t, tr, "/f/a.css", "/f/$", []string{"a.css"}, "T8")
}

func TestTrie_Find_ParamBeforeWildcard(t *testing.T) {
	tr := newTrie(t, "/a/$/x", "/a/*")
	checkFind(t, tr, "/a/1/x", "/a/$/x", []string{"1"}, "T8")
	checkFind(t, tr, "/a/1/y", "/a/*", nil, "T8")
}

func TestTrie_Find_FallsBackToOuterWildcard(t *testing.T) {
	tr := newTrie(t, "/lv1/*", "/lv1/lv2/lv3/pong")
	checkFind(t, tr, "/lv1/lv2/lv3/extra", "/lv1/*", nil, "T8")
	checkFind(t, tr, "/lv1/lv2/lv3/pong", "/lv1/lv2/lv3/pong", nil, "T8")
}

func TestTrie_Find_NodeValueBeforeEndWildcard(t *testing.T) {
	tr := newTrie(t, "/a", "/a/*")
	checkFind(t, tr, "/a", "/a", nil, "T8")
	checkFind(t, tr, "/a/b", "/a/*", nil, "T8")
}

func TestTrie_Find_DropsAbandonedParams(t *testing.T) {
	tr := newTrie(t, "/a/$/b/$/c", "/a/$/b/*")
	checkFind(t, tr, "/a/1/b/2/d", "/a/$/b/*", []string{"1"}, "T9")
	checkFind(t, tr, "/a/1/b/2/c", "/a/$/b/$/c", []string{"1", "2"}, "T9")
	tr = newTrie(t, "/a/$/x", "/a/*")
	checkFind(t, tr, "/a/1/y", "/a/*", nil, "T9")
}

func TestTrie_Find_MissKeepsParams(t *testing.T) {
	tr := newTrie(t, "/a/$/b")
	value, params := tr.Find("/a/1/c", nil)
	if value != nil {
		t.Error(test.DiffMessage(*value, nil, "T9: miss returns nil"))
	}
	if len(params) != 0 {
		t.Error(test.DiffMessage(params, nil, "T9: miss returns the input params unchanged"))
	}
}

func TestTrie_Find_InsertOrderIndependent(t *testing.T) {
	patterns := []string{"/p/a*", "/p/*bc", "/p/$", "/p/*"}
	paths := []struct {
		path   string
		want   string
		params []string
	}{
		{"/p/abc", "/p/*bc", nil},
		{"/p/axx", "/p/a*", nil},
		{"/p/zzz", "/p/$", []string{"zzz"}},
		{"/p/zzz/q", "/p/*", nil},
	}
	for _, order := range permutations(patterns) {
		tr := newTrie(t, order...)
		for _, p := range paths {
			checkFind(t, tr, p.path, p.want, p.params, "T10: order "+strings.Join(order, " "))
		}
	}
}

func TestTrie_Find_GlobOrder(t *testing.T) {
	for _, order := range permutations([]string{"/g/a*", "/g/*bc"}) {
		tr := newTrie(t, order...)
		checkFind(t, tr, "/g/abc", "/g/*bc", nil, "T10: more literal bytes first")
	}
	for _, order := range permutations([]string{"/t/a*", "/t/*a"}) {
		tr := newTrie(t, order...)
		checkFind(t, tr, "/t/aa", "/t/*a", nil, "T10: tie broken by pattern byte order")
	}
}

func TestTrie_Find_SkipsEmptySegments(t *testing.T) {
	tr := newTrie(t, "/a/b", "/c/$")
	checkFind(t, tr, "//a///b/", "/a/b", nil, "T11")
	checkFind(t, tr, "//c//5//", "/c/$", []string{"5"}, "T11")
}

func TestTrie_Find_DotSegmentsArePlainText(t *testing.T) {
	tr := newTrie(t, "/a/$")
	checkFind(t, tr, "/a/..", "/a/$", []string{".."}, "T11")
	checkFind(t, tr, "/a/.", "/a/$", []string{"."}, "T11")
}

func TestTrie_Find_StaticNoAlloc(t *testing.T) {
	tr := newTrie(t, "/feeds/all", "/users/$")
	allocs := testing.AllocsPerRun(100, func() {
		tr.Find("/feeds/all", nil)
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "T12: allocations of a static match"))
	}
}

func TestTrie_Find_MissNoAlloc(t *testing.T) {
	tr := newTrie(t, "/feeds/all", "/users/$")
	allocs := testing.AllocsPerRun(100, func() {
		tr.Find("/nope/at/all", nil)
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "T12: allocations of a miss"))
	}
}

func TestTrie_Find_TooManySegments(t *testing.T) {
	tr := newTrie(t, "/*")
	if value, _ := tr.Find(repeatSegments("a", MaxSegments), nil); value == nil {
		t.Error(test.DiffMessage(value, "/*", "T13: 64 segments match"))
	}
	if value, _ := tr.Find(repeatSegments("a", MaxSegments+1), nil); value != nil {
		t.Error(test.DiffMessage(*value, nil, "T13: 65 segments miss"))
	}
}

func TestTrie_Insert_TooManySegments(t *testing.T) {
	tr := NewTrie[string]()
	value := "v"
	if err := tr.Insert(repeatSegments("a", MaxSegments), &value); err != nil {
		t.Error(test.DiffMessage(err, nil, "T13: 64 segments are accepted"))
	}
	before := tr.Len()
	err := tr.Insert(repeatSegments("b", MaxSegments+1), &value)
	if !errors.Is(err, ErrTooManySegments) {
		t.Error(test.DiffMessage(err, ErrTooManySegments, "T13: 65 segments are refused"))
	}
	if tr.Len() != before {
		t.Error(test.DiffMessage(tr.Len(), before, "T13: a refused Insert changes nothing"))
	}
	if value, _ := tr.Find(repeatSegments("b", MaxSegments), nil); value != nil {
		t.Error(test.DiffMessage(*value, nil, "T13: a refused Insert leaves no prefix behind"))
	}
}

func TestTrie_Len(t *testing.T) {
	if got := NewTrie[string]().Len(); got != 0 {
		t.Error(test.DiffMessage(got, 0, "T14: empty trie"))
	}
	tr := newTrie(t, "/a/b")
	if got := tr.Len(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "T14: a pattern counts, its prefixes do not"))
	}
	tr = newTrie(t, "/", "/a", "/a/b", "/users/$", "/x/*", "/f/*.html")
	if got := tr.Len(); got != 6 {
		t.Error(test.DiffMessage(got, 6, "T14: patterns that hold a value"))
	}
}

func TestTrie_Insert_ReplacesValue(t *testing.T) {
	tr := NewTrie[string]()
	first, second := "first", "second"
	if err := tr.Insert("/a/$", &first); err != nil {
		t.Fatal(err)
	}
	if err := tr.Insert("/a/$", &second); err != nil {
		t.Fatal(err)
	}
	checkFind(t, tr, "/a/1", "second", []string{"1"}, "T14")
	if got := tr.Len(); got != 1 {
		t.Error(test.DiffMessage(got, 1, "T14: the same pattern twice counts once"))
	}
	if err := tr.Insert("", &first); err != nil {
		t.Fatal(err)
	}
	if err := tr.Insert("/", &second); err != nil {
		t.Fatal(err)
	}
	if got := tr.Len(); got != 2 {
		t.Error(test.DiffMessage(got, 2, "T14: \"\" and \"/\" are the same pattern"))
	}
	checkFind(t, tr, "/", "second", nil, "T14")
}

func TestTrie_Find_BufferEqualsNilBuffer(t *testing.T) {
	tr := newTrie(t, "/a/$/b/$/c", "/a/$/b/*", "/x/$", "/y/*/z/$", "/g/a*")
	paths := []string{"/a/1/b/2/c", "/a/1/b/2/d", "/x/9", "/y/q/z/7", "/g/abc", "/none", "/x/9/extra"}
	for _, size := range []int{0, 1, 2, 8} {
		buf := make([]string, 0, size)
		for _, path := range paths {
			wantValue, wantParams := tr.Find(path, nil)
			gotValue, gotParams := tr.Find(path, buf[:0])
			if wantValue != gotValue {
				t.Error(test.DiffMessage(gotValue, wantValue, "H1: same value with a buffer for "+path))
			}
			if !slices.Equal(wantParams, gotParams) {
				t.Error(test.DiffMessage(gotParams, wantParams, "H1: same params with a buffer for "+path))
			}
		}
	}
}
