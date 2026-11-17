package benchmarks

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
)

type Controller struct {
	ginject.HTTP
	ginject.WS
	ginject.Middleware
	ginject.Guard
	ginject.Interceptor
	ginject.ExceptionFilter
}

func (instance Controller) NewController() ginject.Controller {
	instance.BindMiddleware(LogModuleMiddleware{})
	instance.BindGuard(LogModuleHTTPGuard{})
	instance.BindGuard(LogModuleWSGuard{})
	instance.BindInterceptor(LogModuleHTTPInterceptor{})
	instance.BindInterceptor(LogModuleWSInterceptor{})
	instance.BindExceptionFilter(LogModuleHTTPExceptionFilter{})
	instance.BindExceptionFilter(LogModuleWSExceptionFilter{})

	return instance
}

func (instance Controller) READ_ping(query ginject.Query) ginject.Map {

	if query.Get("error") == "true" {
		panic(exception.InternalServerErrorException("READ_ping error triggered"))
	}

	return ginject.Map{
		"message": "Hello, World!",
	}
}

func (instance Controller) SUBSCRIBE_chat_PERSON_ANY() ginject.Map {
	return ginject.Map{
		"message": "Hello, World!",
	}
}
