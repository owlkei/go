package log

import (
	"reflect"
	"slices"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestLogger_ExtendsLogAdapter(t *testing.T) {
	adapter := reflect.TypeFor[LogAdapter]()
	logger := reflect.TypeFor[Logger]()
	if adapter.NumMethod() != 5 {
		t.Error(test.DiffMessage(adapter.NumMethod(), 5, "I1: LogAdapter is the five methods Debug, Info, Warn, Error, Fatal"))
	}
	for _, name := range []string{"Debug", "Info", "Warn", "Error", "Fatal"} {
		if _, ok := adapter.MethodByName(name); !ok {
			t.Error(test.DiffMessage(false, true, "I1: LogAdapter has "+name))
		}
	}
	if _, ok := adapter.MethodByName("From"); ok {
		t.Error(test.DiffMessage(true, false, "I1: LogAdapter has no From"))
	}
	if logger.NumMethod() != 6 || !logger.Implements(adapter) {
		t.Error(test.DiffMessage(logger.NumMethod(), 6, "I1: Logger is LogAdapter plus From"))
	}
	from, ok := logger.MethodByName("From")
	if !ok || from.Type.NumIn() != 1 || from.Type.In(0).String() != "context.Context" || from.Type.NumOut() != 1 || from.Type.Out(0) != logger {
		t.Error(test.DiffMessage(from.Type, "func(context.Context) Logger", "I2: the signature of From"))
	}
}

func TestLogger_ExtendsLogAdapterWithFrom(t *testing.T) {
	adapter := new(fromAdapter)
	var _ LogAdapter = adapter
	logger := reflect.TypeFor[Logger]()
	base := reflect.TypeFor[LogAdapter]()
	if base.NumMethod() != 5 || logger.NumMethod() != 6 {
		t.Error(test.DiffMessage([]int{base.NumMethod(), logger.NumMethod()}, []int{5, 6}, "I1: LogAdapter has the five methods, Logger adds From"))
	}
	if !logger.Implements(base) {
		t.Error(test.DiffMessage(false, true, "I1: a Logger is a LogAdapter"))
	}
	if reflect.TypeOf(adapter).Implements(logger) || !reflect.TypeOf(adapter).Implements(base) {
		t.Error(test.DiffMessage(true, false, "I1: a type with the five methods and no From is a LogAdapter, not a Logger"))
	}
}

func TestLogger_WithoutFromUnchanged(t *testing.T) {
	l, buf := newTestLog(FormatJSON, LogOptions{})
	l.Info("hello", "a", 1, "b", "two")
	got := jsonTime.ReplaceAllString(buf.String(), `"time":""`)
	want := `{"time":"","level":"INFO","msg":"hello","a":1,"b":"two"}` + "\n"
	if got != want {
		t.Error(test.DiffMessage(got, want, "I3: a call that does not use From writes the same bytes"))
	}
	adapter := &fromAdapter{}
	WrapLogger(adapter, []string{"password"}).Info("m", "password", "x", "k", 1)
	if calls := adapter.all(); len(calls) != 1 || !slices.Equal(calls[0].args, []any{"password", maskPlaceholder, "k", 1}) {
		t.Error(test.DiffMessage(calls, "[password [REDACTED] k 1]", "I3: the same arguments reach an adapter"))
	}
}
