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
	traceTailBytes = 10
	traceIDBytes   = traceTimeBytes + traceTailBytes
	scopeBytes     = traceTailBytes + spanIDBytes
	sourceSize     = 504
)

type source struct {
	buf  [sourceSize]byte
	left int
}

func (s *source) read(dst []byte) {
	if s.left < len(dst) {
		_, _ = rand.Read(s.buf[:])
		s.left = sourceSize
	}
	start := sourceSize - s.left
	copy(dst, s.buf[start:start+len(dst)])
	s.left -= len(dst)
}

var sources = sync.Pool{New: func() any { return new(source) }}

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
	readNonZero(raw[:], len(raw))
	hex.Encode(dst, raw[:])
}

func putTraceID(dst []byte, now time.Time) {
	var tail [traceTailBytes]byte
	readNonZero(tail[:], len(tail))
	encodeTraceID(dst, now, tail[:])
}

func putTraceAndSpan(traceDst, spanDst []byte, now time.Time) {
	var raw [scopeBytes]byte
	readNonZero(raw[:], traceTailBytes)
	encodeTraceID(traceDst, now, raw[:traceTailBytes])
	hex.Encode(spanDst, raw[traceTailBytes:])
}

func encodeTraceID(dst []byte, now time.Time, tail []byte) {
	var raw [traceIDBytes]byte
	ms := uint64(now.UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	copy(raw[traceTimeBytes:], tail)
	hex.Encode(dst, raw[:])
}

func readNonZero(dst []byte, split int) {
	s := sources.Get().(*source)
	for {
		s.read(dst)
		if !allZero(dst[:split]) && !allZero(dst[split:]) {
			break
		}
	}
	sources.Put(s)
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return len(b) > 0
}
