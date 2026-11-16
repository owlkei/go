package tracecontext

import "strings"

const (
	traceparentLength = 55
	traceIDStart      = 3
	traceIDEnd        = 35
	parentIDStart     = 36
	parentIDEnd       = 52
	flagsStart        = 53
)

func ParseTraceparent(value string) (traceID, parentID string, flags byte, ok bool) {
	if len(value) < traceparentLength {
		return "", "", 0, false
	}
	version, isHex := hexByte(value[0], value[1])
	if !isHex || version == 0xff {
		return "", "", 0, false
	}
	if version == 0 && len(value) != traceparentLength {
		return "", "", 0, false
	}
	if len(value) > traceparentLength && value[traceparentLength] != '-' {
		return "", "", 0, false
	}
	if value[2] != '-' || value[traceIDEnd] != '-' || value[parentIDEnd] != '-' {
		return "", "", 0, false
	}
	if !isNonZeroLowerHex(value[traceIDStart:traceIDEnd]) || !isNonZeroLowerHex(value[parentIDStart:parentIDEnd]) {
		return "", "", 0, false
	}
	flags, isHex = hexByte(value[flagsStart], value[flagsStart+1])
	if !isHex {
		return "", "", 0, false
	}
	if len(value) > traceparentLength {
		value = strings.Clone(value[:parentIDEnd])
	}
	return value[traceIDStart:traceIDEnd], value[parentIDStart:parentIDEnd], flags, true
}

func hexByte(hi, lo byte) (byte, bool) {
	h, okHi := hexDigit(hi)
	l, okLo := hexDigit(lo)
	return h<<4 | l, okHi && okLo
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

func isNonZeroLowerHex(s string) bool {
	nonZero := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
		if c != '0' {
			nonZero = true
		}
	}
	return nonZero
}
