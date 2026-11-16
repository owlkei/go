package core

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

const freshProcessEnv = "GINJECT_FRESH_PROCESS"

func freshProcess(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	if os.Getenv(freshProcessEnv) == t.Name() {
		body(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), freshProcessEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed in a fresh process:\n%s", t.Name(), out)
	}
	if !bytes.Contains(out, []byte("--- PASS: "+t.Name())) {
		t.Fatalf("%s did not run in the fresh process:\n%s", t.Name(), out)
	}
}

func createPanicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			text = fmt.Sprint(r)
		}
	}()
	fn()
	return "", false
}

func createPanicValue(fn func()) (value any, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			value = r
		}
	}()
	fn()
	return nil, false
}

type createRecorder struct {
	mu    sync.Mutex
	items []string
}

func (r *createRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, s)
}

func (r *createRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

func (r *createRecorder) count(s string) int {
	n := 0
	for _, item := range r.list() {
		if item == s {
			n++
		}
	}
	return n
}

func (r *createRecorder) hook(s string) func() {
	return func() { r.add(s) }
}

type createLogCall struct {
	level string
	msg   string
	args  []any
}

type createLogger struct {
	mu    sync.Mutex
	calls []createLogCall
}

func (l *createLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, createLogCall{level: level, msg: msg, args: append([]any(nil), args...)})
}

func (l *createLogger) Debug(msg string, args ...any) { l.record("debug", msg, args) }
func (l *createLogger) Info(msg string, args ...any)  { l.record("info", msg, args) }
func (l *createLogger) Warn(msg string, args ...any)  { l.record("warn", msg, args) }
func (l *createLogger) Error(msg string, args ...any) { l.record("error", msg, args) }
func (l *createLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args) }

func (l *createLogger) named(msg string) []createLogCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []createLogCall
	for _, c := range l.calls {
		if c.msg == msg {
			out = append(out, c)
		}
	}
	return out
}

func createRequest(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func createGet(app *App, target string) *httptest.ResponseRecorder {
	return createRequest(app, http.MethodGet, target)
}

func createDump(v reflect.Value) string {
	var b strings.Builder
	dumpValue(&b, v, map[uintptr]bool{})
	return b.String()
}

func dumpValue(b *strings.Builder, v reflect.Value, seen map[uintptr]bool) {
	if !v.IsValid() {
		b.WriteString("invalid")
		return
	}
	b.WriteString(v.Type().String())
	b.WriteByte('{')
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
		} else if seen[v.Pointer()] {
			b.WriteString("cycle")
		} else {
			seen[v.Pointer()] = true
			dumpValue(b, v.Elem(), seen)
			delete(seen, v.Pointer())
		}
	case reflect.Interface:
		if v.IsNil() {
			b.WriteString("nil")
		} else {
			dumpValue(b, v.Elem(), seen)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			b.WriteString(v.Type().Field(i).Name)
			b.WriteByte('=')
			dumpValue(b, v.Field(i), seen)
			b.WriteByte(';')
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("nil")
			break
		}
		for i := 0; i < v.Len(); i++ {
			dumpValue(b, v.Index(i), seen)
			b.WriteByte(',')
		}
	case reflect.Map:
		if v.IsNil() {
			b.WriteString("nil")
			break
		}
		entries := make([]string, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			var e strings.Builder
			dumpValue(&e, iter.Key(), seen)
			e.WriteString("=>")
			dumpValue(&e, iter.Value(), seen)
			entries = append(entries, e.String())
		}
		sort.Strings(entries)
		b.WriteString(strings.Join(entries, ","))
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		fmt.Fprintf(b, "nil=%v", v.IsNil())
	case reflect.String:
		b.WriteString(v.String())
	case reflect.Bool:
		fmt.Fprintf(b, "%v", v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(b, "%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(b, "%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(b, "%v", v.Float())
	}
	b.WriteByte('}')
}

func mustCreate(t *testing.T, app *App, m *Module) {
	t.Helper()
	if text, panicked := createPanicText(func() { app.Create(m) }); panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "Create succeeds"))
	}
}

func sprint(v any) string { return fmt.Sprint(v) }
