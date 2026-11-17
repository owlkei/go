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
	PatternToFuncNameMap map[string]string
	FuncNameToPatternMap map[string]string
	RouterMap            map[string]any
}

type Prefix struct {
	Value    string
	Handlers []any
}

func (r *HTTP) addToRouters(fnName, route, version, method string, injectableHandler any) {
	if r.RouterMap == nil {
		r.RouterMap = make(map[string]any)
	}

	if r.PatternToFuncNameMap == nil {
		r.PatternToFuncNameMap = map[string]string{}
	}

	if r.FuncNameToPatternMap == nil {
		r.FuncNameToPatternMap = map[string]string{}
	}

	pattern := routing.MethodRouteVersionToPattern(method, route, version)

	r.RouterMap[pattern] = injectableHandler
	r.PatternToFuncNameMap[pattern] = fnName
	r.FuncNameToPatternMap[fnName] = pattern
}

func (r *HTTP) GetPrefixes() []map[string]string {
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

func (r *HTTP) AddHandlerToRouterMap(modulePrefixes []string, fnName string, handler any) {
	prefixes := r.GetPrefixes()

	httpMethod, route, version := ParseFuncNameToURL(fnName)
	if httpMethod != "" {
		route = r.addPrefixesToRoute(route, fnName, prefixes)
		for _, modulePrefix := range modulePrefixes {
			route = modulePrefix + route
		}

		r.addToRouters(fnName, route, version, httpMethod, handler)
	}
}
