package shared

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogHTTPExceptionFilter struct {
	ginject.ExceptionFilter
	log.Logger
}

func (instance LogHTTPExceptionFilter) Catch(c ginject.HTTPContext, ex *exception.Exception) {
	if c.Query().Get("error_module_http_ex") == "true" {
		panic(exception.InternalServerErrorException("LogHTTPExceptionFilter error triggered"))
	}

	c.Status(ex.Code()).JSON(ginject.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
