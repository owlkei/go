package color

import (
	"testing"
)

func TestFmt_Codes(t *testing.T) {
	for _, c := range fmtCases {
		got := c.fn("hello")
		want := c.open + "hello" + c.close
		if got != want {
			diff(t, got, want, "C1: "+c.name)
		}
	}
}

func TestFmt_NoArgsVerbatim(t *testing.T) {
	for _, c := range fmtCases {
		got := c.fn("100%")
		want := c.open + "100%" + c.close
		if got != want {
			diff(t, got, want, "C1: verbatim "+c.name)
		}
		got = c.fn("%d %s")
		want = c.open + "%d %s" + c.close
		if got != want {
			diff(t, got, want, "C1: verbatim verbs "+c.name)
		}
	}
}

func TestFmt_ArgsFormatted(t *testing.T) {
	for _, c := range fmtCases {
		got := c.fn("count: %d, label: %s, %d%%", 42, "ok", 7)
		want := c.open + "count: 42, label: ok, 7%" + c.close
		if got != want {
			diff(t, got, want, "C1: formatted "+c.name)
		}
	}
}
