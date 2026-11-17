package ctx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

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

func TestWSContext_ID(t *testing.T) {
	c := NewWSContext()
	if c.ID() != "" {
		t.Error(test.DiffMessage(c.ID(), "", "B16: ID is empty before WSInit"))
	}
	WSInit(c, nil)
	if len(c.ID()) != 16 || !isLowerHex(c.ID()) {
		t.Error(test.DiffMessage(c.ID(), "<16 lowercase hexadecimal characters>", "B16: ID returns what GetID returned"))
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

func TestWSHooks(t *testing.T) {
	c := NewWSContext()
	WSInit(c, &websocket.Conn{})
	if WSTimestamp(c).IsZero() {
		t.Error(test.DiffMessage(WSTimestamp(c).IsZero(), false, "B18: WSInit sets the timestamp"))
	}
	var sent any
	WSSetSend(c, func(data any) { sent = data })
	c.Send("x")
	if sent != "x" {
		t.Error(test.DiffMessage(sent, "x", "B18: WSSetSend sets the send function"))
	}
	WSSetPayload(c, WSPayload{"k": "v"})
	if !reflect.DeepEqual(c.WSPayload(), WSPayload{"k": "v"}) {
		t.Error(test.DiffMessage(c.WSPayload(), WSPayload{"k": "v"}, "B18: WSSetPayload sets the payload"))
	}
	WSSetNext(c, func() {})
	if WSNext(c) == nil {
		t.Error(test.DiffMessage(nil, "<func>", "B18: WSSetNext is read back by WSNext"))
	}
	a := aggregation.NewAggregation()
	WSAddAggregation(c, a)
	if got := WSAggregations(c); len(got) != 1 || got[0] != a {
		t.Error(test.DiffMessage(got, []*aggregation.Aggregation{a}, "B18: WSAddAggregation and WSAggregations"))
	}
	WSReset(c)
	if WSNext(c) != nil || len(WSAggregations(c)) != 0 || c.ID() != "" || c.WSPayload() != nil {
		t.Error(test.DiffMessage(c, NewWSContext(), "B18: WSReset leaves the context as a new one"))
	}
}
