package common

import (
	"errors"
	"reflect"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/ctx"
)

type WSCatch = func(*ctx.WSContext, *exception.Exception)

type WSExceptionFilterItem struct {
	EventName string
	Common    CommonItem
}

type wsExceptionFilter interface {
	Catch(*ctx.WSContext, *exception.Exception)
}

func AsWSExceptionFilter(exceptionFilterable any) (WSCatch, bool) {
	layer, ok := exceptionFilterable.(wsExceptionFilter)
	if !ok {
		return nil, false
	}
	return layer.Catch, true
}

func InjectProvidersIntoWSExceptionFilters(e *ExceptionFilter, ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []ExceptionFilterItem {
	exceptionFilterItemArr := make([]ExceptionFilterItem, 0, len(ws.funcNameByEvent)*len(e.exceptionFilterHandlers))

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

		catch, ok := AsWSExceptionFilter(exceptionFilterHandler.exceptionFilterable)
		if !ok {
			if _, ok = AsHTTPExceptionFilter(exceptionFilterHandler.exceptionFilterable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid exception filter: %v.%s must be func(owlky.WSContext, *exception.Exception) to be bound as a WS exception filter",
				exceptionFilterableType,
				ExceptionFilterMethodName,
			)))
		}

		targets := wsHandlerTargets(exceptionFilterHandler.handlers, "exception filter", exceptionFilterableType)

		for pattern := range ws.funcNameByEvent {
			if targets.has(pattern) {
				exceptionFilterItemArr = append(exceptionFilterItemArr, ExceptionFilterItem{
					WS: WSExceptionFilterItem{
						EventName: pattern,
						Common: CommonItem{
							Handler: catch,
							Name:    exceptionFilterableType.String(),
						},
					},
				})
			}
		}
	}

	return exceptionFilterItemArr
}

func RunWSCatchChain(c *ctx.WSContext, catchFns []WSCatch, rec any) {
	for _, catchFn := range catchFns {
		var isHandled bool
		if rec, isHandled = runWSCatch(c, catchFn, rec); isHandled {
			return
		}
	}
}

func runWSCatch(c *ctx.WSContext, catchFn WSCatch, rec any) (next any, isHandled bool) {
	defer func() {
		if !isHandled {
			next = recover()
		}
	}()
	catchFn(c, NormalizeRecovered(rec))
	return nil, true
}
