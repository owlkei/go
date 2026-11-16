package common

import (
	"errors"
	"reflect"
	"runtime"
	"strings"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/routing"
)

type handlerTargets struct {
	keys  map[string]bool
	isAll bool
}

func (t handlerTargets) has(key string) bool {
	return t.isAll || t.keys[key]
}

func GetFuncName(handler any) string {
	name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, "-fm")
}

func ParseFuncNameToURL(fnName string) (string, string, string) {
	method := ""
	route := ""
	version := ""

	subStr := strings.FieldsFunc(fnName, func(r rune) bool { return r == '_' })
	j := -1

	for i, b := range subStr {

		if j >= 0 && i < j {
			continue
		}

		s := b

		if i == 0 {
			var ok bool
			method, ok = HTTPOperations[s]
			if !ok {
				return "", "", ""
			}
		}

		if s == TokenVersion {
			if i+1 < len(subStr) {
				version = strings.Join(subStr[i+1:], "_")
			}
			break
		}

		if _, ok := HTTPOperations[s]; ok || s == TokenOf {
			i++
			path := ""
			isAny := false

			for i < len(subStr) &&
				subStr[i] != TokenBy &&
				subStr[i] != TokenAnd &&
				subStr[i] != TokenOf &&
				subStr[i] != TokenVersion {

				if subStr[i] == TokenAny {
					if method == routing.SERVE && path != "" && isGroupEnd(subStr, i+1) {
						path += "/*"
					} else {
						path += "*"
					}
					isAny = true
				}

				if subStr[i] == TokenFile {
					lastWildcardIndex := strings.LastIndex(path, "*")
					if lastWildcardIndex > -1 {
						remainPath := "*"
						extension := strings.ToLower(path[lastWildcardIndex+1:])
						path = remainPath + "." + extension
					} else {
						lastWildcardIndex := strings.LastIndex(path, "_")
						if lastWildcardIndex > -1 {
							remainPath := path[:lastWildcardIndex]
							if remainPath == TokenAny {
								remainPath = "*"
							}
							extension := strings.ToLower(path[lastWildcardIndex+1:])

							path = remainPath + "." + extension
						}
					}
				}

				if subStr[i] != TokenAny && subStr[i] != TokenFile {
					if path == "" || isAny {
						path += subStr[i]
						isAny = false
					} else {
						path += "_" + subStr[i]
					}
				}
				i++
			}
			j = i

			route = path + "/" + route
			continue
		}

		if s == TokenBy || s == TokenAnd {
			firstSlashIndex := strings.Index(route, "/")
			resource := route[:firstSlashIndex]
			rest := route[firstSlashIndex:]

			i++
			start := i
			for i < len(subStr) && HTTPTokenMap[subStr[i]] == "" {
				i++
			}
			param := strings.Join(subStr[start:i], "_")
			j = i

			at := paramInsertIndex(rest)
			route = resource + rest[:at] + "/{" + param + "}" + rest[at:]
			continue
		}

		if s == TokenAny && (i == len(subStr)-1 || subStr[i+1] == TokenOf) {

			if route == "" {
				route = "*/"
				continue
			}
			firstSlashIndex := strings.Index(route, "/")
			shouldConcatRoute := route[:firstSlashIndex]
			remainRoutes := route[firstSlashIndex:]
			route = "*/" + shouldConcatRoute + remainRoutes
			continue
		}
	}

	return method, "/" + strings.TrimPrefix(route, "/"), version
}

func isGroupEnd(subStr []string, i int) bool {
	return i >= len(subStr) ||
		subStr[i] == TokenBy ||
		subStr[i] == TokenAnd ||
		subStr[i] == TokenOf ||
		subStr[i] == TokenVersion
}

func ParseWSFuncNameToEvent(fnName string) (string, bool) {
	op, rest, found := strings.Cut(fnName, "_")
	if !found || rest == "" {
		return "", false
	}
	if _, ok := WSOperations[op]; !ok {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(rest))
	hasSeg := false
	for rest != "" {
		var p string
		p, rest, _ = strings.Cut(rest, "_")
		if p == "" {
			continue
		}
		if hasSeg {
			b.WriteByte('.')
		}
		if p == TokenAny {
			b.WriteByte('*')
		} else {
			b.WriteString(strings.ToLower(p))
		}
		hasSeg = true
	}
	if !hasSeg {
		return "", false
	}
	return b.String(), true
}

func paramInsertIndex(rest string) int {
	at := 0
	for at+1 < len(rest) && rest[at+1] == '{' {
		end := strings.Index(rest[at:], "}/")
		if end < 0 {
			break
		}
		at += end + 1
	}
	return at
}

func ToWSEventName(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "/"), "/")
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

func wsHandlerTargets(handlers []any, category string, layerType reflect.Type) handlerTargets {
	return resolveHandlerTargets(handlers, category, layerType, ParseWSFuncNameToEvent, func(fnName string) bool {
		method, _, _ := ParseFuncNameToURL(fnName)
		return method != ""
	})
}

var accessDeniedException = exception.ForbiddenException("Access denied")
