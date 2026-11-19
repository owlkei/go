package benchmarks

import (
	"net/http"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleMiddleware struct {
	owlky.Middleware
	log.Logger
}

func (instance LogModuleMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlky.Next) {
	if r.URL.Query().Get("error_module_mw") == "true" {
		panic(exception.InternalServerErrorException("LogModuleMiddleware error triggered"))
	}

	next()

}
