package pattern

import (
	"strings"
)

type Kind uint8

const (
	KindExact Kind = iota
	KindGlobal
	KindSuffixWildcard
	KindComplex
)

type Pattern struct {
	raw      string
	segments []string
	dotted   string
	kind     Kind
}

func NewPattern(raw string) Pattern {
	if raw == "*" {
		return Pattern{raw: raw, kind: KindGlobal}
	}
	if !hasWildSegment(raw) {
		return Pattern{raw: raw, kind: KindExact}
	}
	if n := len(raw); n >= 2 && raw[n-2:] == ".*" && !hasWildSegment(raw[:n-2]) {
		return Pattern{raw: raw, kind: KindSuffixWildcard, dotted: raw[:n-1]}
	}
	return Pattern{raw: raw, segments: strings.Split(raw, "."), kind: KindComplex}
}

func hasWildSegment(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '*' && (i == 0 || s[i-1] == '.') && (i == len(s)-1 || s[i+1] == '.') {
			return true
		}
	}
	return false
}

func (p Pattern) Match(topic string) bool {
	switch p.kind {
	case KindGlobal:
		return true
	case KindExact:
		return p.raw == topic
	case KindSuffixWildcard:
		return len(topic) > len(p.dotted) && strings.HasPrefix(topic, p.dotted)
	default:
		return p.matchSegments(topic)
	}
}

func (p Pattern) Raw() string {
	return p.raw
}

func (p Pattern) Kind() Kind {
	return p.kind
}

func (p Pattern) SimplePrefix() string {
	if p.dotted == "" {
		return ""
	}
	return p.dotted[:len(p.dotted)-1]
}

func (p Pattern) IsExact() bool {
	return p.kind == KindExact
}

func (p Pattern) IsGlobal() bool {
	return p.kind == KindGlobal
}

func (p Pattern) matchSegments(topic string) bool {
	rest, done := topic, false
	last := len(p.segments) - 1
	for i, want := range p.segments {
		if done {
			return false
		}
		if want == "*" && i == last {
			return true
		}
		seg := rest
		if j := strings.IndexByte(rest, '.'); j >= 0 {
			seg, rest = rest[:j], rest[j+1:]
		} else {
			done = true
		}
		if want != "*" && want != seg {
			return false
		}
	}
	return done
}
