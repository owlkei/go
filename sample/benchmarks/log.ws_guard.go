package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleWSGuard struct {
	owlky.Guard
	log.Logger
}

func (instance LogModuleWSGuard) CanActivate(c owlky.WSContext) bool {
	if c.Conn.Config().Location.Query().Get("error_module_ws_guard") == "true" {
		panic(exception.InternalServerErrorException("LogModuleWSGuard error triggered"))
	}

	return true
}
