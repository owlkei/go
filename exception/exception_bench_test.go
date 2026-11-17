package exception

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func BenchmarkNewException_NoOpts(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewException("body", http.StatusBadRequest)
	}
}

func BenchmarkNewException_StringOpt(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewException("body", http.StatusBadRequest, "custom message")
	}
}

func BenchmarkNewException_ErrorOpt(b *testing.B) {
	cause := errors.New("underlying error")
	b.ReportAllocs()
	for b.Loop() {
		_ = NewException("body", http.StatusBadRequest, cause)
	}
}

func BenchmarkNewException_ExceptionOptions_Cause(b *testing.B) {
	cause := errors.New("db error")
	b.ReportAllocs()
	for b.Loop() {
		_ = NewException("body", http.StatusBadRequest, ExceptionOptions{Cause: cause})
	}
}

func BenchmarkException_StatusText(b *testing.B) {
	e := NewException("body", http.StatusBadRequest)
	b.ReportAllocs()
	for b.Loop() {
		_ = e.StatusText()
	}
}

func BenchmarkException_Error(b *testing.B) {
	e := NewException("body", http.StatusBadRequest)
	b.ReportAllocs()
	for b.Loop() {
		_ = e.Error()
	}
}

func BenchmarkException_ErrorsIs(b *testing.B) {
	cause := errors.New("db error")
	e := NewException("body", http.StatusInternalServerError, ExceptionOptions{Cause: cause})
	b.ReportAllocs()
	for b.Loop() {
		_ = errors.Is(e, cause)
	}
}

func BenchmarkNewException_DescriptionAndCause(b *testing.B) {
	cause := errors.New("db error")
	b.ReportAllocs()
	for b.Loop() {
		_ = NewException("body", http.StatusBadRequest, ExceptionOptions{Description: "custom", Cause: cause})
	}
}

func BenchmarkException_AppendJSON(b *testing.B) {
	e := NewException("Cannot GET /users", http.StatusNotFound)
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		buf = e.AppendJSON(buf[:0])
	}
}

func BenchmarkException_AppendJSON_Escaped(b *testing.B) {
	e := NewException("Cannot GET /<script>\"a&b\"</script>\n", http.StatusNotFound, "bad \\ input \u2028")
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		buf = e.AppendJSON(buf[:0])
	}
}

func BenchmarkException_MarshalMap(b *testing.B) {
	e := NewException("Cannot GET /users", http.StatusNotFound)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(map[string]any{"code": e.Code(), "error": e.Error(), "message": e.Message()})
	}
}

func BenchmarkAppendJSON_Plain(b *testing.B) {
	e := NewException("invalid input", http.StatusBadRequest)
	b.ReportAllocs()
	for b.Loop() {
		_ = e.AppendJSON(nil)
	}
}

func BenchmarkAppendJSON_Escaped(b *testing.B) {
	e := NewException("</script> \"quoted\" &   line\nbreak", http.StatusBadRequest, "bad \\ input")
	b.ReportAllocs()
	for b.Loop() {
		_ = e.AppendJSON(nil)
	}
}
