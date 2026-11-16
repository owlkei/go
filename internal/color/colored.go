package color

import "log/slog"

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
