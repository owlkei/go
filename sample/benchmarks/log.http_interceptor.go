package benchmarks

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogModuleHTTPInterceptor struct {
	log.Logger
}

func (instance LogModuleHTTPInterceptor) Intercept(c owlky.HTTPContext, aggregation owlky.Aggregation) any {

	if c.Query().Get("error_module_http_pre_intercept") == "true" {
		panic(exception.InternalServerErrorException("LogModuleHTTPInterceptor error triggered"))
	}

	return aggregation.Pipe(
		aggregation.Transform(func(data any) any {

			if c.Query().Get("error_module_http_post_intercept") == "true" {
				panic(exception.InternalServerErrorException("LogModuleHTTPInterceptor error triggered"))
			}

			transformedData := owlky.Map{
				"data": data,
			}
			return transformedData
		}),
	)
}
