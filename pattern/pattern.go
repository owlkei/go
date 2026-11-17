package pattern

import (
	"strings"
)

const (
	singleWildcard = "*"
	multiWildcard  = "**"
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
	if raw == multiWildcard {
		return Pattern{raw: raw, kind: KindGlobal}
	}
	prefix, last, found := cutLast(raw)
	hasSingle := hasSingleWildcard(prefix)
	if !hasSingle && (last != multiWildcard || !found) && last != singleWildcard {
		return Pattern{raw: raw, kind: KindExact}
	}
	if found && last == multiWildcard && !hasSingle && !hasMultiWildcard(prefix) {
		return Pattern{raw: raw, kind: KindSuffixWildcard, dotted: raw[:len(prefix)+1]}
	}
	return Pattern{raw: raw, segments: strings.Split(raw, "."), kind: KindComplex}
}

func cutLast(raw string) (prefix, last string, found bool) {
	i := strings.LastIndexByte(raw, '.')
	if i < 0 {
		return "", raw, false
	}
	return raw[:i], raw[i+1:], true
}

func hasSingleWildcard(s string) bool {
	return hasSegment(s, singleWildcard)
}

func hasMultiWildcard(s string) bool {
	return hasSegment(s, multiWildcard)
}

func hasSegment(s, seg string) bool {
	for s != "" {
		var cur string
		cur, s, _ = strings.Cut(s, ".")
		if cur == seg {
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

func (p Pattern) matchSegments(topic string) bool {
	rest, exhausted := topic, false
	last := len(p.segments) - 1
	for i, seg := range p.segments {
		if exhausted {
			return false
		}
		if seg == multiWildcard && i == last {
			return true
		}
		var current string
		current, rest, exhausted = cutSegment(rest)
		if seg != singleWildcard && seg != current {
			return false
		}
	}
	return exhausted
}

func cutSegment(s string) (current, rest string, exhausted bool) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i], s[i+1:], false
	}
	return s, "", true
}
