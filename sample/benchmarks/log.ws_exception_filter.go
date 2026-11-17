package benchmarks

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogModuleWSExceptionFilter struct {
	ginject.ExceptionFilter
	log.Logger
}

func (instance LogModuleWSExceptionFilter) Catch(c ginject.WSContext, ex *exception.Exception) {
	if c.Conn.Config().Location.Query().Get("error_module_ws_ex") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSExceptionFilter error triggered"))
	}

	c.Send(ginject.Map{
		"code":    ex.Code(),
		"error":   ex.Error(),
		"message": ex.Message(),
	})
}
