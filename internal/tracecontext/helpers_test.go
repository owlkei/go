package tracecontext

import (
	"crypto/rand"
	"sync"
	"testing"
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
