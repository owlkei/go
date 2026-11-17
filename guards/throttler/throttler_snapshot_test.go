package throttler

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

func snapshotOf(t testing.TB, a activator) map[string]any {
	t.Helper()
	s, ok := a.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(a, "an inspect.Snapshotter", "I1: the compiled throttler implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "throttler" {
		t.Error(test.DiffMessage(snap.Name, "throttler", "I4: snapshot name"))
	}
	fields := make(map[string]any, len(snap.Fields))
	for _, f := range snap.Fields {
		fields[strings.ToLower(f.Key)] = f.Value
	}
	return fields
}

func numberOf(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint64:
		return int64(n), true
	}
	return 0, false
}

func TestThrottler_Snapshot(t *testing.T) {
	a := compile(t, Throttler{Limit: 2, TTL: 24 * time.Hour, Strategy: StrategySlidingWindow})
	for i := range 5 {
		try(a, newCtx("203.0.113.9:"+strconv.Itoa(i+1), nil))
	}
	try(a, newCtx("203.0.113.10:1", nil))
	f := snapshotOf(t, a)

	if n, ok := numberOf(f["limit"]); !ok || n != 2 {
		t.Error(test.DiffMessage(f["limit"], 2, "I4: effective limit"))
	}
	if f["ttl"] != 24*time.Hour {
		t.Error(test.DiffMessage(f["ttl"], 24*time.Hour, "I4: effective TTL"))
	}
	if _, ok := f["strategy"]; !ok {
		t.Error(test.DiffMessage(f, "a strategy field", "I4: effective strategy"))
	}
	if n, ok := numberOf(f["keys"]); !ok || n != 2 {
		t.Error(test.DiffMessage(f["keys"], 2, "I4: keys is the store's count (two clients, sliding window stores the current window)"))
	}
	if n, ok := numberOf(f["rejected"]); !ok || n != 3 {
		t.Error(test.DiffMessage(f["rejected"], 3, "I4: rejected counts refusals"))
	}
	if n, ok := numberOf(f["evictions"]); !ok || n != 0 {
		t.Error(test.DiffMessage(f["evictions"], 0, "I4: evictions come from the store"))
	}
}

func TestThrottler_Snapshot_CustomBackendHasNoStoreFields(t *testing.T) {
	a := compile(t, Throttler{Backend: newTestCache(), KeyFunc: fixedKeyFunc("k")})
	try(a, newCtx("203.0.113.9:1", nil))
	f := snapshotOf(t, a)
	for _, name := range []string{"keys", "evictions"} {
		if _, ok := f[name]; ok {
			t.Error(test.DiffMessage(f[name], "absent", "I4: with a custom Backend the "+name+" field is absent"))
		}
	}
	if _, ok := f["rejected"]; !ok {
		t.Error(test.DiffMessage(f, "a rejected field", "I4: rejected stays"))
	}
}

func TestThrottler_Snapshot_NotBuiltOnRequestPath(t *testing.T) {
	a := compile(t, Throttler{Limit: 1 << 30, KeyFunc: fixedKeyFunc("k")})
	c := newCtx("203.0.113.9:1", nil)
	allocs := testing.AllocsPerRun(100, func() { a.CanActivate(c) })
	if allocs > 8 {
		t.Error(test.DiffMessage(allocs, 8, "I2: a passing request allocates no snapshot"))
	}
}

func TestThrottler_StoreBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("1,000,000 distinct keys")
	}
	var next int
	a := compile(t, Throttler{Limit: 1, TTL: 24 * time.Hour, KeyFunc: func(*ctx.HTTPContext) string {
		next++
		return "key-" + strconv.Itoa(next)
	}})
	c := newCtx("203.0.113.9:1", nil)
	for i := range 1_000_000 {
		a.CanActivate(c)
		if i%100_000 == 99_999 {
			if n, ok := numberOf(snapshotOf(t, a)["keys"]); !ok || n > 32768 {
				t.Fatal(test.DiffMessage(n, 32768, "T4: entry count after "+strconv.Itoa(i+1)+" distinct keys"))
			}
		}
	}
	f := snapshotOf(t, a)
	if n, ok := numberOf(f["evictions"]); !ok || n < 1_000_000-32768 {
		t.Error(test.DiffMessage(f["evictions"], 1_000_000-32768, "T4: every key past the bound evicts another"))
	}
}

func TestThrottler_StoreBounded_SpoofedHeaderFlood(t *testing.T) {
	a := compile(t, Throttler{Limit: 5, TTL: 24 * time.Hour})
	for i := range 100_000 {
		ip := "10." + strconv.Itoa(i>>16&255) + "." + strconv.Itoa(i>>8&255) + "." + strconv.Itoa(i&255)
		try(a, newCtx("203.0.113.9:1", map[string]string{"X-Forwarded-For": ip, "X-Real-IP": ip}))
	}
	if n, ok := numberOf(snapshotOf(t, a)["keys"]); !ok || n != 1 {
		t.Error(test.DiffMessage(n, 1, "T1, T4: a flood of spoofed headers from one peer stores one key"))
	}
}
