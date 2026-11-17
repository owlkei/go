package ctx

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func newResponseContext(target string) (*HTTPContext, *httptest.ResponseRecorder) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	c.ResponseWriter = w
	return c, w
}

func jsonOK(t *testing.T, c *HTTPContext, data any, args ...any) {
	t.Helper()
	noPanic(t, "JSON", func() { c.JSON(data, args...) })
}

func jsonpOK(t *testing.T, c *HTTPContext, data any, args ...any) {
	t.Helper()
	noPanic(t, "JSONP", func() { c.JSONP(data, args...) })
}

func callbackTarget(callback string) string {
	return "/?callback=" + url.QueryEscape(callback)
}

type jsonStructDTO struct {
	Name string   `json:"name"`
	Age  int      `json:"age"`
	Tags []string `json:"tags"`
	Opt  *string  `json:"opt,omitempty"`
}

type jsonAppenderValue struct {
	body string
}

func (a jsonAppenderValue) AppendJSON(dst []byte) []byte {
	return append(dst, a.body...)
}

type jsonAppenderAndMarshaler struct{}

func (jsonAppenderAndMarshaler) AppendJSON(dst []byte) []byte {
	return append(dst, `"appender"`...)
}

func (jsonAppenderAndMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"marshaler"`), nil
}

var errMarshalFailed = errors.New("marshal failed")

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errMarshalFailed
}

func TestHTTPContext_Text(t *testing.T) {
	c, w := newResponseContext("/")

	c.Text("hello world")

	if w.Body.String() != "hello world" {
		t.Error(test.DiffMessage(w.Body.String(), "hello world", "B29: Text should write the data as is"))
	}
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B29: Text should write the context's status code"))
	}
	if got := w.Header()["Content-Type"]; len(got) != 1 || got[0] != "text/plain; charset=utf-8" {
		t.Error(test.DiffMessage(got, []string{"text/plain; charset=utf-8"}, "B29: Text should set exactly one Content-Type value"))
	}
}

func TestHTTPContext_TextStatusFromCode(t *testing.T) {
	c, w := newResponseContext("/")
	c.Status(http.StatusAccepted).Text("queued")
	if w.Code != http.StatusAccepted {
		t.Error(test.DiffMessage(w.Code, http.StatusAccepted, "B29: Text should write HTTPCode(c)"))
	}
}

func TestHTTPContext_TextPercentWithoutArgs(t *testing.T) {
	c, w := newResponseContext("/")
	c.Text("100% sure %s %d")
	if w.Body.String() != "100% sure %s %d" {
		t.Error(test.DiffMessage(w.Body.String(), "100% sure %s %d", "B29: Text without args should not format"))
	}
}

func TestHTTPContext_TextWithArgs(t *testing.T) {
	c, w := newResponseContext("/")
	c.Text("hello %s, you are %d", "joe", 30)
	if w.Body.String() != "hello joe, you are 30" {
		t.Error(test.DiffMessage(w.Body.String(), "hello joe, you are 30", "B29: Text with args should format them"))
	}
}

func TestHTTPContext_TextReplacesPresetContentType(t *testing.T) {
	c, w := newResponseContext("/")
	w.Header().Add("Content-Type", "text/html")
	c.Text("x")
	if got := w.Header()["Content-Type"]; len(got) != 1 || got[0] != "text/plain; charset=utf-8" {
		t.Error(test.DiffMessage(got, []string{"text/plain; charset=utf-8"}, "B32: Content-Type should hold exactly one value"))
	}
}

func TestHTTPContext_JSON(t *testing.T) {
	str := "x"
	payloads := map[string]any{
		"map":         Map{"foo": "bar", "n": 1},
		"plain map":   map[string]any{"foo": "bar", "n": 1.5},
		"map pointer": &Map{"foo": "bar"},
		"struct":      jsonStructDTO{Name: "joe", Age: 3, Tags: []string{"a"}},
		"pointer":     &jsonStructDTO{Name: "joe", Opt: &str},
		"slice":       []any{1, "two", nil, true},
		"int slice":   []int{1, 2, 3},
		"nil":         nil,
		"number":      42,
		"bool":        true,
		"nil map":     Map(nil),
		"empty slice": []string{},
	}
	for name, data := range payloads {
		c, w := newResponseContext("/")
		expected, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}

		jsonOK(t, c, data)

		if w.Body.String() != string(expected) {
			t.Error(test.DiffMessage(w.Body.String(), string(expected), "B30: JSON of "+name+" should equal json.Marshal"))
		}
		if got := w.Header()["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
			t.Error(test.DiffMessage(got, []string{"application/json"}, "B30: JSON of "+name+" should set exactly one Content-Type value"))
		}
		if w.Code != http.StatusOK {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "B30: JSON of "+name+" should write HTTPCode(c)"))
		}
	}
}

func TestHTTPContext_JSONStatusFromCode(t *testing.T) {
	c, w := newResponseContext("/")
	c.Status(http.StatusCreated)
	jsonOK(t, c, Map{"ok": true})
	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B30: JSON should write HTTPCode(c)"))
	}
}

func TestHTTPContext_JSONNoTrailingNewline(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, Map{"a": 1})
	if strings.HasSuffix(w.Body.String(), "\n") {
		t.Error(test.DiffMessage(w.Body.String(), `{"a":1}`, "B30: JSON should not end with a newline"))
	}
}

func TestHTTPContext_JSONPointerEqualsValue(t *testing.T) {
	m := Map{"b": 1, "a": []any{"x", 2.5, nil}, "c": Map{"d": "<&>"}}
	plain := map[string]any(m)

	bodies := make([]string, 0, 4)
	for _, data := range []any{m, &m, plain, &plain} {
		c, w := newResponseContext("/")
		jsonOK(t, c, data)
		bodies = append(bodies, w.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Error(test.DiffMessage(bodies[i], bodies[0], "P4: a Map or map[string]any by pointer and by value should give the same bytes"))
		}
	}
}

func TestHTTPContext_JSONAppender(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, jsonAppenderValue{body: `{"k":"v"}`})
	if w.Body.String() != `{"k":"v"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"k":"v"}`, "B30: JSON of a JSONAppender should write what AppendJSON appends"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B30: JSON of a JSONAppender should set the Content-Type"))
	}
}

func TestHTTPContext_JSONAppenderWinsOverMarshaler(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, jsonAppenderAndMarshaler{})
	if w.Body.String() != `"appender"` {
		t.Error(test.DiffMessage(w.Body.String(), `"appender"`, "B30: a JSONAppender should be used before json.Marshaler"))
	}
}

func TestHTTPContext_JSONAppenderWithStatus(t *testing.T) {
	c, w := newResponseContext("/")
	c.Status(http.StatusBadRequest)
	jsonOK(t, c, jsonAppenderValue{body: `{"error":"x"}`})
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"x"}` {
		t.Error(test.DiffMessage([2]any{w.Code, w.Body.String()}, [2]any{http.StatusBadRequest, `{"error":"x"}`}, "B30: JSON of a JSONAppender should write HTTPCode(c)"))
	}
}

func TestHTTPContext_JSONRawStringWithPercent(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, `{"rate":"100%"}`)
	if w.Body.String() != `{"rate":"100%"}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"rate":"100%"}`, "B15: a raw JSON string without args should be used as is"))
	}
}

func TestHTTPContext_JSONRawStringWithArgs(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, `{"greeting":"%s","n":%d}`, "hi", 3)
	if w.Body.String() != `{"greeting":"hi","n":3}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"greeting":"hi","n":3}`, "B15: a raw JSON string with args should be formatted"))
	}
}

func TestHTTPContext_JSONRawStringIsCompacted(t *testing.T) {
	c, w := newResponseContext("/")
	jsonOK(t, c, "{ \"a\" : 1,\n \"b\" : [ 1, 2 ] }")
	if w.Body.String() != `{"a":1,"b":[1,2]}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"a":1,"b":[1,2]}`, "B30: a raw JSON string should be validated and compacted"))
	}
}

func TestHTTPContext_JSONPanicWritesNothing(t *testing.T) {
	cases := map[string]struct {
		data  any
		args  []any
		cause func(error) bool
	}{
		"channel": {data: make(chan int), cause: func(err error) bool {
			var e *json.UnsupportedTypeError
			return errors.As(err, &e)
		}},
		"invalid raw string": {data: `{not json`},
		"failing marshaler":  {data: failingMarshaler{}, cause: func(err error) bool { return errors.Is(err, errMarshalFailed) }},
		"nested channel":     {data: Map{"a": make(chan int)}},
	}
	for name, tc := range cases {
		c, w := newResponseContext("/")
		c.Status(http.StatusTeapot)

		exc, err := panicException(t, func() { c.JSON(tc.data, tc.args...) })

		if exc.GetCode() != http.StatusInternalServerError {
			t.Error(test.DiffMessage(exc.GetCode(), http.StatusInternalServerError, "B30: "+name+" should panic with an exception of code 500"))
		}
		if tc.cause != nil && !tc.cause(err) {
			t.Error(test.DiffMessage(err, "<chain holding the encoding/json error>", "B30: "+name+" should keep the encoding error in the chain"))
		}
		if len(w.Header()) != 0 {
			t.Error(test.DiffMessage(w.Header(), http.Header{}, "B30: "+name+" should write no header"))
		}
		if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Flushed {
			t.Error(test.DiffMessage([3]any{w.Code, w.Body.Len(), w.Flushed}, [3]any{http.StatusOK, 0, false}, "B30: "+name+" should write no status and no byte"))
		}
	}
}

func TestHTTPContext_JSONAfterFailedJSONStillWorks(t *testing.T) {
	c, w := newResponseContext("/")
	_ = panicError(t, func() { c.JSON(make(chan int)) })

	jsonOK(t, c, Map{"a": 1})

	if w.Body.String() != `{"a":1}` {
		t.Error(test.DiffMessage(w.Body.String(), `{"a":1}`, "B30: JSON should work after a failed JSON on the same context"))
	}
}

func TestHTTPContext_JSONReplacesPresetContentType(t *testing.T) {
	c, w := newResponseContext("/")
	w.Header().Add("Content-Type", "text/html")
	jsonOK(t, c, Map{"a": 1})
	if got := w.Header()["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Error(test.DiffMessage(got, []string{"application/json"}, "B32: Content-Type should hold exactly one value"))
	}
}

func TestHTTPContext_ContentTypeSharedValueNotMutated(t *testing.T) {
	writers := []func(c *HTTPContext){
		func(c *HTTPContext) { jsonOK(t, c, Map{"a": 1}) },
		func(c *HTTPContext) { c.Text("x") },
		func(c *HTTPContext) { jsonpOK(t, c, Map{"a": 1}) },
	}
	expected := []string{"application/json", "text/plain; charset=utf-8", "text/javascript; charset=utf-8"}
	for i, write := range writers {
		c1, w1 := newResponseContext("/?callback=cb")
		write(c1)
		w1.Header()["Content-Type"][0] = "evil/one"
		w1.Header().Add("Content-Type", "evil/two")
		w1.Header().Set("Content-Type", "evil/three")

		c2, w2 := newResponseContext("/?callback=cb")
		write(c2)

		if got := w2.Header()["Content-Type"]; len(got) != 1 || got[0] != expected[i] {
			t.Error(test.DiffMessage(got, []string{expected[i]}, "B32: a later change to one response's Content-Type must not reach another response"))
		}
	}
}

func TestHTTPContext_JSONP(t *testing.T) {
	c, w := newResponseContext("/?callback=cb")

	jsonpOK(t, c, map[string]any{"foo": "bar"})

	expected := `/**/ typeof cb === 'function' && cb({"foo":"bar"});`
	if w.Body.String() != expected {
		t.Error(test.DiffMessage(w.Body.String(), expected, "B31: JSONP should wrap the JSON body exactly"))
	}
	if got := w.Header()["Content-Type"]; len(got) != 1 || got[0] != "text/javascript; charset=utf-8" {
		t.Error(test.DiffMessage(got, []string{"text/javascript; charset=utf-8"}, "B31: JSONP should set the JavaScript Content-Type"))
	}
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B31: JSONP should write HTTPCode(c)"))
	}
}

func TestHTTPContext_JSONPStatusAndRawString(t *testing.T) {
	c, w := newResponseContext("/?callback=cb")
	c.Status(http.StatusCreated)
	jsonpOK(t, c, `{"rate":"100%"}`)
	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B31: JSONP should write HTTPCode(c)"))
	}
	expected := `/**/ typeof cb === 'function' && cb({"rate":"100%"});`
	if w.Body.String() != expected {
		t.Error(test.DiffMessage(w.Body.String(), expected, "B31: JSONP of a raw string should be B30's body"))
	}
}

func TestHTTPContext_JSONPNoSniff(t *testing.T) {
	c, w := newResponseContext("/?callback=cb")
	jsonpOK(t, c, Map{"a": 1})
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Error(test.DiffMessage(got, "nosniff", "B17: JSONP should carry X-Content-Type-Options: nosniff"))
	}
}

func TestHTTPContext_JSONPSanitizesCallback(t *testing.T) {
	cases := []struct {
		name, callback, expected string
	}{
		{"call", "alert(1)//", "alert1"},
		{"semicolon", "a;b", "ab"},
		{"tags", "<script>", "script"},
		{"cookie", "alert(document.cookie)//", "alertdocument.cookie"},
		{"spaces and quotes", `a b"c'd`, "abcd"},
		{"allowed set", "jQuery_1.$cb[0]", "jQuery_1.$cb[0]"},
		{"newline", "a\r\nb", "ab"},
		{"too long", strings.Repeat("a", 500), strings.Repeat("a", 128)},
		{"long after cleaning", strings.Repeat("a;", 300), strings.Repeat("a", 128)},
	}
	for _, tc := range cases {
		c, w := newResponseContext(callbackTarget(tc.callback))

		jsonpOK(t, c, Map{"a": 1})

		expected := "/**/ typeof " + tc.expected + " === 'function' && " + tc.expected + `({"a":1});`
		if w.Body.String() != expected {
			t.Error(test.DiffMessage(w.Body.String(), expected, "B17: JSONP should reduce the callback "+tc.name+" to identifier characters and 128 bytes"))
		}
	}
}

func TestHTTPContext_JSONPCallbackOnlyInvalidCharsFallsBackToJSON(t *testing.T) {
	for _, callback := range []string{"();//", "<>", " ", `"'`, "\n"} {
		c, w := newResponseContext(callbackTarget(callback))

		jsonpOK(t, c, Map{"a": 1})

		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Error(test.DiffMessage(got, "application/json", "B17: callback "+callback+" leaves nothing, so the response should be plain JSON"))
		}
		if w.Body.String() != `{"a":1}` {
			t.Error(test.DiffMessage(w.Body.String(), `{"a":1}`, "B17: a callback with nothing left should give B30's body"))
		}
	}
}

func TestHTTPContext_JSONPReadsCachedQuery(t *testing.T) {
	c, w := newResponseContext("/?callback=cb")
	c.Query().Set("callback", "changed")

	jsonpOK(t, c, Map{"a": 1})

	if !strings.Contains(w.Body.String(), "changed(") {
		t.Error(test.DiffMessage(w.Body.String(), "<body calling changed>", "B31: the callback should be read from the cached c.Query(), not parsed again"))
	}
}

func TestHTTPContext_JSONPEscapesLineSeparators(t *testing.T) {
	ls, ps := string(rune(0x2028)), string(rune(0x2029))
	text := "a" + ls + "b" + ps + "c<d>e&f"
	values := map[string]any{
		"encoded value": Map{"k": text},
		"raw string":    `{"k":"` + text + `"}`,
	}
	bs := string(rune(92))
	escapes := []string{bs + "u2028", bs + "u2029", bs + "u003c", bs + "u003e", bs + "u0026"}
	for name, data := range values {
		var marshaled []byte
		if s, ok := data.(string); ok {
			marshaled, _ = json.Marshal(json.RawMessage(s))
		} else {
			marshaled, _ = json.Marshal(data)
		}

		c, w := newResponseContext("/?callback=cb")
		jsonpOK(t, c, data)

		body := w.Body.String()
		expected := "/**/ typeof cb === 'function' && cb(" + string(marshaled) + ");"
		if body != expected {
			t.Error(test.DiffMessage(body, expected, "B31: JSONP of a "+name+" should wrap json.Marshal's bytes"))
		}
		for _, want := range escapes {
			if !strings.Contains(body, want) {
				t.Error(test.DiffMessage(body, want, "B31: JSONP of a "+name+" should keep "+want+" escaped"))
			}
		}
		for _, raw := range []string{ls, ps} {
			if strings.Contains(body, raw) {
				t.Error(test.DiffMessage(body, "<no literal U+2028 or U+2029>", "B31: JSONP of a "+name+" should hold no literal line separator"))
			}
		}
	}
}

func TestHTTPContext_JSONPPanicWritesNothing(t *testing.T) {
	c, w := newResponseContext("/?callback=cb")
	exc, _ := panicException(t, func() { c.JSONP(make(chan int)) })
	if exc.GetCode() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(exc.GetCode(), http.StatusInternalServerError, "B30: JSONP should panic with an exception of code 500"))
	}
	if len(w.Header()) != 0 || w.Body.Len() != 0 {
		t.Error(test.DiffMessage([2]any{w.Header(), w.Body.Len()}, [2]any{http.Header{}, 0}, "B30: a failed JSONP should write nothing"))
	}
}

type randomPayloadStruct struct {
	A string         `json:"a"`
	B int            `json:"b"`
	C []any          `json:"c"`
	D map[string]any `json:"d"`
	E *string        `json:"e"`
	F float64        `json:"f,omitempty"`
}

func randomJSONString(r *rand.Rand) string {
	pieces := []string{"a", "b", "<", ">", "&", " ", " ", `"`, `\`, "\n", "\t", "\x00", "\xff", "\xc3", "é", "日本", "%s", "%", " ", "/", "\u007f"}
	var sb strings.Builder
	for i, n := 0, r.IntN(7); i < n; i++ {
		sb.WriteString(pieces[r.IntN(len(pieces))])
	}
	return sb.String()
}

func randomJSONValue(r *rand.Rand, depth int) any {
	kinds := 8
	if depth <= 0 {
		kinds = 5
	}
	switch r.IntN(kinds) {
	case 0:
		return nil
	case 1:
		return r.IntN(2) == 0
	case 2:
		return float64(r.IntN(2000) - 1000)
	case 3:
		return r.NormFloat64() * 1e7
	case 4:
		return randomJSONString(r)
	case 5:
		n := r.IntN(4)
		arr := make([]any, n)
		for i := range arr {
			arr[i] = randomJSONValue(r, depth-1)
		}
		return arr
	case 6:
		n := r.IntN(4)
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			m[randomJSONString(r)] = randomJSONValue(r, depth-1)
		}
		return m
	}
	n := r.IntN(4)
	m := make(Map, n)
	for i := 0; i < n; i++ {
		m[randomJSONString(r)] = randomJSONValue(r, depth-1)
	}
	return m
}

func randomJSONPayload(r *rand.Rand) any {
	asMap := func() map[string]any {
		n := r.IntN(5)
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			m[randomJSONString(r)] = randomJSONValue(r, 3)
		}
		return m
	}
	asStruct := func() randomPayloadStruct {
		s := randomPayloadStruct{A: randomJSONString(r), B: r.IntN(100), F: r.NormFloat64()}
		s.C, _ = randomJSONValue(r, 2).([]any)
		s.D, _ = randomJSONValue(r, 2).(map[string]any)
		if r.IntN(2) == 0 {
			e := randomJSONString(r)
			s.E = &e
		}
		return s
	}
	switch r.IntN(9) {
	case 0:
		return Map(asMap())
	case 1:
		m := Map(asMap())
		return &m
	case 2:
		return asMap()
	case 3:
		m := asMap()
		return &m
	case 4:
		return asStruct()
	case 5:
		s := asStruct()
		return &s
	case 6:
		return []any{randomJSONValue(r, 3), randomJSONValue(r, 3)}
	case 7:
		return []randomPayloadStruct{asStruct(), asStruct()}
	}
	return []string{randomJSONString(r), randomJSONString(r)}
}

func TestHTTPContext_JSONEqualsMarshal_RandomPayloads(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 9))
	for i := 0; i < 1000; i++ {
		data := randomJSONPayload(r)
		expected, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}

		c, w := newResponseContext("/")
		jsonOK(t, c, data)

		if w.Body.String() != string(expected) {
			t.Error(test.DiffMessage(w.Body.String(), string(expected), "P3, P4: JSON of payload "+strconv.Itoa(i)+" should equal json.Marshal byte for byte"))
			return
		}
	}
}

func TestHTTPContext_JSONPEqualsMarshal_RandomPayloads(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 13))
	for i := 0; i < 300; i++ {
		data := randomJSONPayload(r)
		expected, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}

		c, w := newResponseContext("/?callback=cb")
		jsonpOK(t, c, data)

		want := "/**/ typeof cb === 'function' && cb(" + string(expected) + ");"
		if w.Body.String() != want {
			t.Error(test.DiffMessage(w.Body.String(), want, "P3, P4: JSONP body should wrap json.Marshal's bytes"))
			return
		}
	}
}

func TestHTTPContext_JSONNilAppenderPointerWritesNull(t *testing.T) {
	c, w := newResponseContext("/")
	var nilAppender *jsonAppenderValue
	jsonOK(t, c, nilAppender)
	if w.Body.String() != "null" {
		t.Error(test.DiffMessage(w.Body.String(), "null", "JSON of a nil pointer to a JSONAppender writes null instead of panicking with runtime text"))
	}
}
