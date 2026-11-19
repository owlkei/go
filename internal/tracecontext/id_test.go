package tracecontext

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/test"
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

func TestSource_StreamEqualsReader(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	var s source
	got := make([]byte, 0, 8*151)
	for range 151 {
		var dst [8]byte
		s.read(dst[:])
		got = append(got, dst[:]...)
	}
	want := streamWant(0, len(got))
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(hex.EncodeToString(got[:32]), hex.EncodeToString(want[:32]), "R1: consecutive reads are the reader's stream in order across refills"))
	}
	if r.reads() != 3 {
		t.Error(test.DiffMessage(r.reads(), 3, "R1: 151 reads of 8 bytes need three refills of 504 bytes"))
	}
}

func TestSource_RefillDropsLeftover(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	var s source
	var dst [8]byte
	for range 63 {
		s.read(dst[:])
	}
	if r.reads() != 1 {
		t.Error(test.DiffMessage(r.reads(), 1, "R1: 63 reads of 8 bytes use one refill"))
	}
	s.read(dst[:])
	if r.reads() != 2 {
		t.Error(test.DiffMessage(r.reads(), 2, "R1: the 64th read triggers the second refill"))
	}
	if !slices.Equal(dst[:], streamWant(504, 8)) {
		t.Error(test.DiffMessage(dst[:], streamWant(504, 8), "R1: the 64th read starts the new block"))
	}
}

func TestSource_LazyFill(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	var s source
	if r.reads() != 0 {
		t.Error(test.DiffMessage(r.reads(), 0, "R1: a zero source makes no reader call before its first read"))
	}
	var dst [1]byte
	s.read(dst[:])
	if r.reads() != 1 {
		t.Error(test.DiffMessage(r.reads(), 1, "R1: the first read fills the buffer once"))
	}
}

func TestSource_ReadLargerThanLeft(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)

	var s source
	first := make([]byte, 494)
	s.read(first)
	var dst [18]byte
	s.read(dst[:])
	if !slices.Equal(dst[:], streamWant(504, 18)) {
		t.Error(test.DiffMessage(dst[:], streamWant(504, 18), "R1: with 10 bytes left an 18-byte read refills and returns the first 18 bytes of the new block"))
	}
	if r.reads() != 2 {
		t.Error(test.DiffMessage(r.reads(), 2, "R1: the leftover is dropped, not completed"))
	}
}

func TestSource_EveryReaderCallIs504Bytes(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)
	resetSources()

	var wg sync.WaitGroup
	now := time.Now()
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 300 {
				NewSpanID()
				NewTraceID(now)
				NewRequest(t.Context(), nil, now)
				NewMessage(t.Context(), now)
			}
		}()
	}
	wg.Wait()

	if r.reads() == 0 {
		t.Fatal(test.DiffMessage(r.reads(), "> 0", "R5: the generator reads from crypto/rand"))
	}
	for i, size := range r.sizes {
		if size != 504 {
			t.Fatal(test.DiffMessage(size, 504, fmt.Sprintf("R5: reader call %d asks for exactly 504 bytes", i)))
		}
	}
}

func TestNewSpanID_BytesAreContiguousInStream(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)
	resetSources()

	for i := range 200 {
		raw := mustDecode(t, NewSpanID())
		if !isConsecutive(raw) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw), "<8 consecutive stream values>", fmt.Sprintf("R2: span ID %d is one run of the stream", i)))
		}
	}
}

func TestNewTraceID_TailIsContiguousInStream(t *testing.T) {
	r := &streamReader{}
	installReader(t, r)
	resetSources()

	for i := range 200 {
		raw := mustDecode(t, NewTraceID(fixedNow))
		if !isConsecutive(raw[6:]) {
			t.Fatal(test.DiffMessage(hex.EncodeToString(raw[6:]), "<10 consecutive stream values>", fmt.Sprintf("R2: trace ID %d takes its tail from one run of the stream", i)))
		}
	}
}
