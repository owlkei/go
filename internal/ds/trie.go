package ds

import (
	"errors"
	"slices"
	"strings"
)

const MaxSegments = 64

var ErrTooManySegments = errors.New("ds: pattern has more than 64 segments")

type Trie[T any] struct {
	root  node[T]
	count int
}

type node[T any] struct {
	value    *T
	statics  map[string]*node[T]
	globs    []glob[T]
	param    *node[T]
	wildcard *node[T]
}

type glob[T any] struct {
	pattern  string
	literals int
	child    *node[T]
}

func NewTrie[T any]() *Trie[T] {
	return &Trie[T]{}
}

func (t *Trie[T]) Len() int {
	return t.count
}

func (t *Trie[T]) Insert(pattern string, value *T) error {
	if countSegments(pattern) > MaxSegments {
		return ErrTooManySegments
	}

	n := &t.root
	for seg, next, ok := nextSegment(pattern, 0); ok; seg, next, ok = nextSegment(pattern, next) {
		n = n.child(seg)
	}

	if n.value == nil && value != nil {
		t.count++
	} else if n.value != nil && value == nil {
		t.count--
	}
	n.value = value

	return nil
}

func (t *Trie[T]) Find(path string, params []string) (*T, []string) {
	if len(path) > 2*MaxSegments && countSegments(path) > MaxSegments {
		return nil, params
	}

	return t.root.find(path, 0, params, len(params))
}

func (n *node[T]) child(seg string) *node[T] {
	switch {
	case seg == "$":
		if n.param == nil {
			n.param = &node[T]{}
		}
		return n.param
	case seg == "*":
		if n.wildcard == nil {
			n.wildcard = &node[T]{}
		}
		return n.wildcard
	case strings.IndexByte(seg, '*') >= 0:
		return n.globChild(seg)
	}

	if c := n.statics[seg]; c != nil {
		return c
	}
	if n.statics == nil {
		n.statics = make(map[string]*node[T])
	}
	c := &node[T]{}
	n.statics[seg] = c
	return c
}

func (n *node[T]) globChild(pattern string) *node[T] {
	g := glob[T]{pattern: pattern, literals: len(pattern) - strings.Count(pattern, "*")}
	i, found := slices.BinarySearchFunc(n.globs, g, compareGlobs[T])
	if found {
		return n.globs[i].child
	}
	g.child = &node[T]{}
	n.globs = slices.Insert(n.globs, i, g)
	return g.child
}

func compareGlobs[T any](a, b glob[T]) int {
	if a.literals != b.literals {
		return b.literals - a.literals
	}
	return strings.Compare(a.pattern, b.pattern)
}

func (n *node[T]) find(path string, start int, params []string, captured int) (*T, []string) {
	seg, next, ok := nextSegment(path, start)
	if !ok {
		if n.value != nil {
			return n.value, collect(params, captured)
		}
		if n.wildcard != nil && n.wildcard.value != nil {
			return n.wildcard.value, collect(params, captured)
		}
		return nil, params
	}

	if c := n.statics[seg]; c != nil {
		if value, out := c.find(path, next, params, captured); value != nil {
			return value, out
		}
	}

	for i := range n.globs {
		if matchGlob(seg, n.globs[i].pattern) {
			if value, out := n.globs[i].child.find(path, next, params, captured); value != nil {
				return value, out
			}
		}
	}

	if n.param != nil {
		if value, out := n.param.find(path, next, params, captured+1); value != nil {
			out[captured] = seg
			return value, out
		}
	}

	if w := n.wildcard; w != nil {
		if value, out := w.find(path, next, params, captured); value != nil {
			return value, out
		}
		if w.value != nil {
			return w.value, collect(params, captured)
		}
	}

	return nil, params
}

func matchGlob(str, route string) bool {
	prefix, rest, found := strings.Cut(route, "*")
	if !found {
		return str == route
	}

	if prefix != "" {
		if len(str) < len(prefix) || str[:len(prefix)] != prefix {
			return false
		}
		str = str[len(prefix):]
	}

	for {
		mid, next, found := strings.Cut(rest, "*")
		if !found {
			if mid == "" {
				return true
			}
			return len(str) >= len(mid) && str[len(str)-len(mid):] == mid
		}
		rest = next

		if mid == "" {
			continue
		}

		idx := strings.Index(str, mid)
		if idx < 0 {
			return false
		}
		str = str[idx+len(mid):]
	}
}

func collect(params []string, captured int) []string {
	if captured <= cap(params) {
		return params[:captured]
	}
	out := make([]string, captured)
	copy(out, params)
	return out
}

func nextSegment(s string, start int) (string, int, bool) {
	for start < len(s) && s[start] == '/' {
		start++
	}
	if start == len(s) {
		return "", start, false
	}
	end := strings.IndexByte(s[start:], '/')
	if end < 0 {
		return s[start:], len(s), true
	}
	return s[start : start+end], start + end, true
}

func countSegments(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '/' && (i == 0 || s[i-1] == '/') {
			count++
		}
	}
	return count
}
