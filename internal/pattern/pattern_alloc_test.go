package pattern_test

import (
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/pattern"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestMatch_NoAllocations(t *testing.T) {
	cases := [][2]string{
		{"user.created", "user.created"},
		{"**", "user.created"},
		{"user.**", "user.profile.updated"},
		{"tenant.*.user.**", "tenant.abc.user.profile.updated"},
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
	for _, raw := range []string{"user.profile.created", "user.profile.**", "a.**.b", "**"} {
		raw = strings.Clone(raw)
		if n := testing.AllocsPerRun(100, func() { pattern.NewPattern(raw) }); n != 0 {
			t.Error(test.DiffMessage(n, 0, "allocations of NewPattern("+raw+")"))
		}
	}
}

func FuzzMatch_ComplexEqualsSplitReference(f *testing.F) {
	for _, s := range [][2]string{{"chat.*.typing", "chat.a.typing"}, {"tenant.*.user.**", "tenant.1.user.x"}, {"*.a", ".a"}, {"*", ""}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, raw, topic string) {
		p := pattern.NewPattern(raw)
		if p.Kind() != pattern.KindComplex {
			return
		}
		if got, want := p.Match(topic), splitReferenceMatch(raw, topic); got != want {
			t.Fatal(test.DiffMessage(got, want, "Match("+raw+", "+topic+")"))
		}
	})
}

func FuzzMatch_KindsAgreeOnNonEmptySegments(f *testing.F) {
	f.Add("user.**", "user.a.b")
	f.Add("a.b", "a.b")
	f.Fuzz(func(t *testing.T, raw, topic string) {
		if strings.Contains(topic, "..") || strings.HasPrefix(topic, ".") || strings.HasSuffix(topic, ".") || topic == "" {
			return
		}
		if got, want := pattern.NewPattern(raw).Match(topic), splitReferenceMatch(raw, topic); got != want {
			t.Fatal(test.DiffMessage(got, want, "Match("+raw+", "+topic+")"))
		}
	})
}

func TestPattern_SimplePrefixOfOtherKinds(t *testing.T) {
	for _, raw := range []string{"user.created", "**", "user.*", "a.*.**"} {
		if got := pattern.NewPattern(raw).SimplePrefix(); got != "" {
			t.Error(test.DiffMessage(got, "", "SimplePrefix of "+raw))
		}
	}
}
