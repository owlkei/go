package shared

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
)

type LogHTTPInterceptor struct {
	log.Logger
}

func (instance LogHTTPInterceptor) Intercept(c owlky.HTTPContext, aggregation owlky.Aggregation) any {

	if c.Query().Get("error_http_pre_intercept") == "true" {
		panic(exception.InternalServerErrorException("LogHTTPInterceptor error triggered"))
	}

	return aggregation.Pipe(
		aggregation.Transform(func(data any) any {

			if c.Query().Get("error_http_post_intercept") == "true" {
				panic(exception.InternalServerErrorException("LogHTTPInterceptor error triggered"))
			}

			transformedData := owlky.Map{
				"data": data,
			}
			return transformedData
		}),
	)
}
