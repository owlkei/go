package benchmarks

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogModuleWSGuard struct {
	ginject.Guard
	log.Logger
}

func (instance LogModuleWSGuard) CanActivate(c ginject.WSContext) bool {
	if c.Conn.Config().Location.Query().Get("error_module_ws_guard") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSGuard error triggered"))
	}

	return true
}
