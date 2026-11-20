package common

import (
	"errors"
	"reflect"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/routing"
	"go.owlkei.com/owlkei/internal/str"
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

func InjectProvidersIntoHTTPExceptionFilters(e *ExceptionFilter, r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []ExceptionFilterItem {
	exceptionFilterItemArr := make([]ExceptionFilterItem, 0, len(r.patternToFuncNameMap)*len(e.exceptionFilterHandlers))

	for _, exceptionFilterHandler := range e.exceptionFilterHandlers {
		exceptionFilterableType := reflect.TypeOf(exceptionFilterHandler.exceptionFilterable)
		exceptionFilterableValue := reflect.ValueOf(exceptionFilterHandler.exceptionFilterable)
		newExceptionFilter := reflect.New(exceptionFilterableType)

		for i := 0; i < exceptionFilterableType.NumField(); i++ {
			cb(i, exceptionFilterableType, exceptionFilterableValue, newExceptionFilter)
		}

		newExceptionFilterable := newExceptionFilter.Interface()
		newExceptionFilterable = construct(newExceptionFilterable, "NewExceptionFilter")
		exceptionFilterHandler.exceptionFilterable = newExceptionFilterable

		catch, ok := AsHTTPExceptionFilter(exceptionFilterHandler.exceptionFilterable)
		if !ok {
			if _, ok = AsWSExceptionFilter(exceptionFilterHandler.exceptionFilterable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid exception filter: %v.%s must be func(owlkei.HTTPContext, *exception.Exception) to be bound as a HTTP exception filter",
				exceptionFilterableType,
				ExceptionFilterMethodName,
			)))
		}

		targets := httpHandlerTargets(r, exceptionFilterHandler.handlers, "exception filter", exceptionFilterableType)

		for pattern := range r.patternToFuncNameMap {
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
							MainHandlerName: r.patternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return exceptionFilterItemArr
}
