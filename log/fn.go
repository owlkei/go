package log

import (
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.owlky.com/owlky/internal/color"
)

const (
	maxDepth             = 16
	maxNodes             = 1000
	maxResolve           = 100
	maskPlaceholder      = "[REDACTED]"
	depthPlaceholder     = "[MAX_DEPTH]"
	truncatedPlaceholder = "[TRUNCATED]"
)

var (
	stringerType  = reflect.TypeFor[fmt.Stringer]()
	errorType     = reflect.TypeFor[error]()
	logValuerType = reflect.TypeFor[slog.LogValuer]()
	timeType      = reflect.TypeFor[time.Time]()
	durationType  = reflect.TypeFor[time.Duration]()
	coloredType   = reflect.TypeFor[color.Colored]()
	attrsType     = reflect.TypeFor[[]slog.Attr]()
)

type slotEntry struct {
	owner  any
	logger Logger
}

type kv struct {
	key string
	val reflect.Value
}

func dereference(rv reflect.Value) reflect.Value {
	for hops := 0; rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface; hops++ {
		if rv.IsNil() || hops > maxDepth {
			return reflect.Value{}
		}
		rv = rv.Elem()
	}
	return rv
}

func implementsText(t reflect.Type) bool {
	if t.Implements(errorType) || t.Implements(stringerType) || t.Implements(logValuerType) {
		return true
	}
	if t.Kind() == reflect.Pointer || t.Kind() == reflect.Interface {
		return false
	}
	p := reflect.PointerTo(t)
	return p.Implements(errorType) || p.Implements(stringerType) || p.Implements(logValuerType)
}

func methodReceiver(rv reflect.Value) any {
	t := rv.Type()
	if t.Implements(errorType) || t.Implements(stringerType) || t.Implements(logValuerType) {
		return rv.Interface()
	}
	if rv.CanAddr() {
		return rv.Addr().Interface()
	}
	p := reflect.New(t)
	p.Elem().Set(rv)
	return p.Interface()
}

func resolveLogValuer(lv slog.LogValuer) slog.Value {
	v := lv.LogValue()
	for i := 0; v.Kind() == slog.KindLogValuer && i < maxResolve; i++ {
		v = v.LogValuer().LogValue()
	}
	return v
}

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if c := b[j]; c >= 'A' && c <= 'Z' {
					b[j] = c + ('a' - 'A')
				}
			}
			return string(b)
		}
	}
	return s
}
