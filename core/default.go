package core

import (
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

var defaultException = exception.InternalServerErrorException("Unhandled exception has occurred")

type globalHTTPExceptionFilter struct{}

func (g globalHTTPExceptionFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	code := ex.GetCode()
	err := ex.Error()
	message := ex.GetMessage()
	if code != 0 && err != "" && message != "" {
		c.Status(code).JSON(ex)
		return
	}

	if code == 0 {
		code = defaultException.GetCode()
	}
	if err == "" {
		err = defaultException.Error()
	}
	if message == "" {
		message = defaultException.GetMessage()
	}

	normalized := exception.NewException(message, code, err)
	c.Status(code).JSON(&normalized)
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
