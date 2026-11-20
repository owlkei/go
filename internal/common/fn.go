package common

import (
	"errors"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/routing"
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

func ParseFuncNameToURL(fnName string, style PathStyle) (string, string, string) {
	words := strings.Split(fnName, "_")
	method, ok := HTTPOperations[words[0]]
	if !ok {
		return "", "", ""
	}
	if words[0] == PageRender || words[0] == PageSubmit {
		refusePageWords(words, fnName)
	}

	p := routeParser{name: fnName, words: words, i: 1, isServe: method == routing.SERVE, joint: "-"}
	if style == PathStyleAsWritten {
		p.joint = "_"
	}

	var resources [][]string
	for {
		segments := p.resource()
		isOf := p.at(TokenOf)
		if len(segments) == 0 && (isOf || len(resources) > 0) {
			panic(errors.New(color.FmtRed("invalid route: OF in %s needs a resource or a parameter on both sides", fnName)))
		}
		resources = append(resources, segments)
		if !isOf {
			break
		}
		p.i++
	}

	version := ""
	if p.at(TokenVersion) {
		version = strings.Join(slices.DeleteFunc(slices.Clone(words[p.i+1:]), isEmptyWord), "_")
	}

	var b strings.Builder
	b.Grow(len(fnName) + 2)
	b.WriteByte('/')
	for i := len(resources) - 1; i >= 0; i-- {
		for _, segment := range resources[i] {
			b.WriteString(segment)
			b.WriteByte('/')
		}
	}

	return method, b.String(), version
}

func refusePageWords(words []string, fnName string) {
	for _, word := range words[1:] {
		reason := ""
		switch word {
		case TokenAny:
			reason = "wildcard"
		case TokenVersion:
			reason = "version"
		case TokenFile:
			reason = "file extension"
		default:
			continue
		}
		panic(errors.New(color.FmtRed("invalid route: %s in %s: a page route has no %s", word, fnName, reason)))
	}
}

type routeParser struct {
	name    string
	words   []string
	i       int
	isServe bool
	joint   string
}

type routeItem struct {
	text  string
	isAny bool
	isExt bool
}

func (p *routeParser) at(token string) bool {
	return p.i < len(p.words) && p.words[p.i] == token
}

func (p *routeParser) resource() []string {
	var items []routeItem
	for ; p.i < len(p.words); p.i++ {
		word := p.words[p.i]
		switch word {
		case TokenOf, TokenVersion:
			return p.renderItems(items)
		case TokenBy, TokenAnd:
			return append(p.renderItems(items), p.params()...)
		case TokenAny:
			if len(items) > 0 && items[len(items)-1].isAny {
				panic(errors.New(color.FmtRed("invalid route: ANY twice in a row in %s; one ANY already matches any text", p.name)))
			}
			items = append(items, routeItem{isAny: true})
		case TokenFile:
			if len(items) < 2 {
				panic(errors.New(color.FmtRed("invalid route: FILE in %s needs a name and an extension before it", p.name)))
			}
			if slices.ContainsFunc(items, func(item routeItem) bool { return item.isExt }) {
				panic(errors.New(color.FmtRed("invalid route: FILE twice in one resource of %s", p.name)))
			}
			if items[len(items)-1].isAny {
				panic(errors.New(color.FmtRed("invalid route: FILE in %s needs a word as the extension, not ANY", p.name)))
			}
			last := &items[len(items)-1]
			last.isExt = true
			last.text = strings.ToLower(last.text)
		case "":
		default:
			checkLowerWord(word, p.name)
			items = append(items, routeItem{text: word})
		}
	}
	return p.renderItems(items)
}

func (p *routeParser) params() []string {
	var segments []string
	for p.at(TokenBy) || p.at(TokenAnd) {
		keyword := p.words[p.i]
		p.i++
		start := p.i
		for p.i < len(p.words) && !isRouteWord(p.words[p.i]) {
			p.i++
		}
		param := strings.Join(p.words[start:p.i], "_")
		if strings.Trim(param, "_") == "" {
			panic(errors.New(color.FmtRed("invalid route: %s in %s has no parameter name", keyword, p.name)))
		}
		segments = append(segments, "{"+param+"}")
	}
	if p.at(TokenAny) {
		segments = append(segments, "*")
		p.i++
	}
	for p.i < len(p.words) && p.words[p.i] == "" {
		p.i++
	}
	if p.i < len(p.words) && !p.at(TokenOf) && !p.at(TokenVersion) {
		panic(errors.New(color.FmtRed("invalid route: %s in %s comes after a parameter; start the next resource with OF", p.words[p.i], p.name)))
	}
	return segments
}

func (p *routeParser) renderItems(items []routeItem) []string {
	if len(items) == 0 {
		return nil
	}
	last := len(items) - 1
	if p.isServe && last > 0 && items[last].isAny && !items[last].isExt {
		return []string{p.join(items[:last]), "*"}
	}
	return []string{p.join(items)}
}

func (p *routeParser) join(items []routeItem) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			if item.isExt {
				b.WriteByte('.')
			} else if !item.isAny && !items[i-1].isAny {
				b.WriteString(p.joint)
			}
		}
		if item.isAny {
			b.WriteByte('*')
		} else {
			b.WriteString(item.text)
		}
	}
	return b.String()
}

func isRouteWord(word string) bool {
	_, ok := HTTPTokenMap[word]
	return ok
}

func isEmptyWord(word string) bool {
	return word == ""
}

func checkLowerWord(word, fnName string) {
	hasUpper := false
	for _, r := range word {
		if unicode.IsLower(r) {
			return
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
	}
	if hasUpper {
		panic(errors.New(color.FmtRed("invalid route: %s in %s is upper case but not a route word; write it in lower case or camelCase", word, fnName)))
	}
}

func ParseWSFuncNameToEvent(fnName string) (string, bool) {
	op, rest, found := strings.Cut(fnName, "_")
	if !found || rest == "" {
		return "", false
	}
	if _, ok := WSOperations[op]; !ok {
		return "", false
	}
	words := strings.FieldsFunc(rest, func(r rune) bool { return r == '_' })
	if len(words) == 0 {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(rest) + 1)
	for i, word := range words {
		if i > 0 {
			b.WriteByte('.')
		}
		switch word {
		case TokenAll:
			if i != len(words)-1 {
				panic(errors.New(color.FmtRed("invalid route: ALL in %s must be the last word", fnName)))
			}
			b.WriteString("**")
		case TokenAny:
			b.WriteByte('*')
		default:
			checkLowerWord(word, fnName)
			b.WriteString(strings.ToLower(word))
		}
	}
	return b.String(), true
}

func ToWSEventName(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "/"), "/")
}

func httpHandlerTargets(r *HTTP, handlers []any, category string, layerType reflect.Type) handlerTargets {
	return resolveHandlerTargets(handlers, category, layerType, func(fnName string) (string, bool) {
		pattern, ok := r.funcNameToPatternMap[fnName]
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
		method, _, _ := ParseFuncNameToURL(fnName, PathStyleKebab)
		return method != ""
	})
}

var accessDeniedException = exception.ForbiddenException("Access denied")
