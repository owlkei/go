package shared

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogWSExceptionFilter struct {
	ginject.ExceptionFilter
	log.Logger
}

func (instance LogWSExceptionFilter) Catch(c ginject.WSContext, ex *exception.Exception) {

	if c.Conn.Config().Location.Query().Get("error_ws_ex") == "true" {
		panic(exception.InternalServerErrorException("LogWSExceptionFilter error triggered"))
	}

	c.Send(ginject.Map{
		"code":    ex.GetCode(),
		"error":   ex.Error(),
		"message": ex.GetMessage(),
	})
}
