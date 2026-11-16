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
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/tracecontext"
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

const (
	badKey         = "!BADKEY"
	maxPooledBytes = 4 << 10
	maxFieldBytes  = 128
	droppedMessage = "LogDropped"
)

var exit = os.Exit

var droppedKeys = [4]string{"debug", "info", "warn", "error"}

var accessContext = context.WithValue(context.Background(), accessKey{}, true)

type accessKey struct{}

type logger struct {
	level     slog.Level
	addSource bool
	format    logFormat
	sink      *logpipe.Sink
	conv      converter
	encoders  *sync.Pool
	report    func(dropped [4]uint64) []byte
	base      *logger
	scope     *scopeFields
	slot      slotEntry
}

type scopeFields struct {
	src    *tracecontext.Scope
	attrs  []slog.Attr
	suffix []byte
}

type encoder struct {
	buf      []byte
	escapeC1 bool
	out      slog.Handler
	fatal    slog.Handler
	pretty   *prettyHandler
}

func NewLog(opts *LogOptions) common.Logger {
	return newLogSink(logpipe.Stdout(), isTerminal(os.Stdout), opts)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newLog(out io.Writer, isTerminal bool, opts *LogOptions) common.Logger {
	return newLogSink(logpipe.New(out), isTerminal, opts)
}

func newLogSink(s *logpipe.Sink, isTerminal bool, opts *LogOptions) common.Logger {
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
		format:    format,
		sink:      s,
		conv:      converter{rules: mergeRules(nil, o.MaskFields), mode: modeText},
		encoders:  newEncoderPool(format, o.AddSource),
	}
	if format == PrettyFormat {
		l.conv.mode = modePretty
	}
	l.report = l.dropReport
	return l
}

func newEncoderPool(format logFormat, addSource bool) *sync.Pool {
	var pretty *prettyHandler
	if format == PrettyFormat {
		pretty = newPrettyHandler()
	}
	return &sync.Pool{New: func() any {
		e := &encoder{buf: make([]byte, 0, 512), pretty: pretty}
		plainOpts := &slog.HandlerOptions{AddSource: addSource}
		fatalOpts := &slog.HandlerOptions{AddSource: addSource, ReplaceAttr: fatalLabel}
		switch format {
		case TextFormat:
			e.out, e.fatal = slog.NewTextHandler(e, plainOpts), slog.NewTextHandler(e, fatalOpts)
		case JSONFormat:
			e.escapeC1 = true
			e.out, e.fatal = slog.NewJSONHandler(e, plainOpts), slog.NewJSONHandler(e, fatalOpts)
		}
		return e
	}}
}

func fatalLabel(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey && len(groups) == 0 {
		a.Value = slog.StringValue(labelFatal)
	}
	return a
}

func (e *encoder) Write(p []byte) (int, error) {
	if e.escapeC1 && bytes.IndexByte(p, 0xc2) >= 0 {
		e.buf = appendEscapedC1(e.buf, p)
	} else {
		e.buf = append(e.buf, p...)
	}
	return len(p), nil
}

func appendEscapedC1(dst, p []byte) []byte {
	for i := 0; i < len(p); i++ {
		if p[i] == 0xc2 && i+1 < len(p) && p[i+1] >= 0x80 && p[i+1] <= 0x9f {
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[p[i+1]>>4], hexDigits[p[i+1]&0x0f])
			i++
			continue
		}
		dst = append(dst, p[i])
	}
	return dst
}

func (l *logger) Debug(msg string, args ...any) { l.log(DebugLevel, msg, args) }
func (l *logger) Info(msg string, args ...any)  { l.log(InfoLevel, msg, args) }
func (l *logger) Warn(msg string, args ...any)  { l.log(WarnLevel, msg, args) }
func (l *logger) Error(msg string, args ...any) { l.log(ErrorLevel, msg, args) }

func (l *logger) Fatal(msg string, args ...any) {
	l.log(FatalLevel, msg, args)
	l.sink.Flush(logpipe.FlushTimeout)
	exit(1)
}

func (l *logger) From(ctx context.Context) common.Logger {
	s := tracecontext.ScopeOf(ctx)
	if s == nil {
		return l
	}
	base := l
	if l.base != nil {
		base = l.base
	}
	if entry, ok := s.LogSlot.Load().(*slotEntry); ok && entry.owner == any(base) {
		return entry.logger
	}
	if !hasFields(s) {
		return l
	}
	scoped := base.scoped(s)
	s.LogSlot.Store(&scoped.slot)
	return scoped
}

func hasFields(s *tracecontext.Scope) bool {
	return s.TraceID != "" || s.Request != "" || s.Connection != "" || s.Message != "" || s.Upstream != ""
}

type scopedLogger struct {
	logger
	fields scopeFields
}

func (l *logger) scoped(s *tracecontext.Scope) *logger {
	c := &scopedLogger{logger: *l}
	c.base = l
	c.fields = l.scopeFields(s)
	c.scope = &c.fields
	c.slot = slotEntry{owner: l, logger: &c.logger}
	return &c.logger
}

func (l *logger) scopeFields(s *tracecontext.Scope) scopeFields {
	pairs := [...][2]string{
		{tracecontext.KeyTraceID, s.TraceID},
		{tracecontext.KeyRequest, s.Request},
		{tracecontext.KeyConnection, s.Connection},
		{tracecontext.KeyMessage, s.Message},
		{tracecontext.KeyUpstream, s.Upstream},
	}
	fast := l.format != PrettyFormat
	for _, p := range pairs {
		if p[1] != "" && (!isPlainField(p[1]) || l.masked(p[0])) {
			fast = false
		}
	}
	f := scopeFields{src: s}
	if fast {
		f.suffix = make([]byte, 0, 160)
		for _, p := range pairs {
			if p[1] == "" {
				continue
			}
			if l.format == JSONFormat {
				f.suffix = append(append(append(append(append(f.suffix, ",\""...), p[0]...), "\":\""...), p[1]...), '"')
			} else {
				f.suffix = append(append(append(append(f.suffix, ' '), p[0]...), '='), p[1]...)
			}
		}
		return f
	}
	f.attrs = make([]slog.Attr, 0, len(pairs))
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		v := slog.StringValue(p[1])
		if l.masked(p[0]) {
			v = slog.StringValue(maskPlaceholder)
		}
		f.attrs = append(f.attrs, slog.Attr{Key: p[0], Value: v})
	}
	return f
}

func isPlainField(v string) bool {
	if len(v) > maxFieldBytes {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != ':' && c != '-' {
			return false
		}
	}
	return true
}

func (l *logger) masked(key string) bool {
	return len(l.conv.rules) > 0 && l.conv.leafMasked(lowerASCII(key))
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
	e := l.encoders.Get().(*encoder)
	if onlyScalars(args) {
		l.encode(e, level, msg, pc, args)
	} else {
		l.encodeGuarded(e, level, msg, pc, args)
	}
	l.sink.Write(pipeLevel(level), e.buf, l.report)
	l.release(e)
}

func (l *logger) release(e *encoder) {
	if cap(e.buf) > maxPooledBytes {
		return
	}
	e.buf = e.buf[:0]
	l.encoders.Put(e)
}

func pipeLevel(level slog.Level) logpipe.Level {
	switch {
	case level < InfoLevel:
		return logpipe.Debug
	case level < WarnLevel:
		return logpipe.Info
	case level < ErrorLevel:
		return logpipe.Warn
	case level < FatalLevel:
		return logpipe.Error
	default:
		return logpipe.Fatal
	}
}

func onlyScalars(args []any) bool {
	for i := 1; i < len(args); i += 2 {
		if _, ok := args[i-1].(string); !ok || !isScalar(args[i]) {
			return false
		}
	}
	return len(args)%2 == 0
}

func (l *logger) encodeGuarded(e *encoder, level slog.Level, msg string, pc uintptr, args []any) {
	defer func() {
		if rec := recover(); rec != nil {
			r := slog.NewRecord(time.Now(), level, msg, pc)
			r.AddAttrs(slog.String("log_error", fmt.Sprint(rec)))
			l.handle(context.Background(), e, r)
		}
	}()
	l.encode(e, level, msg, pc, args)
}

func (l *logger) encode(e *encoder, level slog.Level, msg string, pc uintptr, args []any) {
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
	l.handle(context.Background(), e, r)
}

func (l *logger) handle(ctx context.Context, e *encoder, r slog.Record) {
	if l.scope != nil && l.scope.attrs != nil {
		r.AddAttrs(l.scope.attrs...)
	}
	e.buf = e.buf[:0]
	switch {
	case e.pretty != nil:
		e.buf = e.pretty.appendRecord(e.buf, r, ctx == accessContext)
	case r.Level >= FatalLevel:
		_ = e.fatal.Handle(ctx, r)
	default:
		_ = e.out.Handle(ctx, r)
	}
	if l.scope != nil && l.scope.suffix != nil {
		e.buf = l.splice(e.buf, l.scope.suffix)
	}
}

func (l *logger) splice(buf, suffix []byte) []byte {
	tail := "\n"
	if l.format == JSONFormat {
		tail = "}\n"
	}
	if len(buf) < len(tail) || string(buf[len(buf)-len(tail):]) != tail {
		return buf
	}
	buf = append(buf[:len(buf)-len(tail)], suffix...)
	return append(buf, tail...)
}

func (l *logger) value(key string, v any) slog.Value {
	if isScalar(v) {
		if l.masked(key) {
			return slog.StringValue(maskPlaceholder)
		}
		return slog.AnyValue(v)
	}
	if err, ok := v.(error); ok {
		if l.masked(key) {
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

func (l *logger) dropReport(dropped [4]uint64) []byte {
	args := make([]any, 0, 2*len(dropped))
	for i, n := range dropped {
		if n > 0 {
			args = append(args, droppedKeys[i], n)
		}
	}
	e := l.encoders.New().(*encoder)
	l.encode(e, WarnLevel, droppedMessage, 0, args)
	return e.buf
}

func (l *logger) LogAccess(line logpipe.AccessLine) {
	if InfoLevel < l.level {
		return
	}
	var pc uintptr
	if l.addSource {
		var pcs [1]uintptr
		runtime.Callers(2, pcs[:])
		pc = pcs[0]
	}
	r := slog.NewRecord(time.Now(), InfoLevel, line.Msg, pc)
	if len(l.conv.rules) > 0 {
		for i := range line.Attrs {
			if l.masked(line.Attrs[i].Key) {
				line.Attrs[i].Value = slog.StringValue(maskPlaceholder)
			}
		}
	}
	r.AddAttrs(line.Attrs...)
	e := l.encoders.Get().(*encoder)
	l.handle(accessContext, e, r)
	l.sink.Write(logpipe.Info, e.buf, l.report)
	l.release(e)
}
