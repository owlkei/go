package benchmarks

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogModuleHTTPExceptionFilter struct {
	ginject.ExceptionFilter
	log.Logger
}

func (instance LogModuleHTTPExceptionFilter) Catch(c ginject.HTTPContext, ex *exception.Exception) {
	if c.Query().Get("error_module_http_ex") == "true" {
		panic(exception.InternalServerErrorException("LogModuleHTTPExceptionFilter error triggered"))
	}

	c.Status(ex.GetCode()).JSON(ginject.Map{
		"code":    ex.GetCode(),
		"error":   ex.Error(),
		"message": ex.GetMessage(),
	})
}
