package tracecontext

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	spanIDBytes    = 8
	traceTimeBytes = 6
	traceIDBytes   = 16
	sourceSize     = 512
)

var source struct {
	mu   sync.Mutex
	buf  [sourceSize]byte
	left int
}

func NewSpanID() string {
	var text [2 * spanIDBytes]byte
	putSpanID(text[:])
	return string(text[:])
}

func NewTraceID(now time.Time) string {
	var text [2 * traceIDBytes]byte
	putTraceID(text[:], now)
	return string(text[:])
}

func putSpanID(dst []byte) {
	var raw [spanIDBytes]byte
	readNonZero(raw[:])
	hex.Encode(dst, raw[:])
}

func putTraceID(dst []byte, now time.Time) {
	var raw [traceIDBytes]byte
	ms := uint64(now.UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	readNonZero(raw[traceTimeBytes:])
	hex.Encode(dst, raw[:])
}

func readNonZero(dst []byte) {
	for {
		readRandom(dst)
		for _, b := range dst {
			if b != 0 {
				return
			}
		}
	}
}

func readRandom(dst []byte) {
	source.mu.Lock()
	if source.left < len(dst) {
		_, _ = rand.Read(source.buf[:])
		source.left = sourceSize
	}
	start := sourceSize - source.left
	copy(dst, source.buf[start:start+len(dst)])
	source.left -= len(dst)
	source.mu.Unlock()
}
