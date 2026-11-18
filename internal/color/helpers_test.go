package color

import (
	"testing"
)

var fmtCases = []struct {
	name  string
	fn    func(string, ...any) string
	open  string
	close string
}{
	{"FmtWhite", FmtWhite, "\x1b[97m", "\x1b[0m"},
	{"FmtBlue", FmtBlue, "\x1b[34m", "\x1b[0m"},
	{"FmtGreen", FmtGreen, "\x1b[32m", "\x1b[0m"},
	{"FmtCyan", FmtCyan, "\x1b[36m", "\x1b[0m"},
	{"FmtYellow", FmtYellow, "\x1b[33m", "\x1b[0m"},
	{"FmtRed", FmtRed, "\x1b[31m", "\x1b[0m"},
	{"FmtMagenta", FmtMagenta, "\x1b[35m", "\x1b[0m"},
	{"FmtPurple", FmtPurple, "\x1b[38;5;129m", "\x1b[39m"},
	{"FmtOrange", FmtOrange, "\x1b[38;5;208m", "\x1b[0m"},
	{"FmtDim", FmtDim, "\x1b[2m", "\x1b[0m"},
	{"FmtBold", FmtBold, "\x1b[1m", "\x1b[0m"},
	{"FmtItalic", FmtItalic, "\x1b[3m", "\x1b[0m"},
	{"FmtBGBlue", FmtBGBlue, "\x1b[44m", "\x1b[0m"},
	{"FmtBGGreen", FmtBGGreen, "\x1b[42m", "\x1b[0m"},
	{"FmtBGRed", FmtBGRed, "\x1b[41m", "\x1b[0m"},
	{"FmtBGYellow", FmtBGYellow, "\x1b[43m", "\x1b[0m"},
	{"FmtBGGrey", FmtBGGrey, "\x1b[47m", "\x1b[49m"},
	{"FmtBGDim", FmtBGDim, "\x1b[48;5;236m", "\x1b[0m"},
}

func diff(t *testing.T, got, want, desc string) {
	t.Helper()
	t.Errorf("%s\nExpected: %q\nActual: %q", desc, want, got)
}
