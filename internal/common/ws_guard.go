package common

import (
	"errors"
	"reflect"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/ctx"
)

type WSCanActivate = func(*ctx.WSContext) bool

type WSGuardItem struct {
	EventName string
	Common    CommonItem
}

type wsGuard interface {
	CanActivate(*ctx.WSContext) bool
}

func AsWSGuard(guarder any) (WSCanActivate, bool) {
	layer, ok := guarder.(wsGuard)
	if !ok {
		return nil, false
	}
	return layer.CanActivate, true
}

func InjectProvidersIntoWSGuards(g *Guard, ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []GuardItem {
	guardItemArr := make([]GuardItem, 0, len(ws.funcNameByEvent)*len(g.guardHandlers))

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

		canActivate, ok := AsWSGuard(guardHandler.guarder)
		if !ok {
			if _, ok = AsHTTPGuard(guardHandler.guarder); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid guard: %v.%s must be func(owlky.WSContext) bool to be bound as a WS guard",
				guarderType,
				GuardMethodName,
			)))
		}

		targets := wsHandlerTargets(guardHandler.handlers, "guard", guarderType)

		for pattern := range ws.funcNameByEvent {
			if targets.has(pattern) {
				guardItemArr = append(guardItemArr, GuardItem{
					WS: WSGuardItem{
						EventName: pattern,
						Common: CommonItem{
							Handler: canActivate,
							Name:    guarderType.String(),
						},
					},
				})
			}
		}
	}

	return guardItemArr
}

func BuildWSGuardMiddleware(canActivateFn WSCanActivate) ctx.WSHandler {
	return func(c *ctx.WSContext) {
		if !canActivateFn(c) {
			panic(accessDeniedException)
		}
		ctx.WSNext(c)()
	}
}
