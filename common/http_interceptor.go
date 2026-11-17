package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/str"
)

type HTTPIntercept = func(*ctx.HTTPContext, *aggregation.Aggregation) any

type HTTPInterceptorItem struct {
	Method  string
	Route   string
	Version string
	Pattern string
	Common  CommonItem
}

type httpInterceptor interface {
	Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any
}

func AsHTTPInterceptor(interceptable any) (HTTPIntercept, bool) {
	layer, ok := interceptable.(httpInterceptor)
	if !ok {
		return nil, false
	}
	return layer.Intercept, true
}

func (i *Interceptor) InjectProvidersIntoHTTPInterceptors(r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []InterceptorItem {
	interceptorItemArr := make([]InterceptorItem, 0, len(r.PatternToFuncNameMap)*len(i.InterceptorHandlers))

	for _, interceptorHandler := range i.InterceptorHandlers {
		interceptableType := reflect.TypeOf(interceptorHandler.interceptable)
		interceptableValue := reflect.ValueOf(interceptorHandler.interceptable)
		newInterceptor := reflect.New(interceptableType)

		for j := 0; j < interceptableType.NumField(); j++ {
			cb(j, interceptableType, interceptableValue, newInterceptor)
		}

		newInterceptable := newInterceptor.Interface()
		newInterceptable = construct(newInterceptable, "NewInterceptor")
		interceptorHandler.interceptable = newInterceptable.(Interceptable)

		intercept, ok := AsHTTPInterceptor(interceptorHandler.interceptable)
		if !ok {
			if _, ok = AsWSInterceptor(interceptorHandler.interceptable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid interceptor: %v.%s must be func(*ctx.HTTPContext, *aggregation.Aggregation) any to be bound as a HTTP interceptor",
				interceptableType,
				InterceptorMethodName,
			)))
		}

		targets := httpHandlerTargets(r, interceptorHandler.handlers, "interceptor", interceptableType)

		for pattern := range r.PatternToFuncNameMap {
			if targets.has(pattern) {
				method, route, version := routing.PatternToMethodRouteVersion(pattern)
				httpMethod := routing.OperationsMapHTTPMethods[method]

				interceptorItemArr = append(interceptorItemArr, InterceptorItem{
					HTTP: HTTPInterceptorItem{
						Method:  httpMethod,
						Route:   str.Enclose(route, '/'),
						Version: version,
						Pattern: pattern,
						Common: CommonItem{
							Handler:         intercept,
							Name:            interceptableType.String(),
							MainHandlerName: r.PatternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return interceptorItemArr
}
