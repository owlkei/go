package shared

import (
	"net/http"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogMiddleware struct {
	owlky.Middleware
	log.Logger
}

func (instance LogMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlky.Next) {

	if r.URL.Query().Get("error_mw") == "true" {
		panic(exception.InternalServerErrorException("LogMiddleware error triggered"))
	}

	next()
}
