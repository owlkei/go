package common

import (
	"net/http"
	"reflect"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/routing"
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
	MiddlewareHandlers []middlewareHandler
}

func (m *Middleware) BindMiddleware(middlewareFn MiddlewareFn, handlers ...any) *Middleware {
	middlewareHandler := middlewareHandler{
		middlewareFn: middlewareFn,
		handlers:     handlers,
	}

	m.MiddlewareHandlers = append(m.MiddlewareHandlers, middlewareHandler)
	return m
}

func (m *Middleware) InjectProvidersIntoHTTPMiddlewares(r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []MiddlewareItem {
	middlewareItemArr := make([]MiddlewareItem, 0, len(r.PatternToFuncNameMap)*len(m.MiddlewareHandlers))

	for _, middlewareHandler := range m.MiddlewareHandlers {
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

		for pattern := range r.PatternToFuncNameMap {
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
							MainHandlerName: r.PatternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return middlewareItemArr
}
