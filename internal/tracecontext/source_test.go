package tracecontext

import (
	"encoding/hex"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
)

func streamWant(from, n int) []byte {
	want := make([]byte, n)
	for i := range want {
		want[i] = streamByte(from + i)
	}
	return want
}

func resetSources() {
	runtime.GC()
	runtime.GC()
}

func isConsecutive(b []byte) bool {
	for i := 1; i < len(b); i++ {
		if b[i] != byte(int(b[i-1])%251+1) {
			return false
		}
	}
	return true
}

func mustDecode(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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

type blockReader struct {
	mu     sync.Mutex
	blocks [][]byte
	next   int
}

func (r *blockReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	block := r.blocks[min(r.next, len(r.blocks)-1)]
	r.next++
	for i := range b {
		b[i] = block[i%len(block)]
	}
	return len(b), nil
}

func patternBlock(parts ...[]byte) []byte {
	var block []byte
	for _, p := range parts {
		block = append(block, p...)
	}
	return block
}

func repeated(b byte, n int) []byte {
	return slices.Repeat([]byte{b}, n)
}

func TestNewRequest_RedrawsZeroRequestID(t *testing.T) {
	block := patternBlock(repeated(0x11, 10), repeated(0, 8))
	for range 28 {
		block = append(block, patternBlock(repeated(0x22, 18))...)
	}
	block = block[:504]
	installReader(t, &blockReader{blocks: [][]byte{block}})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if s.Request == "0000000000000000" {
		t.Fatal(test.DiffMessage(s.Request, "<not all zeros>", "R4: a zero request ID is drawn again"))
	}
	if want := "2222222222222222"; s.Request != want {
		t.Error(test.DiffMessage(s.Request, want, "R4: the whole 18-byte read is drawn again"))
	}
	if want := "22222222222222222222"; s.TraceID[12:] != want {
		t.Error(test.DiffMessage(s.TraceID[12:], want, "R4: the trace tail comes from the new draw too"))
	}
}

func TestNewRequest_RedrawsZeroTraceTail(t *testing.T) {
	block := patternBlock(repeated(0, 10), repeated(0x11, 8))
	for range 28 {
		block = append(block, repeated(0x22, 18)...)
	}
	block = block[:504]
	installReader(t, &blockReader{blocks: [][]byte{block}})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if want := "22222222222222222222"; s.TraceID[12:] != want {
		t.Error(test.DiffMessage(s.TraceID[12:], want, "R4: an all-zero trace tail is drawn again"))
	}
	if want := "2222222222222222"; s.Request != want {
		t.Error(test.DiffMessage(s.Request, want, "R4: the request ID comes from the new draw"))
	}
}

func TestNewRequest_RedrawsAllZeroBlock(t *testing.T) {
	installReader(t, &zeroFirstReader{})
	resetSources()

	_, s := NewRequest(t.Context(), nil, fixedNow)
	if s.Request == "0000000000000000" || s.TraceID[12:] == "00000000000000000000" {
		t.Error(test.DiffMessage([]string{s.Request, s.TraceID[12:]}, "<non-zero>", "R4: zeros from the reader never reach an ID"))
	}
}
