package core

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/trace"
)

func BenchmarkReturnHTTP_String(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf("hello world")
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Int(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(123456)
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Float(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(3.14159)
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Map(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(ctx.Map{"message": "ok", "count": 3})
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_PointerToStruct(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(&returnUser{Name: "ann"})
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_InterfaceString(b *testing.B) {
	c := newHTTPContext()
	v := anyValue("hello world")
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkGetPkgFromControllerKey(b *testing.B) {
	key := "[123456789]github.com/dangduoc08/ginject/sample/modules/user.UserController"
	b.ReportAllocs()
	for b.Loop() {
		getPkgFromControllerKey(key)
	}
}

func BenchmarkGenFieldKey(b *testing.B) {
	t := reflect.TypeOf(&mockProvider{})
	b.ReportAllocs()
	for b.Loop() {
		genFieldKey(t)
	}
}

func BenchmarkBuildUseMiddleware_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	c.Next = func() {}
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "bench.Middleware", trace.TransportHTTP)
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}

func BenchmarkBuildUseMiddleware_WithListener(b *testing.B) {
	ev := event.NewEvent()
	ev.On(trace.EventName, func(args ...any) {})
	c := newHTTPContext()
	c.Next = func() {}
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "bench.Middleware", trace.TransportHTTP)
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}
