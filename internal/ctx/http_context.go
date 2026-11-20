package ctx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/aggregation"
	"go.owlkei.com/owlkei/internal/tracecontext"
)

type JSONAppender interface {
	AppendJSON(dst []byte) []byte
}

type HTTPContext struct {
	*http.Request
	http.ResponseWriter

	scope *tracecontext.Scope

	body         Body
	form         Form
	file         File
	query        Query
	header       Header
	param        Param
	aggregations []*aggregation.Aggregation
	paramKeys    map[string][]int
	paramValues  []string

	next      Next
	code      int
	timestamp time.Time
	isPage    bool

	onRedirectRefused func(c *HTTPContext, target, note string)
}

const (
	textPlainUTF8      = "text/plain; charset=utf-8"
	textJavaScriptUTF8 = "text/javascript; charset=utf-8"
	jsonpCallbackLimit = 128
)

func NewHTTPContext() *HTTPContext {
	return &HTTPContext{
		code: http.StatusOK,
	}
}

func HTTPInit(c *HTTPContext, w http.ResponseWriter, r *http.Request) {
	c.timestamp = time.Now()
	c.ResponseWriter = w
	scopeContext, scope := tracecontext.NewRequest(r.Context(), r.Header, c.timestamp)
	c.scope = scope
	c.Request = r.WithContext(scopeContext)
}

func (c *HTTPContext) ID() string {
	if c.scope == nil {
		return ""
	}
	return c.scope.Request
}

func HTTPReset(c *HTTPContext) {
	paramValues := keepCapacity(c.paramValues)
	aggregations := keepCapacity(c.aggregations)
	*c = HTTPContext{code: http.StatusOK, paramValues: paramValues, aggregations: aggregations, onRedirectRefused: c.onRedirectRefused}
}

func HTTPSetRedirectRefused(c *HTTPContext, fn func(c *HTTPContext, target, note string)) {
	c.onRedirectRefused = fn
}

const keptCapacityLimit = 64

func keepCapacity[T any](values []T) []T {
	if cap(values) > keptCapacityLimit {
		return nil
	}
	clear(values[:cap(values)])
	return values[:0]
}

func HTTPAddAggregation(c *HTTPContext, a *aggregation.Aggregation) {
	c.aggregations = append(c.aggregations, a)
}

func HTTPAggregations(c *HTTPContext) []*aggregation.Aggregation {
	return c.aggregations
}

func HTTPSetParams(c *HTTPContext, keys map[string][]int, values []string) {
	c.paramKeys = keys
	c.paramValues = values
}

func HTTPParamValues(c *HTTPContext) []string {
	return c.paramValues
}

func HTTPNext(c *HTTPContext) Next {
	return c.next
}

func HTTPSetNext(c *HTTPContext, next Next) {
	c.next = next
}

func HTTPCode(c *HTTPContext) int {
	return c.code
}

func HTTPSetCode(c *HTTPContext, code int) {
	c.code = code
}

func HTTPSetPage(c *HTTPContext) {
	c.isPage = true
}

func HTTPIsPage(c *HTTPContext) bool {
	return c.isPage
}

func HTTPTimestamp(c *HTTPContext) *time.Time {
	return &c.timestamp
}

func (c *HTTPContext) Status(code int) *HTTPContext {
	c.code = code
	return c
}

func (c *HTTPContext) Text(data string, args ...any) {
	c.ResponseWriter.Header()["Content-Type"] = []string{textPlainUTF8}
	c.ResponseWriter.WriteHeader(c.code)
	writeText(c.ResponseWriter, data, args)
}

func writeText(w io.Writer, data string, args []any) {
	if len(args) == 0 {
		_, _ = io.WriteString(w, data)
		return
	}
	_, _ = fmt.Fprintf(w, data, args...)
}

func (c *HTTPContext) JSON(data any, args ...any) {
	body := encodeJSON(data, args)
	c.ResponseWriter.Header()["Content-Type"] = []string{applicationJSON}
	c.ResponseWriter.WriteHeader(c.code)
	_, _ = c.ResponseWriter.Write(body)
}

func (c *HTTPContext) JSONP(data any, args ...any) {
	callback := sanitizeCallback(c.Query().Get("callback"))
	if callback == "" {
		c.JSON(data, args...)
		return
	}

	body := encodeJSON(data, args)
	out := make([]byte, 0, len(body)+2*len(callback)+40)
	out = append(out, "/**/ typeof "...)
	out = append(out, callback...)
	out = append(out, " === 'function' && "...)
	out = append(out, callback...)
	out = append(out, '(')
	out = append(out, body...)
	out = append(out, ");"...)

	h := c.ResponseWriter.Header()
	h["Content-Type"] = []string{textJavaScriptUTF8}
	h["X-Content-Type-Options"] = []string{"nosniff"}
	c.ResponseWriter.WriteHeader(c.code)
	_, _ = c.ResponseWriter.Write(out)
}

const (
	redirectNote         = "c.Redirect accepts a path of this site; another site needs c.RedirectExternal"
	redirectExternalNote = "c.RedirectExternal accepts a path or an http(s) URL"
)

func (c *HTTPContext) Redirect(target string) {
	if !isLocalTarget(target) {
		c.refuseRedirect(target, redirectNote)
		return
	}
	c.redirect(target)
}

func (c *HTTPContext) RedirectExternal(target string) {
	if !isLocalTarget(target) && !isExternalTarget(target) {
		c.refuseRedirect(target, redirectExternalNote)
		return
	}
	c.redirect(target)
}

func (c *HTTPContext) refuseRedirect(target, note string) {
	if c.onRedirectRefused != nil {
		c.onRedirectRefused(c, target, note)
	}
	c.redirect("/")
}

func (c *HTTPContext) redirect(target string) {
	if c.code < http.StatusMultipleChoices || c.code >= http.StatusBadRequest {
		c.code = http.StatusMovedPermanently
		if c.isPage {
			c.code = http.StatusSeeOther
		}
	}
	http.Redirect(c.ResponseWriter, c.Request, target, c.code)
}

func isLocalTarget(target string) bool {
	if len(target) == 0 || target[0] != '/' {
		return false
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}
	return isCleanTarget(target)
}

func isCleanTarget(target string) bool {
	for i := 0; i < len(target); i++ {
		if b := target[i]; b < 0x20 || b == 0x7f || b == '\\' {
			return false
		}
	}
	return true
}

func isExternalTarget(target string) bool {
	if !isCleanTarget(target) {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

func encodeJSON(data any, args []any) []byte {
	var body []byte
	var err error
	switch v := data.(type) {
	case string:
		if len(args) > 0 {
			v = fmt.Sprintf(v, args...)
		}
		body, err = json.Marshal(json.RawMessage(v))
	case JSONAppender:
		if isNilPointer(v) {
			return []byte("null")
		}
		return v.AppendJSON(nil)
	case Map:
		body, err = json.Marshal(&v)
	case map[string]any:
		body, err = json.Marshal(&v)
	default:
		body, err = json.Marshal(data)
	}
	if err != nil {
		panic(exception.InternalServerErrorException("Cannot encode JSON response", err))
	}
	return body
}

func isNilPointer(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func sanitizeCallback(callback string) string {
	if len(callback) <= jsonpCallbackLimit && isCallbackClean(callback) {
		return callback
	}
	out := make([]byte, 0, min(len(callback), jsonpCallbackLimit))
	for i := 0; i < len(callback) && len(out) < jsonpCallbackLimit; i++ {
		if isCallbackChar(callback[i]) {
			out = append(out, callback[i])
		}
	}
	return string(out)
}

func isCallbackClean(callback string) bool {
	for i := 0; i < len(callback); i++ {
		if !isCallbackChar(callback[i]) {
			return false
		}
	}
	return true
}

func isCallbackChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '$' || c == '.' || c == '[' || c == ']'
}
