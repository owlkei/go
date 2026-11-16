package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/routing"
)

type HTTPCatch = func(*ctx.HTTPContext, *exception.Exception)

type HTTPExceptionFilterItem struct {
	Method  string
	Route   string
	Version string
	Pattern string
	Common  CommonItem
}

type httpExceptionFilter interface {
	Catch(*ctx.HTTPContext, *exception.Exception)
}

func AsHTTPExceptionFilter(exceptionFilterable any) (HTTPCatch, bool) {
	layer, ok := exceptionFilterable.(httpExceptionFilter)
	if !ok {
		return nil, false
	}
	return layer.Catch, true
}

func (e *ExceptionFilter) InjectProvidersIntoHTTPExceptionFilters(r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value)) []ExceptionFilterItem {
	exceptionFilterItemArr := make([]ExceptionFilterItem, 0, len(r.PatternToFuncNameMap)*len(e.ExceptionFilterHandlers))

	for _, exceptionFilterHandler := range e.ExceptionFilterHandlers {
		exceptionFilterableType := reflect.TypeOf(exceptionFilterHandler.exceptionFilterable)
		exceptionFilterableValue := reflect.ValueOf(exceptionFilterHandler.exceptionFilterable)
		newExceptionFilter := reflect.New(exceptionFilterableType)

		for i := 0; i < exceptionFilterableType.NumField(); i++ {
			cb(i, exceptionFilterableType, exceptionFilterableValue, newExceptionFilter)
		}

		newExceptionFilterable := newExceptionFilter.Interface()
		newExceptionFilterable = Construct(newExceptionFilterable, "NewExceptionFilter")
		exceptionFilterHandler.exceptionFilterable = newExceptionFilterable.(ExceptionFilterable)

		catch, ok := AsHTTPExceptionFilter(exceptionFilterHandler.exceptionFilterable)
		if !ok {
			if _, ok = AsWSExceptionFilter(exceptionFilterHandler.exceptionFilterable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid exception filter: %v.%s must be func(*ctx.HTTPContext, *exception.Exception) to be bound as a HTTP exception filter",
				exceptionFilterableType,
				ExceptionFilterMethodName,
			)))
		}

		targets := httpHandlerTargets(r, exceptionFilterHandler.handlers, "exception filter", exceptionFilterableType)

		for pattern := range r.PatternToFuncNameMap {
			if targets.has(pattern) {
				method, route, version := routing.PatternToMethodRouteVersion(pattern)
				httpMethod := routing.OperationsMapHTTPMethods[method]

				exceptionFilterItemArr = append(exceptionFilterItemArr, ExceptionFilterItem{
					HTTP: HTTPExceptionFilterItem{
						Method:  httpMethod,
						Route:   str.Enclose(route, '/'),
						Version: version,
						Pattern: pattern,
						Common: CommonItem{
							Handler:         catch,
							Name:            exceptionFilterableType.String(),
							MainHandlerName: r.PatternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return exceptionFilterItemArr
}
