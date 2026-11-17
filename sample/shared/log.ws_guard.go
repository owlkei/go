package shared

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogWSGuard struct {
	ginject.Guard
	log.Logger
}

func (instance LogWSGuard) CanActivate(c ginject.WSContext) bool {

	if c.Conn.Config().Location.Query().Get("error_ws_guard") == "true" {
		panic(exception.InternalServerErrorException("LogWSGuard error triggered"))
	}

	return true
}
