package common

import (
	"net/http"
	"reflect"

	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/routing"
	"go.owlky.com/owlky/internal/str"
)

type Use = func(*http.Request, http.ResponseWriter, ctx.Next)

type MiddlewareFn interface {
	Use(*http.Request, http.ResponseWriter, ctx.Next)
}

type HTTPMiddlewareItem struct {
	Method  string
	Route   string
	Version string
	Pattern string
	Common  CommonItem
}

type MiddlewareItem struct {
	HTTP HTTPMiddlewareItem
}

type middlewareHandler struct {
	middlewareFn MiddlewareFn
	handlers     []any
}

type Middleware struct {
	middlewareHandlers []middlewareHandler
}

func (m *Middleware) BindMiddleware(middlewareFn MiddlewareFn, handlers ...any) *Middleware {
	middlewareHandler := middlewareHandler{
		middlewareFn: middlewareFn,
		handlers:     handlers,
	}

	m.middlewareHandlers = append(m.middlewareHandlers, middlewareHandler)
	return m
}

func InjectProvidersIntoHTTPMiddlewares(m *Middleware, r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []MiddlewareItem {
	middlewareItemArr := make([]MiddlewareItem, 0, len(r.patternToFuncNameMap)*len(m.middlewareHandlers))

	for _, middlewareHandler := range m.middlewareHandlers {
		middlewarerType := reflect.TypeOf(middlewareHandler.middlewareFn)
		middlewarerValue := reflect.ValueOf(middlewareHandler.middlewareFn)
		newMiddleware := reflect.New(middlewarerType)

		for i := 0; i < middlewarerType.NumField(); i++ {
			cb(i, middlewarerType, middlewarerValue, newMiddleware)
		}

		newMiddlewareFn := newMiddleware.Interface()
		newMiddlewareFn = construct(newMiddlewareFn, "NewMiddleware")
		middlewareHandler.middlewareFn = newMiddlewareFn.(MiddlewareFn)

		targets := httpHandlerTargets(r, middlewareHandler.handlers, "middleware", middlewarerType)

		for pattern := range r.patternToFuncNameMap {
			if targets.has(pattern) {
				method, route, version := routing.PatternToMethodRouteVersion(pattern)
				httpMethod := routing.OperationsMapHTTPMethods[method]

				middlewareItemArr = append(middlewareItemArr, MiddlewareItem{
					HTTP: HTTPMiddlewareItem{
						Method:  httpMethod,
						Route:   str.Enclose(route, '/'),
						Version: version,
						Pattern: pattern,
						Common: CommonItem{
							Handler:         middlewareHandler.middlewareFn.Use,
							Name:            middlewarerType.String(),
							MainHandlerName: r.patternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return middlewareItemArr
}
