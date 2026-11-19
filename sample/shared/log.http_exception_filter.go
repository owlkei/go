package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogHTTPExceptionFilter struct {
	owlky.ExceptionFilter
	log.Logger
}

func (instance LogHTTPExceptionFilter) Catch(c owlky.HTTPContext, ex *exception.Exception) {
	if c.Query().Get("error_module_http_ex") == "true" {
		panic(exception.InternalServerErrorException("LogHTTPExceptionFilter error triggered"))
	}

	c.Status(ex.Code()).JSON(owlky.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
