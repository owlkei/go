package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dangduoc08/ginject/internal/color"
)

const (
	branchWidth    = 4
	prettyMaxDepth = 2 * maxDepth
	rowPrefix      = "  "
	hexDigits      = "0123456789abcdef"
)

const (
	ansiReset   = "\x1b[0m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiOrange  = "\x1b[38;5;208m"
	ansiTimePfx = "\x1b[48;5;236m "
	ansiTimeSfx = " \x1b[0m"
	ansiMsgPfx  = ansiCyan + " ["
	ansiMsgSfx  = "]" + ansiReset + " "
	ansiNull    = ansiMagenta + "null" + ansiReset
)

type levelLabel struct {
	label   string
	trailer string
}

type prettyHandler struct {
	mu     sync.Mutex
	w      io.Writer
	labels [5]levelLabel
}

type node struct {
	kv
	children []node
}

type prettyBuffer struct {
	buf   []byte
	width int
}

func newPrettyHandler(out io.Writer) *prettyHandler {
	build := func(label string, bg func(string, ...any) string) levelLabel {
		space := " "
		if label == labelInfo || label == labelWarn {
			space = "  "
		}
		return levelLabel{color.FmtBold("%s", bg(" "+label+space)), bg(" ")}
	}
	return &prettyHandler{
		w: out,
		labels: [5]levelLabel{
			build(labelDebug, color.FmtBGBlue),
			build(labelInfo, color.FmtBGGreen),
			build(labelWarn, color.FmtBGYellow),
			build(labelError, color.FmtBGRed),
			build(labelFatal, color.FmtBGRed),
		},
	}
}

func (h *prettyHandler) label(l slog.Level) levelLabel {
	switch {
	case l < InfoLevel:
		return h.labels[0]
	case l < WarnLevel:
		return h.labels[1]
	case l < ErrorLevel:
		return h.labels[2]
	case l < FatalLevel:
		return h.labels[3]
	default:
		return h.labels[4]
	}
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	p := prettyBuffer{buf: make([]byte, 0, 512)}
	p.buf = append(p.buf, '\n')

	label := h.label(r.Level)
	p.buf = append(p.buf, label.label...)
	p.buf = append(p.buf, ansiTimePfx...)
	p.buf = r.Time.AppendFormat(p.buf, time.DateTime)
	p.buf = append(p.buf, ansiTimeSfx...)
	p.buf = append(p.buf, label.trailer...)

	if r.Message != "" {
		p.buf = append(p.buf, ansiMsgPfx...)
		p.buf = appendEscaped(p.buf, r.Message, "")
		p.buf = append(p.buf, ansiMsgSfx...)
	}

	items := make([]kv, 0, r.NumAttrs()+1)
	r.Attrs(func(a slog.Attr) bool {
		items = append(items, kv{escapeKey(a.Key), attrReflectValue(a.Value)})
		return true
	})
	if r.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		items = append(items, kv{"source", reflect.ValueOf(frame.File + ":" + strconv.Itoa(frame.Line))})
	}

	nodes := make([]node, len(items))
	for i, item := range items {
		nodes[i] = p.buildNode(rowPrefix, item, 0)
	}
	for i, n := range nodes {
		p.buf = append(p.buf, '\n')
		p.writeNode(rowPrefix, n, i == len(nodes)-1)
	}
	p.buf = append(p.buf, '\n')

	h.mu.Lock()
	_, err := h.w.Write(p.buf)
	h.mu.Unlock()
	return err
}

func attrReflectValue(v slog.Value) reflect.Value {
	switch v.Kind() {
	case slog.KindGroup:
		return reflect.ValueOf(v.Group())
	default:
		if a := v.Any(); a != nil {
			return reflect.ValueOf(a)
		}
		return reflect.Value{}
	}
}

func escapeKey(key string) string {
	for i := 0; i < len(key); i++ {
		if c := key[i]; c < 0x20 || c >= 0x7f {
			return string(appendEscaped(nil, key, ""))
		}
	}
	return key
}

func appendEscaped(buf []byte, s string, cont string) []byte {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '\n' && cont != "":
				buf = append(buf, '\n')
				buf = append(buf, cont...)
			case c == '\n':
				buf = append(buf, '\\', 'n')
			case c == '\r':
				buf = append(buf, '\\', 'r')
			case c == '\t':
				buf = append(buf, '\\', 't')
			case c < 0x20 || c == 0x7f:
				buf = appendHex(buf, c)
			default:
				buf = append(buf, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			buf = appendHex(buf, c)
		case r >= 0x80 && r <= 0x9f:
			buf = appendHex(buf, byte(r))
		default:
			buf = append(buf, s[i:i+size]...)
		}
		i += size
	}
	return buf
}

func appendHex(buf []byte, b byte) []byte {
	return append(buf, '\\', 'x', hexDigits[b>>4], hexDigits[b&0x0f])
}

func isBranchable(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		return !implementsText(rv.Type())
	default:
		return false
	}
}

func indent(prefix string, parentWasLast bool) string {
	if parentWasLast {
		return prefix + "    "
	}
	return prefix + "│   "
}

func mapKeyString(k reflect.Value) string {
	if k.Kind() == reflect.String {
		return k.String()
	}
	return fmt.Sprint(k.Interface())
}

func collectChildren(rv reflect.Value, depth int) []kv {
	if rv.Type() == attrsType {
		attrs := rv.Interface().([]slog.Attr)
		out := make([]kv, len(attrs))
		for i, a := range attrs {
			out[i] = kv{escapeKey(a.Key), attrReflectValue(a.Value)}
		}
		return out
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		var out []kv
		for i := range rv.Len() {
			elem := dereference(rv.Index(i))
			if elem.IsValid() && isBranchable(elem) && depth < prettyMaxDepth {
				out = append(out, collectChildren(elem, depth+1)...)
			} else {
				out = append(out, kv{"", rv.Index(i)})
			}
		}
		return out
	case reflect.Map:
		out := make([]kv, 0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out = append(out, kv{escapeKey(mapKeyString(iter.Key())), iter.Value()})
		}
		sort.Slice(out, func(a, b int) bool {
			return out[a].key < out[b].key
		})
		return out
	case reflect.Struct:
		fields := taggedFields(rv)
		for i := range fields {
			fields[i].key = escapeKey(fields[i].key)
		}
		return fields
	}
	return nil
}

func (p *prettyBuffer) buildNode(prefix string, item kv, depth int) node {
	if w := utf8.RuneCountInString(prefix) + branchWidth + utf8.RuneCountInString(item.key); w > p.width {
		p.width = w
	}
	if depth > prettyMaxDepth {
		return node{kv: kv{item.key, reflect.ValueOf(depthPlaceholder)}}
	}
	n := node{kv: item}
	rv := dereference(item.val)
	if rv.IsValid() && isBranchable(rv) {
		childPrefix := indent(prefix, false)
		kids := collectChildren(rv, depth)
		n.children = make([]node, len(kids))
		for i, c := range kids {
			n.children[i] = p.buildNode(childPrefix, c, depth+1)
		}
	}
	return n
}

func (p *prettyBuffer) writeKey(prefix, key string) {
	p.buf = append(p.buf, ansiRed...)
	p.buf = append(p.buf, key...)
	p.buf = append(p.buf, ansiReset...)
	pad := p.width - utf8.RuneCountInString(prefix) - branchWidth - utf8.RuneCountInString(key)
	for range pad + 1 {
		p.buf = append(p.buf, ' ')
	}
}

func (p *prettyBuffer) writeNode(prefix string, n node, last bool) {
	p.buf = append(p.buf, prefix...)
	if last {
		p.buf = append(p.buf, "└── "...)
	} else {
		p.buf = append(p.buf, "├── "...)
	}
	if n.key != "" {
		p.writeKey(prefix, n.key)
	}

	rv := dereference(n.val)
	if !rv.IsValid() {
		p.buf = append(p.buf, ansiNull...)
		return
	}
	cont := indent(prefix, last)
	if n.children != nil || isBranchable(rv) {
		for i, c := range n.children {
			p.buf = append(p.buf, '\n')
			p.writeNode(cont, c, i == len(n.children)-1)
		}
		return
	}
	p.writeScalar(rv, cont)
}

func (p *prettyBuffer) writeScalar(rv reflect.Value, cont string) {
	t := rv.Type()
	if t == coloredType {
		c := rv.Interface().(color.Colored)
		p.buf = append(p.buf, c.Color...)
		p.buf = appendEscaped(p.buf, c.Text, cont)
		p.buf = append(p.buf, ansiReset...)
		return
	}
	if implementsText(t) {
		p.writeString(textOf(methodReceiver(rv)), cont)
		return
	}
	switch rv.Kind() {
	case reflect.String:
		p.writeString(rv.String(), cont)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p.buf = append(p.buf, ansiOrange...)
		p.buf = strconv.AppendInt(p.buf, rv.Int(), 10)
		p.buf = append(p.buf, ansiReset...)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p.buf = append(p.buf, ansiOrange...)
		p.buf = strconv.AppendUint(p.buf, rv.Uint(), 10)
		p.buf = append(p.buf, ansiReset...)
	case reflect.Float32, reflect.Float64:
		p.buf = append(p.buf, ansiOrange...)
		p.buf = strconv.AppendFloat(p.buf, rv.Float(), 'g', -1, 64)
		p.buf = append(p.buf, ansiReset...)
	case reflect.Bool:
		p.buf = append(p.buf, ansiBlue...)
		p.buf = strconv.AppendBool(p.buf, rv.Bool())
		p.buf = append(p.buf, ansiReset...)
	default:
		p.writeString(fmt.Sprint(rv.Interface()), cont)
	}
}

func (p *prettyBuffer) writeString(s, cont string) {
	p.buf = append(p.buf, ansiGreen...)
	p.buf = appendEscaped(p.buf, s, cont)
	p.buf = append(p.buf, ansiReset...)
}

func textOf(x any) string {
	switch v := x.(type) {
	case slog.LogValuer:
		return resolveLogValuer(v).String()
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	}
	return fmt.Sprint(x)
}
