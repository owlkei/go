package log

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestWrapLogger_ExactRule_MasksScalarLeaf(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "password", "secret")

	if cap.args()[1] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[1], maskPlaceholder, "exact rule should mask the exact top-level path"))
	}
}

func TestWrapLogger_ExactRule_MatchesByTrailingSegmentAtAnyDepth(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "user", map[string]any{"password": "secret", "name": "duoc"})

	user := cap.args()[1].(map[string]any)
	if user["password"] != maskPlaceholder {
		t.Error(test.DiffMessage(user["password"], maskPlaceholder, "a bare \"password\" rule should mask \"user.password\" too, not just a top-level \"password\" key"))
	}
	if user["name"] != "duoc" {
		t.Error(test.DiffMessage(user["name"], "duoc", "unrelated sibling fields must stay untouched"))
	}
}

func TestWrapLogger_ExactRule_RequiresDotBoundary(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "userpassword", "secret")

	if cap.args()[1] != "secret" {
		t.Error(test.DiffMessage(cap.args()[1], "secret", "\"password\" must not match \"userpassword\" — the match needs a dot boundary, not just a suffix of the raw string"))
	}
}

func TestWrapLogger_MultiSegmentExactRule_MatchesOnlyItsOwnSuffix(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"user.password"})
	l.Info("test",
		"user", map[string]any{"password": "secret1"},
		"admin", map[string]any{"password": "secret2"},
	)

	args := cap.args()
	user := args[1].(map[string]any)
	admin := args[3].(map[string]any)
	if user["password"] != maskPlaceholder {
		t.Error(test.DiffMessage(user["password"], maskPlaceholder, "\"user.password\" should mask user.password"))
	}
	if admin["password"] != "secret2" {
		t.Error(test.DiffMessage(admin["password"], "secret2", "\"user.password\" must not match admin.password, a different suffix"))
	}
}

func TestWrapLogger_WildcardRule_MasksMatchingNodeWhole(t *testing.T) {
	for _, rule := range []string{"password.*", "password.**"} {
		cap := &capturingLogger{}
		WrapLogger(cap, []string{rule}).Info("test", "password", map[string]any{
			"person": map[string]any{"value": "secret"},
		})
		password := cap.args()[1].(map[string]any)
		if password["person"] != maskPlaceholder {
			t.Error(test.DiffMessage(password["person"], maskPlaceholder, rule+" should mask password.person whole, children included"))
		}
	}
}

func TestWrapLogger_WildcardRule_OneSegmentOrMore(t *testing.T) {
	value := map[string]any{"card": map[string]any{"cvv": "1"}}
	cases := []struct {
		rule string
		want any
	}{
		{"user.*.cvv", maskPlaceholder},
		{"user.**", maskPlaceholder},
		{"user.*.*.cvv", "1"},
	}
	for _, c := range cases {
		cap := &capturingLogger{}
		WrapLogger(cap, []string{c.rule}).Info("test", "user", value)
		user := cap.args()[1].(map[string]any)
		got := user["card"]
		if card, ok := got.(map[string]any); ok {
			got = card["cvv"]
		}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "rule "+c.rule+" on user.card.cvv"))
		}
	}
}

func TestWrapLogger_WildcardRule_MatchesAnySuffixOfThePath(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"card.*"}).Info("test", "user", map[string]any{"card": map[string]any{"cvv": "1", "brand": "visa"}})
	card := cap.args()[1].(map[string]any)["card"].(map[string]any)
	if card["cvv"] != maskPlaceholder || card["brand"] != maskPlaceholder {
		t.Error(test.DiffMessage(card, map[string]any{"cvv": maskPlaceholder, "brand": maskPlaceholder}, "card.* masks user.card.cvv through the suffix card.cvv"))
	}
}

func TestWrapLogger_WildcardRule_MasksSlogGroupWhole(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"user.*"}).Info("test", slog.Group("user", slog.Group("card", slog.String("cvv", "1"))))
	got := cap.args()[0].(slog.Attr).Value.Group()[0]
	if got.Value.String() != maskPlaceholder {
		t.Error(test.DiffMessage(got.Value.String(), maskPlaceholder, "user.* masks the group user.card whole"))
	}
}

func TestWrapLogger_WildcardRule_DoesNotMatchBareTopLevelKey(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password.*"})
	l.Info("test", "password", "secret")

	if cap.args()[1] != "secret" {
		t.Error(test.DiffMessage(cap.args()[1], "secret", "password.* must not match the bare \"password\" path itself, only paths nested under it"))
	}
}

func TestWrapLogger_ExactRule_MasksStructValueWholesale(t *testing.T) {
	type Password struct {
		Value string `log:"value"`
	}
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "password", Password{Value: "secret"})

	if cap.args()[1] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[1], maskPlaceholder, "an exact rule should mask a struct-valued field wholesale without recursing into it"))
	}
}

func TestWrapLogger_TaggedStructUnderWildcardRule(t *testing.T) {
	type Person struct {
		Value string `log:"value"`
	}
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"person.*"})
	l.Info("test", "password", map[string]any{"person": Person{Value: "secret"}})

	password := cap.args()[1].(map[string]any)
	person := password["person"].(map[string]any)
	if person["value"] != maskPlaceholder {
		t.Error(test.DiffMessage(person["value"], maskPlaceholder, "wildcard masking should reach into tagged struct fields too, not just plain maps"))
	}
}

func TestWrapLogger_NoRules_PassesValuesThroughUnmasked(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, nil)
	l.Info("test", "password", "secret")

	if cap.args()[1] != "secret" {
		t.Error(test.DiffMessage(cap.args()[1], "secret", "with no MaskFields configured, nothing should be masked"))
	}
}

func TestWrapLogger_UnrelatedKeyNotMasked(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "username", "duoc")

	if cap.args()[1] != "duoc" {
		t.Error(test.DiffMessage(cap.args()[1], "duoc", "a mask rule for password must not affect an unrelated key"))
	}
}

func TestWrapLogger_MultipleRules(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password", "token.*"})
	l.Info("test",
		"password", "secret1",
		"token", map[string]any{"raw": "secret2"},
		"username", "duoc",
	)

	args := cap.args()
	if args[1] != maskPlaceholder {
		t.Error(test.DiffMessage(args[1], maskPlaceholder, "password rule should mask password"))
	}
	token := args[3].(map[string]any)
	if token["raw"] != maskPlaceholder {
		t.Error(test.DiffMessage(token["raw"], maskPlaceholder, "token.* rule should mask token.raw"))
	}
	if args[5] != "duoc" {
		t.Error(test.DiffMessage(args[5], "duoc", "username should be untouched"))
	}
}

func TestWrapLogger_OddArgCount_TrailingKeyPassedThrough(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", "password", "secret", "dangling")

	args := cap.args()
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
	if args[2] != "dangling" {
		t.Error(test.DiffMessage(args[2], "dangling", "a trailing key with no value should be passed through unchanged"))
	}
}

func TestWrapLogger_NonStringKey_PassedThrough(t *testing.T) {
	cap := &capturingLogger{}
	l := WrapLogger(cap, []string{"password"})
	l.Info("test", 42, "secret")

	if cap.args()[1] != "secret" {
		t.Error(test.DiffMessage(cap.args()[1], "secret", "a non-string key means this isn't a well-formed key/value pair; the value must pass through untouched"))
	}
}

func TestWrapLogger_ConcurrentMaskingCallsNoDataRace(t *testing.T) {
	l := WrapLogger(&capturingLogger{}, []string{"password.*"})

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Info("test", "password", map[string]any{"value": "secret"})
		}()
	}
	wg.Wait()
}

type maskUser struct {
	Name     string
	Password string
}

type maskUserTagged struct {
	Name     string `log:"name"`
	Password string `log:"password"`
}

func maskedJSON(rules []string, msg string, args ...any) string {
	logger, buf := newTestLog(FormatJSON, LogOptions{MaskFields: rules})
	logger.Info(msg, args...)
	return buf.String()
}

func TestWrapLogger_CaseInsensitive_Key(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("x", "Password", "s", "PASSWORD", "s", "pAsSwOrD", "s")
	for _, i := range []int{1, 3, 5} {
		if cap.args()[i] != maskPlaceholder {
			t.Error(test.DiffMessage(cap.args()[i], maskPlaceholder, "M2: the rule password masks the key "+cap.args()[i-1].(string)))
		}
	}
}

func TestWrapLogger_CaseInsensitive_UntaggedField(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("x", "u", maskUser{Name: "n", Password: "s"})
	u := cap.args()[1].(map[string]any)
	if u["Password"] != maskPlaceholder || u["Name"] != "n" {
		t.Error(test.DiffMessage(u, "Password masked, Name kept", "M2: an untagged field Password is masked by the rule password"))
	}
}

func TestWrapLogger_CaseInsensitive_RuleInUpperCase(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"PASSWORD", "USER.TOKEN"}).Info("x", "password", "s", "user", map[string]any{"token": "t"})
	if cap.args()[1] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[1], maskPlaceholder, "M2: a rule in upper case masks a lower case key"))
	}
	if cap.args()[3].(map[string]any)["token"] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[3], "token masked", "M2: a dotted rule in upper case matches"))
	}
}

func TestWrapLogger_CaseInsensitive_WildcardRule(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"Secrets.*"}).Info("x", "SECRETS", map[string]any{"Key": "k"})
	if cap.args()[1].(map[string]any)["Key"] != maskPlaceholder {
		t.Error(test.DiffMessage(cap.args()[1], "Key masked", "M2: a * rule ignores case"))
	}
}

func TestWrapLogger_CaseInsensitive_DoesNotMatchOtherWords(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("x", "MyPassword", "s")
	if cap.args()[1] != "s" {
		t.Error(test.DiffMessage(cap.args()[1], "s", "M1/M2: case folding keeps the dot boundary"))
	}
}

func TestWrapLogger_MasksInsideSlice(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "users", []maskUser{{Name: "a", Password: "s1"}, {Name: "b", Password: "s2"}})
	want := `"users":[{"Name":"a","Password":"[REDACTED]"},{"Name":"b","Password":"[REDACTED]"}]`
	if !strings.Contains(out, want) || strings.Contains(out, "s1") || strings.Contains(out, "s2") {
		t.Error(test.DiffMessage(out, want, "M4: a slice of structs is masked"))
	}
}

func TestWrapLogger_MasksInsideArray(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "users", [2]maskUser{{Name: "a", Password: "s1"}, {Name: "b", Password: "s2"}})
	if strings.Contains(out, "s1") || strings.Contains(out, "s2") || strings.Count(out, maskPlaceholder) != 2 {
		t.Error(test.DiffMessage(out, "two masked passwords", "M4: an array of structs is masked"))
	}
}

func TestWrapLogger_MasksInsideSliceOfMaps(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "rows", []map[string]any{{"password": "s1", "n": 1}})
	if strings.Contains(out, "s1") || !strings.Contains(out, `"rows":[{"n":1,"password":"[REDACTED]"}]`) {
		t.Error(test.DiffMessage(out, `"rows":[{"n":1,"password":"[REDACTED]"}]`, "M4: a slice of maps is masked"))
	}
}

func TestWrapLogger_MasksInsideIntKeyMap(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "m", map[int]maskUser{7: {Name: "a", Password: "s1"}})
	if strings.Contains(out, "s1") || !strings.Contains(out, `"m":{"7":{"Name":"a","Password":"[REDACTED]"}}`) {
		t.Error(test.DiffMessage(out, `"m":{"7":{"Name":"a","Password":"[REDACTED]"}}`, "M4: a map with integer keys is masked"))
	}
}

func TestWrapLogger_MasksInsideNestedSlices(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "g", map[string][]maskUser{"admins": {{Name: "a", Password: "s1"}}})
	if strings.Contains(out, "s1") {
		t.Error(test.DiffMessage(out, "no secret", "M4: maps of slices of structs are masked"))
	}
}

func TestWrapLogger_AttrArgument(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", slog.String("password", "s1"), slog.Any("user", maskUser{Name: "a", Password: "s2"}), slog.String("name", "kept"))
	if strings.Contains(out, "s1") || strings.Contains(out, "s2") || !strings.Contains(out, `"name":"kept"`) {
		t.Error(test.DiffMessage(out, "attrs masked, name kept", "M4: slog.Attr arguments are masked"))
	}
}

func TestWrapLogger_GroupValue(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", slog.Group("user", "password", "s1", "name", "kept"))
	if strings.Contains(out, "s1") || !strings.Contains(out, `"user":{"password":"[REDACTED]","name":"kept"}`) {
		t.Error(test.DiffMessage(out, `"user":{"password":"[REDACTED]","name":"kept"}`, "M4: the attributes of a group are masked"))
	}
}

func TestWrapLogger_GroupValuePathRule(t *testing.T) {
	out := maskedJSON([]string{"user.password"}, "x", slog.Group("user", "password", "s1"), slog.Group("admin", "password", "s2"))
	if strings.Contains(out, "s1") || !strings.Contains(out, "s2") {
		t.Error(test.DiffMessage(out, "user.password masked, admin.password kept", "M4: a group's path is its key"))
	}
}

func TestWrapLogger_TaggedFieldsMaskedByTagName(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "u", maskUserTagged{Name: "a", Password: "s1"})
	if strings.Contains(out, "s1") || !strings.Contains(out, `"password":"[REDACTED]"`) {
		t.Error(test.DiffMessage(out, "password masked", "M2: the path uses the tag name"))
	}
}

func TestWrapLogger_StringerMaskedByItsPath(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "h", User: url.UserPassword("a", "s1")}
	out := maskedJSON([]string{"password"}, "x", "password", u, "url", u)
	if strings.Count(out, maskPlaceholder) != 1 || !strings.Contains(out, `"url":"https://a:s1@h"`) {
		t.Error(test.DiffMessage(out, "masked under password, text kept under url", "M4: a Stringer is masked only by its own path; its text is never searched"))
	}
}

func TestWrapLogger_ErrorMaskedByItsPathOnly(t *testing.T) {
	out := maskedJSON([]string{"password"}, "x", "password", errors.New("e1"), "err", errors.New("password=s2"))
	if strings.Contains(out, "e1") || !strings.Contains(out, "password=s2") {
		t.Error(test.DiffMessage(out, "error under password masked, text kept elsewhere", "M4/M5: an error is masked by its path, its text is not searched"))
	}
}

func TestWrapLogger_MessageNeverMasked(t *testing.T) {
	out := maskedJSON([]string{"password"}, "password", "k", "v")
	if !strings.Contains(out, `"msg":"password"`) {
		t.Error(test.DiffMessage(out, `"msg":"password"`, "M5: the message is never masked"))
	}
}

func TestWrapLogger_KeysNeverMasked(t *testing.T) {
	out := maskedJSON([]string{"user.password"}, "x", "user", map[string]any{"password": "s1"}, "Password", "s2")
	if !strings.Contains(out, `"password":"[REDACTED]"`) || !strings.Contains(out, `"user":`) {
		t.Error(test.DiffMessage(out, "keys kept, values masked", "M5: only values are replaced"))
	}
}

func TestWrapLogger_MapWithOtherKeysNotLookedInto(t *testing.T) {
	m := map[[2]int]string{{1, 2}: "password"}
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"password"}).Info("x", "m", m)
	if got, ok := cap.args()[1].(map[[2]int]string); !ok || got[[2]int{1, 2}] != "password" {
		t.Error(test.DiffMessage(cap.args()[1], m, "M5: a map with keys that are neither strings nor integers is not looked into"))
	}
}

func TestWrapLogger_NoMaskedValueEverLeaks(t *testing.T) {
	out := maskedJSON([]string{"password", "token.*"}, "x",
		"a", map[string]any{"b": []any{map[string]any{"Password": "leak1"}}},
		"token", map[string]any{"deep": []maskUser{{Name: "leak2"}}},
	)
	if strings.Contains(out, "leak1") || strings.Contains(out, "leak2") {
		t.Error(test.DiffMessage(out, "no leak", "M4: masking reaches every depth"))
	}
}

func TestWrapLogger_BuiltInWithoutFieldsIsSame(t *testing.T) {
	built := NewLog(nil)
	if WrapLogger(built, nil) != built || WrapLogger(built, []string{}) != built {
		t.Error(test.DiffMessage("a new logger", "the same logger", "M6: a built-in logger without fields is returned as it is"))
	}
	wrapped := WrapLogger(built, []string{"password"})
	if WrapLogger(wrapped, nil) != wrapped || WrapLogger(wrapped, []string{}) != wrapped {
		t.Error(test.DiffMessage("a new logger", "the same logger", "M6: a wrapped logger without fields is returned as it is"))
	}
}

func TestWrapLogger_BuiltInWithFieldsMasksOnce(t *testing.T) {
	logger, buf := newTestLog(FormatJSON, LogOptions{})
	wrapped := WrapLogger(logger, []string{"password"})
	wrapped.Info("x", "password", "s1", "u", maskUser{Name: "a", Password: "s2"})
	out := buf.String()
	if strings.Contains(out, "s1") || strings.Contains(out, "s2") || strings.Count(out, "\n") != 1 {
		t.Error(test.DiffMessage(out, "one masked line", "M6: a built-in logger with fields masks and writes once"))
	}
}

func TestWrapLogger_BuiltInWithFieldsReachesWriterOnce(t *testing.T) {
	w := &countingWriter{}
	built := newLog(w, false, &LogOptions{Format: FormatJSON, MaskFields: []string{"a"}})
	WrapLogger(WrapLogger(built, []string{"b"}), []string{"c"}).Info("x", "a", 1, "b", 2, "c", 3)
	if w.writes != 1 {
		t.Error(test.DiffMessage(w.writes, 1, "M6: each call reaches the innermost logger once"))
	}
	if strings.Count(w.buf.String(), maskPlaceholder) != 3 {
		t.Error(test.DiffMessage(w.buf.String(), "a, b and c masked", "M6: the rules of every layer apply"))
	}
}

type callCounter struct {
	capturingLogger
	mu    sync.Mutex
	calls int
}

func (c *callCounter) Info(msg string, args ...any) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	c.capturingLogger.Info(msg, args...)
}

func TestWrapLogger_TwiceMergesRules(t *testing.T) {
	custom := &callCounter{}
	wrapped := WrapLogger(WrapLogger(custom, []string{"a"}), []string{"b"})
	wrapped.Info("x", "a", 1, "b", 2, "c", 3)
	if custom.calls != 1 {
		t.Error(test.DiffMessage(custom.calls, 1, "M6: the custom logger is called once per line"))
	}
	args := custom.args()
	if args[1] != maskPlaceholder || args[3] != maskPlaceholder || args[5] != 3 {
		t.Error(test.DiffMessage(args, "a and b masked, c kept", "M6: the rules of both wrappers apply"))
	}
}

func TestWrapLogger_TwiceDoesNotStackLayers(t *testing.T) {
	custom := &callCounter{}
	first := WrapLogger(custom, []string{"a"})
	second := WrapLogger(first, []string{"b"})
	if second == first {
		t.Error(test.DiffMessage("the same logger", "a logger with both rules", "M6: new fields make a new logger"))
	}
	first.Info("x", "a", 1, "b", 2)
	if custom.args()[1] != maskPlaceholder || custom.args()[3] != 2 {
		t.Error(test.DiffMessage(custom.args(), "only a masked", "M6: the first logger keeps its own rules"))
	}
}

func TestWrapLogger_CustomLoggerGetsMaps(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, nil).Info("x", "u", maskUser{Name: "a"}, "s", []maskUser{{Name: "b"}})
	if _, ok := cap.args()[1].(map[string]any); !ok {
		t.Error(test.DiffMessage(cap.args()[1], "map[string]any", "M6: a custom logger gets structs as maps"))
	}
	if _, ok := cap.args()[3].([]any); !ok {
		t.Error(test.DiffMessage(cap.args()[3], "[]any", "M6: a custom logger gets changed slices as []any"))
	}
}

func TestWrapLogger_CustomLoggerCalledOncePerMethod(t *testing.T) {
	rec := &instanceRecorder{}
	l := WrapLogger(rec, []string{"password"})
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Fatal("f")
	if !reflect.DeepEqual(rec.calls, []string{"d", "i", "w", "e", "f"}) {
		t.Error(test.DiffMessage(rec.calls, []string{"d", "i", "w", "e", "f"}, "M6: each method calls the same method of the custom logger once"))
	}
}

func TestWrapLogger_NilPanics(t *testing.T) {
	want := color.FmtRed("invalid logger: WrapLogger needs a logger")
	for _, fields := range [][]string{nil, {"password"}} {
		var rec any
		func() {
			defer func() { rec = recover() }()
			WrapLogger(nil, fields)
		}()
		if rec != want {
			t.Error(test.DiffMessage(rec, want, "M7: WrapLogger(nil, …) panics with the bootstrap message"))
		}
	}
}

func randomArgs(r *rand.Rand) []any {
	pool := []func() any{
		func() any { return r.Intn(1000) },
		func() any { return "s" + strconv.Itoa(r.Intn(100)) },
		func() any { return r.Float64() > 0.5 },
		func() any { return maskUser{Name: "n" + strconv.Itoa(r.Intn(9)), Password: "p"} },
		func() any { return &maskUser{Name: "m"} },
		func() any { return map[string]any{"a": r.Intn(5), "Password": "x", "n": []int{1, 2}} },
		func() any { return map[string]int{"a": 1, "b": 2} },
		func() any { return map[int]string{1: "a", 2: "b"} },
		func() any { return []string{"a", "b"} },
		func() any { return []maskUser{{Name: "a"}, {Name: "b", Password: "z"}} },
		func() any { return [2]int{1, 2} },
		func() any { return errors.New("err" + strconv.Itoa(r.Intn(9))) },
		func() any { return time.Duration(r.Intn(1e6)) },
		func() any { return slog.String("password", "attr") },
		func() any { return slog.Group("g", "password", "p", "k", 1) },
		func() any { return (*maskUser)(nil) },
		func() any { return nil },
	}
	n := 1 + r.Intn(5)
	args := make([]any, 0, n*2)
	for i := range n {
		pick := pool[r.Intn(len(pool))]()
		if _, isAttr := pick.(slog.Attr); isAttr {
			args = append(args, pick)
			continue
		}
		args = append(args, "key"+strconv.Itoa(i), pick)
	}
	return args
}

func TestLog_FastPathEqualsGeneralPath(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 300 {
		args := randomArgs(r)
		for _, format := range []Format{FormatJSON, FormatText} {
			fastLogger, fast := newTestLog(format, LogOptions{})
			slowLogger, slow := newTestLog(format, LogOptions{MaskFields: []string{"zzz-never-matches"}})
			fastLogger.Info("m", args...)
			slowLogger.Info("m", args...)
			a := regexp.MustCompile(`time=\S+|"time":"[^"]*"`).ReplaceAllString(fast.String(), "")
			b := regexp.MustCompile(`time=\S+|"time":"[^"]*"`).ReplaceAllString(slow.String(), "")
			if a != b {
				t.Fatal(test.DiffMessage(b, a, "M8: the fast path produces what the general path produces ("+formatName(format)+")"))
			}
		}
	}
}

func TestLog_FastPathEqualsGeneralPath_Pretty(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	stamp := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	for range 200 {
		args := randomArgs(r)
		fastLogger, fast := newTestLog(FormatPretty, LogOptions{})
		slowLogger, slow := newTestLog(FormatPretty, LogOptions{MaskFields: []string{"zzz-never-matches"}})
		fastLogger.Info("m", args...)
		slowLogger.Info("m", args...)
		if stamp.ReplaceAllString(fast.String(), "") != stamp.ReplaceAllString(slow.String(), "") {
			t.Fatal(test.DiffMessage(slow.String(), fast.String(), "M8: Pretty output is the same on both paths"))
		}
	}
}

func TestLog_ScalarFastPathEqualsGeneralPath(t *testing.T) {
	args := []any{"a", 1, "b", "two", "c", 3.5, "d", true, "e", errors.New("e"), "f", time.Second, "g", uint8(7)}
	for _, format := range allFormats {
		fastLogger, fast := newTestLog(format, LogOptions{})
		slowLogger, slow := newTestLog(format, LogOptions{MaskFields: []string{"zzz"}})
		fastLogger.Info("m", args...)
		slowLogger.Info("m", args...)
		stamp := regexp.MustCompile(`time=\S+|"time":"[^"]*"|\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
		if stamp.ReplaceAllString(fast.String(), "") != stamp.ReplaceAllString(slow.String(), "") {
			t.Error(test.DiffMessage(slow.String(), fast.String(), "M8: the scalar fast path equals the general path ("+formatName(format)+")"))
		}
	}
}

func TestWrapLogger_InlineGroupAddsNoSegment(t *testing.T) {
	inline := func() slog.Attr {
		return slog.Group("user", slog.Group("", slog.Group("card", slog.String("cvv", "SECRET"))))
	}
	for _, rule := range []string{"user.*.cvv", "user.card.cvv", "card.cvv", "cvv", "user.**"} {
		cap := &capturingLogger{}
		WrapLogger(cap, []string{rule}).Info("m", inline())
		if strings.Contains(fmt.Sprint(cap.args()), "SECRET") {
			t.Error(test.DiffMessage(cap.args(), "cvv masked", "rule "+rule+" through an inline group"))
		}
	}
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"user.*"}).Info("m", inline())
	user := cap.args()[0].(slog.Attr).Value.Group()
	if user[0].Key != "" || user[0].Value.Kind() != slog.KindGroup {
		t.Error(test.DiffMessage(user[0], "the inline group kept, its card masked", "user.* masks user.card, not the inline group"))
	}
}

func TestWrapLogger_EmptyTopLevelKeyAddsNoDot(t *testing.T) {
	cap := &capturingLogger{}
	WrapLogger(cap, []string{"card.cvv"}).Info("m", slog.Group("", slog.Group("card", slog.String("cvv", "SECRET"))))
	if strings.Contains(fmt.Sprint(cap.args()), "SECRET") {
		t.Error(test.DiffMessage(cap.args(), "cvv masked", "an empty top-level group key adds no leading dot to the path"))
	}
}
