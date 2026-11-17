package shared

import (
	"net/http"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/log"
)

type LogMiddleware struct {
	ginject.Middleware
	log.Logger
}

func (instance LogMiddleware) Use(r *http.Request, w http.ResponseWriter, next ginject.Next) {

	if r.URL.Query().Get("error_mw") == "true" {
		panic(exception.InternalServerErrorException("LogMiddleware error triggered"))
	}

	next()
}
