package versioning

import (
	"strings"

	"go.owlky.com/owlky/internal/ctx"
)

type Type int

const (
	TypeQuery Type = iota + 1
	TypeHeader
	TypeCustom
	TypeMediaType
)

const NeutralVersion = "NEUTRAL"

type ExtractorHandler = func(*ctx.HTTPContext) string

type Options struct {
	Type           Type
	Key            string
	DefaultVersion string
	Extractor      ExtractorHandler
}

func TypeString(v *Options) string {
	switch v.Type {
	case TypeQuery:
		return "query"
	case TypeHeader:
		return "header"
	case TypeCustom:
		return "custom"
	case TypeMediaType:
		return "media_type"
	default:
		return ""
	}
}

func Version(v *Options, c *ctx.HTTPContext) string {
	switch v.Type {
	case TypeQuery:
		if version := c.Query().Get(v.Key); version != "" {
			return version
		}
	case TypeHeader:
		if version := c.Header().Get(v.Key); version != "" {
			return version
		}
	case TypeCustom:
		if v.Extractor != nil {
			if version := v.Extractor(c); version != "" {
				return version
			}
		}
	case TypeMediaType:
		if version := mediaTypeVersion(c.Header().Get("Accept"), v.Key); version != "" {
			return version
		}
	}

	return v.DefaultVersion
}

func mediaTypeVersion(accept, key string) string {
	i := 0
	for i < len(accept) {
		for i < len(accept) && accept[i] != ';' && accept[i] != ',' {
			i++
		}
		for i < len(accept) && accept[i] == ';' {
			i++
			start := i
			for i < len(accept) && accept[i] != ';' && accept[i] != ',' {
				i++
			}
			if version := paramValue(accept[start:i], key); version != "" {
				return version
			}
		}
		i++
	}

	return ""
}

func paramValue(param, key string) string {
	name, value, ok := strings.Cut(param, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), key) {
		return ""
	}

	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}
