package ctx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
)

type benchJSONStruct struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Tags  []string `json:"tags"`
}

func benchSlice50() []benchJSONStruct {
	items := make([]benchJSONStruct, 50)
	for i := range items {
		items[i] = benchJSONStruct{ID: i, Name: "John Doe", Email: "john.doe@example.com", Tags: []string{"a", "b"}}
	}
	return items
}

func benchResponseContext(target string) *HTTPContext {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", target, nil)
	c.ResponseWriter = newDiscardResponseWriter()
	return c
}

func BenchmarkHTTPContext_Init(b *testing.B) {
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	r := httptest.NewRequest("GET", "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		c.Init(w, r)
		c.Reset()
	}
}

func BenchmarkHTTPContext_Reset(b *testing.B) {
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	r := httptest.NewRequest("GET", "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		c.Init(w, r)
		c.ParamKeys = nil
		c.Reset()
	}
}

func BenchmarkHTTPContext_Text(b *testing.B) {
	c := benchResponseContext("/")
	b.ReportAllocs()
	for b.Loop() {
		c.Text("hello world")
	}
}

func BenchmarkHTTPContext_TextWithArgs(b *testing.B) {
	c := benchResponseContext("/")
	b.ReportAllocs()
	for b.Loop() {
		c.Text("hello %s", "joe")
	}
}

func BenchmarkHTTPContext_JSON_Map(b *testing.B) {
	c := benchResponseContext("/")
	data := Map{"foo": "bar", "n": 1}
	b.ReportAllocs()
	for b.Loop() {
		c.JSON(data)
	}
}

func BenchmarkHTTPContext_JSON_Struct(b *testing.B) {
	c := benchResponseContext("/")
	data := benchJSONStruct{ID: 1, Name: "John Doe", Email: "john.doe@example.com", Tags: []string{"a", "b"}}
	b.ReportAllocs()
	for b.Loop() {
		c.JSON(data)
	}
}

func BenchmarkHTTPContext_JSON_Slice50(b *testing.B) {
	c := benchResponseContext("/")
	data := benchSlice50()
	b.ReportAllocs()
	for b.Loop() {
		c.JSON(data)
	}
}

func BenchmarkHTTPContext_JSON_Appender(b *testing.B) {
	c := benchResponseContext("/")
	data := jsonAppenderValue{body: `{"code":400,"message":"Bad Request"}`}
	b.ReportAllocs()
	for b.Loop() {
		c.JSON(data)
	}
}

func BenchmarkHTTPContext_JSON_RawString(b *testing.B) {
	c := benchResponseContext("/")
	raw := `{"status":"ok","rate":"100%"}`
	b.ReportAllocs()
	for b.Loop() {
		c.JSON(raw)
	}
}

func BenchmarkHTTPContext_JSONP(b *testing.B) {
	c := benchResponseContext("/?callback=cb")
	data := Map{"foo": "bar", "n": 1}
	b.ReportAllocs()
	for b.Loop() {
		c.JSONP(data)
	}
}

func BenchmarkHTTPContext_Redirect(b *testing.B) {
	c := benchResponseContext("/")
	b.ReportAllocs()
	for b.Loop() {
		c.Status(http.StatusFound).Redirect("/new-location")
	}
}

func BenchmarkHTTPContext_Init_Traceparent(b *testing.B) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		c.Init(w, r)
		c.Reset()
	}
}

func BenchmarkReset_WithInterceptor(b *testing.B) {
	a := aggregation.NewAggregation()
	c := newTestHTTPContext()
	b.ReportAllocs()
	for b.Loop() {
		c.AddAggregation(a)
		c.Reset()
	}
}
