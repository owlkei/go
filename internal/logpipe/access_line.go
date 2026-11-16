package logpipe

import "log/slog"

type AccessLine struct {
	Msg   string
	Attrs []slog.Attr
}
