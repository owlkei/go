package common

import (
	"net/http"
	"strings"

	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/str"
)

var HTTPOperations = map[string]string{
	"READ":        http.MethodGet,
	"CREATE":      http.MethodPost,
	"UPDATE":      http.MethodPut,
	"MODIFY":      http.MethodPatch,
	"DELETE":      http.MethodDelete,
	"PREFLIGHT":   http.MethodOptions,
	routing.SERVE: routing.SERVE,
}

const (
	TokenBy      = "BY"
	TokenAnd     = "AND"
	TokenOf      = "OF"
	TokenAny     = "ANY"
	TokenFile    = "FILE"
	TokenVersion = "VERSION"
)

var HTTPTokenMap = map[string]string{
	TokenBy:      TokenBy,
	TokenAnd:     TokenAnd,
	TokenOf:      TokenOf,
	TokenAny:     TokenAny,
	TokenFile:    TokenFile,
	TokenVersion: TokenVersion,
}

type HTTPLayer struct {
	Handler         any
	ControllerPath  string
	Name            string
	Route           string
	Version         string
	Method          string
	Pattern         string
	MainHandlerName string
}

type HTTP struct {
	prefixes             []Prefix
	patternToFuncNameMap map[string]string
	funcNameToPatternMap map[string]string
	routerMap            map[string]any
}

type Prefix struct {
	Value    string
	Handlers []any
}

func (r *HTTP) addToRouters(fnName, route, version, method string, injectableHandler any) {
	if r.routerMap == nil {
		r.routerMap = make(map[string]any)
	}

	if r.patternToFuncNameMap == nil {
		r.patternToFuncNameMap = map[string]string{}
	}

	if r.funcNameToPatternMap == nil {
		r.funcNameToPatternMap = map[string]string{}
	}

	pattern := routing.MethodRouteVersionToPattern(method, route, version)

	r.routerMap[pattern] = injectableHandler
	r.patternToFuncNameMap[pattern] = fnName
	r.funcNameToPatternMap[fnName] = pattern
}

func (r *HTTP) getPrefixes() []map[string]string {
	prefixes := make([]map[string]string, 0, len(r.prefixes))

	for _, prefixConf := range r.prefixes {
		prefixValue := str.Enclose(prefixConf.Value, '/')
		prefixHandlers := prefixConf.Handlers

		if len(prefixHandlers) == 0 {
			prefixes = append(prefixes, map[string]string{prefixValue: "*"})
		} else {
			for _, handler := range prefixHandlers {
				prefixes = append(prefixes, map[string]string{prefixValue: GetFuncName(handler)})
			}
		}
	}

	return prefixes
}

func (r *HTTP) addPrefixesToRoute(route, fnName string, prefixes []map[string]string) string {
	for _, prefix := range prefixes {
		for prefixValue, prefixFnName := range prefix {
			if prefixFnName == "*" || prefixFnName == fnName {
				route = prefixValue + strings.TrimPrefix(route, "/")
			}
		}
	}

	return route
}

func (r *HTTP) Prefix(v string, handlers ...any) *HTTP {
	r.prefixes = append([]Prefix{
		{
			Value:    v,
			Handlers: handlers,
		},
	}, r.prefixes...)

	return r
}

func AddHandlerToRouterMap(r *HTTP, modulePrefixes []string, fnName string, handler any) {
	prefixes := r.getPrefixes()

	httpMethod, route, version := ParseFuncNameToURL(fnName)
	if httpMethod != "" {
		route = r.addPrefixesToRoute(route, fnName, prefixes)
		for _, modulePrefix := range modulePrefixes {
			route = modulePrefix + route
		}

		r.addToRouters(fnName, route, version, httpMethod, handler)
	}
}

func HTTPPatternToFuncNameMap(h *HTTP) map[string]string {
	return h.patternToFuncNameMap
}

func HTTPFuncNameToPatternMap(h *HTTP) map[string]string {
	return h.funcNameToPatternMap
}

func HTTPRouterMap(h *HTTP) map[string]any {
	return h.routerMap
}
