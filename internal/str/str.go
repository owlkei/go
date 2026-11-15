package str

import (
	"strings"
	"unicode"
)

func RemoveSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func Segment(s string, sep byte, start int) (string, int) {
	if len(s) == 0 || start < 0 || start > len(s)-1 {
		return "", -1
	}

	i := strings.IndexByte(s[start+1:], sep)
	if i < 0 {
		return s[start:], i
	}

	next := i + start + 1
	return s[start+1 : next], next
}

func Enclose(s string, sep byte) string {
	if isEnclosed(s, sep) {
		return s
	}
	return encloseSlow(s, sep)
}

func isEnclosed(s string, sep byte) bool {
	if len(s) == 0 || s[0] != sep || s[len(s)-1] != sep {
		return false
	}
	prev := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isASCIISpace(c) || (c == prev && (c == sep || c == '*')) {
			return false
		}
		prev = c
	}
	return true
}

func encloseSlow(s string, sep byte) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(sep)
	prev := sep
	hadContent := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if isASCIISpace(c) {
			continue
		}
		hadContent = true
		if (c == sep && prev == sep) || (c == '*' && prev == '*') {
			continue
		}
		b.WriteByte(c)
		prev = c
	}

	if !hadContent {
		return ""
	}

	if prev != sep {
		b.WriteByte(sep)
	}
	return b.String()
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func RemoveDup(s string, c byte) string {
	if !hasRun(s, c) {
		return s
	}
	return removeDupSlow(s, c)
}

func hasRun(s string, c byte) bool {
	for i := 1; i < len(s); i++ {
		if s[i] == c && s[i-1] == c {
			return true
		}
	}
	return false
}

func removeDupSlow(s string, c byte) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i] == c && s[i-1] == c {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
