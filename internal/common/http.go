package common

import (
	"errors"
	"net/http"
	"strings"

	"go.owlky.com/owlky/internal/color"

	"go.owlky.com/owlky/internal/routing"
	"go.owlky.com/owlky/internal/str"
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
	patternToFuncNameMap map[string]string
	funcNameToPatternMap map[string]string
	routerMap            map[string]any
}

type Prefix struct {
	Value    string
	Handlers []any
	exactFor string
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
		if prefixConf.exactFor != "" {
			continue
		}
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
	entries := r.prefixes[:len(r.prefixes):len(r.prefixes)]
	for _, handler := range handlers {
		name, isFunc := handlerName(handler)
		if !isFunc {
			panic(errors.New(color.FmtRed("invalid route: Path(%q) is bound to '%s', which is not a handler of the controller", v, name)))
		}
		if previous, ok := findExactPath(entries, name); ok {
			panic(errors.New(color.FmtRed("invalid route: %s has two exact paths: %q and %q", name, previous.Value, v)))
		}
		entries = append(entries, Prefix{Value: v, exactFor: name})
	}
	r.prefixes = entries

	return r
}

func refuseSubmitTarget(call, v string, handlers []any) {
	for _, handler := range handlers {
		if name, isFunc := handlerName(handler); isFunc && PageOperation(name) == PageSubmit {
			panic(errors.New(color.FmtRed("invalid route: %s(%q) is bound to '%s': a SUBMIT_ uses the path of its RENDER_", call, v, name)))
		}
	}
}

func findExactPath(entries []Prefix, fnName string) (Prefix, bool) {
	for _, entry := range entries {
		if entry.exactFor == fnName {
			return entry, true
		}
	}
	return Prefix{}, false
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
	if path, ok := findExactPath(r.prefixes, pathName); ok {
		route = str.Enclose(path.Value, '/')
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
	for _, entry := range r.prefixes {
		if entry.exactFor == "" {
			continue
		}
		if _, ok := r.funcNameToPatternMap[entry.exactFor]; !ok {
			panic(errors.New(color.FmtRed("invalid route: Path(%q) is bound to '%s', which is not a handler of the controller", entry.Value, entry.exactFor)))
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
