package tracecontext

import (
	"cmp"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestNewSpanID_Format(t *testing.T) {
	for range 10000 {
		id := NewSpanID()
		if len(id) != 16 || !isLowerHex(id) {
			t.Fatal(test.DiffMessage(id, "<16 characters in 0-9a-f>", "G1: a span ID is 16 lower-case hex characters"))
		}
	}
}

func TestNewSpanID_NeverZero(t *testing.T) {
	installReader(t, &zeroFirstReader{})
	for range 300 {
		id := NewSpanID()
		if id == "0000000000000000" {
			t.Fatal(test.DiffMessage(id, "<not all zeros>", "G1: a span ID is never 0000000000000000"))
		}
		if len(id) != 16 || !isLowerHex(id) {
			t.Fatal(test.DiffMessage(id, "<16 characters in 0-9a-f>", "G1: a span ID stays well formed while the source returns zeros"))
		}
	}
}

func TestNewTraceID_Format(t *testing.T) {
	now := time.Now()
	for range 10000 {
		id := NewTraceID(now)
		if len(id) != 32 || !isLowerHex(id) {
			t.Fatal(test.DiffMessage(id, "<32 characters in 0-9a-f>", "G2: a trace ID is 32 lower-case hex characters"))
		}
	}
}

func TestNewTraceID_TimePrefix(t *testing.T) {
	for _, ms := range []int64{1763300000123, 1, 1 << 40, 1<<48 - 1} {
		now := time.UnixMilli(ms)
		id := NewTraceID(now)
		want := fmt.Sprintf("%012x", ms)
		if id[:12] != want {
			t.Error(test.DiffMessage(id[:12], want, "G2: the first 12 characters are the milliseconds as a 48-bit big-endian number"))
		}
	}
}

func TestNewTraceID_SubMillisecondIgnored(t *testing.T) {
	base := time.UnixMilli(1763300000123)
	a := NewTraceID(base)
	b := NewTraceID(base.Add(999 * time.Microsecond))
	if a[:12] != b[:12] {
		t.Error(test.DiffMessage(b[:12], a[:12], "G2: the prefix is UnixMilli, so times in the same millisecond share it"))
	}
}

func TestNewTraceID_SortsByTime(t *testing.T) {
	base := time.UnixMilli(1763300000000)
	for i := range 1000 {
		earlier := NewTraceID(base.Add(time.Duration(i) * time.Millisecond))
		later := NewTraceID(base.Add(time.Duration(i+1) * time.Millisecond))
		if earlier >= later {
			t.Fatal(test.DiffMessage(earlier+" >= "+later, earlier+" < "+later, "G2: IDs made 1 ms apart sort as strings in creation order"))
		}
	}
}

func TestNewTraceID_RandomTail(t *testing.T) {
	var seen [20][16]bool
	now := time.Now()
	for range 10000 {
		id := NewTraceID(now)
		for i := range 20 {
			c := id[12+i]
			digit := c - '0'
			if c >= 'a' {
				digit = c - 'a' + 10
			}
			seen[i][digit] = true
		}
	}
	for i := range seen {
		for digit, ok := range seen[i] {
			if !ok {
				t.Error(test.DiffMessage(fmt.Sprintf("digit %x never at tail position %d", digit, i), "every digit at every position", "G2: the last 20 characters are random"))
			}
		}
	}
}

func primeSpanStream(t *testing.T, r *streamReader) string {
	t.Helper()
	for range 200 {
		id := NewSpanID()
		if r.reads() > 0 {
			return id
		}
	}
	t.Fatal(test.DiffMessage(r.reads(), "> 0", "G3: the source refills from crypto/rand"))
	return ""
}

func TestSource_StreamEqualsReader(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	const count = 151
	ids := make([]string, 0, count)
	ids = append(ids, primeSpanStream(t, r))
	for len(ids) < count {
		ids = append(ids, NewSpanID())
	}

	got := make([]byte, 0, 8*count)
	for _, id := range ids {
		raw, err := hex.DecodeString(id)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, raw...)
	}
	want := make([]byte, len(got))
	for i := range want {
		want[i] = streamByte(i)
	}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(hex.EncodeToString(got[:32]), hex.EncodeToString(want[:32]), "G3, H1: the bytes of consecutive IDs are the reader's stream in order, across a refill"))
	}
	if r.reads() != 3 {
		t.Error(test.DiffMessage(r.reads(), 3, "G3, H1: 151 span IDs need three 512-byte refills"))
	}
}

func TestSource_ReadsInBlocksOf512OnDemand(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	primeSpanStream(t, r)
	for range 63 {
		NewSpanID()
	}
	if r.reads() != 1 {
		t.Error(test.DiffMessage(r.reads(), 1, "H1: 64 span IDs come from one refill"))
	}
	NewSpanID()
	if r.reads() != 2 {
		t.Error(test.DiffMessage(r.reads(), 2, "H1: the 65th span ID triggers the next refill"))
	}
	for i, size := range r.sizes {
		if size != 512 {
			t.Error(test.DiffMessage(size, 512, fmt.Sprintf("H1: refill %d reads the whole 512-byte buffer with one call", i)))
		}
	}
}

func TestNewTraceID_TailIsReaderStream(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	primeSpanStream(t, r)
	now := time.UnixMilli(1763300000123)
	for i := range 20 {
		id := NewTraceID(now)
		tail, err := hex.DecodeString(id[12:])
		if err != nil {
			t.Fatal(err)
		}
		want := make([]byte, 10)
		for j := range want {
			want[j] = streamByte(8 + 10*i + j)
		}
		if !slices.Equal(tail, want) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(tail), hex.EncodeToString(want), fmt.Sprintf("G3: trace ID %d takes 10 random bytes from the stream", i)))
		}
	}
}

func TestNewSpanID_OneAllocation(t *testing.T) {
	if got := testing.AllocsPerRun(1000, func() { _ = NewSpanID() }); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "G3: a span ID costs one allocation (the string)"))
	}
}

func TestNewTraceID_OneAllocation(t *testing.T) {
	now := time.Now()
	if got := testing.AllocsPerRun(1000, func() { _ = NewTraceID(now) }); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "G3: a trace ID costs one allocation (the string)"))
	}
}

func TestNewSpanID_NoDuplicateIn10M(t *testing.T) {
	if testing.Short() {
		t.Skip("G4: 10,000,000 IDs are skipped with -short")
	}
	const n = 10_000_000
	values := make([]uint64, n)
	for i := range values {
		v, err := strconv.ParseUint(NewSpanID(), 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		values[i] = v
	}
	slices.Sort(values)
	for i := 1; i < n; i++ {
		if values[i] == values[i-1] {
			t.Fatal(test.DiffMessage(fmt.Sprintf("%016x twice", values[i]), "no duplicate", "G4: 10,000,000 span IDs hold no duplicate"))
		}
	}
}

type traceKey [2]uint64

func parseTraceKey(t *testing.T, id string) traceKey {
	t.Helper()
	hi, err := strconv.ParseUint(id[:16], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	lo, err := strconv.ParseUint(id[16:], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return traceKey{hi, lo}
}

func compareTraceKeys(a, b traceKey) int {
	if c := cmp.Compare(a[0], b[0]); c != 0 {
		return c
	}
	return cmp.Compare(a[1], b[1])
}

func TestNewTraceID_NoDuplicateIn10M(t *testing.T) {
	if testing.Short() {
		t.Skip("G4: 10,000,000 IDs are skipped with -short")
	}
	const n = 10_000_000
	now := time.Now()
	values := make([]traceKey, n)
	for i := range values {
		values[i] = parseTraceKey(t, NewTraceID(now))
	}
	slices.SortFunc(values, compareTraceKeys)
	for i := 1; i < n; i++ {
		if values[i] == values[i-1] {
			t.Fatal(test.DiffMessage(fmt.Sprintf("%016x%016x twice", values[i][0], values[i][1]), "no duplicate", "G4: 10,000,000 trace IDs made in one millisecond hold no duplicate"))
		}
	}
}

func TestNewSpanID_ParallelUnique(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 100_000
	results := make([][]string, goroutines)
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]string, perGoroutine)
			for i := range ids {
				ids[i] = NewSpanID()
			}
			results[g] = ids
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine)
	for _, ids := range results {
		for _, id := range ids {
			if len(id) != 16 || !isLowerHex(id) {
				t.Fatal(test.DiffMessage(id, "<16 hex characters>", "G5: IDs made in parallel are well formed"))
			}
			if _, dup := seen[id]; dup {
				t.Fatal(test.DiffMessage(id, "no duplicate", "G4: 16 goroutines generating 100,000 span IDs each produce no duplicate"))
			}
			seen[id] = struct{}{}
		}
	}
}

func TestNewTraceID_ParallelUnique(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 100_000
	now := time.Now()
	results := make([][]string, goroutines)
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]string, perGoroutine)
			for i := range ids {
				ids[i] = NewTraceID(now)
			}
			results[g] = ids
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine)
	for _, ids := range results {
		for _, id := range ids {
			if len(id) != 32 || !isLowerHex(id) {
				t.Fatal(test.DiffMessage(id, "<32 hex characters>", "G5: IDs made in parallel are well formed"))
			}
			if _, dup := seen[id]; dup {
				t.Fatal(test.DiffMessage(id, "no duplicate", "G4: 16 goroutines generating 100,000 trace IDs each produce no duplicate"))
			}
			seen[id] = struct{}{}
		}
	}
}
