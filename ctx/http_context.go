package ctx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/exception"
)

type JSONAppender interface {
	AppendJSON(dst []byte) []byte
}

type HTTPContext struct {
	*http.Request
	http.ResponseWriter

	contextID

	body         Body
	form         Form
	file         File
	query        Query
	header       Header
	param        Param
	aggregations []*aggregation.Aggregation
	ParamKeys    map[string][]int
	ParamValues  []string

	Next      Next
	Code      int
	Timestamp time.Time
}

const (
	textPlainUTF8      = "text/plain; charset=utf-8"
	textJavaScriptUTF8 = "text/javascript; charset=utf-8"
	jsonpCallbackLimit = 128
)

func NewHTTPContext() *HTTPContext {
	return &HTTPContext{
		Code: http.StatusOK,
	}
}

func (c *HTTPContext) Init(w http.ResponseWriter, r *http.Request) {
	c.Timestamp = time.Now()
	c.ResponseWriter = w
	c.Request = r
	c.SetID()
}

func (c *HTTPContext) Reset() {
	*c = HTTPContext{Code: http.StatusOK}
}

func (c *HTTPContext) AddAggregation(a *aggregation.Aggregation) {
	c.aggregations = append(c.aggregations, a)
}

func (c *HTTPContext) Aggregations() []*aggregation.Aggregation {
	return c.aggregations
}

func (c *HTTPContext) Status(code int) *HTTPContext {
	c.Code = code
	return c
}

func (c *HTTPContext) Text(data string, args ...any) {
	c.ResponseWriter.Header()["Content-Type"] = []string{textPlainUTF8}
	c.ResponseWriter.WriteHeader(c.Code)
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
	c.ResponseWriter.WriteHeader(c.Code)
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
	c.ResponseWriter.WriteHeader(c.Code)
	_, _ = c.ResponseWriter.Write(out)
}

func (c *HTTPContext) Redirect(url string) {
	if c.Code < http.StatusMultipleChoices || c.Code >= http.StatusBadRequest {
		c.Code = http.StatusMovedPermanently
	}
	http.Redirect(c.ResponseWriter, c.Request, url, c.Code)
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
