package pattern_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/pattern"
)

// refMatch is the matcher as it was before the rewrite (split both on "."), kept as the
// definition the faster code must agree with.
func refMatch(raw, topic string) bool {
	if raw == "*" {
		return true
	}
	segs := strings.Split(raw, ".")
	if !slices.Contains(segs, "*") {
		return raw == topic
	}
	if len(segs) >= 2 && segs[len(segs)-1] == "*" && !slices.Contains(segs[:len(segs)-1], "*") {
		prefix := strings.Join(segs[:len(segs)-1], ".")
		return strings.HasPrefix(topic, prefix+".") && len(topic) > len(prefix)+1
	}
	ts := strings.Split(topic, ".")
	pi, ti := 0, 0
	for pi < len(segs) {
		if segs[pi] == "*" {
			if pi == len(segs)-1 {
				return ti < len(ts)
			}
			if ti >= len(ts) {
				return false
			}
			pi++
			ti++
			continue
		}
		if ti >= len(ts) || segs[pi] != ts[ti] {
			return false
		}
		pi++
		ti++
	}
	return pi == len(segs) && ti == len(ts)
}

func TestMatch_SameAsReference(t *testing.T) {
	parts := []string{"", "*", "a", "b", "ab", "**", "a*"}
	var raws []string
	var build func(prefix string, depth int)
	build = func(prefix string, depth int) {
		if depth == 0 {
			return
		}
		for _, p := range parts {
			s := p
			if prefix != "" || depth < 4 {
				s = prefix + "." + p
			}
			raws = append(raws, s)
			build(s, depth-1)
		}
	}
	build("", 4)
	raws = append(raws, parts...)
	topics := append([]string{}, raws...)
	for _, raw := range raws {
		p := pattern.NewPattern(raw)
		for _, topic := range topics {
			if got, want := p.Match(topic), refMatch(raw, topic); got != want {
				t.Fatal(test.DiffMessage(got, want, "Match("+raw+", "+topic+")"))
			}
		}
	}
}

func FuzzMatch(f *testing.F) {
	for _, s := range [][2]string{{"user.*", "user.created"}, {"tenant.*.user.*", "tenant.1.user.x"}, {"*.a", ".a"}, {"a.*", "a."}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, raw, topic string) {
		if got, want := pattern.NewPattern(raw).Match(topic), refMatch(raw, topic); got != want {
			t.Fatal(test.DiffMessage(got, want, "Match("+raw+", "+topic+")"))
		}
	})
}

func TestMatch_NoAllocations(t *testing.T) {
	cases := [][2]string{
		{"user.created", "user.created"},
		{"*", "user.created"},
		{"user.*", "user.profile.updated"},
		{"tenant.*.user.*", "tenant.abc.user.profile.updated"},
		{"*.created", "order.created"},
	}
	for _, c := range cases {
		p := pattern.NewPattern(c[0])
		if n := testing.AllocsPerRun(100, func() { p.Match(c[1]) }); n != 0 {
			t.Error(test.DiffMessage(n, 0, "allocations of Match("+c[0]+", "+c[1]+")"))
		}
	}
}

func TestNewPattern_ExactAndSuffixDoNotAllocate(t *testing.T) {
	raw := strings.Clone("user.profile.created")
	if n := testing.AllocsPerRun(100, func() { pattern.NewPattern(raw) }); n != 0 {
		t.Error(test.DiffMessage(n, 0, "allocations of NewPattern(exact)"))
	}
	raw = strings.Clone("user.profile.*")
	if n := testing.AllocsPerRun(100, func() { pattern.NewPattern(raw) }); n != 0 {
		t.Error(test.DiffMessage(n, 0, "allocations of NewPattern(suffix)"))
	}
}

func TestMatch_EmptySegments(t *testing.T) {
	assertMatch(t, "user.*", "user.", false)
	assertMatch(t, "user.*", "user..x", true)
	assertMatch(t, "*.created", ".created", true)
	assertMatch(t, "a.*.b", "a..b", true)
	assertMatch(t, "", "", true)
	assertMatch(t, "", "a", false)
	assertMatch(t, "*", "", true)
}

func TestNewPattern_StarInsideSegmentIsLiteral(t *testing.T) {
	assertKind(t, "user.a*", pattern.KindExact)
	assertKind(t, "**", pattern.KindExact)
	assertMatch(t, "user.a*", "user.ab", false)
	assertMatch(t, "user.a*", "user.a*", true)
}

func TestPattern_Accessors(t *testing.T) {
	cases := []struct {
		raw, prefix   string
		exact, global bool
	}{
		{"user.created", "", true, false},
		{"*", "", false, true},
		{"user.*", "user", false, false},
		{".*", "", false, false},
		{"tenant.*.user", "", false, false},
	}
	for _, c := range cases {
		p := pattern.NewPattern(c.raw)
		got := []any{p.Raw(), p.SimplePrefix(), p.IsExact(), p.IsGlobal()}
		want := []any{c.raw, c.prefix, c.exact, c.global}
		for i := range got {
			if got[i] != want[i] {
				t.Error(test.DiffMessage(got[i], want[i], "accessor "+string(rune('0'+i))+" of "+c.raw))
			}
		}
	}
}
