package shared

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogHTTPGuard struct {
	ginject.Guard
	log.Logger
}

func (instance LogHTTPGuard) CanActivate(c ginject.HTTPContext) bool {

	if c.Query().Get("error_http_guard") == "true" {
		panic(exception.InternalServerErrorException("LogHTTPGuard error triggered"))
	}

	return true
}
