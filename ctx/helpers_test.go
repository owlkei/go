package ctx

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

func panicError(t *testing.T, fn func()) (err error) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic, got none")
			return
		}
		e, ok := r.(error)
		if !ok {
			t.Fatal(test.DiffMessage(fmt.Sprintf("%T", r), "error", "the panic value should be an error"))
			return
		}
		err = e
	}()
	fn()
	return nil
}

func panicException(t *testing.T, fn func()) (exception.Exception, error) {
	t.Helper()
	err := panicError(t, fn)
	var exc exception.Exception
	if !errors.As(err, &exc) {
		t.Fatal(test.DiffMessage(fmt.Sprintf("%T", err), "exception.Exception", "the panic should carry an exception.Exception"))
	}
	return exc, err
}

func noPanic(t *testing.T, desc string, fn func()) (ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Error(test.DiffMessage(r, "<no panic>", desc+": must not panic"))
			ok = false
		}
	}()
	fn()
	return true
}

func findFieldLevel(fls []FieldLevel, tag string) *FieldLevel {
	for i := range fls {
		if fls[i].Tag() == tag {
			return &fls[i]
		}
	}
	return nil
}

type discardResponseWriter struct {
	header http.Header
}

func newDiscardResponseWriter() *discardResponseWriter {
	return &discardResponseWriter{header: http.Header{}}
}

func (d *discardResponseWriter) Header() http.Header         { return d.header }
func (d *discardResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (d *discardResponseWriter) WriteHeader(int)             {}
