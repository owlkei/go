package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleWSInterceptor struct {
	log.Logger
}

func (instance LogModuleWSInterceptor) Intercept(c owlky.WSContext, aggregation owlky.Aggregation) any {
	if c.Conn.Config().Location.Query().Get("error_module_ws_pre_intercept") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSInterceptor error triggered"))
	}

	return aggregation.Pipe()
}
