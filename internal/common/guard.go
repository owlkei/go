package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/internal/color"
)

const GuardMethodName = "CanActivate"

type GuardItem struct {
	HTTP HTTPGuardItem
	WS   WSGuardItem
}

type guardHandler struct {
	guarder  any
	handlers []any
}

type Guard struct {
	guardHandlers []guardHandler
}

func (g *Guard) BindGuard(guard any, handlers ...any) *Guard {
	guardHandler := guardHandler{
		guarder:  guard,
		handlers: handlers,
	}

	g.guardHandlers = append(g.guardHandlers, guardHandler)
	return g
}

func GuardShapeError(guarder any) error {
	return errors.New(color.FmtRed(
		"invalid guard: %v has no %s method usable as a guard",
		reflect.TypeOf(guarder),
		GuardMethodName,
	))
}
