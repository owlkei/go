package benchmarks

import (
	"net/http"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogModuleMiddleware struct {
	ginject.Middleware
	log.Logger
}

func (instance LogModuleMiddleware) Use(r *http.Request, w http.ResponseWriter, next ginject.Next) {
	if r.URL.Query().Get("error_module_mw") == "true" {
		panic(exception.InternalServerErrorException("LogModuleMiddleware error triggered"))
	}

	next()

}
