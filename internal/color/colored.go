package color

import (
	"log/slog"
	"time"
)

type Color string

const (
	Gray   Color = "\x1b[90m"
	Green  Color = "\x1b[32m"
	Blue   Color = "\x1b[34m"
	Yellow Color = "\x1b[33m"
	Orange Color = "\x1b[38;5;208m"
	Red    Color = "\x1b[31m"
)

type Colored struct {
	Text  string
	Color Color
}

func (c Colored) LogValue() slog.Value {
	return slog.StringValue(c.Text)
}

func ForDuration(d time.Duration) Color {
	switch {
	case d < 10*time.Millisecond:
		return Gray
	case d < 50*time.Millisecond:
		return Green
	case d < 100*time.Millisecond:
		return Blue
	case d < 300*time.Millisecond:
		return Yellow
	case d < 1000*time.Millisecond:
		return Orange
	default:
		return Red
	}
}
