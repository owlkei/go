package versioning

import (
	"net/http"
	"net/url"

	"go.owlkei.com/owlkei/internal/ctx"
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

func makeCtxWithQuery(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	u := &url.URL{RawQuery: url.Values{key: {val}}.Encode()}
	c.Request = &http.Request{URL: u}
	return c
}

func makeCtxWithHeader(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	h := http.Header{key: {val}}
	c.Request = &http.Request{Header: h}
	return c
}

func makeCtxEmpty() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	u := &url.URL{RawQuery: ""}
	c.Request = &http.Request{URL: u, Header: http.Header{}}
	return c
}

func acceptVersion(accept, key string) string {
	v := &Options{Type: TypeMediaType, Key: key, DefaultVersion: "dflt"}
	return Version(v, makeCtxWithHeader("Accept", accept))
}
