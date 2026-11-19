package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogWSInterceptor struct {
	log.Logger
}

func (instance LogWSInterceptor) Intercept(c owlky.WSContext, aggregation owlky.Aggregation) any {

	if c.Conn.Config().Location.Query().Get("error_ws_pre_intercept") == "true" {
		panic(exception.InternalServerErrorException("LogWSInterceptor error triggered"))
	}

	return aggregation.Pipe()
}
