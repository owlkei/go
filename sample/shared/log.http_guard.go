package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogHTTPGuard struct {
	owlky.Guard
	log.Logger
}

func (instance LogHTTPGuard) CanActivate(c owlky.HTTPContext) bool {

	if c.Query().Get("error_http_guard") == "true" {
		panic(exception.InternalServerErrorException("LogHTTPGuard error triggered"))
	}

	return true
}
