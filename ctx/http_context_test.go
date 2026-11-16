package ctx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
)

func newTestHTTPContext() *HTTPContext {
	return NewHTTPContext()
}

func TestNewHTTPContext_DefaultCode(t *testing.T) {
	c := NewHTTPContext()
	if c.Code != http.StatusOK {
		t.Error(test.DiffMessage(c.Code, http.StatusOK, "NewHTTPContext default Code"))
	}
}

func TestSetID_IgnoresRequestIDHeader(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(RequestID, "test-request-id")
	c.Init(httptest.NewRecorder(), r)
	if c.id == "test-request-id" {
		t.Error(test.DiffMessage(c.id, "<generated id, not the header value>", "SetID must always generate its own ID and ignore the X-Request-Id header"))
	}
	if c.id == "" {
		t.Error(test.DiffMessage(c.id, "<non-empty id>", "SetID must still generate an ID when a header is present"))
	}
}

func TestSetID_GeneratedWhenNoHeader(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	c.Init(httptest.NewRecorder(), r)
	if len(c.id) != 16 || !isLowerHex(c.id) {
		t.Error(test.DiffMessage(c.id, "<16 lowercase hexadecimal characters>", "B24: Init should generate the id eagerly"))
	}
}

func TestSetID_Idempotent(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	c.Init(httptest.NewRecorder(), r)
	first := c.id
	c.SetID()
	if c.id != first {
		t.Error(test.DiffMessage(c.id, first, "SetID idempotent"))
	}
}

func TestGetID(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	c.Init(httptest.NewRecorder(), r)
	if c.GetID() == "" {
		t.Error(test.DiffMessage(c.GetID(), "<non-empty id>", "GetID"))
	}
	if c.GetID() != c.id {
		t.Error(test.DiffMessage(c.GetID(), c.id, "GetID must return the same ID stored on the context"))
	}
}

func TestHTTPContext_GetIDConcurrentReads_NoDataRace(t *testing.T) {
	c := newTestHTTPContext()
	c.Init(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	want := c.GetID()

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if c.GetID() != want {
					t.Error(test.DiffMessage(c.GetID(), want, "B24: GetID after Init should be a pure read"))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestHTTPContext_Status(t *testing.T) {
	c := newTestHTTPContext()
	ret := c.Status(http.StatusNotFound)
	if c.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(c.Code, http.StatusNotFound, "Status sets Code"))
	}
	if ret != c {
		t.Error(test.DiffMessage(ret, c, "Status returns self"))
	}
}

func TestHTTPContext_Init(t *testing.T) {
	c := newTestHTTPContext()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	c.Init(w, r)

	if c.Timestamp.IsZero() {
		t.Error(test.DiffMessage(c.Timestamp.IsZero(), false, "B26: Init should set Timestamp"))
	}
	if c.ResponseWriter != http.ResponseWriter(w) {
		t.Error(test.DiffMessage(c.ResponseWriter, w, "B26: Init should set ResponseWriter"))
	}
	if c.Request != r {
		t.Error(test.DiffMessage(c.Request, r, "B26: Init should set Request"))
	}
	if len(c.GetID()) != 16 {
		t.Error(test.DiffMessage(len(c.GetID()), 16, "B26: Init should set the id"))
	}
	if c.Code != http.StatusOK {
		t.Error(test.DiffMessage(c.Code, http.StatusOK, "B26: Init should leave Code at 200"))
	}

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		switch name {
		case "Request", "ResponseWriter", "contextID", "Timestamp", "Code":
			continue
		}
		if !v.Field(i).IsZero() {
			t.Error(test.DiffMessage(v.Field(i), "<zero>", "B26: Init should set nothing else, but "+name+" is set"))
		}
	}
}

func TestHTTPContext_Init_KeepsCodeSetBefore(t *testing.T) {
	c := newTestHTTPContext()
	c.Status(http.StatusTeapot)
	c.Init(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if c.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(c.Code, http.StatusTeapot, "B26: Init should not touch Code"))
	}
}

func TestReset_ClearsAllFields(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set(RequestID, "some-id")
	w := httptest.NewRecorder()
	c.Init(w, r)
	c.Status(http.StatusNotFound)
	_ = c.Query()
	_ = c.Header()
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = []string{"1"}
	_ = c.Param()
	c.Next = func() {}

	c.Reset()

	if c.Code != http.StatusOK {
		t.Error(test.DiffMessage(c.Code, http.StatusOK, "Reset Code"))
	}
	if c.id != "" {
		t.Error(test.DiffMessage(c.id, "", "Reset ID"))
	}
	if c.Request != nil {
		t.Error(test.DiffMessage(c.Request, nil, "Reset Request"))
	}
	if c.ResponseWriter != nil {
		t.Error(test.DiffMessage(c.ResponseWriter, nil, "Reset ResponseWriter"))
	}
	if c.body != nil {
		t.Error(test.DiffMessage(c.body, nil, "Reset body"))
	}
	if c.query != nil {
		t.Error(test.DiffMessage(c.query, nil, "Reset query"))
	}
	if c.header != nil {
		t.Error(test.DiffMessage(c.header, nil, "Reset header"))
	}
	if c.param != nil {
		t.Error(test.DiffMessage(c.param, nil, "Reset param"))
	}
	if c.ParamKeys != nil {
		t.Error(test.DiffMessage(c.ParamKeys, nil, "Reset ParamKeys"))
	}
	if len(c.ParamValues) != 0 {
		t.Error(test.DiffMessage(c.ParamValues, []string{}, "H3: Reset ParamValues must be empty"))
	}
	if c.Next != nil {
		t.Error(test.DiffMessage(c.Next, nil, "Reset Next"))
	}
}

func TestReset_EveryFieldIsZeroAfterUse(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	r := httptest.NewRequest("POST", "/path?callback=cb&a=1", buf)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	c.Init(w, r)
	c.Status(http.StatusAccepted)
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = []string{"1"}
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	c.AddAggregation(aggregation.NewAggregation())
	c.Next = func() {}
	c.JSON(Map{"a": 1})
	c.Text("x")

	c.Reset()

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		switch name {
		case "Code":
			if c.Code != http.StatusOK {
				t.Error(test.DiffMessage(c.Code, http.StatusOK, "B27: Reset should set Code to 200"))
			}
			continue
		case "Timestamp":
			continue
		case "ParamValues", "aggregations":
			if v.Field(i).Len() != 0 {
				t.Error(test.DiffMessage(v.Field(i).Len(), 0, "H3: after Reset "+name+" must be empty"))
			}
			continue
		}
		if !v.Field(i).IsZero() {
			t.Error(test.DiffMessage(v.Field(i), "<zero>", "B27: after Reset the context should hold no reference to the request, the writer, the payload or parsed input, but "+name+" is set"))
		}
	}
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B27: Reset should drop the aggregations"))
	}
}

func TestReset_ClearsParamValuesAndAggregations(t *testing.T) {
	c := newTestHTTPContext()
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = append(c.ParamValues, "42", "x")
	c.AddAggregation(aggregation.NewAggregation())
	c.AddAggregation(aggregation.NewAggregation())

	c.Reset()

	if len(c.ParamValues) != 0 {
		t.Error(test.DiffMessage(len(c.ParamValues), 0, "H3: Reset should cut ParamValues to length 0"))
	}
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: Reset should empty the aggregation list"))
	}
}

func TestHTTPContext_Aggregations_EmptyByDefault(t *testing.T) {
	c := newTestHTTPContext()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: a new context should have no aggregation"))
	}
}

func TestHTTPContext_Aggregations_Order(t *testing.T) {
	c := newTestHTTPContext()
	first := aggregation.NewAggregation()
	second := aggregation.NewAggregation()
	third := aggregation.NewAggregation()
	c.AddAggregation(first)
	c.AddAggregation(second)
	c.AddAggregation(third)

	got := c.Aggregations()
	if len(got) != 3 || got[0] != first || got[1] != second || got[2] != third {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{first, second, third}, "B36: Aggregations should return the list in the order added"))
	}
}

func TestHTTPContext_ResetClearsAggregations(t *testing.T) {
	c := newTestHTTPContext()
	c.Init(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	c.AddAggregation(aggregation.NewAggregation())
	c.Reset()
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B36: Reset should empty the aggregation list"))
	}

	again := aggregation.NewAggregation()
	c.Init(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	c.AddAggregation(again)
	got := c.Aggregations()
	if len(got) != 1 || got[0] != again {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{again}, "B36: a context reused after Reset should start with no aggregation"))
	}
}

func TestHTTPContext_ReuseAfterResetHasNoTraceOfPreviousRequest(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	rA := httptest.NewRequest("POST", "/a?q=a", buf)
	rA.Header.Set("Content-Type", contentType)
	rA.Header.Set("X-A", "a")
	c.Init(httptest.NewRecorder(), rA)
	idA := c.GetID()
	c.Status(http.StatusNotFound)
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = []string{"1"}
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	_ = c.Body()
	c.AddAggregation(aggregation.NewAggregation())
	c.Next = func() {}
	c.Reset()

	rB := httptest.NewRequest("GET", "/b?q=b", nil)
	rB.Header.Set("X-B", "b")
	c.Init(httptest.NewRecorder(), rB)

	if c.GetID() == idA {
		t.Error(test.DiffMessage(c.GetID(), "<an id other than "+idA+">", "B28: the id should be new"))
	}
	if c.Code != http.StatusOK {
		t.Error(test.DiffMessage(c.Code, http.StatusOK, "B28: Code should be 200"))
	}
	if len(c.Body()) != 0 {
		t.Error(test.DiffMessage(c.Body(), Body{}, "B28: body should be empty"))
	}
	if len(c.Form()) != 0 {
		t.Error(test.DiffMessage(c.Form(), Form{}, "B28: form should be empty"))
	}
	if len(c.File()) != 0 {
		t.Error(test.DiffMessage(c.File(), File{}, "B28: files should be empty"))
	}
	if c.Query().Get("q") != "b" || len(c.Query()) != 1 {
		t.Error(test.DiffMessage(c.Query(), Query{"q": {"b"}}, "B28: query should be the new request's"))
	}
	if c.Header().Get("X-A") != "" || c.Header().Get("X-B") != "b" {
		t.Error(test.DiffMessage(c.Header(), "<headers of request B>", "B28: header should be the new request's"))
	}
	if len(c.Param()) != 0 {
		t.Error(test.DiffMessage(c.Param(), Param{}, "B28: params should be empty"))
	}
	if c.Next != nil {
		t.Error(test.DiffMessage(c.Next, nil, "B28: Next should be nil"))
	}
	if len(c.Aggregations()) != 0 {
		t.Error(test.DiffMessage(len(c.Aggregations()), 0, "B28: aggregations should be empty"))
	}
}

func TestHTTPContext_ConcurrentRequests_NoDataRace(t *testing.T) {
	const goroutines = 16
	const rounds = 50
	pool := sync.Pool{New: func() any { return NewHTTPContext() }}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				payload := `{"g":` + strconv.Itoa(g) + `,"i":` + strconv.Itoa(i) + `}`
				r := httptest.NewRequest("POST", "/", strings.NewReader(payload))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				c := pool.Get().(*HTTPContext)
				c.Init(w, r)
				id := c.GetID()
				body := c.Body()
				if body.Get("g") != float64(g) || body.Get("i") != float64(i) {
					t.Error(test.DiffMessage(body, payload, "B28: each goroutine should read its own body"))
				}
				if len(id) != 16 || c.GetID() != id {
					t.Error(test.DiffMessage(c.GetID(), id, "B28: each goroutine should keep its own id"))
				}
				c.JSON(Map{"g": g, "id": id})
				expected := `{"g":` + strconv.Itoa(g) + `,"id":"` + id + `"}`
				if w.Body.String() != expected {
					t.Error(test.DiffMessage(w.Body.String(), expected, "B28: each goroutine should write its own response"))
				}
				c.Reset()
				pool.Put(c)
			}
		}()
	}
	wg.Wait()
}

func TestHTTPContext_RedirectKeeps3xxStatus(t *testing.T) {
	for _, code := range []int{http.StatusMultipleChoices, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		c := newTestHTTPContext()
		c.Request = httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		c.ResponseWriter = w

		c.Status(code).Redirect("/new-location")

		if w.Code != code {
			t.Error(test.DiffMessage(w.Code, code, "B18: Redirect should answer the 3xx status set before it"))
		}
		if c.Code != code {
			t.Error(test.DiffMessage(c.Code, code, "B18: Code should hold the code sent"))
		}
		if loc := w.Header().Get("Location"); loc != "/new-location" {
			t.Error(test.DiffMessage(loc, "/new-location", "B18: Redirect should set the Location header"))
		}
	}
}

func TestHTTPContext_RedirectDefaultsTo301(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	c.ResponseWriter = w

	c.Redirect("/new-location")

	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "B18: Redirect should answer 301 by default"))
	}
	if c.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(c.Code, http.StatusMovedPermanently, "B18: Code should hold the code sent"))
	}
	if loc := w.Header().Get("Location"); loc != "/new-location" {
		t.Error(test.DiffMessage(loc, "/new-location", "Redirect should set the Location header"))
	}
}

func TestHTTPContext_RedirectIgnoresNon3xxStatus(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusCreated, 299, http.StatusBadRequest, http.StatusInternalServerError} {
		c := newTestHTTPContext()
		c.Request = httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()
		c.ResponseWriter = w

		c.Status(code).Redirect("/new-location")

		if w.Code != http.StatusMovedPermanently {
			t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "B18: Redirect should answer 301 when the status is "+strconv.Itoa(code)))
		}
		if c.Code != http.StatusMovedPermanently {
			t.Error(test.DiffMessage(c.Code, http.StatusMovedPermanently, "B18: Code should hold the code sent"))
		}
	}
}

func TestHTTPContext_Redirect(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	c.ResponseWriter = w

	c.Redirect("/new-location")

	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "Redirect should respond with 301"))
	}
	if loc := w.Header().Get("Location"); loc != "/new-location" {
		t.Error(test.DiffMessage(loc, "/new-location", "Redirect should set the Location header"))
	}
}

func dirtyHTTPContext(t *testing.T) *HTTPContext {
	t.Helper()
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	r := httptest.NewRequest("POST", "/path?callback=cb&a=1", buf)
	r.Header.Set("Content-Type", contentType)
	c.Init(httptest.NewRecorder(), r)
	c.Status(http.StatusAccepted)
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = append(make([]string, 0, 8), "1", "2")
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	_ = c.Body()
	c.AddAggregation(aggregation.NewAggregation())
	c.AddAggregation(aggregation.NewAggregation())
	c.Next = func() {}
	return c
}

func TestReset_EqualsNewContextExceptCapacity(t *testing.T) {
	c := dirtyHTTPContext(t)
	c.Reset()

	if len(c.ParamValues) != 0 || len(c.aggregations) != 0 {
		t.Error(test.DiffMessage([]int{len(c.ParamValues), len(c.aggregations)}, []int{0, 0}, "H3-eq: after Reset both kept slices have length 0"))
	}
	got := *c
	got.ParamValues = nil
	got.aggregations = nil
	want := *NewHTTPContext()
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "H3-eq: after Reset the context equals NewHTTPContext() in every field except the two capacities"))
	}
}

func TestReset_KeepsCapacityOfParamValuesAndAggregations(t *testing.T) {
	c := dirtyHTTPContext(t)
	c.Reset()

	if cap(c.ParamValues) < 8 {
		t.Error(test.DiffMessage(cap(c.ParamValues), 8, "H3: Reset keeps the capacity of ParamValues"))
	}
	if cap(c.aggregations) < 2 {
		t.Error(test.DiffMessage(cap(c.aggregations), 2, "H3: Reset keeps the capacity of the aggregation list"))
	}
}

func TestReset_ClearsElements(t *testing.T) {
	c := dirtyHTTPContext(t)
	c.Reset()

	for i, v := range c.ParamValues[:cap(c.ParamValues)] {
		if v != "" {
			t.Error(test.DiffMessage(v, "", "H3-eq: every element of the ParamValues backing array is zero after Reset, index "+strconv.Itoa(i)))
		}
	}
	for i, a := range c.aggregations[:cap(c.aggregations)] {
		if a != nil {
			t.Error(test.DiffMessage(a, nil, "H3-eq: every element of the aggregation backing array is zero after Reset, index "+strconv.Itoa(i)))
		}
	}
}

func TestReset_DropsOver64(t *testing.T) {
	c := newTestHTTPContext()
	c.ParamValues = make([]string, 0, 65)
	c.aggregations = make([]*aggregation.Aggregation, 0, 65)
	c.Reset()

	if c.ParamValues != nil {
		t.Error(test.DiffMessage(cap(c.ParamValues), 0, "H3: Reset drops a ParamValues whose capacity is over 64"))
	}
	if c.aggregations != nil {
		t.Error(test.DiffMessage(cap(c.aggregations), 0, "H3: Reset drops an aggregation list whose capacity is over 64"))
	}
}

func TestReset_Keeps64(t *testing.T) {
	c := newTestHTTPContext()
	c.ParamValues = make([]string, 0, 64)
	c.aggregations = make([]*aggregation.Aggregation, 0, 64)
	c.Reset()

	if cap(c.ParamValues) != 64 {
		t.Error(test.DiffMessage(cap(c.ParamValues), 64, "H3: Reset keeps a ParamValues whose capacity is exactly 64"))
	}
	if cap(c.aggregations) != 64 {
		t.Error(test.DiffMessage(cap(c.aggregations), 64, "H3: Reset keeps an aggregation list whose capacity is exactly 64"))
	}
}

func TestReset_AddAggregationOnWarmContextDoesNotAllocate(t *testing.T) {
	a := aggregation.NewAggregation()
	c := newTestHTTPContext()
	c.AddAggregation(a)
	c.Reset()

	allocs := testing.AllocsPerRun(100, func() {
		c.AddAggregation(a)
		c.Reset()
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "P6, H3: AddAggregation on a warm context allocates nothing"))
	}
}
