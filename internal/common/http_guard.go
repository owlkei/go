package common

import (
	"errors"
	"reflect"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/routing"
	"go.owlky.com/owlky/internal/str"
)

type HTTPCanActivate = func(*ctx.HTTPContext) bool

type HTTPGuardItem struct {
	Method  string
	Route   string
	Version string
	Pattern string
	Common  CommonItem
}

type httpGuard interface {
	CanActivate(*ctx.HTTPContext) bool
}

func AsHTTPGuard(guarder any) (HTTPCanActivate, bool) {
	layer, ok := guarder.(httpGuard)
	if !ok {
		return nil, false
	}
	return layer.CanActivate, true
}

func InjectProvidersIntoHTTPGuards(g *Guard, r *HTTP, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []GuardItem {
	guardItemArr := make([]GuardItem, 0, len(r.patternToFuncNameMap)*len(g.guardHandlers))

	for _, guardHandler := range g.guardHandlers {
		guarderType := reflect.TypeOf(guardHandler.guarder)
		guarderValue := reflect.ValueOf(guardHandler.guarder)
		newGuard := reflect.New(guarderType)

		for i := 0; i < guarderType.NumField(); i++ {
			cb(i, guarderType, guarderValue, newGuard)
		}

		newGuarder := newGuard.Interface()
		newGuarder = construct(newGuarder, "NewGuard")
		guardHandler.guarder = newGuarder

		canActivate, ok := AsHTTPGuard(guardHandler.guarder)
		if !ok {
			if _, ok = AsWSGuard(guardHandler.guarder); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid guard: %v.%s must be func(owlky.HTTPContext) bool to be bound as a HTTP guard",
				guarderType,
				GuardMethodName,
			)))
		}

		targets := httpHandlerTargets(r, guardHandler.handlers, "guard", guarderType)

		for pattern := range r.patternToFuncNameMap {
			if targets.has(pattern) {
				method, route, version := routing.PatternToMethodRouteVersion(pattern)
				httpMethod := routing.OperationsMapHTTPMethods[method]

				guardItemArr = append(guardItemArr, GuardItem{
					HTTP: HTTPGuardItem{
						Method:  httpMethod,
						Route:   str.Enclose(route, '/'),
						Version: version,
						Pattern: pattern,
						Common: CommonItem{
							Handler:         canActivate,
							Name:            guarderType.String(),
							MainHandlerName: r.patternToFuncNameMap[pattern],
						},
					},
				})
			}
		}
	}

	return guardItemArr
}
