package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleHTTPGuard struct {
	owlky.Guard
	log.Logger
}

func (instance LogModuleHTTPGuard) CanActivate(c owlky.HTTPContext) bool {

	if c.Query().Get("error_module_http_guard") == "true" {
		panic(exception.InternalServerErrorException("LogModuleHTTPGuard error triggered"))
	}

	return true
}
