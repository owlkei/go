package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleWSExceptionFilter struct {
	owlky.ExceptionFilter
	log.Logger
}

func (instance LogModuleWSExceptionFilter) Catch(c owlky.WSContext, ex *exception.Exception) {
	if c.Conn.Config().Location.Query().Get("error_module_ws_ex") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSExceptionFilter error triggered"))
	}

	c.Send(owlky.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
