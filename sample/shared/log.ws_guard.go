package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogWSGuard struct {
	owlky.Guard
	log.Logger
}

func (instance LogWSGuard) CanActivate(c owlky.WSContext) bool {

	if c.Conn.Config().Location.Query().Get("error_ws_guard") == "true" {
		panic(exception.InternalServerErrorException("LogWSGuard error triggered"))
	}

	return true
}
