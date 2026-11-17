package routing

import (
	"errors"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/ds"
	"github.com/dangduoc08/ginject/internal/str"
)

const SERVE = "SERVE"

const (
	MaxPathLength   = 4096
	MaxPathSegments = ds.MaxSegments
)

var OperationsMapHTTPMethods = map[string]string{
	http.MethodGet:     http.MethodGet,
	http.MethodHead:    http.MethodHead,
	http.MethodPost:    http.MethodPost,
	http.MethodPut:     http.MethodPut,
	http.MethodPatch:   http.MethodPatch,
	http.MethodDelete:  http.MethodDelete,
	http.MethodConnect: http.MethodConnect,
	http.MethodOptions: http.MethodOptions,
	http.MethodTrace:   http.MethodTrace,
	SERVE:              http.MethodGet,
}

var HTTPMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodConnect,
	http.MethodOptions,
	http.MethodTrace,
	SERVE,
}

const (
	callerAdd = iota + 1
	callerUse
	callerFor
)

const partialParamReason = "parameter must be a whole segment like {name}"

const literalDollarReason = "a $ segment is reserved for parameters; name it like {name}"

type RouterItem struct {
	Method       string
	Version      string
	Pattern      string
	Index        int
	HandlerIndex int
	Handlers     []ctx.HTTPHandler
	ParamKeys    map[string][]int
	ParamNames   []string
	Injectable   any
}

type routeTree struct {
	method           string
	version          string
	trie             *ds.Trie[RouterItem]
	itemByStaticPath map[string]*RouterItem
}

type Router struct {
	trees              []*routeTree
	items              []*RouterItem
	itemByPattern      map[string]*RouterItem
	GlobalMiddlewares  []ctx.HTTPHandler
	InjectableHandlers map[string]any
}

func NewRouter() *Router {
	return &Router{
		itemByPattern:      make(map[string]*RouterItem),
		GlobalMiddlewares:  []ctx.HTTPHandler{},
		InjectableHandlers: make(map[string]any),
	}
}

func (r *Router) Use(handlers ...ctx.HTTPHandler) *Router {
	r.GlobalMiddlewares = append(r.GlobalMiddlewares, handlers...)

	for _, item := range r.items {
		item.Handlers = append(item.Handlers, handlers...)
	}

	return r
}

func (r *Router) For(methodInclusions []string, route string, version string) func(handlers ...ctx.HTTPHandler) *Router {
	return func(handlers ...ctx.HTTPHandler) *Router {
		for _, method := range methodInclusions {
			r.push(method, route, version, callerFor, handlers...)
		}

		return r
	}
}

func (r *Router) Add(method, route, version string, handler ctx.HTTPHandler) *Router {
	r.push(method, route, version, callerAdd, handler)

	return r
}

func (r *Router) AddInjectableHandler(method, route, version string, handler any) *Router {
	var handlerKind reflect.Kind
	if handler != nil {
		handlerKind = reflect.TypeOf(handler).Kind()
	}
	if handler == nil || handlerKind != reflect.Func {
		panic(errors.New(
			color.FmtRed(
				"invalid handler: %v is not a handler",
				handlerKind,
			),
		))
	}

	item := r.push(method, route, version, callerAdd, nil)
	item.Injectable = handler
	r.InjectableHandlers[item.Pattern] = handler

	return r
}

func (r *Router) Match(method, path, version string, params []string) (*RouterItem, []string) {
	tree, path := r.lookup(method, path, version)
	if tree == nil {
		return nil, params
	}

	if item := tree.itemByStaticPath[staticKey(path)]; item != nil {
		return item, params
	}

	return tree.trie.Find(path, params)
}

func (r *Router) matchTrie(method, path, version string, params []string) (*RouterItem, []string) {
	tree, path := r.lookup(method, path, version)
	if tree == nil {
		return nil, params
	}

	return tree.trie.Find(path, params)
}

func (r *Router) lookup(method, path, version string) (*routeTree, string) {
	tree := r.tree(method, version)
	if tree == nil {
		return nil, ""
	}
	path, ok := normalizePath(path)
	if !ok {
		return nil, ""
	}
	return tree, path
}

func (r *Router) tree(method, version string) *routeTree {
	for _, tree := range r.trees {
		if tree.method == method && tree.version == version {
			return tree
		}
	}
	return nil
}

func (r *Router) push(method, route, version string, caller int, handlers ...ctx.HTTPHandler) *RouterItem {
	pattern := MethodRouteVersionToPattern(method, route, version)
	item := r.itemByPattern[pattern]
	if item == nil {
		item = r.newItem(method, route, version, pattern)
	}

	handlerTotal := len(item.Handlers)
	globalMiddlewareTotal := len(r.GlobalMiddlewares)

	switch caller {
	case callerUse:
		item.Handlers = append(item.Handlers, handlers...)
	case callerFor:
		if handlerTotal == 0 && globalMiddlewareTotal > 0 {
			item.Handlers = append(item.Handlers, r.GlobalMiddlewares...)
		}
		item.Handlers = append(item.Handlers, handlers...)
	case callerAdd:
		switch {
		case handlerTotal == 0:
			item.Handlers = append(item.Handlers, r.GlobalMiddlewares...)
			item.Handlers = append(item.Handlers, handlers...)
			item.HandlerIndex = globalMiddlewareTotal
		case item.HandlerIndex > -1:
			item.Handlers = append(item.Handlers[:item.HandlerIndex], item.Handlers[item.HandlerIndex+1:]...)
			item.Handlers = append(item.Handlers, handlers...)
			item.HandlerIndex = handlerTotal - 1
		default:
			item.HandlerIndex = handlerTotal
			item.Handlers = append(item.Handlers, handlers...)
		}
	}

	return item
}

func (r *Router) newItem(method, route, version, pattern string) *RouterItem {
	compiled := compileRoute(route)

	item := &RouterItem{
		Method:       method,
		Version:      version,
		Pattern:      pattern,
		Index:        len(r.items),
		HandlerIndex: -1,
		ParamNames:   compiled.paramNames,
	}
	if len(compiled.paramNames) > 0 {
		item.ParamKeys = make(map[string][]int, len(compiled.paramNames))
		for i, name := range compiled.paramNames {
			item.ParamKeys[name] = append(item.ParamKeys[name], i)
		}
	}

	tree := r.tree(method, version)
	if tree == nil {
		tree = &routeTree{
			method:  method,
			version: version,
			trie:    ds.NewTrie[RouterItem](),
		}
		r.trees = append(r.trees, tree)
	}
	if err := tree.trie.Insert(compiled.triePattern, item); err != nil {
		panic(invalidRoute(route, err.Error()))
	}
	if compiled.isStatic {
		if tree.itemByStaticPath == nil {
			tree.itemByStaticPath = make(map[string]*RouterItem)
		}
		tree.itemByStaticPath[compiled.triePattern] = item
	}

	r.items = append(r.items, item)
	r.itemByPattern[pattern] = item

	return item
}

type compiledRoute struct {
	triePattern string
	paramNames  []string
	isStatic    bool
}

func compileRoute(route string) compiledRoute {
	segments := strings.FieldsFunc(str.Enclose(route, '/'), func(r rune) bool { return r == '/' })
	if len(segments) > MaxPathSegments {
		panic(invalidRoute(route, "more than "+strconv.Itoa(MaxPathSegments)+" segments"))
	}

	compiled := compiledRoute{isStatic: true}
	var triePattern strings.Builder
	for _, seg := range segments {
		triePattern.WriteByte('/')
		if strings.ContainsAny(seg, "{}") {
			if !isParamSegment(seg) {
				panic(invalidRoute(route, partialParamReason))
			}
			compiled.paramNames = append(compiled.paramNames, seg[1:len(seg)-1])
			compiled.isStatic = false
			triePattern.WriteByte('$')
			continue
		}
		if seg == "$" {
			panic(invalidRoute(route, literalDollarReason))
		}
		if strings.Contains(seg, "*") {
			compiled.isStatic = false
		}
		triePattern.WriteString(seg)
	}

	if triePattern.Len() == 0 {
		triePattern.WriteByte('/')
	}
	compiled.triePattern = triePattern.String()

	return compiled
}

func isParamSegment(seg string) bool {
	return len(seg) >= 3 && seg[0] == '{' && seg[len(seg)-1] == '}' && !strings.ContainsAny(seg[1:len(seg)-1], "{}")
}

func invalidRoute(route, reason string) error {
	return errors.New(color.FmtRed("invalid route: %s: %s", route, reason))
}

func normalizePath(p string) (string, bool) {
	if len(p) > MaxPathLength {
		return "", false
	}
	if !hasDotSegment(p) {
		return p, true
	}
	if len(p) > 2*MaxPathSegments && countSegments(p) > MaxPathSegments {
		return "", false
	}
	if p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p), true
}

func hasDotSegment(p string) bool {
	if len(p) > 0 && p[0] == '.' {
		return true
	}
	for i := 0; i+1 < len(p); i++ {
		if p[i] == '/' && p[i+1] == '.' {
			return true
		}
	}
	return false
}

func staticKey(p string) string {
	if len(p) > 1 && p[len(p)-1] == '/' {
		return p[:len(p)-1]
	}
	if p == "" {
		return "/"
	}
	return p
}

func countSegments(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '/' && (i == 0 || s[i-1] == '/') {
			count++
		}
	}
	return count
}
