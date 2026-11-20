package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/ctx"
)

func BenchmarkGlobalHTTPExceptionFilterFull(b *testing.B) {
	filter := globalHTTPExceptionFilter{}
	ex := exception.BadRequestException("validation error")

	for b.Loop() {
		c := ctx.NewHTTPContext()
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.ResponseWriter = httptest.NewRecorder()
		filter.Catch(c, &ex)
	}
}

func BenchmarkGlobalHTTPExceptionFilterFallback(b *testing.B) {
	filter := globalHTTPExceptionFilter{}
	ex := exception.InternalServerErrorException("")

	for b.Loop() {
		c := ctx.NewHTTPContext()
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.ResponseWriter = httptest.NewRecorder()
		filter.Catch(c, &ex)
	}
}
