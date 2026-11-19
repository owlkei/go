package pattern_test

import (
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/pattern"
	"go.owlky.com/owlky/internal/test"
)

func TestNewPattern_Kind(t *testing.T) {
	cases := []struct {
		raw  string
		want pattern.Kind
	}{
		{"user.created", pattern.KindExact},
		{"a", pattern.KindExact},
		{"", pattern.KindExact},
		{"a*b", pattern.KindExact},
		{"a.**.b", pattern.KindExact},
		{"**", pattern.KindGlobal},
		{"user.**", pattern.KindSuffixWildcard},
		{"a.b.c.**", pattern.KindSuffixWildcard},
		{"*", pattern.KindComplex},
		{"user.*", pattern.KindComplex},
		{"chat.*.msg", pattern.KindComplex},
		{"tenant.*.user.**", pattern.KindComplex},
	}
	for _, c := range cases {
		if got := pattern.NewPattern(c.raw).Kind(); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "Kind of "+c.raw))
		}
	}
}

func TestNewPattern_SimplePrefix(t *testing.T) {
	cases := map[string]string{"user.**": "user", "a.b.**": "a.b", "a.b.c.**": "a.b.c"}
	for raw, want := range cases {
		if got := pattern.NewPattern(raw).SimplePrefix(); got != want {
			t.Error(test.DiffMessage(got, want, "SimplePrefix of "+raw))
		}
	}
}

func TestPattern_RawAndIsExact(t *testing.T) {
	cases := []struct {
		raw     string
		isExact bool
	}{
		{"user.password", true},
		{"user.*", false},
		{"user.**", false},
		{"**", false},
	}
	for _, c := range cases {
		p := pattern.NewPattern(c.raw)
		if p.Raw() != c.raw || p.IsExact() != c.isExact {
			t.Error(test.DiffMessage([]any{p.Raw(), p.IsExact()}, []any{c.raw, c.isExact}, "Raw/IsExact of "+c.raw))
		}
	}
}

func TestMatch_SemanticsTable(t *testing.T) {
	cases := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"chat.room1", "chat.room1", true},
		{"chat.room1", "chat.room2", false},

		{"chat.*", "chat.room1", true},
		{"chat.*", "chat.room1.typing", false},
		{"chat.*", "chat", false},
		{"chat.*.typing", "chat.room1.typing", true},
		{"chat.*.typing", "chat.room1.thread5.typing", false},
		{"*.room1", "chat.room1", true},
		{"*", "chat", true},
		{"*", "chat.room1", false},

		{"chat.**", "chat.room1", true},
		{"chat.**", "chat.room1.typing", true},
		{"chat.**", "chat", false},
		{"chat.**", "chatx.room1", false},
		{"chat.*.**", "chat.room1", false},
		{"chat.*.**", "chat.room1.typing", true},
		{"**", "chat", true},
		{"**", "chat.room1.typing", true},

		{"a*b", "axxb", false},
		{"a*b", "a*b", true},
		{"a.**.b", "a.x.b", false},
		{"a.**.b", "a.**.b", true},
		{"", "", true},

		{"chat.**", "chat.", false},
		{"chat.*.**", "chat.room1.", true},
	}
	for _, c := range cases {
		assertMatch(t, c.pattern, c.topic, c.want)
	}
}

func TestMatch_ComplexEqualsSplitReference(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	patternSegs := []string{"a", "b", "*", "**", ""}
	topicSegs := []string{"a", "b", "c", ""}
	for range 300000 {
		var ps, ts []string
		for range 1 + r.IntN(5) {
			ps = append(ps, patternSegs[r.IntN(len(patternSegs))])
		}
		for range 1 + r.IntN(6) {
			ts = append(ts, topicSegs[r.IntN(len(topicSegs))])
		}
		raw, topic := strings.Join(ps, "."), strings.Join(ts, ".")
		if pattern.NewPattern(raw).Kind() != pattern.KindComplex {
			continue
		}
		if got, want := pattern.NewPattern(raw).Match(topic), splitReferenceMatch(raw, topic); got != want {
			t.Fatal(test.DiffMessage(got, want, "pattern "+raw+" topic "+topic))
		}
	}
}

func TestMatch_LongHostileTopicIsCheap(t *testing.T) {
	p := pattern.NewPattern("a.*.z")
	topic := strings.Repeat("seg.", 250000) + "end"
	start := time.Now()
	p.Match(topic)
	if elapsed := time.Since(start); elapsed > time.Millisecond {
		t.Error(test.DiffMessage(elapsed.String(), "< 1ms", "matching stops at the first mismatching segment"))
	}
}

func TestMatchingTableIsUpToDate(t *testing.T) {
	want := renderMatchingTable()
	if *updateMatching {
		if err := os.WriteFile("MATCHING.md", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile("MATCHING.md")
	if err != nil {
		t.Fatal(test.DiffMessage(err.Error(), "MATCHING.md present", "run with -update-matching to create it"))
	}
	if string(got) != want {
		t.Error(test.DiffMessage("MATCHING.md is stale", "MATCHING.md matches Match", "run go test ./pattern -run TestMatchingTableIsUpToDate -update-matching"))
	}
}

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
