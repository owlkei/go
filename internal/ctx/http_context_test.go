package ctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/aggregation"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/tracecontext"
)

func TestNewHTTPContext_DefaultCode(t *testing.T) {
	c := NewHTTPContext()
	if HTTPCode(c) != http.StatusOK {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusOK, "NewHTTPContext default Code"))
	}
}

func TestHTTPContext_Init_GetIDIsRequest(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	s := tracecontext.ScopeOf(c.Context())
	if s == nil {
		t.Fatal(test.DiffMessage(s, "<a scope>", "S1: Init puts a scope in the request context"))
	}
	if c.ID() != s.Request {
		t.Error(test.DiffMessage(c.ID(), s.Request, "S1: GetID is Scope.Request"))
	}
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "S2: the ID is a span ID"))
	}
}

func TestHTTPContext_Init_IgnoresRequestIDHeader(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(RequestID, "test-request-id")
	HTTPInit(c, httptest.NewRecorder(), r)
	if c.ID() == "test-request-id" {
		t.Error(test.DiffMessage(c.ID(), "<generated id, not the header value>", "S2: no header can set the request ID"))
	}
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "S2: Init still generates an ID when a header is present"))
	}
	if s := tracecontext.ScopeOf(c.Context()); s.Upstream != "test-request-id" {
		t.Error(test.DiffMessage(s.Upstream, "test-request-id", "S4: a valid X-Request-Id is the upstream"))
	}
}

func TestHTTPContext_Init_IgnoresTraceparentAsRequestID(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	HTTPInit(c, httptest.NewRecorder(), r)
	s := tracecontext.ScopeOf(c.Context())
	if c.ID() == "00f067aa0ba902b7" {
		t.Error(test.DiffMessage(c.ID(), "<own ID>", "S2: the incoming parent ID is not the request ID"))
	}
	if s.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || s.Parent != "00f067aa0ba902b7" {
		t.Error(test.DiffMessage([]string{s.TraceID, s.Parent}, []string{"4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"}, "S3: Init continues the incoming trace"))
	}
}

func TestHTTPContext_Init_TraceIDUsesTimestamp(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	s := tracecontext.ScopeOf(c.Context())
	if want := fmt.Sprintf("%012x", HTTPTimestamp(c).UnixMilli()); s.TraceID[:12] != want {
		t.Error(test.DiffMessage(s.TraceID[:12], want, "S1: the scope is built with HTTPTimestamp(c)"))
	}
}

func TestHTTPContext_Init_RequestCarriesScope(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/path?q=1", nil)
	HTTPInit(c, httptest.NewRecorder(), r)

	s := tracecontext.ScopeOf(c.Context())
	if s == nil {
		t.Fatal(test.DiffMessage(s, "<a scope>", "S1: c.Context() carries the scope"))
	}
	if tracecontext.ScopeOf(c.Request.Context()) != s {
		t.Error(test.DiffMessage(tracecontext.ScopeOf(c.Request.Context()), s, "S1: c.Request.Context() carries the same scope"))
	}
	if c.Request == r {
		t.Error(test.DiffMessage(c.Request, "<a copy of the request>", "S1: Init replaces the request with r.WithContext(scope)"))
	}
	if c.Request.URL != r.URL || c.Request.Method != r.Method {
		t.Error(test.DiffMessage(c.Request.URL, r.URL, "S1: the copy is the same request"))
	}
	if tracecontext.ScopeOf(r.Context()) != nil {
		t.Error(test.DiffMessage(tracecontext.ScopeOf(r.Context()), nil, "S1: the request passed in is not modified"))
	}
}

func TestHTTPContext_Init_ContextKeepsRequestContextValues(t *testing.T) {
	type key struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	r := httptest.NewRequest("GET", "/", nil).WithContext(parent)
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), r)
	if c.Context().Value(key{}) != "v" {
		t.Error(test.DiffMessage(c.Context().Value(key{}), "v", "S1: the scope context wraps the request's context"))
	}
	cancel()
	select {
	case <-c.Context().Done():
	case <-time.After(10 * time.Second):
		t.Fatal("S9: cancelling the server's request context cancels c.Context()")
	}
}

func TestHTTPContext_Init_NestedScopeUsesHeaders(t *testing.T) {
	outer, outerScope := tracecontext.NewRequest(context.Background(), http.Header{}, time.Now())
	r := httptest.NewRequest("GET", "/", nil).WithContext(outer)
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), r)
	s := tracecontext.ScopeOf(c.Context())
	if s == outerScope || s.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Error(test.DiffMessage([]any{s == outerScope, s.TraceID}, []any{false, "4bf92f3577b34da6a3ce929d0e0e4736"}, "S11: Init builds a new scope from the headers whatever the context carries"))
	}
}

func TestHTTPContext_Init_GetIDEmptyBeforeInitAndAfterReset(t *testing.T) {
	c := newTestHTTPContext()
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "S10: GetID is empty before Init"))
	}
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	HTTPReset(c)
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "S10: GetID is empty after Reset"))
	}
}

func TestHTTPContext_ScopeSurvivesReuse(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/first", nil))
	kept := c.Context()
	firstID := c.ID()
	firstTrace := tracecontext.ScopeOf(kept).TraceID
	firstTraceparent := tracecontext.ScopeOf(kept).Traceparent()

	HTTPReset(c)
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/second", nil))

	s := tracecontext.ScopeOf(kept)
	if s == nil {
		t.Fatal(test.DiffMessage(s, "<a scope>", "S8: the kept context still carries its scope after Reset and reuse"))
	}
	if s.Request != firstID || s.TraceID != firstTrace || s.Traceparent() != firstTraceparent {
		t.Error(test.DiffMessage([]string{s.Request, s.TraceID}, []string{firstID, firstTrace}, "S8: the kept context reads the first request's IDs"))
	}
	if c.ID() == firstID || tracecontext.ScopeOf(c.Context()) == s {
		t.Error(test.DiffMessage(c.ID(), "<the second request's own ID>", "S8: the reused context holds a new scope"))
	}
}

func TestHTTPContext_ScopeSurvivesCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/", nil).WithContext(parent)
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), r)
	kept := c.Context()
	id := c.ID()
	cancel()
	HTTPReset(c)
	if s := tracecontext.ScopeOf(kept); s == nil || s.Request != id {
		t.Error(test.DiffMessage(s, id, "S8: a cancelled context still reads its IDs"))
	}
}

func TestHTTPContext_Init_ConcurrentContextsIsolated(t *testing.T) {
	const goroutines = 16
	const rounds = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]struct{}{}
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := newTestHTTPContext()
			var kept []context.Context
			var ids []string
			for i := 0; i < rounds; i++ {
				HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
				kept = append(kept, c.Context())
				ids = append(ids, c.ID())
				HTTPReset(c)
			}
			for i, k := range kept {
				if s := tracecontext.ScopeOf(k); s == nil || s.Request != ids[i] {
					t.Error(test.DiffMessage(s, ids[i], "S8: every kept context reads its own request ID"))
					return
				}
			}
			mu.Lock()
			for _, id := range ids {
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != goroutines*rounds {
		t.Error(test.DiffMessage(len(seen), goroutines*rounds, "S2: request IDs are unique across contexts"))
	}
}

func TestHTTPContext_SetIDRemoved(t *testing.T) {
	if _, ok := reflect.TypeFor[*HTTPContext]().MethodByName("SetID"); ok {
		t.Error(test.DiffMessage("SetID", "<no such method>", "SetID is removed: the ID comes from the scope built in Init"))
	}
}

func TestHTTPContext_ContextMethodFromRequest(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	var asContext Context = c
	if asContext.Context() != c.Request.Context() {
		t.Error(test.DiffMessage(asContext.Context(), c.Request.Context(), "Context() on the interface is the request's context"))
	}
}

func TestGetID(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/", nil)
	HTTPInit(c, httptest.NewRecorder(), r)
	if c.ID() == "" {
		t.Error(test.DiffMessage(c.ID(), "<non-empty id>", "GetID"))
	}
	if c.ID() != tracecontext.ScopeOf(c.Context()).Request {
		t.Error(test.DiffMessage(c.ID(), tracecontext.ScopeOf(c.Context()).Request, "GetID must return the request ID of the scope"))
	}
}

func TestHTTPContext_GetIDConcurrentReads_NoDataRace(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	want := c.ID()

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if c.ID() != want {
					t.Error(test.DiffMessage(c.ID(), want, "B24: GetID after Init should be a pure read"))
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
	if HTTPCode(c) != http.StatusNotFound {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusNotFound, "Status sets Code"))
	}
	if ret != c {
		t.Error(test.DiffMessage(ret, c, "Status returns self"))
	}
}

func TestHTTPContext_Init(t *testing.T) {
	c := newTestHTTPContext()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	HTTPInit(c, w, r)

	if HTTPTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(HTTPTimestamp(c).IsZero(), false, "B26: Init should set Timestamp"))
	}
	if c.ResponseWriter != http.ResponseWriter(w) {
		t.Error(test.DiffMessage(c.ResponseWriter, w, "B26: Init should set ResponseWriter"))
	}
	if c.Request == nil || c.Request.URL != r.URL || c.Request.Method != r.Method {
		t.Error(test.DiffMessage(c.Request, r, "B26: Init should set Request to a copy of the request"))
	}
	if len(c.ID()) != 16 {
		t.Error(test.DiffMessage(len(c.ID()), 16, "B26: Init should set the id"))
	}
	if HTTPCode(c) != http.StatusOK {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusOK, "B26: Init should leave Code at 200"))
	}

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		switch name {
		case "Request", "ResponseWriter", "scope", "timestamp", "code":
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
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if HTTPCode(c) != http.StatusTeapot {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusTeapot, "B26: Init should not touch Code"))
	}
}

func TestReset_ClearsAllFields(t *testing.T) {
	c := newTestHTTPContext()
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set(RequestID, "some-id")
	w := httptest.NewRecorder()
	HTTPInit(c, w, r)
	c.Status(http.StatusNotFound)
	_ = c.Query()
	_ = c.Header()
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"1"})
	_ = c.Param()
	HTTPSetNext(c, func() {})

	HTTPReset(c)

	if HTTPCode(c) != http.StatusOK {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusOK, "Reset Code"))
	}
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "Reset ID"))
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
	if c.paramKeys != nil {
		t.Error(test.DiffMessage(c.paramKeys, nil, "Reset ParamKeys"))
	}
	if len(c.paramValues) != 0 {
		t.Error(test.DiffMessage(c.paramValues, []string{}, "H3: Reset ParamValues must be empty"))
	}
	if HTTPNext(c) != nil {
		t.Error(test.DiffMessage(HTTPNext(c), nil, "Reset Next"))
	}
}

func TestReset_EveryFieldIsZeroAfterUse(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "bytes")
	c := newTestHTTPContext()
	r := httptest.NewRequest("POST", "/path?callback=cb&a=1", buf)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	HTTPInit(c, w, r)
	c.Status(http.StatusAccepted)
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"1"})
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPSetNext(c, func() {})
	c.JSON(Map{"a": 1})
	c.Text("x")

	HTTPReset(c)

	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		switch name {
		case "code":
			if HTTPCode(c) != http.StatusOK {
				t.Error(test.DiffMessage(HTTPCode(c), http.StatusOK, "B27: Reset should set Code to 200"))
			}
			continue
		case "timestamp":
			continue
		case "paramValues", "aggregations":
			if v.Field(i).Len() != 0 {
				t.Error(test.DiffMessage(v.Field(i).Len(), 0, "H3: after Reset "+name+" must be empty"))
			}
			continue
		}
		if !v.Field(i).IsZero() {
			t.Error(test.DiffMessage(v.Field(i), "<zero>", "B27: after Reset the context should hold no reference to the request, the writer, the payload or parsed input, but "+name+" is set"))
		}
	}
	if len(HTTPAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(HTTPAggregations(c)), 0, "B27: Reset should drop the aggregations"))
	}
}

func TestReset_ClearsParamValuesAndAggregations(t *testing.T) {
	c := newTestHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}}, append(c.paramValues, "42", "x"))
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPAddAggregation(c, aggregation.NewAggregation())

	HTTPReset(c)

	if len(c.paramValues) != 0 {
		t.Error(test.DiffMessage(len(c.paramValues), 0, "H3: Reset should cut ParamValues to length 0"))
	}
	if len(HTTPAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(HTTPAggregations(c)), 0, "B36: Reset should empty the aggregation list"))
	}
}

func TestHTTPContext_Aggregations_EmptyByDefault(t *testing.T) {
	c := newTestHTTPContext()
	if len(HTTPAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(HTTPAggregations(c)), 0, "B36: a new context should have no aggregation"))
	}
}

func TestHTTPContext_Aggregations_Order(t *testing.T) {
	c := newTestHTTPContext()
	first := aggregation.NewAggregation()
	second := aggregation.NewAggregation()
	third := aggregation.NewAggregation()
	HTTPAddAggregation(c, first)
	HTTPAddAggregation(c, second)
	HTTPAddAggregation(c, third)

	got := HTTPAggregations(c)
	if len(got) != 3 || got[0] != first || got[1] != second || got[2] != third {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{first, second, third}, "B36: Aggregations should return the list in the order added"))
	}
}

func TestHTTPContext_ResetClearsAggregations(t *testing.T) {
	c := newTestHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPReset(c)
	if len(HTTPAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(HTTPAggregations(c)), 0, "B36: Reset should empty the aggregation list"))
	}

	again := aggregation.NewAggregation()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	HTTPAddAggregation(c, again)
	got := HTTPAggregations(c)
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
	HTTPInit(c, httptest.NewRecorder(), rA)
	idA := c.ID()
	c.Status(http.StatusNotFound)
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"1"})
	_ = c.Param()
	_ = c.Query()
	_ = c.Header()
	_ = c.Form()
	_ = c.File()
	_ = c.Body()
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPSetNext(c, func() {})
	HTTPReset(c)

	rB := httptest.NewRequest("GET", "/b?q=b", nil)
	rB.Header.Set("X-B", "b")
	HTTPInit(c, httptest.NewRecorder(), rB)

	if c.ID() == idA {
		t.Error(test.DiffMessage(c.ID(), "<an id other than "+idA+">", "B28: the id should be new"))
	}
	if HTTPCode(c) != http.StatusOK {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusOK, "B28: Code should be 200"))
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
	if HTTPNext(c) != nil {
		t.Error(test.DiffMessage(HTTPNext(c), nil, "B28: Next should be nil"))
	}
	if len(HTTPAggregations(c)) != 0 {
		t.Error(test.DiffMessage(len(HTTPAggregations(c)), 0, "B28: aggregations should be empty"))
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
				HTTPInit(c, w, r)
				id := c.ID()
				body := c.Body()
				if body.Get("g") != float64(g) || body.Get("i") != float64(i) {
					t.Error(test.DiffMessage(body, payload, "B28: each goroutine should read its own body"))
				}
				if len(id) != 16 || c.ID() != id {
					t.Error(test.DiffMessage(c.ID(), id, "B28: each goroutine should keep its own id"))
				}
				c.JSON(Map{"g": g, "id": id})
				expected := `{"g":` + strconv.Itoa(g) + `,"id":"` + id + `"}`
				if w.Body.String() != expected {
					t.Error(test.DiffMessage(w.Body.String(), expected, "B28: each goroutine should write its own response"))
				}
				HTTPReset(c)
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
		if HTTPCode(c) != code {
			t.Error(test.DiffMessage(HTTPCode(c), code, "B18: Code should hold the code sent"))
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
	if HTTPCode(c) != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(HTTPCode(c), http.StatusMovedPermanently, "B18: Code should hold the code sent"))
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
		if HTTPCode(c) != http.StatusMovedPermanently {
			t.Error(test.DiffMessage(HTTPCode(c), http.StatusMovedPermanently, "B18: Code should hold the code sent"))
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

func TestReset_EqualsNewContextExceptCapacity(t *testing.T) {
	c := dirtyHTTPContext(t)
	HTTPReset(c)

	if len(c.paramValues) != 0 || len(c.aggregations) != 0 {
		t.Error(test.DiffMessage([]int{len(c.paramValues), len(c.aggregations)}, []int{0, 0}, "H3-eq: after Reset both kept slices have length 0"))
	}
	got := *c
	got.paramValues = nil
	got.aggregations = nil
	want := *NewHTTPContext()
	if !reflect.DeepEqual(got, want) {
		t.Error(test.DiffMessage(got, want, "H3-eq: after Reset the context equals NewHTTPContext() in every field except the two capacities"))
	}
}

func TestReset_KeepsCapacityOfParamValuesAndAggregations(t *testing.T) {
	c := dirtyHTTPContext(t)
	HTTPReset(c)

	if cap(c.paramValues) < 8 {
		t.Error(test.DiffMessage(cap(c.paramValues), 8, "H3: Reset keeps the capacity of ParamValues"))
	}
	if cap(c.aggregations) < 2 {
		t.Error(test.DiffMessage(cap(c.aggregations), 2, "H3: Reset keeps the capacity of the aggregation list"))
	}
}

func TestReset_ClearsElements(t *testing.T) {
	c := dirtyHTTPContext(t)
	HTTPReset(c)

	for i, v := range c.paramValues[:cap(c.paramValues)] {
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
	c.paramValues = make([]string, 0, 65)
	c.aggregations = make([]*aggregation.Aggregation, 0, 65)
	HTTPReset(c)

	if c.paramValues != nil {
		t.Error(test.DiffMessage(cap(c.paramValues), 0, "H3: Reset drops a ParamValues whose capacity is over 64"))
	}
	if c.aggregations != nil {
		t.Error(test.DiffMessage(cap(c.aggregations), 0, "H3: Reset drops an aggregation list whose capacity is over 64"))
	}
}

func TestReset_Keeps64(t *testing.T) {
	c := newTestHTTPContext()
	c.paramValues = make([]string, 0, 64)
	c.aggregations = make([]*aggregation.Aggregation, 0, 64)
	HTTPReset(c)

	if cap(c.paramValues) != 64 {
		t.Error(test.DiffMessage(cap(c.paramValues), 64, "H3: Reset keeps a ParamValues whose capacity is exactly 64"))
	}
	if cap(c.aggregations) != 64 {
		t.Error(test.DiffMessage(cap(c.aggregations), 64, "H3: Reset keeps an aggregation list whose capacity is exactly 64"))
	}
}

func TestReset_AddAggregationOnWarmContextDoesNotAllocate(t *testing.T) {
	a := aggregation.NewAggregation()
	c := newTestHTTPContext()
	HTTPAddAggregation(c, a)
	HTTPReset(c)

	allocs := testing.AllocsPerRun(100, func() {
		HTTPAddAggregation(c, a)
		HTTPReset(c)
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "P6, H3: AddAggregation on a warm context allocates nothing"))
	}
}

func TestHTTPContext_ID(t *testing.T) {
	c := NewHTTPContext()
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "B16: ID is empty before HTTPInit"))
	}
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "B16: ID returns what GetID returned"))
	}
	HTTPReset(c)
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "B16: ID is empty after HTTPReset"))
	}
}

func TestHTTPContext_StatusSetsWrittenCode(t *testing.T) {
	w := httptest.NewRecorder()
	c := NewHTTPContext()
	HTTPInit(c, w, httptest.NewRequest("GET", "/", nil))
	c.Status(http.StatusCreated).JSON(Map{"a": 1})
	if w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B17: Status(201) then JSON writes 201"))
	}

	w = httptest.NewRecorder()
	c = NewHTTPContext()
	HTTPInit(c, w, httptest.NewRequest("GET", "/", nil))
	c.JSON(Map{"a": 1})
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17: the default written code is 200"))
	}
}

func TestHTTPHooks_CodeNextTimestamp(t *testing.T) {
	c := NewHTTPContext()
	if HTTPCode(c) != http.StatusOK || HTTPNext(c) != nil {
		t.Error(test.DiffMessage([2]any{HTTPCode(c), HTTPNext(c) == nil}, [2]any{200, true}, "B18: a new context has code 200 and no next"))
	}
	HTTPSetCode(c, http.StatusTeapot)
	called := false
	HTTPSetNext(c, func() { called = true })
	HTTPNext(c)()
	if HTTPCode(c) != http.StatusTeapot || !called {
		t.Error(test.DiffMessage([2]any{HTTPCode(c), called}, [2]any{418, true}, "B18: HTTPSetCode and HTTPSetNext are read back"))
	}
	if HTTPTimestamp(c) == nil {
		t.Fatal(test.DiffMessage(nil, "<*time.Time>", "B18: HTTPTimestamp is never nil"))
	}
	if !HTTPTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(HTTPTimestamp(c).IsZero(), true, "B18: the timestamp is zero before HTTPInit"))
	}
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if HTTPTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(HTTPTimestamp(c).IsZero(), false, "B18: HTTPInit sets the timestamp"))
	}
}

func TestHTTPHooks_SetParamsFeedsParam(t *testing.T) {
	c := NewHTTPContext()
	HTTPSetParams(c, map[string][]int{"id": {0}, "tags": {1, 2}}, []string{"42", "a", "b"})
	want := Param{"id": {"42"}, "tags": {"a", "b"}}
	if !reflect.DeepEqual(c.Param(), want) {
		t.Error(test.DiffMessage(c.Param(), want, "B18: HTTPSetParams followed by Param gives the parameters"))
	}
	if !reflect.DeepEqual(HTTPParamValues(c), []string{"42", "a", "b"}) {
		t.Error(test.DiffMessage(HTTPParamValues(c), []string{"42", "a", "b"}, "B18: HTTPParamValues returns the values set"))
	}
}

func TestHTTPHooks_ResetEqualsFreshContext(t *testing.T) {
	c := NewHTTPContext()
	HTTPInit(c, httptest.NewRecorder(), httptest.NewRequest("GET", "/x?a=1", nil))
	HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"1"})
	HTTPSetCode(c, http.StatusAccepted)
	HTTPSetNext(c, func() {})
	HTTPAddAggregation(c, aggregation.NewAggregation())
	HTTPReset(c)
	if HTTPCode(c) != http.StatusOK || HTTPNext(c) != nil || len(HTTPParamValues(c)) != 0 || len(HTTPAggregations(c)) != 0 || c.ID() != "" || len(c.Param()) != 0 {
		t.Error(test.DiffMessage(c, NewHTTPContext(), "B18: HTTPReset leaves the context as a new one"))
	}
}

func TestHTTPHooks_AggregationsKeepOrder(t *testing.T) {
	c := NewHTTPContext()
	a, b := aggregation.NewAggregation(), aggregation.NewAggregation()
	HTTPAddAggregation(c, a)
	HTTPAddAggregation(c, b)
	got := HTTPAggregations(c)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{a, b}, "B18: aggregations are returned in the order added"))
	}
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

		if exc.Code() != http.StatusInternalServerError {
			t.Error(test.DiffMessage(exc.Code(), http.StatusInternalServerError, "B30: "+name+" should panic with an exception of code 500"))
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
	if exc.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(exc.Code(), http.StatusInternalServerError, "B30: JSONP should panic with an exception of code 500"))
	}
	if len(w.Header()) != 0 || w.Body.Len() != 0 {
		t.Error(test.DiffMessage([2]any{w.Header(), w.Body.Len()}, [2]any{http.Header{}, 0}, "B30: a failed JSONP should write nothing"))
	}
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

func redirectOutcome(redirect func(*HTTPContext, string), target string, isPage bool, code int) (int, string) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c.ResponseWriter = w
	if isPage {
		HTTPSetPage(c)
	}
	if code != 0 {
		c.Status(code)
	}
	redirect(c, target)
	return w.Code, w.Header().Get("Location")
}

func redirectLocal(c *HTTPContext, target string) { c.Redirect(target) }

func redirectExternal(c *HTTPContext, target string) { c.RedirectExternal(target) }

var redirectAccepted = []string{"/", "/bookings", "/a?next=/b#top", "/a?u=https://x.test"}

var redirectRefused = []string{
	"", "https://evil.test", "//evil.test", "/\\evil.test", "\\\\evil.test", "/a\\b",
	"javascript:alert(1)", "evil.test", "bookings", "/a\r\nSet-Cookie: x=1", "/\t/evil.test", "/a\x00b", "/a\x7fb", "/a\nb",
}

func TestHTTPContext_RedirectSameOriginAccepts(t *testing.T) {
	for _, isPage := range []bool{false, true} {
		for _, target := range redirectAccepted {
			_, loc := redirectOutcome(redirectLocal, target, isPage, 0)
			if loc != target {
				t.Error(test.DiffMessage(loc, target, "R1: c.Redirect accepts "+strconv.Quote(target)+" (page "+strconv.FormatBool(isPage)+")"))
			}
		}
	}
}

func TestHTTPContext_RedirectSameOriginRefuses(t *testing.T) {
	for _, isPage := range []bool{false, true} {
		want := http.StatusMovedPermanently
		if isPage {
			want = http.StatusSeeOther
		}
		for _, target := range redirectRefused {
			code, loc := redirectOutcome(redirectLocal, target, isPage, 0)
			if loc != "/" || code != want {
				t.Error(test.DiffMessage([]any{code, loc}, []any{want, "/"}, "R1, R4: c.Redirect refuses "+strconv.Quote(target)+" and goes to / (page "+strconv.FormatBool(isPage)+")"))
			}
		}
	}
}

func TestHTTPContext_RedirectRefusalKeepsStatus(t *testing.T) {
	code, loc := redirectOutcome(redirectLocal, "https://evil.test", false, http.StatusFound)
	if code != http.StatusFound || loc != "/" {
		t.Error(test.DiffMessage([]any{code, loc}, []any{http.StatusFound, "/"}, "R4: a refused target redirects to / with the same status"))
	}
}

func TestHTTPContext_RedirectExternalAccepts(t *testing.T) {
	targets := append([]string{"https://example.com", "http://example.com/cb?x=1&y=2#f", "HTTPS://Example.com/Path", "https://example.com:8443/a"}, redirectAccepted...)
	for _, target := range targets {
		_, loc := redirectOutcome(redirectExternal, target, false, 0)
		if loc != target {
			t.Error(test.DiffMessage(loc, target, "R2: c.RedirectExternal accepts "+strconv.Quote(target)))
		}
	}
}

func TestHTTPContext_RedirectExternalRefuses(t *testing.T) {
	targets := []string{
		"javascript:alert(1)", "data:text/html,x", "//evil.test", "https://user@evil.test", "https://user:pw@evil.test", "https:///path",
		"ftp://example.com", "", "evil.test", "bookings", "/\\evil.test", "https://example.com/a\\b", "https://example.com/\r\nSet-Cookie: x=1", "https://exa\x00mple.com",
	}
	for _, target := range targets {
		code, loc := redirectOutcome(redirectExternal, target, false, 0)
		if loc != "/" || code != http.StatusMovedPermanently {
			t.Error(test.DiffMessage([]any{code, loc}, []any{http.StatusMovedPermanently, "/"}, "R2, R4: c.RedirectExternal refuses "+strconv.Quote(target)))
		}
	}
}

func TestHTTPContext_RedirectStatus(t *testing.T) {
	for name, row := range map[string]struct {
		isPage bool
		code   int
		want   int
	}{
		"page":         {true, 0, http.StatusSeeOther},
		"api":          {false, 0, http.StatusMovedPermanently},
		"explicit 302": {true, http.StatusFound, http.StatusFound},
		"explicit 307": {false, http.StatusTemporaryRedirect, http.StatusTemporaryRedirect},
		"explicit 200": {true, http.StatusOK, http.StatusSeeOther},
	} {
		for label, redirect := range map[string]func(*HTTPContext, string){"Redirect": redirectLocal, "RedirectExternal": redirectExternal} {
			code, loc := redirectOutcome(redirect, "/next", row.isPage, row.code)
			if code != row.want || loc != "/next" {
				t.Error(test.DiffMessage([]any{code, loc}, []any{row.want, "/next"}, "R3: "+label+" status, "+name))
			}
		}
	}
}

func TestHTTPContext_RedirectNoHeaderInjection(t *testing.T) {
	for _, target := range []string{"/a\r\nSet-Cookie: x=1", "https://example.com/\r\nSet-Cookie: x=1"} {
		for _, redirect := range []func(*HTTPContext, string){redirectLocal, redirectExternal} {
			c := newTestHTTPContext()
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			c.ResponseWriter = w
			redirect(c, target)
			if w.Header().Get("Set-Cookie") != "" || strings.ContainsAny(w.Header().Get("Location"), "\r\n") {
				t.Error(test.DiffMessage(w.Header(), "no injected header", "R4, H1: a CR LF target never reaches a header"))
			}
		}
	}
}
