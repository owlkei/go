package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func newHTTPContext() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func TestGlobalHTTPExceptionFilterFullException(t *testing.T) {
	filter := globalHTTPExceptionFilter{}
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)

	ex := exception.BadRequestException("invalid input")
	filter.Catch(c, &ex)

	if w.Code != http.StatusBadRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusBadRequest, "HTTP status for BadRequest"))
	}
}

func TestGlobalHTTPExceptionFilterEmptyMessageFallsBackToDefault(t *testing.T) {
	filter := globalHTTPExceptionFilter{}
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)

	ex := exception.InternalServerErrorException("")
	filter.Catch(c, &ex)

	if w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, "HTTP status"))
	}
}

func TestGlobalWSExceptionFilterFullException(t *testing.T) {
	filter := globalWSExceptionFilter{}
	c := ctx.NewWSContext()

	var got any
	ctx.WSSetSend(c, func(data any) { got = data })

	ex := exception.PolicyViolationException("nope")
	filter.Catch(c, &ex)

	env, ok := got.(ctx.Map)
	if !ok {
		t.Fatalf("expected ctx.Map envelope, got %T: %+v", got, got)
	}
	if env["code"] != 1008 {
		t.Error(test.DiffMessage(env["code"], 1008, "WS status code for PolicyViolation"))
	}
	if env["error"] != "Policy Violation" {
		t.Error(test.DiffMessage(env["error"], "Policy Violation", "WS status text"))
	}
	if env["message"] != "nope" {
		t.Error(test.DiffMessage(env["message"], "nope", "WS message"))
	}
}

func TestGlobalWSExceptionFilterEmptyMessageFallsBackToDefault(t *testing.T) {
	filter := globalWSExceptionFilter{}
	c := ctx.NewWSContext()

	var got any
	ctx.WSSetSend(c, func(data any) { got = data })

	ex := exception.InternalServerErrorException("")
	filter.Catch(c, &ex)

	env, ok := got.(ctx.Map)
	if !ok {
		t.Fatalf("expected ctx.Map envelope, got %T: %+v", got, got)
	}
	if env["code"] != http.StatusInternalServerError {
		t.Error(test.DiffMessage(env["code"], http.StatusInternalServerError, "WS status code fallback"))
	}
}

func defaultFilterBody(ex exception.Exception) (int, string, string) {
	filter := globalHTTPExceptionFilter{}
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	filter.Catch(c, &ex)
	return w.Code, w.Header().Get("Content-Type"), w.Body.String()
}

func TestGlobalHTTPExceptionFilter_BodyBytes(t *testing.T) {
	cases := map[string]struct {
		ex   exception.Exception
		code int
		body string
	}{
		"bad request": {
			exception.BadRequestException("invalid input"),
			400, `{"code":400,"error":"Bad Request","message":"invalid input"}`,
		},
		"description": {
			exception.NotFoundException("Cannot GET /x", exception.ExceptionOptions{Description: "gone"}),
			404, `{"code":404,"error":"gone","message":"Cannot GET /x"}`,
		},
		"empty message falls back": {
			exception.InternalServerErrorException(""),
			500, `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`,
		},
		"zero value falls back": {
			exception.Exception{},
			500, `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`,
		},
		"unknown code without text": {
			exception.NewException("m", 599),
			599, `{"code":599,"error":"Internal Server Error","message":"m"}`,
		},
		"hostile message": {
			exception.BadRequestException("</script>\"&\u2028\\\n"),
			400, `{"code":400,"error":"Bad Request","message":"\u003c/script\u003e\"\u0026\u2028\\\n"}`,
		},
	}
	for name, tc := range cases {
		code, contentType, body := defaultFilterBody(tc.ex)
		if code != tc.code {
			t.Error(test.DiffMessage(code, tc.code, "default filter status: "+name))
		}
		if contentType != "application/json" {
			t.Error(test.DiffMessage(contentType, "application/json", "default filter content type: "+name))
		}
		if body != tc.body {
			t.Error(test.DiffMessage(body, tc.body, "default filter body bytes: "+name))
		}
	}
}
