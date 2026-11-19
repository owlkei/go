package ctx

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"go.owlky.com/owlky/internal/test"
)

func TestBodySet(t *testing.T) {
	body := Body{
		"data": map[string]any{
			"facebook":  "https://facebook.com",
			"instagram": "https://instagram.com",
		},
		"information": map[string]any{
			"fullname": "John Doe jr",
			"parent": map[string]any{
				"father": map[string]any{
					"age":      70,
					"fullname": "John Doe",
				},
			},
		},
	}

	key1 := "information.parent2.father.spouse"
	val1 := "Jane Doe 1"
	body.Set(key1, val1)
	if body.Get(key1).(string) != val1 {
		t.Errorf("%v = %v, should be %v", key1, body.Get(key1), val1)
	}

	key2 := "information2.parent.father.spouse"
	val2 := "Jane Doe 2"
	body.Set(key2, val2)
	if body.Get(key2).(string) != val2 {
		t.Errorf("%v = %v, should be %v", key2, body.Get(key2), val2)
	}

	key3 := "information.parent2.father.spouse2"
	val3 := "Jane Doe 3"
	body.Set(key3, val3)
	if body.Get(key3).(string) != val3 {
		t.Errorf("%v = %v, should be %v", key3, body.Get(key3), val3)
	}
}

func TestBodyGet(t *testing.T) {
	body := Body{
		"data": map[string]any{
			"facebook":  "https://facebook.com",
			"instagram": "https://instagram.com",
		},
		"information": map[string]any{
			"fullname": "John Doe jr",
			"parent": map[string]any{
				"father": map[string]any{
					"age":      70,
					"fullname": "John Doe",
				},
			},
		},
	}

	if body.Get("information.parent.father.age").(int) != 70 {
		t.Errorf("key information.parent.father.age = %v, should be %v", body.Get("information.parent.father.age"), 70)
	}

	if body.Get("information.children.father.fullname") != nil {
		t.Errorf("key information.children.father.fullname = %v, should be %v", body.Get("information.children.father.fullname"), nil)
	}
}

func TestBodySet_OverwritesExistingMapValue(t *testing.T) {
	b := Body{"information": map[string]any{"fullname": "John"}}
	b.Set("information", "newValue")

	if b.Get("information") != "newValue" {
		t.Error(test.DiffMessage(b.Get("information"), "newValue", "Set should overwrite a key even when its existing value is a nested map"))
	}
}

func TestBodyGet_MissingIntermediateKeyReturnsNil(t *testing.T) {
	b := Body{"y": "sneaky", "other": 1}

	if got := b.Get("x.y"); got != nil {
		t.Error(test.DiffMessage(got, nil, "Get should return nil when an intermediate path segment does not exist, not fall through to an unrelated sibling key"))
	}
}

func TestHTTPContext_BodyJSON(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"foo":"bar"}`))
	c.Request.Header.Set("Content-Type", applicationJSON)

	b := c.Body()
	if b.Get("foo") != "bar" {
		t.Error(test.DiffMessage(b.Get("foo"), "bar", "Body should parse a JSON request body"))
	}
}

func TestHTTPContext_BodyNonJSON(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader("foo=bar"))
	c.Request.Header.Set("Content-Type", applicationXWWWFormUrlencoded)

	b := c.Body()
	if len(b) != 0 {
		t.Error(test.DiffMessage(b, Body{}, "Body should be empty when the content type is not application/json"))
	}
}

func TestHTTPContext_BodyIsCached(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"foo":"bar"}`))
	c.Request.Header.Set("Content-Type", applicationJSON)

	b1 := c.Body()
	b1.Set("foo", "mutated")
	b2 := c.Body()

	if b2.Get("foo") != "mutated" {
		t.Error(test.DiffMessage(b2.Get("foo"), "mutated", "Body should be cached across calls on the same context"))
	}
}

func TestBody_Del(t *testing.T) {
	b := Body{"foo": "bar"}
	b.Del("foo")
	if b.Has("foo") {
		t.Error(test.DiffMessage(b.Has("foo"), false, "Has should be false after Del"))
	}
}

func TestBody_Has(t *testing.T) {
	b := Body{"foo": "bar"}
	if !b.Has("foo") {
		t.Error(test.DiffMessage(b.Has("foo"), true, "Has should be true for an existing key"))
	}
	if b.Has("missing") {
		t.Error(test.DiffMessage(b.Has("missing"), false, "Has should be false for a missing key"))
	}
}

func TestBody_Bind(t *testing.T) {
	b := Body{"name": "joe"}
	result, fls := b.Bind(bodyBindDTO{})

	dto, ok := result.(bodyBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, bodyBindDTO{}, "Bind should return a bodyBindDTO"))
	}
	if dto.Name != "joe" {
		t.Error(test.DiffMessage(dto.Name, "joe", "Bind should populate fields from the body map"))
	}
	if len(fls) != 1 {
		t.Error(test.DiffMessage(len(fls), 1, "Bind should report a FieldLevel per bound field"))
	}
}

func TestHTTPContext_BodyNull(t *testing.T) {
	c := newBodyContext(applicationJSON, "null")
	var b Body
	if !noPanic(t, "B8 Body of null", func() { b = c.Body() }) {
		return
	}
	if b == nil {
		t.Fatal(test.DiffMessage(b, Body{}, "B8: Body should never be nil"))
	}
	if len(b) != 0 {
		t.Error(test.DiffMessage(b, Body{}, "B8: null should give an empty Body"))
	}
	noPanic(t, "B8 Set on the Body of null", func() { b.Set("a", 1) })
}

func TestHTTPContext_BodyEmptyWithJSONContentType(t *testing.T) {
	for name, payload := range map[string]string{"empty": "", "spaces": "   ", "newlines and tabs": " \n\t\r\n "} {
		c := newBodyContext(applicationJSON, payload)
		var b Body
		if !noPanic(t, "B8 Body of an "+name+" body", func() { b = c.Body() }) {
			continue
		}
		if b == nil || len(b) != 0 {
			t.Error(test.DiffMessage(b, Body{}, "B8: an "+name+" body should give an empty, non-nil Body"))
		}
	}
}

func TestHTTPContext_BodyNullReadTwice(t *testing.T) {
	c := newBodyContext(applicationJSON, "null")
	var first, second Body
	if !noPanic(t, "B8 reading the Body of null twice", func() {
		first = c.Body()
		second = c.Body()
	}) {
		return
	}
	if first == nil || second == nil {
		t.Fatal(test.DiffMessage([2]any{first, second}, "<non-nil Bodies>", "B8: neither read should return nil"))
	}
	first.Set("k", "v")
	if second.Get("k") != "v" {
		t.Error(test.DiffMessage(second.Get("k"), "v", "B8: the second read should return the cached Body"))
	}
}

func TestHTTPContext_BodyIsReadOnce(t *testing.T) {
	reads := 0
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", io.NopCloser(readCounter{r: strings.NewReader(`{"a":1}`), n: &reads}))
	c.Request.Header.Set("Content-Type", applicationJSON)

	c.Body()
	after := reads
	c.Body()

	if reads != after {
		t.Error(test.DiffMessage(reads, after, "B8: the body should be read once and cached"))
	}
}

func TestHTTPContext_BodyMalformedIsBadRequest(t *testing.T) {
	for name, payload := range map[string]string{"garbage": "{not-json", "truncated": `{"a":`, "trailing": `{"a":1} x`, "unquoted key": "{a:1}"} {
		c := newBodyContext(applicationJSON, payload)
		exc, err := panicException(t, func() { c.Body() })
		if exc.Code() != http.StatusBadRequest {
			t.Error(test.DiffMessage(exc.Code(), http.StatusBadRequest, "B9: "+name+" should panic with code 400"))
		}
		if exc.Message() != "Invalid JSON body" {
			t.Error(test.DiffMessage(exc.Message(), "Invalid JSON body", "B9: "+name+" should have the message"))
		}
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if !errors.As(err, &syn) && !errors.As(err, &typ) {
			t.Error(test.DiffMessage(err, "<chain reaching the encoding/json error>", "B9: "+name+" should keep the encoding/json error in the chain"))
		}
	}
}

func TestHTTPContext_BodyMalformedReachesSyntaxError(t *testing.T) {
	c := newBodyContext(applicationJSON, "{not-json")
	_, err := panicException(t, func() { c.Body() })
	var syn *json.SyntaxError
	if !errors.As(err, &syn) {
		t.Error(test.DiffMessage(err, "<*json.SyntaxError in the chain>", "B9: errors.As should reach the encoding/json syntax error"))
	}
}

func TestHTTPContext_BodyTopLevelArrayIsBadRequest(t *testing.T) {
	for name, payload := range map[string]string{"array": "[1]", "string": `"x"`, "number": "1", "bool": "true"} {
		c := newBodyContext(applicationJSON, payload)
		exc, err := panicException(t, func() { c.Body() })
		if exc.Code() != http.StatusBadRequest || exc.Message() != "Invalid JSON body" {
			t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Invalid JSON body"}, "B9: a top-level "+name+" should be a bad request"))
		}
		var typ *json.UnmarshalTypeError
		if !errors.As(err, &typ) {
			t.Error(test.DiffMessage(err, "<*json.UnmarshalTypeError in the chain>", "B9: errors.As should reach the encoding/json error for a top-level "+name))
		}
	}
}

func TestHTTPContext_BodyTooLargeIs413(t *testing.T) {
	c := newBodyContext(applicationJSON, `{"a":"`+strings.Repeat("x", 100)+`"}`)
	c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 10)

	exc, err := panicException(t, func() { c.Body() })

	if exc.Code() != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(exc.Code(), http.StatusRequestEntityTooLarge, "B9: a body over the limit should panic with code 413"))
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "B9: errors.As should reach the limit error"))
	}
}

func TestHTTPContext_BodyReadErrorIsBadRequest(t *testing.T) {
	boom := errors.New("boom")
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", io.NopCloser(iotest.ErrReader(boom)))
	c.Request.Header.Set("Content-Type", applicationJSON)

	exc, err := panicException(t, func() { c.Body() })

	if exc.Code() != http.StatusBadRequest || exc.Message() != "Cannot read request body" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Cannot read request body"}, "B9: a read error should be a bad request"))
	}
	if !errors.Is(err, boom) {
		t.Error(test.DiffMessage(err, boom, "B9: the read error should stay in the chain"))
	}
}

func TestHTTPContext_BodyContentTypeCaseInsensitive(t *testing.T) {
	for _, ct := range []string{"Application/JSON", "APPLICATION/JSON; charset=UTF-8", "application/json ; charset=utf-8", "  application/json  ", "application/json;charset=utf-8"} {
		c := newBodyContext(ct, `{"foo":"bar"}`)
		if got := c.Body().Get("foo"); got != "bar" {
			t.Error(test.DiffMessage(got, "bar", "B10: Content-Type "+ct+" should be parsed as JSON"))
		}
	}
}

func TestHTTPContext_BodyPlusJSONSuffix(t *testing.T) {
	for _, ct := range []string{"application/problem+json", "application/vnd.api+json; charset=utf-8", "Application/LD+JSON"} {
		c := newBodyContext(ct, `{"foo":"bar"}`)
		if got := c.Body().Get("foo"); got != "bar" {
			t.Error(test.DiffMessage(got, "bar", "B10: Content-Type "+ct+" should be parsed as JSON"))
		}
	}
}

func TestHTTPContext_BodyContentTypeParameterIsNotMediaType(t *testing.T) {
	for _, ct := range []string{"text/plain; note=application/json", "text/plain", "application/jsonx", "application/xml", "multipart/form-data; boundary=application/json"} {
		c := newBodyContext(ct, `{"foo":"bar"}`)
		var b Body
		if !noPanic(t, "B10 Body with Content-Type "+ct, func() { b = c.Body() }) {
			continue
		}
		if b == nil || len(b) != 0 {
			t.Error(test.DiffMessage(b, Body{}, "B10: Content-Type "+ct+" is not JSON, so the Body should be empty"))
		}
	}
}

func TestBody_DelDottedPath(t *testing.T) {
	b := Body{"a": map[string]any{"b": float64(1), "c": float64(2)}, "d": "keep"}

	b.Del("a.b")

	if b.Get("a.b") != nil || b.Has("a.b") {
		t.Error(test.DiffMessage(b.Get("a.b"), nil, "B20: Del should remove the key at a dotted path"))
	}
	if b.Get("a.c") != float64(2) || b.Get("d") != "keep" {
		t.Error(test.DiffMessage(b, "<a.c and d kept>", "B20: Del should keep the other keys"))
	}
}

func TestBody_DelDeepPathAndBlankSegments(t *testing.T) {
	b := Body{"a": map[string]any{"b": map[string]any{"c": 1, "d": 2}}}
	b.Del("a. .b..c")
	if b.Has("a.b.c") || !b.Has("a.b.d") {
		t.Error(test.DiffMessage(b, "<a.b.c removed, a.b.d kept>", "B20, B34: Del should ignore blank segments"))
	}
}

func TestBody_DelMissingPath(t *testing.T) {
	b := Body{"a": map[string]any{"b": 1}, "n": float64(1), "s": "text"}
	before := len(b)

	paths := []string{"x", "x.y.z", "a.x", "a.b.c", "n.x", "s.x.y", "a.b.c.d"}
	for _, path := range paths {
		noPanic(t, "B20: Del of the missing path "+path, func() { b.Del(path) })
	}

	if len(b) != before || b.Get("a.b") != 1 || b.Get("n") != float64(1) || b.Get("s") != "text" {
		t.Error(test.DiffMessage(b, "<unchanged>", "B20: Del of a missing path should be a no-op"))
	}
}

func TestBody_DelPlainKeyStillWorks(t *testing.T) {
	b := Body{"foo": "bar", "baz": "qux"}
	b.Del("foo")
	if b.Has("foo") || !b.Has("baz") {
		t.Error(test.DiffMessage(b, Body{"baz": "qux"}, "Del of a plain key should remove only that key"))
	}
}

func TestBody_HasNullValue(t *testing.T) {
	b := Body{"a": nil, "n": map[string]any{"x": nil, "y": 1}}

	if !b.Has("a") {
		t.Error(test.DiffMessage(b.Has("a"), true, "B21: Has should be true for a key whose value is null"))
	}
	if !b.Has("n.x") {
		t.Error(test.DiffMessage(b.Has("n.x"), true, "B21: Has should be true for a dotted path whose value is null"))
	}
	if b.Has("n.z") || b.Has("missing") || b.Has("a.b") || b.Has("missing.deeper") {
		t.Error(test.DiffMessage(b.Has("n.z"), false, "B21: Has should stay false for absent paths"))
	}
}

func TestBody_PathBlankSegmentsIgnored(t *testing.T) {
	b := Body{"a": map[string]any{"b": float64(1)}}

	for _, path := range []string{"a.b", "a. .b", "a..b", ".a.b", "a.b."} {
		if got := b.Get(path); got != float64(1) {
			t.Error(test.DiffMessage(got, float64(1), "B34: Get("+path+") should ignore blank segments"))
		}
		if !b.Has(path) {
			t.Error(test.DiffMessage(b.Has(path), true, "B34: Has("+path+") should ignore blank segments"))
		}
	}

	b.Set("a. .c", float64(5))
	if b.Get("a.c") != float64(5) {
		t.Error(test.DiffMessage(b.Get("a.c"), float64(5), "B34: Set should ignore blank segments"))
	}
}

func TestBody_PathKeptSegmentsAreNotTrimmed(t *testing.T) {
	b := Body{" a": float64(1), "x": map[string]any{" y ": float64(2)}}

	if b.Get(" a") != float64(1) || b.Get("a") != nil {
		t.Error(test.DiffMessage([2]any{b.Get(" a"), b.Get("a")}, [2]any{float64(1), nil}, "B34: a kept segment should not be trimmed"))
	}
	if b.Get("x. y ") != float64(2) || b.Get("x.y") != nil {
		t.Error(test.DiffMessage([2]any{b.Get("x. y "), b.Get("x.y")}, [2]any{float64(2), nil}, "B34: a kept dotted segment should not be trimmed"))
	}

	b.Set(" k", "v")
	if _, ok := b[" k"]; !ok {
		t.Error(test.DiffMessage(b, "<key ' k'>", "B34: Set should store the untrimmed key"))
	}
}

func TestBody_GetEmptyPathReturnsBody(t *testing.T) {
	b := Body{"a": float64(1)}
	for _, path := range []string{"", " ", ".", " . ", ".."} {
		got, ok := b.Get(path).(Body)
		if !ok {
			t.Error(test.DiffMessage(b.Get(path), b, "B34: Get("+path+") should return the body itself"))
			continue
		}
		got["probe"] = true
		if _, shared := b["probe"]; !shared {
			t.Error(test.DiffMessage(got, b, "B34: Get("+path+") should return the same map, not a copy"))
		}
		delete(b, "probe")
	}
}

func TestBody_PlainKeyEqualsPath_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	pool := []string{"a", "b", " a", "a ", "ab", "key", "k1", "日本", "x_y", "A"}
	for i := 0; i < 1000; i++ {
		b := Body{}
		for _, k := range pool[:r.IntN(len(pool)+1)] {
			switch r.IntN(4) {
			case 0:
				b[k] = nil
			case 1:
				b[k] = float64(r.IntN(10))
			case 2:
				b[k] = map[string]any{"inner": "v"}
			default:
				b[k] = "s"
			}
		}
		key := pool[r.IntN(len(pool))]
		direct, present := b[key]
		if got := b.Get(key); !reflect.DeepEqual(got, direct) {
			t.Error(test.DiffMessage(got, direct, "P8: Get("+key+") should equal a direct map read"))
			return
		}
		if b.Has(key) != present {
			t.Error(test.DiffMessage(b.Has(key), present, "P8: Has("+key+") should equal key presence"))
			return
		}
	}
}

func TestBody_GetAndHasDoNotAllocate(t *testing.T) {
	b := Body{"key": "value", "a": map[string]any{"b": map[string]any{"c": map[string]any{"d": "value"}}}}
	cases := map[string]func(){
		"Get plain": func() { b.Get("key") },
		"Get deep":  func() { b.Get("a.b.c.d") },
		"Has plain": func() { b.Has("key") },
		"Has deep":  func() { b.Has("a.b.c.d") },
		"Has miss":  func() { b.Has("a.x.y") },
	}
	for name, fn := range cases {
		if allocs := testing.AllocsPerRun(100, fn); allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "P8: "+name+" should not allocate"))
		}
	}
}

func TestBody_BindValueOutOfRangeIsMismatch(t *testing.T) {
	var got any
	if !noPanic(t, "BindValue with an out-of-range number", func() { got = Body{"int8": float64(300), "int16": float64(5)}.BindValue(rangeDTO{}) }) {
		return
	}
	dto := got.(rangeDTO)
	if dto.Int8 != 0 || dto.Int16 != 5 {
		t.Error(test.DiffMessage([2]any{dto.Int8, dto.Int16}, [2]any{0, 5}, "B7: BindValue should refuse the out-of-range field and bind the other"))
	}
}

func TestBody_BindValueEqualsBind(t *testing.T) {
	b := Body{"name": "joe", "age": float64(30)}
	want, _ := b.Bind(bindValueDTO{})
	got := b.BindValue(bindValueDTO{})
	if !reflect.DeepEqual(got, want) || got.(bindValueDTO).Age != 30 {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}

func TestClientText_BodyOverLimitMessage(t *testing.T) {
	c := newBodyContext(applicationJSON, `{"a":"`+strings.Repeat("x", 100)+`"}`)
	c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 10)

	exc, err := panicException(t, func() { c.Body() })

	if exc.Code() != http.StatusRequestEntityTooLarge || exc.Message() != "Request body too large" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusRequestEntityTooLarge, "Request body too large"}, "R24: a body over the limit sends the framework's own words"))
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "R24: the limit error stays as the cause"))
	}
	if exc.StatusText() != http.StatusText(http.StatusRequestEntityTooLarge) {
		t.Error(test.DiffMessage(exc.StatusText(), http.StatusText(http.StatusRequestEntityTooLarge), "R24: the status text"))
	}
}
