package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleHTTPExceptionFilter struct {
	owlky.ExceptionFilter
	log.Logger
}

func (instance LogModuleHTTPExceptionFilter) Catch(c owlky.HTTPContext, ex *exception.Exception) {
	if c.Query().Get("error_module_http_ex") == "true" {
		panic(exception.InternalServerErrorException("LogModuleHTTPExceptionFilter error triggered"))
	}

	c.Status(ex.Code()).JSON(owlky.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
