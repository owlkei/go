package ctx

import (
	"reflect"
	"strconv"
	"strings"
)

func GetTagParams(v string) []string {
	parts := strings.Split(v, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func GetTagParamIndex(v string) (int, string) {
	dotIdx := strings.IndexByte(v, '.')
	if dotIdx < 0 {
		return 0, v
	}
	bindedField := strings.TrimSpace(v[:dotIdx])
	parsedInt, err := strconv.Atoi(strings.TrimSpace(v[dotIdx+1:]))
	if err == nil && parsedInt > -1 {
		return parsedInt, bindedField
	}
	return 0, bindedField
}

var stringType = reflect.TypeFor[string]()

const applicationJSON = "application/json"

const multipartFormData = "multipart/form-data"
