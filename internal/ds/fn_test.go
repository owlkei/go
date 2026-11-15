package ds

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestMatchGlob_Prefix(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"image.png", "image.*", true},
		{"image.", "image.*", true},
		{"image", "image.*", false},
		{"x.image.png", "image.*", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Suffix(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"index.html", "*.html", true},
		{".html", "*.html", true},
		{"a.html.html", "*.html", true},
		{"index.png", "*.html", false},
		{"index.html.png", "*.html", false},
		{"aaaa", "*a", true},
		{"x.y.z", "*.z", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Middle(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"index.html", "in*.html", true},
		{"in.html", "in*.html", true},
		{"in.htm", "in*.html", false},
		{"xin.html", "in*.html", false},
		{"x.y.z", "x.*", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_TwoStars(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"abc", "a*b*c", true},
		{"aXbYc", "a*b*c", true},
		{"abcc", "a*b*c", true},
		{"acb", "a*b*c", false},
		{"ac", "a*b*c", false},
		{"abc", "**", true},
		{"a", "*", true},
		{"", "*", true},
		{"", "**", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}

func TestMatchGlob_Overlap(t *testing.T) {
	cases := []struct {
		str, pattern string
		want         bool
	}{
		{"aba", "ab*ba", false},
		{"abba", "ab*ba", true},
		{"ababa", "ab*ba", true},
		{"ab", "ab*ba", false},
		{"foo", "foo", true},
		{"foobar", "foo", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.str, c.pattern); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "T7: matchGlob("+c.str+", "+c.pattern+")"))
		}
	}
}
