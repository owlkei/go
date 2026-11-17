package core

import (
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
)

type globalHTTPExceptionFilter struct{}

func (g globalHTTPExceptionFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Code = writeErrorResponse(c.ResponseWriter, ex)
}

type globalWSExceptionFilter struct{}

func (g globalWSExceptionFilter) Catch(c *ctx.WSContext, ex *exception.Exception) {
	code := ex.GetCode()
	if code == 0 {
		code = defaultException.GetCode()
	}

	err := ex.Error()
	if err == "" {
		err = defaultException.Error()
	}

	message := ex.GetMessage()
	if message == "" {
		message = defaultException.GetMessage()
	}

	c.Send(ctx.Map{
		"code":    code,
		"error":   err,
		"message": message,
	})
}
