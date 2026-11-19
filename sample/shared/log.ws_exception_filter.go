package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogWSExceptionFilter struct {
	owlky.ExceptionFilter
	log.Logger
}

func (instance LogWSExceptionFilter) Catch(c owlky.WSContext, ex *exception.Exception) {

	if c.Conn.Config().Location.Query().Get("error_ws_ex") == "true" {
		panic(exception.InternalServerErrorException("LogWSExceptionFilter error triggered"))
	}

	c.Send(owlky.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
