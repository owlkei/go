package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/internal/color"
)

type CommonItem struct {
	Name            string
	Handler         any
	MainHandlerName string
}

type handlerTargets struct {
	keys  map[string]bool
	isAll bool
}

func (t handlerTargets) has(key string) bool {
	return t.isAll || t.keys[key]
}

func httpHandlerTargets(r *HTTP, handlers []any, category string, layerType reflect.Type) handlerTargets {
	return resolveHandlerTargets(handlers, category, layerType, func(fnName string) (string, bool) {
		pattern, ok := r.FuncNameToPatternMap[fnName]
		return pattern, ok
	}, func(fnName string) bool {
		_, ok := ParseWSFuncNameToEvent(fnName)
		return ok
	})
}

func wsHandlerTargets(handlers []any, category string, layerType reflect.Type) handlerTargets {
	return resolveHandlerTargets(handlers, category, layerType, ParseWSFuncNameToEvent, func(fnName string) bool {
		method, _, _ := ParseFuncNameToURL(fnName)
		return method != ""
	})
}

func resolveHandlerTargets(
	handlers []any,
	category string,
	layerType reflect.Type,
	resolve func(string) (string, bool),
	isOtherTransport func(string) bool,
) handlerTargets {
	if len(handlers) == 0 {
		return handlerTargets{isAll: true}
	}

	keys := make(map[string]bool, len(handlers))
	for _, handler := range handlers {
		name, isFunc := handlerName(handler)
		if isFunc {
			if key, ok := resolve(name); ok {
				keys[key] = true
				continue
			}
			if isOtherTransport(name) {
				continue
			}
		}
		panic(errors.New(color.FmtRed(
			"invalid %s: %v is bound to '%s', which is not a handler of the controller",
			category,
			layerType,
			name,
		)))
	}

	return handlerTargets{keys: keys}
}

func handlerName(handler any) (string, bool) {
	value := reflect.ValueOf(handler)
	if value.Kind() != reflect.Func {
		return value.Kind().String(), false
	}
	return GetFuncName(handler), true
}
