package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/dangduoc08/ginject/common"
)

type logFormat int

type LogOptions struct {
	Level      slog.Level
	LogFormat  logFormat
	AddSource  bool
	MaskFields []string
}

const (
	labelDebug = "DEBUG"
	labelInfo  = "INFO"
	labelWarn  = "WARN"
	labelError = "ERROR"
	labelFatal = "FATAL"

	DebugLevel = slog.LevelDebug
	InfoLevel  = slog.LevelInfo
	WarnLevel  = slog.LevelWarn
	ErrorLevel = slog.LevelError
	FatalLevel = slog.Level(12)

	TextFormat logFormat = iota + 1
	JSONFormat
	PrettyFormat
)

const badKey = "!BADKEY"

var exit = os.Exit

type recordWriter interface {
	Handle(ctx context.Context, r slog.Record) error
}

type logger struct {
	level     slog.Level
	addSource bool
	out       recordWriter
	fatal     recordWriter
	conv      converter
}

func NewLog(opts *LogOptions) common.Logger {
	return newLog(os.Stdout, isTerminal(os.Stdout), opts)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newLog(out io.Writer, isTerminal bool, opts *LogOptions) common.Logger {
	var o LogOptions
	if opts != nil {
		o = *opts
	}
	format := o.LogFormat
	if format == 0 {
		format = JSONFormat
		if isTerminal {
			format = PrettyFormat
		}
	}

	l := &logger{
		level:     o.Level,
		addSource: o.AddSource,
		conv:      converter{rules: mergeRules(nil, o.MaskFields), mode: modeText},
	}

	if format == PrettyFormat {
		pretty := newPrettyHandler(out)
		l.out, l.fatal = pretty, pretty
		l.conv.mode = modePretty
		return l
	}

	w := &lockedWriter{w: out, escapeC1: format != TextFormat}
	plainOpts := &slog.HandlerOptions{AddSource: o.AddSource}
	fatalOpts := &slog.HandlerOptions{AddSource: o.AddSource, ReplaceAttr: fatalLabel}
	if format == TextFormat {
		l.out, l.fatal = slog.NewTextHandler(w, plainOpts), slog.NewTextHandler(w, fatalOpts)
	} else {
		l.out, l.fatal = slog.NewJSONHandler(w, plainOpts), slog.NewJSONHandler(w, fatalOpts)
	}
	return l
}

func fatalLabel(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey && len(groups) == 0 {
		a.Value = slog.StringValue(labelFatal)
	}
	return a
}

type lockedWriter struct {
	mu       sync.Mutex
	w        io.Writer
	escapeC1 bool
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	out := p
	if lw.escapeC1 && bytes.IndexByte(p, 0xc2) >= 0 {
		out = escapeC1(p)
	}
	lw.mu.Lock()
	_, err := lw.w.Write(out)
	lw.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func escapeC1(p []byte) []byte {
	out := make([]byte, 0, len(p)+16)
	for i := 0; i < len(p); i++ {
		if p[i] == 0xc2 && i+1 < len(p) && p[i+1] >= 0x80 && p[i+1] <= 0x9f {
			out = append(out, '\\', 'u', '0', '0', hexDigits[p[i+1]>>4], hexDigits[p[i+1]&0x0f])
			i++
			continue
		}
		out = append(out, p[i])
	}
	return out
}

func (l *logger) Debug(msg string, args ...any) { l.log(DebugLevel, msg, args) }
func (l *logger) Info(msg string, args ...any)  { l.log(InfoLevel, msg, args) }
func (l *logger) Warn(msg string, args ...any)  { l.log(WarnLevel, msg, args) }
func (l *logger) Error(msg string, args ...any) { l.log(ErrorLevel, msg, args) }

func (l *logger) Fatal(msg string, args ...any) {
	l.log(FatalLevel, msg, args)
	exit(1)
}

func (l *logger) log(level slog.Level, msg string, args []any) {
	if level < l.level {
		return
	}
	var pc uintptr
	if l.addSource {
		var pcs [1]uintptr
		runtime.Callers(3, pcs[:])
		pc = pcs[0]
	}
	if onlyScalars(args) {
		l.write(level, msg, pc, args)
		return
	}
	l.writeGuarded(level, msg, pc, args)
}

func onlyScalars(args []any) bool {
	for i := 1; i < len(args); i += 2 {
		if _, ok := args[i-1].(string); !ok || !isScalar(args[i]) {
			return false
		}
	}
	return len(args)%2 == 0
}

func (l *logger) writeGuarded(level slog.Level, msg string, pc uintptr, args []any) {
	defer func() {
		if rec := recover(); rec != nil {
			l.writeRecovered(level, msg, pc, rec)
		}
	}()
	l.write(level, msg, pc, args)
}

func (l *logger) writeRecovered(level slog.Level, msg string, pc uintptr, rec any) {
	r := slog.NewRecord(time.Now(), level, msg, pc)
	r.AddAttrs(slog.String("log_error", fmt.Sprint(rec)))
	_ = l.writer(level).Handle(context.Background(), r)
}

func (l *logger) writer(level slog.Level) recordWriter {
	if level == FatalLevel {
		return l.fatal
	}
	return l.out
}

func (l *logger) write(level slog.Level, msg string, pc uintptr, args []any) {
	r := slog.NewRecord(time.Now(), level, msg, pc)
	for i := 0; i < len(args); {
		switch key := args[i].(type) {
		case string:
			if i+1 >= len(args) {
				r.AddAttrs(slog.String(badKey, key))
				i++
				continue
			}
			r.AddAttrs(slog.Attr{Key: key, Value: l.value(key, args[i+1])})
			i += 2
		case slog.Attr:
			a, _ := l.conv.attr(key)
			r.AddAttrs(a)
			i++
		default:
			r.AddAttrs(slog.Any(badKey, key))
			if i+1 < len(args) {
				r.AddAttrs(slog.Any(badKey, args[i+1]))
			}
			i += 2
		}
	}
	_ = l.writer(level).Handle(context.Background(), r)
}

func (l *logger) value(key string, v any) slog.Value {
	if isScalar(v) {
		if len(l.conv.rules) > 0 && l.conv.leafMasked(lowerASCII(key)) {
			return slog.StringValue(maskPlaceholder)
		}
		return slog.AnyValue(v)
	}
	if err, ok := v.(error); ok {
		if len(l.conv.rules) > 0 && l.conv.leafMasked(lowerASCII(key)) {
			return slog.StringValue(maskPlaceholder)
		}
		if rv := reflect.ValueOf(err); rv.Kind() == reflect.Pointer && rv.IsNil() {
			return slog.AnyValue(nil)
		}
		return slog.StringValue(err.Error())
	}
	out, _ := l.conv.arg(key, v)
	return slog.AnyValue(out)
}
