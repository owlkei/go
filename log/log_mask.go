package log

import (
	"context"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"time"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/logpipe"
	"go.owlky.com/owlky/internal/pattern"
	"go.owlky.com/owlky/internal/tracecontext"
)

type convertMode uint8

const (
	modeKeep convertMode = iota
	modeText
	modePretty
)

type maskRule struct {
	exact   bool
	name    string
	suffix  string
	pattern pattern.Pattern
}

type converter struct {
	rules []maskRule
	mode  convertMode
}

type maskingLogger struct {
	next   LogAdapter
	conv   converter
	base   *maskingLogger
	fields []any
	slot   slotEntry
}

func WrapLogger(next LogAdapter, maskFields []string) Logger {
	if next == nil {
		panic(color.FmtRed("invalid logger: WrapLogger needs a logger"))
	}
	switch l := next.(type) {
	case *logger:
		if len(maskFields) == 0 {
			return l
		}
		root := l
		if l.base != nil {
			root = l.base
		}
		merged := new(logger)
		*merged = *root
		merged.conv.rules = mergeRules(root.conv.rules, maskFields)
		merged.report = merged.dropReport
		if l.scope != nil {
			return merged.scoped(l.scope.src)
		}
		return merged
	case *maskingLogger:
		if len(maskFields) == 0 {
			return l
		}
		merged := &maskingLogger{next: l.next, conv: converter{rules: mergeRules(l.conv.rules, maskFields)}}
		if l.fields != nil {
			merged.fields = merged.conv.args(l.fields)
		}
		return merged
	}
	return &maskingLogger{next: next, conv: converter{rules: mergeRules(nil, maskFields)}}
}

func mergeRules(existing []maskRule, fields []string) []maskRule {
	if len(existing)+len(fields) == 0 {
		return nil
	}
	out := make([]maskRule, 0, len(existing)+len(fields))
	out = append(out, existing...)
	for _, f := range fields {
		name := lowerASCII(f)
		p := pattern.NewPattern(name)
		out = append(out, maskRule{exact: p.IsExact(), name: name, suffix: "." + name, pattern: p})
	}
	return out
}

func (m *maskingLogger) Debug(msg string, args ...any) { m.next.Debug(msg, m.args(args)...) }
func (m *maskingLogger) Info(msg string, args ...any)  { m.next.Info(msg, m.args(args)...) }
func (m *maskingLogger) Warn(msg string, args ...any)  { m.next.Warn(msg, m.args(args)...) }
func (m *maskingLogger) Error(msg string, args ...any) { m.next.Error(msg, m.args(args)...) }

func (m *maskingLogger) Fatal(msg string, args ...any) {
	converted := m.args(args)
	logpipe.Stdout().Flush(logpipe.FlushTimeout)
	m.next.Fatal(msg, converted...)
}

func (m *maskingLogger) args(args []any) []any {
	converted := m.conv.args(args)
	if m.fields == nil {
		return converted
	}
	out := make([]any, 0, len(m.fields)+len(converted))
	out = append(out, m.fields...)
	return append(out, converted...)
}

func (m *maskingLogger) From(ctx context.Context) Logger {
	s := tracecontext.ScopeOf(ctx)
	if s == nil {
		return m
	}
	base := m
	if m.base != nil {
		base = m.base
	}
	if entry, ok := s.LogSlot.Load().(*slotEntry); ok && entry.owner == any(base) {
		return entry.logger
	}
	fields := s.AppendArgs(nil)
	if len(fields) == 0 {
		return m
	}
	scoped := &maskingLogger{next: base.next, conv: base.conv, base: base, fields: base.conv.args(fields)}
	scoped.slot = slotEntry{owner: base, logger: scoped}
	s.LogSlot.Store(&scoped.slot)
	return scoped
}

func (c *converter) args(args []any) []any {
	var out []any
	for i := 0; i < len(args); {
		switch key := args[i].(type) {
		case string:
			if i+1 >= len(args) {
				return orArgs(out, args)
			}
			if v, changed := c.arg(key, args[i+1]); changed {
				if out == nil {
					out = make([]any, len(args))
					copy(out, args)
				}
				out[i+1] = v
			}
			i += 2
		case slog.Attr:
			if a, changed := c.attr(key); changed {
				if out == nil {
					out = make([]any, len(args))
					copy(out, args)
				}
				out[i] = a
			}
			i++
		default:
			i++
		}
	}
	return orArgs(out, args)
}

func orArgs(out, args []any) []any {
	if out != nil {
		return out
	}
	return args
}

func (c *converter) arg(key string, v any) (any, bool) {
	if isScalar(v) {
		if c.masked(c.root(key)) {
			return maskPlaceholder, true
		}
		return v, false
	}
	nodes := 0
	return c.value(c.root(key), v, 0, &nodes)
}

func (c *converter) attr(a slog.Attr) (slog.Attr, bool) {
	nodes := 0
	v, changed := c.slogValue(c.root(a.Key), a.Value, 0, &nodes)
	if !changed {
		return a, false
	}
	return slog.Attr{Key: a.Key, Value: v}, true
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64,
		time.Duration, time.Time:
		return true
	}
	return false
}

func (c *converter) root(key string) string {
	if len(c.rules) == 0 {
		return ""
	}
	return lowerASCII(key)
}

func (c *converter) join(path, key string) string {
	if len(c.rules) == 0 {
		return ""
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

func (c *converter) value(path string, v any, depth int, nodes *int) (any, bool) {
	if depth > maxDepth {
		return depthPlaceholder, true
	}
	if *nodes >= maxNodes {
		return truncatedPlaceholder, true
	}
	*nodes++
	if isScalar(v) {
		if c.masked(path) {
			return maskPlaceholder, true
		}
		return v, false
	}
	out, changed := c.reflectValue(path, reflect.ValueOf(v), depth, nodes)
	if !changed {
		return v, false
	}
	return out, true
}

func (c *converter) child(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	if depth > maxDepth {
		return depthPlaceholder, true
	}
	if *nodes >= maxNodes {
		return truncatedPlaceholder, true
	}
	*nodes++
	return c.reflectValue(path, rv, depth, nodes)
}

func (c *converter) reflectValue(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	var info *typeInfo
	for hops := 0; ; hops++ {
		if !rv.IsValid() {
			return c.leaf(path)
		}
		info = typeInfoOf(rv.Type())
		if info.fixed || (len(c.rules) == 0 && (info.plain || (info.plainKeep && c.mode == modeKeep))) {
			return c.leaf(path)
		}
		if info.text {
			return c.text(path, rv, depth, nodes)
		}
		if hops == 0 && c.masked(path) {
			return maskPlaceholder, true
		}
		if k := rv.Kind(); k != reflect.Pointer && k != reflect.Interface {
			break
		}
		if rv.IsNil() {
			return c.leaf(path)
		}
		if hops == maxDepth {
			return depthPlaceholder, true
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct:
		return c.structValue(path, rv, info, depth, nodes)
	case reflect.Map:
		return c.mapValue(path, rv, depth, nodes)
	case reflect.Slice, reflect.Array:
		return c.sliceValue(path, rv, depth, nodes)
	}
	return c.leaf(path)
}

func (c *converter) leaf(path string) (any, bool) {
	if c.masked(path) {
		return maskPlaceholder, true
	}
	return nil, false
}

func (c *converter) text(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	if c.masked(path) {
		return maskPlaceholder, true
	}
	k := rv.Kind()
	isNil := (k == reflect.Pointer || k == reflect.Interface) && rv.IsNil()
	if c.mode == modeKeep {
		if isNil || len(c.rules) == 0 {
			return nil, false
		}
		return c.keptGroup(path, rv, depth, nodes)
	}
	if isNil {
		return nil, true
	}
	if c.mode == modePretty && rv.Type() == coloredType {
		return nil, false
	}
	switch x := methodReceiver(rv).(type) {
	case slog.LogValuer:
		v := resolveLogValuer(x)
		switch v.Kind() {
		case slog.KindString:
			return v.String(), true
		case slog.KindGroup:
			if masked, changed := c.slogValue(path, v, depth, nodes); changed {
				return masked.Any(), true
			}
		}
		return v.Any(), true
	case error:
		return x.Error(), true
	case interface{ String() string }:
		return x.String(), true
	}
	return nil, false
}

func (c *converter) keptGroup(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	lv, ok := methodReceiver(rv).(slog.LogValuer)
	if !ok {
		return nil, false
	}
	v, ok := recoveredLogValue(lv)
	if !ok || v.Kind() != slog.KindGroup {
		return nil, false
	}
	masked, changed := c.slogValue(path, v, depth, nodes)
	if !changed {
		return nil, false
	}
	if depth > 0 {
		return masked.Any(), true
	}
	return masked, true
}

func recoveredLogValue(lv slog.LogValuer) (v slog.Value, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return resolveLogValuer(lv), true
}

func (c *converter) structValue(path string, rv reflect.Value, info *typeInfo, depth int, nodes *int) (any, bool) {
	rv = addressable(rv, info)
	out := make(map[string]any, len(info.fields))
	for _, f := range info.fields {
		fv := fieldValue(rv, f)
		v, changed := c.child(c.join(path, f.path), fv, depth+1, nodes)
		if !changed {
			v = fv.Interface()
		}
		out[f.key] = v
	}
	return out, true
}

type mapEntry struct {
	key     string
	val     any
	orig    reflect.Value
	changed bool
}

func (c *converter) mapValue(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	if rv.Len() == 0 {
		return c.leaf(path)
	}
	entries := make([]mapEntry, 0, rv.Len())
	var changed bool
	iter := rv.MapRange()
	for iter.Next() {
		key := mapKey(iter.Key())
		orig := iter.Value()
		v, ch := c.child(c.join(path, lowerASCII(key)), orig, depth+1, nodes)
		changed = changed || ch
		entries = append(entries, mapEntry{key: key, val: v, orig: orig, changed: ch})
	}
	if !changed {
		return nil, false
	}
	out := make(map[string]any, len(entries))
	for _, e := range entries {
		if e.changed {
			out[e.key] = e.val
		} else {
			out[e.key] = e.orig.Interface()
		}
	}
	return out, true
}

func mapKey(k reflect.Value) string {
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	default:
		return strconv.FormatUint(k.Uint(), 10)
	}
}

func (c *converter) sliceValue(path string, rv reflect.Value, depth int, nodes *int) (any, bool) {
	n := rv.Len()
	if n == 0 {
		return c.leaf(path)
	}
	var out []any
	for i := range n {
		ev := rv.Index(i)
		v, changed := c.child(path, ev, depth+1, nodes)
		if changed && out == nil {
			out = make([]any, n)
			for j := range i {
				out[j] = rv.Index(j).Interface()
			}
		}
		if out == nil {
			continue
		}
		if !changed {
			v = ev.Interface()
		}
		out[i] = v
	}
	if out == nil {
		return nil, false
	}
	return out, true
}

func (c *converter) slogValue(path string, v slog.Value, depth int, nodes *int) (slog.Value, bool) {
	switch v.Kind() {
	case slog.KindGroup:
		if c.masked(path) {
			return slog.StringValue(maskPlaceholder), true
		}
		attrs := v.Group()
		var out []slog.Attr
		for i, a := range attrs {
			childPath := path
			if a.Key != "" || a.Value.Kind() != slog.KindGroup {
				childPath = c.join(path, lowerASCII(a.Key))
			}
			nv, changed := c.slogValue(childPath, a.Value, depth+1, nodes)
			if changed && out == nil {
				out = make([]slog.Attr, len(attrs))
				copy(out, attrs)
			}
			if changed {
				out[i] = slog.Attr{Key: a.Key, Value: nv}
			}
		}
		if out == nil {
			return v, false
		}
		return slog.GroupValue(out...), true
	case slog.KindAny, slog.KindLogValuer:
		out, changed := c.value(path, v.Any(), depth, nodes)
		if !changed {
			return v, false
		}
		return slog.AnyValue(out), true
	}
	if c.masked(path) {
		return slog.StringValue(maskPlaceholder), true
	}
	return v, false
}

func (c *converter) masked(path string) bool {
	for i := range c.rules {
		r := &c.rules[i]
		if r.exact {
			if path == r.name || strings.HasSuffix(path, r.suffix) {
				return true
			}
			continue
		}
		for p := path; ; {
			if r.pattern.Match(p) {
				return true
			}
			dot := strings.IndexByte(p, '.')
			if dot < 0 {
				break
			}
			p = p[dot+1:]
		}
	}
	return false
}
