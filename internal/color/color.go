package color

import "fmt"

func wrap(open, closing, format string, a []any) string {
	if len(a) == 0 {
		return open + format + closing
	}
	return open + fmt.Sprintf(format, a...) + closing
}

func FmtWhite(format string, a ...any) string {
	return wrap("\x1b[97m", "\x1b[0m", format, a)
}

func FmtBlue(format string, a ...any) string {
	return wrap("\x1b[34m", "\x1b[0m", format, a)
}

func FmtGreen(format string, a ...any) string {
	return wrap("\x1b[32m", "\x1b[0m", format, a)
}

func FmtCyan(format string, a ...any) string {
	return wrap("\x1b[36m", "\x1b[0m", format, a)
}

func FmtYellow(format string, a ...any) string {
	return wrap("\x1b[33m", "\x1b[0m", format, a)
}

func FmtRed(format string, a ...any) string {
	return wrap("\x1b[31m", "\x1b[0m", format, a)
}

func FmtMagenta(format string, a ...any) string {
	return wrap("\x1b[35m", "\x1b[0m", format, a)
}

func FmtPurple(format string, a ...any) string {
	return wrap("\x1b[38;5;129m", "\x1b[39m", format, a)
}

func FmtOrange(format string, a ...any) string {
	return wrap("\x1b[38;5;208m", "\x1b[0m", format, a)
}

func FmtDim(format string, a ...any) string {
	return wrap("\x1b[2m", "\x1b[0m", format, a)
}

func FmtBold(format string, a ...any) string {
	return wrap("\x1b[1m", "\x1b[0m", format, a)
}

func FmtItalic(format string, a ...any) string {
	return wrap("\x1b[3m", "\x1b[0m", format, a)
}

func FmtBGBlue(format string, a ...any) string {
	return wrap("\x1b[44m", "\x1b[0m", format, a)
}

func FmtBGGreen(format string, a ...any) string {
	return wrap("\x1b[42m", "\x1b[0m", format, a)
}

func FmtBGRed(format string, a ...any) string {
	return wrap("\x1b[41m", "\x1b[0m", format, a)
}

func FmtBGYellow(format string, a ...any) string {
	return wrap("\x1b[43m", "\x1b[0m", format, a)
}

func FmtBGGrey(format string, a ...any) string {
	return wrap("\x1b[47m", "\x1b[49m", format, a)
}

func FmtBGDim(format string, a ...any) string {
	return wrap("\x1b[48;5;236m", "\x1b[0m", format, a)
}
