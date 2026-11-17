package ctx

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"
)

func BenchmarkBodyGet_Shallow(b *testing.B) {
	body := Body{"key": "value"}
	b.ReportAllocs()
	for b.Loop() {
		body.Get("key")
	}
}

func BenchmarkBodyGet_Deep(b *testing.B) {
	body := Body{
		"a": map[string]any{
			"b": map[string]any{
				"c": map[string]any{
					"d": "value",
				},
			},
		},
	}
	b.ReportAllocs()
	for b.Loop() {
		body.Get("a.b.c.d")
	}
}

func BenchmarkBodyHas_Shallow(b *testing.B) {
	body := Body{"key": "value"}
	b.ReportAllocs()
	for b.Loop() {
		body.Has("key")
	}
}

func BenchmarkBodySet_Deep(b *testing.B) {
	body := Body{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": "value"}}}}
	b.ReportAllocs()
	for b.Loop() {
		body.Set("a.b.c.d", "other")
	}
}

func BenchmarkBodyDel_Missing(b *testing.B) {
	body := Body{"a": map[string]any{"b": 1}}
	b.ReportAllocs()
	for b.Loop() {
		body.Del("a.x.y")
	}
}

func BenchmarkHTTPContext_BodyJSON(b *testing.B) {
	payload := []byte(`{"name":"John Doe","age":30,"active":true,"tags":["a","b","c"],"address":{"street":"Main St","city":"Metropolis"}}`)
	c := newTestHTTPContext()
	w := newDiscardResponseWriter()
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Content-Type", applicationJSON)
	reader := bytes.NewReader(payload)
	r.Body = io.NopCloser(reader)
	b.ReportAllocs()
	for b.Loop() {
		reader.Reset(payload)
		HTTPInit(c, w, r)
		c.Body()
		HTTPReset(c)
	}
}
