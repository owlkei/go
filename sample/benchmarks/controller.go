package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
)

type Controller struct {
	owlky.HTTP
	owlky.WS
	owlky.Middleware
	owlky.Guard
	owlky.Interceptor
	owlky.ExceptionFilter
}

func (instance Controller) NewController() owlky.Controller {
	instance.BindMiddleware(LogModuleMiddleware{})
	instance.BindGuard(LogModuleHTTPGuard{})
	instance.BindGuard(LogModuleWSGuard{})
	instance.BindInterceptor(LogModuleHTTPInterceptor{})
	instance.BindInterceptor(LogModuleWSInterceptor{})
	instance.BindExceptionFilter(LogModuleHTTPExceptionFilter{})
	instance.BindExceptionFilter(LogModuleWSExceptionFilter{})

	return instance
}

func (instance Controller) READ_ping(query owlky.Query) owlky.Map {

	if query.Get("error") == "true" {
		panic(exception.InternalServerErrorException("READ_ping error triggered"))
	}

	return owlky.Map{
		"message": "Hello, World!",
	}
}

func (instance Controller) SUBSCRIBE_chat_person_ANY() owlky.Map {
	return owlky.Map{
		"message": "Hello, World!",
	}
}
