package common

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dangduoc08/ginject/internal/color"

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
	PageRender:    http.MethodGet,
	PageSubmit:    http.MethodPost,
	routing.SERVE: routing.SERVE,
}

const (
	PageRender = "RENDER"
	PageSubmit = "SUBMIT"
)

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

type PathStyle int

const (
	PathStyleKebab PathStyle = iota + 1
	PathStyleAsWritten
)

type HTTPLayer struct {
	Handler         any
	ControllerPath  string
	Name            string
	Route           string
	Version         string
	Method          string
	Pattern         string
	MainHandlerName string
	Template        string
	IsPage          bool
}

type HTTP struct {
	prefixes             []Prefix
	exactPaths           []exactPath
	patternToFuncNameMap map[string]string
	funcNameToPatternMap map[string]string
	routerMap            map[string]any
}

type exactPath struct {
	value  string
	fnName string
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
	refuseSubmitTarget("Prefix", v, handlers)
	r.prefixes = append([]Prefix{
		{
			Value:    v,
			Handlers: handlers,
		},
	}, r.prefixes...)

	return r
}

func (r *HTTP) Path(v string, handlers ...any) *HTTP {
	if len(handlers) == 0 {
		panic(errors.New(color.FmtRed("invalid route: Path(%q) needs at least one handler", v)))
	}
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		panic(errors.New(color.FmtRed("invalid route: Path(%q) holds '%c': a path has no query or fragment", v, v[i])))
	}

	refuseSubmitTarget("Path", v, handlers)
	paths := r.exactPaths[:len(r.exactPaths):len(r.exactPaths)]
	for _, handler := range handlers {
		name, isFunc := handlerName(handler)
		if !isFunc {
			panic(errors.New(color.FmtRed("invalid route: Path(%q) is bound to '%s', which is not a handler of the controller", v, name)))
		}
		if previous, ok := findExactPath(paths, name); ok {
			panic(errors.New(color.FmtRed("invalid route: %s has two exact paths: %q and %q", name, previous.value, v)))
		}
		paths = append(paths, exactPath{value: v, fnName: name})
	}
	r.exactPaths = paths

	return r
}

func refuseSubmitTarget(call, v string, handlers []any) {
	for _, handler := range handlers {
		if name, isFunc := handlerName(handler); isFunc && PageOperation(name) == PageSubmit {
			panic(errors.New(color.FmtRed("invalid route: %s(%q) is bound to '%s': a SUBMIT_ uses the path of its RENDER_", call, v, name)))
		}
	}
}

func findExactPath(paths []exactPath, fnName string) (exactPath, bool) {
	for _, path := range paths {
		if path.fnName == fnName {
			return path, true
		}
	}
	return exactPath{}, false
}

func AddHandlerToRouterMap(r *HTTP, modulePrefixes []string, style PathStyle, fnName string, handler any) {
	AddHandlerToRouterMapWithPageVersion(r, modulePrefixes, style, fnName, handler, "")
}

func AddHandlerToRouterMapWithPageVersion(r *HTTP, modulePrefixes []string, style PathStyle, fnName string, handler any, pageVersion string) {
	httpMethod, route, version := ParseFuncNameToURL(fnName, style)
	if httpMethod == "" {
		return
	}

	pathName := fnName
	switch PageOperation(fnName) {
	case PageSubmit:
		pathName = RenderNameOf(fnName)
		version = pageVersion
	case PageRender:
		version = pageVersion
	}
	if path, ok := findExactPath(r.exactPaths, pathName); ok {
		route = str.Enclose(path.value, '/')
	}
	route = r.addPrefixesToRoute(route, pathName, r.getPrefixes())
	for _, modulePrefix := range modulePrefixes {
		route = modulePrefix + route
	}

	r.addToRouters(fnName, route, version, httpMethod, handler)
}

func PageOperation(fnName string) string {
	word, _, _ := strings.Cut(fnName, "_")
	if word == PageRender || word == PageSubmit {
		return word
	}
	return ""
}

func RenderNameOf(submitName string) string {
	return PageRender + strings.TrimPrefix(submitName, PageSubmit)
}

func CheckExactPaths(r *HTTP) {
	for _, path := range r.exactPaths {
		if _, ok := r.funcNameToPatternMap[path.fnName]; !ok {
			panic(errors.New(color.FmtRed("invalid route: Path(%q) is bound to '%s', which is not a handler of the controller", path.value, path.fnName)))
		}
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
