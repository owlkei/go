package versioning

import (
	"strings"

	"github.com/dangduoc08/ginject/ctx"
)

const (
	QueryVersion = iota + 1
	HeaderVersion
	CustomVersion
	MediaType
)

const NeutralVersion = "NEUTRAL"

type ExtractorHandler = func(*ctx.HTTPContext) string

type Versioning struct {
	Type           int
	Key            string
	DefaultVersion string
	Extractor      ExtractorHandler
}

func (v *Versioning) GetTypeString() string {
	switch v.Type {
	case QueryVersion:
		return "query"
	case HeaderVersion:
		return "header"
	case CustomVersion:
		return "custom"
	case MediaType:
		return "media_type"
	default:
		return ""
	}
}

func (v *Versioning) GetVersion(c *ctx.HTTPContext) string {
	switch v.Type {
	case QueryVersion:
		if version := c.Query().Get(v.Key); version != "" {
			return version
		}
	case HeaderVersion:
		if version := c.Header().Get(v.Key); version != "" {
			return version
		}
	case CustomVersion:
		if v.Extractor != nil {
			if version := v.Extractor(c); version != "" {
				return version
			}
		}
	case MediaType:
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
