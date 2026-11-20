package httphook

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestOf_NilForPlainResponseWriter(t *testing.T) {
	if got := Of(httptest.NewRecorder()); got != nil {
		t.Error(test.DiffMessage(got, nil, "K5: Of returns nil for a ResponseWriter that is not the framework's"))
	}
}

func TestOf_NilForNilWriter(t *testing.T) {
	if got := Of(nil); got != nil {
		t.Error(test.DiffMessage(got, nil, "K5: Of(nil) returns nil"))
	}
}

func TestOf_ReturnsWriterThatImplementsIt(t *testing.T) {
	rec := httptest.NewRecorder()
	f := &fakeWriter{ResponseWriter: rec, inner: rec}
	got := Of(f)
	if got == nil {
		t.Fatal(test.DiffMessage(got, "a Writer", "K5: a ResponseWriter that implements Writer is returned"))
	}
	if got.Inner() != http.ResponseWriter(rec) {
		t.Error(test.DiffMessage(got.Inner(), rec, "K1: Inner is the writer the response goes to"))
	}
	hook := &countHook{}
	got.OnDone(hook)
	if len(f.hooks) != 1 {
		t.Error(test.DiffMessage(len(f.hooks), 1, "K2: OnDone registers the hook on the writer"))
	}
}
