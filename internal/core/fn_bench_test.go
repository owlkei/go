package core

import (
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
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

func BenchmarkGenFieldKey(b *testing.B) {
	t := reflect.TypeOf(&mockProvider{})
	b.ReportAllocs()
	for b.Loop() {
		genFieldKey(t)
	}
}
