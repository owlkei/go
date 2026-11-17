package versioning

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
)

func benchCtxQuery(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	u := &url.URL{RawQuery: url.Values{key: {val}}.Encode()}
	c.Request = &http.Request{URL: u}
	return c
}

func benchCtxHeader(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = &http.Request{Header: http.Header{key: {val}}}
	return c
}

func benchCtxEmpty() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = &http.Request{URL: &url.URL{}, Header: http.Header{}}
	return c
}

func BenchmarkVersion_Query(b *testing.B) {
	v := &Options{Type: TypeQuery, Key: "version", DefaultVersion: "v1"}
	c := benchCtxQuery("version", "v2")
	b.ReportAllocs()
	for b.Loop() {
		Version(v, c)
	}
}

func BenchmarkVersion_Header(b *testing.B) {
	v := &Options{Type: TypeHeader, Key: "X-Api-Version", DefaultVersion: "v1"}
	c := benchCtxHeader("X-Api-Version", "v2")
	b.ReportAllocs()
	for b.Loop() {
		Version(v, c)
	}
}

func BenchmarkVersion_Custom(b *testing.B) {
	v := &Options{
		Type:      TypeCustom,
		Extractor: func(c *ctx.HTTPContext) string { return "v9" },
	}
	c := benchCtxEmpty()
	b.ReportAllocs()
	for b.Loop() {
		Version(v, c)
	}
}

func BenchmarkVersion_MediaType(b *testing.B) {
	v := &Options{Type: TypeMediaType, Key: "v", DefaultVersion: "v1"}
	c := benchCtxHeader("Accept", "application/json;v=v2")
	b.ReportAllocs()
	for b.Loop() {
		Version(v, c)
	}
}
