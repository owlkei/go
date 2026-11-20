package core

import (
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/ctx"
)

type globalHTTPExceptionFilter struct {
	http *HTTP
}

func (g globalHTTPExceptionFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	ctx.HTTPSetCode(c, g.http.writeError(c, c.ResponseWriter, ex, ctx.HTTPIsPage(c)))
}

type globalWSExceptionFilter struct{}

func (g globalWSExceptionFilter) Catch(c *ctx.WSContext, ex *exception.Exception) {
	code := ex.Code()
	if code == 0 {
		code = defaultException.Code()
	}

	err := ex.Error()
	if err == "" {
		err = defaultException.Error()
	}

	message := ex.Message()
	if message == "" {
		message = defaultException.Message()
	}

	c.Send(ctx.Map{
		"code":    code,
		"error":   err,
		"message": message,
	})
}
