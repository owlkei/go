package log

import (
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
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
