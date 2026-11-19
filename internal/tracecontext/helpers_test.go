package tracecontext

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
	"unsafe"

	"go.owlky.com/owlky/internal/test"
)

const (
	validTraceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
	validParentID = "00f067aa0ba902b7"
	validHeader   = "00-" + validTraceID + "-" + validParentID + "-01"
)

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func streamByte(position int) byte {
	return byte(position%251 + 1)
}

type streamReader struct {
	mu       sync.Mutex
	position int
	sizes    []int
}

func (r *streamReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range b {
		b[i] = streamByte(r.position)
		r.position++
	}
	r.sizes = append(r.sizes, len(b))
	return len(b), nil
}

func (r *streamReader) reads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sizes)
}

type zeroFirstReader struct {
	mu    sync.Mutex
	reads int
}

func (r *zeroFirstReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	for i := range b {
		if r.reads == 1 {
			b[i] = 0
		} else {
			b[i] = 0xab
		}
	}
	return len(b), nil
}

func installReader(t testing.TB, reader interface{ Read([]byte) (int, error) }) {
	t.Helper()
	previous := rand.Reader
	rand.Reader = reader
	t.Cleanup(func() { rand.Reader = previous })
}

func scopedRequest(t *testing.T, remoteAddr string) (*http.Request, *Scope) {
	t.Helper()
	ctx, scope := NewRequest(context.Background(), http.Header{}, time.Now())
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	r.RemoteAddr = remoteAddr
	return r, scope
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

type unrelatedKey struct{}

var fixedNow = time.UnixMilli(1763300000123)

func headerOf(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Add(pairs[i], pairs[i+1])
	}
	return h
}

func flagsOf(t *testing.T, s *Scope) byte {
	t.Helper()
	_, _, flags, ok := ParseTraceparent(s.Traceparent())
	if !ok {
		t.Fatal(test.DiffMessage(s.Traceparent(), "<a valid traceparent>", "S6: Traceparent parses"))
	}
	return flags
}

func connectionContext(t *testing.T, flags string) (context.Context, *Scope) {
	t.Helper()
	return NewRequest(context.Background(), headerOf("Traceparent", "00-"+validTraceID+"-"+validParentID+"-"+flags), fixedNow)
}

func streamDelta(from, to byte) int {
	return ((int(to)-int(from))%251 + 251) % 251
}

func hasDelta(starts []byte, delta int) bool {
	for i := 1; i < len(starts); i++ {
		if streamDelta(starts[i-1], starts[i]) == delta {
			return true
		}
	}
	return false
}

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

func expectRejected(t *testing.T, value, desc string) {
	t.Helper()
	traceID, parentID, flags, ok := ParseTraceparent(value)
	if ok || traceID != "" || parentID != "" || flags != 0 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{"", "", byte(0), false}, desc))
	}
}

func within(s, container string) bool {
	if s == "" || container == "" {
		return false
	}
	start := uintptr(unsafe.Pointer(unsafe.StringData(container)))
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	return p >= start && p+uintptr(len(s)) <= start+uintptr(len(container))
}
