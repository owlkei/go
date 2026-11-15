package ctx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dangduoc08/ginject/exception"
)

type Body map[string]any

const (
	applicationJSON  = "application/json"
	applicationMedia = "application/"
	jsonSuffix       = "+json"
)

func (c *HTTPContext) Body() Body {
	if c.body != nil {
		return c.body
	}

	if c.Request.Body == nil || !isJSONMediaType(c.contentType()) {
		c.body = Body{}
		return c.body
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		panic(readBodyException(err))
	}

	var body Body
	if !isBlankJSON(data) {
		if err := json.Unmarshal(data, &body); err != nil {
			panic(exception.BadRequestException("Invalid JSON body", err))
		}
	}
	if body == nil {
		body = Body{}
	}
	c.body = body
	return c.body
}

func readBodyException(err error) exception.Exception {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return exception.RequestEntityTooLargeException(err.Error(), err)
	}
	return exception.BadRequestException("Cannot read request body", err)
}

func (c *HTTPContext) contentType() string {
	if v := c.Request.Header["Content-Type"]; len(v) > 0 {
		return mediaType(v[0])
	}
	return ""
}

func mediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.TrimSpace(contentType)
}

func isJSONMediaType(mt string) bool {
	if strings.EqualFold(mt, applicationJSON) {
		return true
	}
	return len(mt) > len(applicationMedia)+len(jsonSuffix) &&
		strings.EqualFold(mt[:len(applicationMedia)], applicationMedia) &&
		strings.EqualFold(mt[len(mt)-len(jsonSuffix):], jsonSuffix)
}

func isBlankJSON(data []byte) bool {
	for _, b := range data {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}
	return true
}

func (b Body) Set(k string, v any) {
	var obj map[string]any = b
	key := ""
	for path := k; path != ""; {
		var seg string
		seg, path, _ = strings.Cut(path, ".")
		if isBlank(seg) {
			continue
		}
		if key != "" {
			next, ok := obj[key].(map[string]any)
			if !ok {
				next = make(map[string]any)
				obj[key] = next
			}
			obj = next
		}
		key = seg
	}
	if key != "" {
		obj[key] = v
	}
}

func (b Body) Get(k string) any {
	if strings.IndexByte(k, '.') < 0 && !isBlank(k) {
		return b[k]
	}
	obj, key, ok := b.lookup(k)
	if !ok {
		return nil
	}
	if key == "" {
		return b
	}
	return obj[key]
}

func (b Body) Del(k string) {
	obj, key, ok := b.lookup(k)
	if ok && key != "" {
		delete(obj, key)
	}
}

func (b Body) Has(k string) bool {
	if strings.IndexByte(k, '.') < 0 && !isBlank(k) {
		_, ok := b[k]
		return ok
	}
	obj, key, ok := b.lookup(k)
	if !ok {
		return false
	}
	if key == "" {
		return true
	}
	_, ok = obj[key]
	return ok
}

func (b Body) lookup(path string) (map[string]any, string, bool) {
	var obj map[string]any = b
	key := ""
	for path != "" {
		var seg string
		seg, path, _ = strings.Cut(path, ".")
		if isBlank(seg) {
			continue
		}
		if key != "" {
			next, ok := obj[key].(map[string]any)
			if !ok {
				return nil, "", false
			}
			obj = next
		}
		key = seg
	}
	return obj, key, true
}

func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func (b Body) Bind(s any) (any, []FieldLevel) {
	binder := binderOfValue(s)
	fls := make([]FieldLevel, 0, binder.flHint)
	return binder.bindTop(b, &fls, "", ""), fls
}

func (b Body) BindValue(s any) any {
	return binderOfValue(s).bindTop(b, nil, "", "")
}
