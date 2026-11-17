package benchmarks

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogModuleWSInterceptor struct {
	log.Logger
}

func (instance LogModuleWSInterceptor) Intercept(c ginject.WSContext, aggregation ginject.Aggregation) any {
	if c.Conn.Config().Location.Query().Get("error_module_ws_pre_intercept") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSInterceptor error triggered"))
	}

	return aggregation.Pipe()
}
