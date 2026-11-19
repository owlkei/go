package routing

import (
	"strings"

	"go.owlky.com/owlky/internal/str"
)

var patternByMethod = func() map[string]string {
	m := make(map[string]string, len(HTTPMethods))
	for _, method := range HTTPMethods {
		m[method] = toPattern(method, "[", "]")
	}
	return m
}()

func PatternToMethodRouteVersion(pattern string) (string, string, string) {
	if !strings.HasSuffix(pattern, "]/") {
		return "", "", ""
	}
	rest := pattern[:len(pattern)-2]

	methodStart := strings.LastIndexByte(rest, '[')
	if methodStart < 1 || rest[methodStart-1] != '/' {
		return "", "", ""
	}
	method := rest[methodStart+1:]
	if strings.ContainsAny(method, "/]") {
		return "", "", ""
	}
	rest = rest[:methodStart-1]

	if !strings.HasSuffix(rest, "|") {
		return "", "", ""
	}
	rest = rest[:len(rest)-1]
	versionStart := strings.LastIndexByte(rest, '|')
	if versionStart < 0 {
		return "", "", ""
	}
	version := rest[versionStart+1:]
	if strings.IndexByte(version, '/') >= 0 {
		return "", "", ""
	}

	route := rest[:versionStart]
	if len(route) == 0 || route[0] != '/' || route[len(route)-1] != '/' {
		return "", "", ""
	}

	return method, route[:len(route)-1], version
}

func MethodRouteVersionToPattern(
	method,
	route,
	version string,
) string {
	routePattern := str.Enclose(route, '/')
	if routePattern == "" {
		routePattern = "/"
	}
	versionPattern := toPattern(version, "|", "|")
	methodPattern, ok := patternByMethod[method]
	if !ok {
		methodPattern = toPattern(method, "[", "]")
	}

	var pattern strings.Builder
	pattern.Grow(len(routePattern) + len(versionPattern) + len(methodPattern) + 2)
	pattern.WriteString(routePattern)
	pattern.WriteString(versionPattern)
	pattern.WriteByte('/')
	pattern.WriteString(methodPattern)
	pattern.WriteByte('/')

	return pattern.String()
}

func ParseToParamKey(str string) (string, map[string][]int) {
	if strings.IndexByte(str, '{') < 0 {
		return str, nil
	}

	var paramKey map[string][]int
	var b strings.Builder
	b.Grow(len(str))
	prev := 0
	index := 0
	for {
		open := strings.IndexByte(str[prev:], '{')
		if open < 0 {
			break
		}
		open += prev
		closing := strings.IndexByte(str[open+1:], '}')
		if closing < 0 {
			break
		}
		closing += open + 1

		if paramKey == nil {
			paramKey = make(map[string][]int)
		}
		name := str[open+1 : closing]
		paramKey[name] = append(paramKey[name], index)
		index++

		b.WriteString(str[prev:open])
		b.WriteByte('$')
		prev = closing + 1
	}

	if paramKey == nil {
		return str, nil
	}
	b.WriteString(str[prev:])
	return b.String(), paramKey
}

func toPattern(s, l, r string) string {
	s = strings.TrimSpace(s)

	if s == "" {
		return l + r
	}

	hasL := strings.HasPrefix(s, l)
	hasR := strings.HasSuffix(s, r)
	if hasL && hasR {
		return s
	}
	if !hasL && !hasR {
		return l + s + r
	}
	if !hasL {
		return l + s
	}
	return s + r
}
