package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
	"golang.org/x/net/websocket"
)

func TestHTTPHandlerPlan_InjectsHTTPContext(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HCtx", nil), c)
	if len(h.got) != 1 || h.got[0] != any(c) {
		t.Error(test.DiffMessage(h.got, []any{c}, "B1, B8: *ctx.HTTPContext is the request's context"))
	}
}

func TestHTTPHandlerPlan_InjectsRequest(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HReq", nil), c)
	if len(h.got) != 1 || h.got[0] != any(c.Request) {
		t.Error(test.DiffMessage(h.got, []any{c.Request}, "B1, B8: *http.Request is c.Request"))
	}
}

func TestHTTPHandlerPlan_InjectsResponseWriter(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HWriter", nil), c)
	if len(h.got) != 1 || h.got[0] != any(c.ResponseWriter) {
		t.Error(test.DiffMessage(h.got, []any{c.ResponseWriter}, "B1, B8: http.ResponseWriter is c.ResponseWriter"))
	}
}

func TestHTTPHandlerPlan_InjectsBody(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HBody", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.Body()) {
		t.Error(test.DiffMessage(h.got, c.Body(), "B1, B8: ctx.Body is c.Body()"))
	}
	if got, ok := h.got[0].(ctx.Body); !ok || got["a"] != float64(1) {
		t.Error(test.DiffMessage(h.got[0], ctx.Body{"a": float64(1)}, "B1: the handler sees the parsed body"))
	}
}

func TestHTTPHandlerPlan_InjectsForm(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HForm", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.Form()) {
		t.Error(test.DiffMessage(h.got, c.Form(), "B1, B8: ctx.Form is c.Form()"))
	}
}

func TestHTTPHandlerPlan_InjectsQuery(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HQuery", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.Query()) {
		t.Error(test.DiffMessage(h.got, c.Query(), "B1, B8: ctx.Query is c.Query()"))
	}
	if got, ok := h.got[0].(ctx.Query); !ok || !reflect.DeepEqual(got["q"], []string{"1", "2"}) {
		t.Error(test.DiffMessage(h.got[0], ctx.Query{"q": {"1", "2"}}, "B1: the handler sees the query values"))
	}
}

func TestHTTPHandlerPlan_InjectsHeader(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HHeader", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.Header()) {
		t.Error(test.DiffMessage(h.got, c.Header(), "B1, B8: ctx.Header is c.Header()"))
	}
}

func TestHTTPHandlerPlan_InjectsParam(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HParam", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.Param()) {
		t.Error(test.DiffMessage(h.got, c.Param(), "B1, B8: ctx.Param is c.Param()"))
	}
	if got, ok := h.got[0].(ctx.Param); !ok || !reflect.DeepEqual(got["id"], []string{"7"}) {
		t.Error(test.DiffMessage(h.got[0], ctx.Param{"id": {"7"}}, "B1: the handler sees the route parameters"))
	}
}

func TestHTTPHandlerPlan_InjectsFile(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HFile", nil), c)
	if len(h.got) != 1 || !planSameMap(h.got[0], c.File()) {
		t.Error(test.DiffMessage(h.got, c.File(), "B1, B8: ctx.File is c.File()"))
	}
}

func TestHTTPHandlerPlan_InjectsNext(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	c := planRequestContext()
	ctx.HTTPSetNext(c, func() { planNextCalls++ })
	planInvoke(planHTTP(t, h, "HNext", nil), c)
	if planNextCalls != 1 {
		t.Error(test.DiffMessage(planNextCalls, 1, "B1, B8: ctx.Next acts on c.Next"))
	}
}

func TestHTTPHandlerPlan_InjectsRedirect(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, "HRedirect", nil), c)
	w := recorderOf(c)
	if got := w.Header().Get("Location"); got != "/elsewhere" {
		t.Error(test.DiffMessage(got, "/elsewhere", "B1, B8: ctx.Redirect acts on c"))
	}
	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "B1, B8: ctx.Redirect writes the redirect status"))
	}
}

func TestHTTPHandlerPlan_InjectsPublisher(t *testing.T) {
	pub := planInstallPublisher(t)
	h := &planCtl{}
	planInvoke(planHTTP(t, h, "HPublisher", nil), planRequestContext())
	if len(h.got) != 1 || h.got[0] != any(pub) {
		t.Error(test.DiffMessage(h.got, []any{pub}, "B1: common.Publisher is the value stored under publisherKey"))
	}
}

func TestHTTPHandlerPlan_PublisherIsFixedAtBuild(t *testing.T) {
	pub := planInstallPublisher(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HPublisher", nil)
	planInstallPublisher(t)
	planInvoke(plan, planRequestContext())
	if len(h.got) != 1 || h.got[0] != any(pub) {
		t.Error(test.DiffMessage(h.got, []any{pub}, "O6: the publisher is looked up when the plan is built"))
	}
}

func TestHTTPHandlerPlan_NilResponseWriterIsTypedNil(t *testing.T) {
	h := &planCtl{}
	c := planRequestContext()
	c.ResponseWriter = nil
	plan := planHTTP(t, h, "HWriter", nil)
	if rec := planPanicOf(func() { planInvoke(plan, c) }); rec != nil {
		t.Fatal(test.DiffMessage(rec, nil, "B1: a nil ResponseWriter must not panic"))
	}
	if h.calls != 1 {
		t.Fatal(test.DiffMessage(h.calls, 1, "B1: the handler still runs"))
	}
	if w, _ := h.got[0].(http.ResponseWriter); w != nil || h.got[0] != nil {
		t.Error(test.DiffMessage(h.got[0], nil, "B1: the handler sees a nil ResponseWriter"))
	}
}

func TestHTTPHandlerPlan_MatchesReferenceInvocation(t *testing.T) {
	planResetState(t)
	planInstallPublisher(t)
	providers := planProviders()
	h := &planCtl{}
	recv, method := planMethod(t, h, "HAll")
	plan := newHTTPHandlerPlan(recv, method, providers, planCtr)
	c := planRequestContext()
	ctx.HTTPSetNext(c, func() { planNextCalls++ })

	planPipeCalls = nil
	refResults := planReferenceHTTP(t, recv, method.Name, providers, c)
	refGot := h.got
	refPipes := planPipeCalls
	refCalls := h.calls

	h.got = nil
	h.calls = 0
	planPipeCalls = nil
	results := planInvoke(plan, c)

	if len(refGot) != 19 {
		t.Fatal(test.DiffMessage(len(refGot), 19, "fixture: the reference handler records 19 arguments"))
	}
	if !reflect.DeepEqual(h.got, refGot) {
		t.Error(test.DiffMessage(h.got, refGot, "B8: the plan passes the arguments of the reference invocation"))
	}
	if !reflect.DeepEqual(planPipeCalls, refPipes) {
		t.Error(test.DiffMessage(planPipeCalls, refPipes, "B8, B10: pipes get the sources and metadata of the reference invocation"))
	}
	if h.calls != refCalls {
		t.Error(test.DiffMessage(h.calls, refCalls, "B7: one handler call"))
	}
	if len(results) != len(refResults) {
		t.Fatal(test.DiffMessage(len(results), len(refResults), "B7: results"))
	}
	for i := range results {
		if !reflect.DeepEqual(results[i].Interface(), refResults[i].Interface()) {
			t.Error(test.DiffMessage(results[i].Interface(), refResults[i].Interface(), "B8: result "+strconv.Itoa(i)))
		}
	}
	bp, _ := h.got[13].(provBodyPipe)
	if bp.P.Tag != "injected" {
		t.Error(test.DiffMessage(bp.P.Tag, "injected", "B3: the body pipe's provider field is injected"))
	}
}

func TestNewHTTPHandlerPlan_UnknownTypePanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "Unknown", nil) })
	want := planUnresolved("func(*ctx.HTTPContext, int) string", 1)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: an unknown type panics at build with the func type and the index"))
	}
}

func TestNewHTTPHandlerPlan_UnknownStructPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "UnknownStruct", nil) })
	want := planUnresolved("func(core.planUnknownArg)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: an unknown struct panics at build"))
	}
}

func TestNewHTTPHandlerPlan_VariadicPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "Variadic", nil) })
	want := planUnresolved("func(...int)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: a variadic parameter panics at build"))
	}
}

func TestNewHTTPHandlerPlan_InterfaceParameterPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "Interface", nil) })
	want := planUnresolved("func(error)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: an interface type other than the listed ones panics at build"))
	}
}

func TestNewHTTPHandlerPlan_WSTypeInHTTPHandlerPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "WSContext", nil) })
	want := planUnresolved("func(*ctx.WSContext)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: a WS-only type in an HTTP handler panics at build"))
	}
}

func TestNewHTTPHandlerPlan_WSPayloadPipeInHTTPHandlerPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "WSPayloadPipe", nil) })
	want := planUnresolved("func(core.recWSPayloadPipe)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: a WSPayloadPipeable in an HTTP handler panics at build"))
	}
}

func TestNewHTTPHandlerPlan_LookalikeTypePanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "Lookalike", nil) })
	want := planUnresolved("func(*ctx.HTTPContext) string", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B1, B2: *ctx.HTTPContext of another package named ctx is not the framework's context"))
	}
}

func TestNewHTTPHandlerPlan_MissingPublisherPanics(t *testing.T) {
	planRemovePublisher(t)
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "NeedsPublisher", nil) })
	want := planUnresolved("func(common.Publisher)", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: common.Publisher with nothing stored panics at build"))
	}
}

func TestNewHTTPHandlerPlan_PipeGetsLocalProvider(t *testing.T) {
	h := &planCtl{}
	planInvoke(planHTTP(t, h, "HProvQuery", planProviders()), planRequestContext())
	got, _ := h.got[0].(provQueryPipe)
	if got.P.Tag != "injected" {
		t.Error(test.DiffMessage(got.P.Tag, "injected", "B3: a pipe's provider field comes from the injected providers"))
	}
}

func TestNewHTTPHandlerPlan_PipeGetsGlobalProvider(t *testing.T) {
	planCtr = newContainer()
	planCtr.addGlobal(fnTestProvider{Tag: "global"})
	t.Cleanup(func() { planCtr = newContainer() })
	h := &planCtl{}
	planInvoke(planHTTP(t, h, "HProvQuery", map[string]Provider{}), planRequestContext())
	got, _ := h.got[0].(provQueryPipe)
	if got.P.Tag != "global" {
		t.Error(test.DiffMessage(got.P.Tag, "global", "B3: a pipe's provider field falls back to the global providers"))
	}
}

func TestNewHTTPHandlerPlan_PipeGetsGlobalInterface(t *testing.T) {
	pub := planInstallPublisher(t)
	h := &planCtl{}
	planInvoke(planHTTP(t, h, "HIfaceQuery", map[string]Provider{}), planRequestContext())
	got, _ := h.got[0].(ifaceQueryPipe)
	if got.Pub != common.Publisher(pub) {
		t.Error(test.DiffMessage(got.Pub, pub, "B3: a pipe's interface field falls back to the global interfaces"))
	}
}

func TestNewHTTPHandlerPlan_PipeUnexportedFieldPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, &planCtl{}, "HUnexported", nil) })
	if rec == nil || !strings.Contains(fmt.Sprint(rec), "can't set value to unexported 'hidden' field") {
		t.Error(test.DiffMessage(rec, "can't set value to unexported 'hidden' field", "B3: an unexported pipe field panics at build"))
	}
}

func TestNewHTTPHandlerPlan_PipeUnresolvedProviderPanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, &planCtl{}, "HUnresolved", map[string]Provider{}) })
	if rec == nil || !strings.Contains(fmt.Sprint(rec), "can't resolve dependency") {
		t.Error(test.DiffMessage(rec, "can't resolve dependency ...", "B3: an unresolvable provider field panics at build"))
	}
}

func TestNewHTTPHandlerPlan_ScalarPipeAccepted(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	var plan *httpHandlerPlan
	if rec := planPanicOf(func() { plan = planHTTP(t, h, "HScalarPipe", nil) }); rec != nil {
		t.Fatal(test.DiffMessage(rec, nil, "B4: a pipe that is not a struct is accepted"))
	}
	planInvoke(plan, planRequestContext())
	if len(h.got) != 1 || h.got[0] != any(scalarPipe(42)) {
		t.Error(test.DiffMessage(h.got, []any{scalarPipe(42)}, "B4: the handler receives the transformed value"))
	}
}

func TestNewHTTPHandlerPlan_PointerPipePanics(t *testing.T) {
	rec := planPanicOf(func() { planHTTP(t, planBadCtl{}, "PointerPipe", nil) })
	want := "dependency injection: pipe '*core.recQueryPipe' of the 'func(*core.recQueryPipe)' must not be a pointer"
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B5: a pointer pipe panics at build"))
	}
}

func TestNewHTTPHandlerPlan_DoesNotCallHandlerOrTransform(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	planHTTP(t, h, "HCountingPipe", nil)
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B6: building a plan never calls the handler"))
	}
	if planTransformHits != 0 {
		t.Error(test.DiffMessage(planTransformHits, 0, "B6: building a plan never calls Transform"))
	}
}

func TestHTTPHandlerPlan_ReturnsResultsInOrder(t *testing.T) {
	h := &planCtl{}
	got := planInvoke(planHTTP(t, h, "HThree", nil), planRequestContext())
	if len(got) != 3 {
		t.Fatal(test.DiffMessage(len(got), 3, "B7: three results"))
	}
	if got[0].Interface() != any(202) || got[1].Interface() != any("a") || got[2].Interface() != any("b") {
		t.Error(test.DiffMessage([]any{got[0].Interface(), got[1].Interface(), got[2].Interface()}, []any{202, "a", "b"}, "B7: results in declaration order"))
	}
}

func TestHTTPHandlerPlan_TwoResults(t *testing.T) {
	h := &planCtl{}
	got := planInvoke(planHTTP(t, h, "HTwo", nil), planRequestContext())
	if len(got) != 2 || got[0].Int() != 201 || got[1].String() != "two" {
		t.Error(test.DiffMessage(got, []any{201, "two"}, "B7: (int, string) results"))
	}
}

func TestHTTPHandlerPlan_OneResult(t *testing.T) {
	h := &planCtl{}
	got := planInvoke(planHTTP(t, h, "HOne", nil), planRequestContext())
	if len(got) != 1 || got[0].String() != "one" {
		t.Error(test.DiffMessage(got, []any{"one"}, "B7: one result"))
	}
}

func TestHTTPHandlerPlan_NoResult(t *testing.T) {
	h := &planCtl{}
	got := planInvoke(planHTTP(t, h, "HNone", nil), planRequestContext())
	if len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "B7: a handler without results returns none"))
	}
	if h.calls != 1 {
		t.Error(test.DiffMessage(h.calls, 1, "B7: the handler ran"))
	}
}

func TestHTTPHandlerPlan_UsesControllerValue(t *testing.T) {
	h := &planCtl{tag: "from-controller"}
	got := planInvoke(planHTTP(t, h, "HTag", nil), planRequestContext())
	if len(got) != 1 || got[0].String() != "from-controller" {
		t.Error(test.DiffMessage(got, []any{"from-controller"}, "B7: the handler runs on the controller value given at build"))
	}
}

func TestHTTPHandlerPlan_UsesValueReceiverController(t *testing.T) {
	got := planInvoke(planHTTP(t, planValueCtl{tag: "value"}, "HTag", nil), planRequestContext())
	if len(got) != 1 || got[0].String() != "value" {
		t.Error(test.DiffMessage(got, []any{"value"}, "B7: a controller with value receivers works"))
	}
}

func TestHTTPHandlerPlan_CallsHandlerOnce(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HMixed", nil)
	planInvoke(plan, planRequestContext())
	if h.calls != 1 {
		t.Error(test.DiffMessage(h.calls, 1, "B7: one invoke is one handler call"))
	}
	planInvoke(plan, planRequestContext())
	if h.calls != 2 {
		t.Error(test.DiffMessage(h.calls, 2, "B7: two invokes are two handler calls"))
	}
}

func TestHTTPHandlerPlan_TransformsRunInParameterOrder(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	planInvoke(planHTTP(t, h, "HOrder", nil), planRequestContext())
	want := []string{"a", "b", "c", "handler"}
	if !reflect.DeepEqual(planOrder, want) {
		t.Error(test.DiffMessage(planOrder, want, "B9: Transform in parameter order, then the handler"))
	}
}

func TestHTTPHandlerPlan_ArgumentPanicSkipsLaterArgumentsAndHandler(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HPanicMid", nil)
	rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) })
	if rec != "pipe exploded" {
		t.Error(test.DiffMessage(rec, "pipe exploded", "B9: the panic propagates unchanged"))
	}
	want := []string{"a", "boom"}
	if !reflect.DeepEqual(planOrder, want) {
		t.Error(test.DiffMessage(planOrder, want, "B9: later arguments are not resolved"))
	}
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B9: the handler is not called"))
	}
}

func TestHTTPHandlerPlan_HandlerPanicPropagates(t *testing.T) {
	h := &planCtl{}
	plan := planHTTP(t, h, "HPanics", nil)
	rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) })
	if rec != "handler exploded" {
		t.Error(test.DiffMessage(rec, "handler exploded", "B9: a handler's panic propagates unchanged"))
	}
}

func TestHTTPHandlerPlan_ExceptionFromAccessorPropagates(t *testing.T) {
	planResetState(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HBodyThenPipe", nil)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":"bbbbbbbbbbbbbbbbbbbbbbbb"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Body = http.MaxBytesReader(w, r.Body, 4)
	c := ctx.NewHTTPContext()
	ctx.HTTPInit(c, w, r)

	rec := planPanicOf(func() { planInvoke(plan, c) })
	ex, ok := planExceptionOf(rec)
	if !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception", "B9: an accessor's exception propagates"))
	}
	if ex.Code() != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(ex.Code(), http.StatusRequestEntityTooLarge, "B9: an oversized body is a 413"))
	}
	if h.calls != 0 || len(planOrder) != 0 {
		t.Error(test.DiffMessage([]any{h.calls, planOrder}, []any{0, []string(nil)}, "B9: nothing after the failed accessor runs"))
	}
}

func TestHTTPHandlerPlan_ContextPipe(t *testing.T) {
	planPipeKindCase(t, "HCtxPipe", common.ContextPipeableKey, func(c *ctx.HTTPContext) any { return c }, "context")
}

func TestHTTPHandlerPlan_BodyPipe(t *testing.T) {
	planPipeKindCase(t, "HBodyPipe", common.BodyPipeableKey, func(c *ctx.HTTPContext) any { return c.Body() }, "body")
}

func TestHTTPHandlerPlan_FormPipe(t *testing.T) {
	planPipeKindCase(t, "HFormPipe", common.FormPipeableKey, func(c *ctx.HTTPContext) any { return c.Form() }, "form")
}

func TestHTTPHandlerPlan_QueryPipe(t *testing.T) {
	planPipeKindCase(t, "HQueryPipe", common.QueryPipeableKey, func(c *ctx.HTTPContext) any { return c.Query() }, "query")
}

func TestHTTPHandlerPlan_HeaderPipe(t *testing.T) {
	planPipeKindCase(t, "HHeaderPipe", common.HeaderPipeableKey, func(c *ctx.HTTPContext) any { return c.Header() }, "header")
}

func TestHTTPHandlerPlan_ParamPipe(t *testing.T) {
	planPipeKindCase(t, "HParamPipe", common.ParamPipeableKey, func(c *ctx.HTTPContext) any { return c.Param() }, "param")
}

func TestHTTPHandlerPlan_FilePipe(t *testing.T) {
	planPipeKindCase(t, "HFilePipe", common.FilePipeableKey, func(c *ctx.HTTPContext) any { return c.File() }, "file")
}

func TestHTTPHandlerPlan_PipeMetadataContextTypeIsHTTP(t *testing.T) {
	planResetState(t)
	planInstallPublisher(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HAll", planProviders())
	c := planRequestContext()
	ctx.HTTPSetNext(c, func() {})
	planInvoke(plan, c)
	if len(planPipeCalls) != 7 {
		t.Fatal(test.DiffMessage(len(planPipeCalls), 7, "fixture: seven pipes"))
	}
	for _, call := range planPipeCalls {
		if call.Meta.ContextType != common.ContextTypeHTTP {
			t.Error(test.DiffMessage(call.Meta.ContextType, common.ContextTypeHTTP, "B10 (0005 Q3): ContextType of the "+call.Kind+" pipe"))
		}
	}
}

func TestHTTPHandlerPlan_PipeReceiverIsFreshEachRequest(t *testing.T) {
	h := &planCtl{}
	plan := planHTTP(t, h, "HSeenPipe", nil)
	for i := 0; i < 3; i++ {
		planInvoke(plan, planRequestContext())
		if !reflect.DeepEqual(h.got, []any{1, 1}) {
			t.Error(test.DiffMessage(h.got, []any{1, 1}, "B10: request "+strconv.Itoa(i)+": a Transform that changes its receiver leaves no trace"))
		}
	}
}

func TestHTTPHandlerPlan_ConcurrentInvokeNoRace(t *testing.T) {
	plan := planHTTP(t, planConcurrentCtl{}, "Run", nil)
	ev := &trace.Tracer{}

	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				c := planRequestContext()
				got := plan.invoke(c, ev)
				if len(got) != 1 || got[0].String() != "1:1" {
					t.Error(test.DiffMessage(got, "1:1", "B10: concurrent requests see a fresh receiver"))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestHTTPHandlerPlan_TransformReturnsNil(t *testing.T) {
	h := &planCtl{}
	plan := planHTTP(t, h, "HNilPipe", nil)
	rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) })
	planWantTransformFailure(t, rec, "core.nilPipe", "nil")
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B11: the handler is not called"))
	}
}

func TestHTTPHandlerPlan_TransformReturnsPointer(t *testing.T) {
	h := &planCtl{}
	plan := planHTTP(t, h, "HPointerResult", nil)
	rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) })
	planWantTransformFailure(t, rec, "core.pointerResultPipe", "*core.pointerResultPipe")
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B11: the handler is not called"))
	}
}

func TestHTTPHandlerPlan_TransformReturnsOtherType(t *testing.T) {
	h := &planCtl{}
	plan := planHTTP(t, h, "HOtherResult", nil)
	rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) })
	planWantTransformFailure(t, rec, "core.otherResultPipe", "string")
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B11: the handler is not called"))
	}
}

func TestHTTPHandlerPlan_TransformReturnsAssignableType(t *testing.T) {
	h := &planCtl{}
	var plan *httpHandlerPlan
	if rec := planPanicOf(func() { plan = planHTTP(t, h, "HMapPipe", nil) }); rec != nil {
		t.Fatal(test.DiffMessage(rec, nil, "B4: a pipe that is not a struct is accepted"))
	}
	if rec := planPanicOf(func() { planInvoke(plan, planRequestContext()) }); rec != nil {
		t.Fatal(test.DiffMessage(rec, nil, "B11: a value assignable to the parameter's type is accepted"))
	}
	if len(h.got) != 1 || !reflect.DeepEqual(h.got[0], mapPipe{"a": 1}) {
		t.Error(test.DiffMessage(h.got, []any{mapPipe{"a": 1}}, "B11: the assignable value is passed as is"))
	}
}

func TestHTTPHandlerPlan_NoListenerEmitsNothing(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	log.stop()
	h := &planCtl{}
	plan := planHTTP(t, h, "HMixed", nil)
	plan.invoke(planRequestContext(), ev)
	if got := log.all(); len(got) != 0 {
		t.Error(test.DiffMessage(got, "no events", "B12: without a listener nothing is emitted"))
	}
	if h.calls != 1 {
		t.Error(test.DiffMessage(h.calls, 1, "B12: the handler still runs"))
	}
}

func TestHTTPHandlerPlan_TracedEqualsUntraced(t *testing.T) {
	planResetState(t)
	planInstallPublisher(t)
	h := &planCtl{}
	plan := planHTTP(t, h, "HAll", planProviders())

	c := planRequestContext()
	ctx.HTTPSetNext(c, func() { planNextCalls++ })
	untraced := plan.invoke(c, &trace.Tracer{})
	gotUntraced := h.got
	pipesUntraced := planPipeCalls

	h.got = nil
	planPipeCalls = nil
	ev := &trace.Tracer{}
	planListen(ev)
	traced := plan.invoke(c, ev)

	if !reflect.DeepEqual(h.got, gotUntraced) {
		t.Error(test.DiffMessage(h.got, gotUntraced, "B12: the arguments are the same with a listener"))
	}
	if !reflect.DeepEqual(planPipeCalls, pipesUntraced) {
		t.Error(test.DiffMessage(planPipeCalls, pipesUntraced, "B12: the pipe calls are the same with a listener"))
	}
	if len(traced) != len(untraced) {
		t.Fatal(test.DiffMessage(len(traced), len(untraced), "B12: the results are the same with a listener"))
	}
	for i := range traced {
		if !reflect.DeepEqual(traced[i].Interface(), untraced[i].Interface()) {
			t.Error(test.DiffMessage(traced[i].Interface(), untraced[i].Interface(), "B12: result "+strconv.Itoa(i)))
		}
	}
}

func TestHTTPHandlerPlan_PipeEmitsStagePipe(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	c := planRequestContext()
	planHTTP(t, &planCtl{}, "HSlow", nil).invoke(c, ev)

	got := log.byStage(trace.StagePipe)
	if len(got) != 1 {
		t.Fatal(test.DiffMessage(len(got), 1, "B13: one pipe event for one pipe"))
	}
	e := got[0]
	if e.ID != c.ID() || e.ID == "" {
		t.Error(test.DiffMessage(e.ID, c.ID(), "B13: ID"))
	}
	if want := reflect.TypeOf(slowPipe{}).String(); e.Name != want {
		t.Error(test.DiffMessage(e.Name, want, "B13: Name is the parameter type"))
	}
	if e.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(e.Transport, trace.TransportHTTP, "B13: Transport"))
	}
	if e.Duration < 20*time.Millisecond {
		t.Error(test.DiffMessage(e.Duration, ">= 20ms", "B13: Duration covers the Transform"))
	}
}

func TestHTTPHandlerPlan_TwoPipesEmitInOrder(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	planHTTP(t, &planCtl{}, "HTwoSlow", nil).invoke(planRequestContext(), ev)

	got := log.byStage(trace.StagePipe)
	want := []string{reflect.TypeOf(slowPipe{}).String(), reflect.TypeOf(slowPipeB{}).String()}
	if len(got) != 2 || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Error(test.DiffMessage(got, want, "B13: pipe events in parameter order"))
	}
}

func TestHTTPHandlerPlan_NonPipeEmitsNoStagePipe(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	h := &planCtl{}
	c := planRequestContext()
	planHTTP(t, h, "HCtx", nil).invoke(c, ev)

	if got := log.byStage(trace.StagePipe); len(got) != 0 {
		t.Error(test.DiffMessage(got, "no pipe events", "B13: other arguments emit nothing"))
	}
}

func TestHTTPHandlerPlan_PipePanicStillEmitsStagePipe(t *testing.T) {
	planResetState(t)
	ev := &trace.Tracer{}
	log := planListen(ev)
	plan := planHTTP(t, &planCtl{}, "HPanicMid", nil)
	rec := planPanicOf(func() { plan.invoke(planRequestContext(), ev) })
	if rec != "pipe exploded" {
		t.Fatal(test.DiffMessage(rec, "pipe exploded", "fixture: the pipe panics"))
	}
	got := log.byStage(trace.StagePipe)
	want := []string{reflect.TypeOf(orderPipeA{}).String(), reflect.TypeOf(panicPipe{}).String()}
	if len(got) != 2 || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Error(test.DiffMessage(got, want, "B13: the panicking pipe also emits its event"))
	}
	if h := log.byStage(trace.StageHandler); len(h) != 0 {
		t.Error(test.DiffMessage(h, "no handler event", "B14: no handler event when an argument failed"))
	}
}

func TestHTTPHandlerPlan_WrongPipeResultStillEmitsStagePipe(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	plan := planHTTP(t, &planCtl{}, "HNilPipe", nil)
	rec := planPanicOf(func() { plan.invoke(planRequestContext(), ev) })
	if _, ok := planExceptionOf(rec); !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception", "fixture: the result is refused"))
	}
	got := log.byStage(trace.StagePipe)
	if len(got) != 1 || got[0].Name != "core.nilPipe" {
		t.Error(test.DiffMessage(got, "one pipe event for core.nilPipe", "B13: a refused result still emits the pipe event"))
	}
}

func TestHTTPHandlerPlan_EmitsStageHandler(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	c := planRequestContext()
	planHTTP(t, &planCtl{}, "HOne", nil).invoke(c, ev)

	got := log.byStage(trace.StageHandler)
	if len(got) != 1 {
		t.Fatal(test.DiffMessage(len(got), 1, "B14: one handler event"))
	}
	e := got[0]
	if e.Name != "HOne" {
		t.Error(test.DiffMessage(e.Name, "HOne", "B14: Name is the method name"))
	}
	if e.ID != c.ID() || e.ID == "" {
		t.Error(test.DiffMessage(e.ID, c.ID(), "B14: ID"))
	}
	if e.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(e.Transport, trace.TransportHTTP, "T7: the handler event carries its Transport"))
	}
}

func TestHTTPHandlerPlan_HandlerPanicStillEmitsStageHandler(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	plan := planHTTP(t, &planCtl{}, "HPanics", nil)
	rec := planPanicOf(func() { plan.invoke(planRequestContext(), ev) })
	if rec != "handler exploded" {
		t.Fatal(test.DiffMessage(rec, "handler exploded", "fixture: the handler panics"))
	}
	got := log.byStage(trace.StageHandler)
	if len(got) != 1 || got[0].Name != "HPanics" {
		t.Error(test.DiffMessage(got, "one handler event named HPanics", "B14: a panicking handler still emits its event"))
	}
}

func TestHTTPHandlerPlan_ArgumentFailureEmitsNoStageHandler(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	plan := planHTTP(t, &planCtl{}, "HNilPipe", nil)
	_ = planPanicOf(func() { plan.invoke(planRequestContext(), ev) })
	if got := log.byStage(trace.StageHandler); len(got) != 0 {
		t.Error(test.DiffMessage(got, "no handler event", "B14: no handler event when an argument failed"))
	}
}

func TestHTTPHandlerPlan_HandlerDurationExcludesPipes(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	planHTTP(t, &planCtl{}, "HSlow", nil).invoke(planRequestContext(), ev)

	got := log.byStage(trace.StageHandler)
	if len(got) != 1 {
		t.Fatal(test.DiffMessage(len(got), 1, "B14: one handler event"))
	}
	if got[0].Duration >= 20*time.Millisecond {
		t.Error(test.DiffMessage(got[0].Duration, "< 20ms", "B14: the handler duration excludes the pipes' time"))
	}
}

func TestHTTPHandlerPlan_ListenerAddedBetweenCalls(t *testing.T) {
	ev := &trace.Tracer{}
	plan := planHTTP(t, &planCtl{}, "HOne", nil)

	plan.invoke(planRequestContext(), ev)
	log := planListen(ev)
	plan.invoke(planRequestContext(), ev)
	if got := log.byStage(trace.StageHandler); len(got) != 1 {
		t.Errorf("B17: a listener added between calls takes effect on the next call: %d handler events", len(got))
	}

	log.stop()
	plan.invoke(planRequestContext(), ev)
	if got := log.byStage(trace.StageHandler); len(got) != 1 {
		t.Errorf("B17: a listener removed between calls takes effect on the next call: %d handler events", len(got))
	}
}

func TestApp_LookalikeParameterPanicsAtCreate(t *testing.T) {
	app := New()

	rec := planPanicOf(func() {
		app.Create(NewModuleBuilder().Controllers(appLookalikeController{}).Build())
	})

	want := planUnresolved("func(*ctx.HTTPContext) string", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "bug 1, B1, B2: a lookalike *ctx.HTTPContext stops Create()"))
	}
}

func TestApp_PipeWrongResultIs500(t *testing.T) {
	w := serveResult(t, "/wrongpipe")
	if w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, "bug 5, B11: a wrong Transform result is a 500"))
	}
	if strings.Contains(w.Body.String(), "reflect:") {
		t.Error(test.DiffMessage(w.Body.String(), "no reflection text", "bug 5, B11: the client never sees reflection's message"))
	}
	if strings.Contains(w.Body.String(), "unreachable") {
		t.Error(test.DiffMessage(w.Body.String(), "no handler output", "B11: the handler is not called"))
	}
}

func TestApp_ServeRouteHandlerEventHasName(t *testing.T) {
	names := handlerEventNames(t, "/logo.png")
	if len(names) != 1 || names[0] != "SERVE_logo_png_FILE" {
		t.Error(test.DiffMessage(names, []string{"SERVE_logo_png_FILE"}, "bug 7, B16: the handler event of a SERVE route carries the method name"))
	}
}

func TestApp_ReadRouteHandlerEventHasName(t *testing.T) {
	names := handlerEventNames(t, "/named")
	if len(names) != 1 || names[0] != "READ_named" {
		t.Error(test.DiffMessage(names, []string{"READ_named"}, "B16: the handler event of a READ route carries the method name"))
	}
}

func TestWSHandlerPlan_InjectsWSContext(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WCtx", nil).invoke(c, &trace.Tracer{})
	if len(h.got) != 1 || h.got[0] != any(c) {
		t.Error(test.DiffMessage(h.got, []any{c}, "B1, B8: *ctx.WSContext is the message's context"))
	}
}

func TestWSHandlerPlan_InjectsConn(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WConn", nil).invoke(c, &trace.Tracer{})
	if len(h.got) != 1 || h.got[0] != any(c.Conn) {
		t.Error(test.DiffMessage(h.got, []any{c.Conn}, "B1, B8: *websocket.Conn is c.Conn"))
	}
}

func TestWSHandlerPlan_InjectsWSPayload(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WPayload", nil).invoke(c, &trace.Tracer{})
	if len(h.got) != 1 || !planSameMap(h.got[0], c.WSPayload()) {
		t.Error(test.DiffMessage(h.got, c.WSPayload(), "B1, B8: ctx.WSPayload is c.WSPayload()"))
	}
	if got, _ := h.got[0].(ctx.WSPayload); got["k"] != "v" {
		t.Error(test.DiffMessage(h.got[0], ctx.WSPayload{"k": "v"}, "B1: the handler sees the payload"))
	}
}

func TestWSHandlerPlan_InjectsNext(t *testing.T) {
	planResetState(t)
	h := &planWSCtl{}
	c := planWSContext()
	ctx.WSSetNext(c, func() { planNextCalls++ })
	planWS(t, h, "WNext", nil).invoke(c, &trace.Tracer{})
	if planNextCalls != 1 {
		t.Error(test.DiffMessage(planNextCalls, 1, "B1, B8: ctx.Next acts on c.Next"))
	}
}

func TestWSHandlerPlan_InjectsPublisher(t *testing.T) {
	pub := planInstallPublisher(t)
	h := &planWSCtl{}
	planWS(t, h, "WPublisher", nil).invoke(planWSContext(), &trace.Tracer{})
	if len(h.got) != 1 || h.got[0] != any(pub) {
		t.Error(test.DiffMessage(h.got, []any{pub}, "B1: common.Publisher is the value stored under publisherKey"))
	}
}

func TestWSHandlerPlan_MatchesReferenceInvocation(t *testing.T) {
	planResetState(t)
	planInstallPublisher(t)
	h := &planWSCtl{}
	recv, method := planMethod(t, h, "WAll")
	plan := newWSHandlerPlan(recv, method, nil, planCtr)
	c := planWSContext()
	ctx.WSSetNext(c, func() { planNextCalls++ })

	planPipeCalls = nil
	handler := recv.MethodByName(method.Name)
	ft := handler.Type()
	args := make([]reflect.Value, ft.NumIn())
	pub, _ := planCtr.interfaceFor(publisherKey)
	for i := range args {
		switch at := ft.In(i); at {
		case reflect.TypeOf((*ctx.WSContext)(nil)):
			args[i] = reflect.ValueOf(c)
		case reflect.TypeOf((*websocket.Conn)(nil)):
			args[i] = reflect.ValueOf(c.Conn)
		case reflect.TypeOf(ctx.WSPayload(nil)):
			args[i] = reflect.ValueOf(c.WSPayload())
		case reflect.TypeOf(func() {}):
			args[i] = reflect.ValueOf(ctx.WSNext(c))
		case reflect.TypeOf((*common.Publisher)(nil)).Elem():
			args[i] = reflect.ValueOf(pub)
		default:
			inst, err := injectDependencies(reflect.New(at).Elem().Interface(), "pipe", nil, planCtr)
			if err != nil {
				t.Fatal(err)
			}
			out := inst.Interface().(common.WSPayloadPipeable).Transform(c.WSPayload(), common.ArgumentMetadata{
				ContextType: common.ContextTypeWS,
				ParamType:   common.WSPayloadPipeableKey,
			})
			args[i] = reflect.ValueOf(out)
		}
	}
	refResults := handler.Call(args)
	refGot := h.got
	refPipes := planPipeCalls

	h.got = nil
	h.calls = 0
	planPipeCalls = nil
	results := plan.invoke(c, &trace.Tracer{})

	if len(refGot) != 6 {
		t.Fatal(test.DiffMessage(len(refGot), 6, "fixture: the reference handler records 6 arguments"))
	}
	if !reflect.DeepEqual(h.got, refGot) {
		t.Error(test.DiffMessage(h.got, refGot, "B8: the plan passes the arguments of the reference invocation"))
	}
	if !reflect.DeepEqual(planPipeCalls, refPipes) {
		t.Error(test.DiffMessage(planPipeCalls, refPipes, "B8, B10: the pipe gets the source and metadata of the reference invocation"))
	}
	if h.calls != 1 {
		t.Error(test.DiffMessage(h.calls, 1, "B7: one handler call"))
	}
	if len(results) != len(refResults) {
		t.Fatal(test.DiffMessage(len(results), len(refResults), "B7: results"))
	}
	for i := range results {
		if !reflect.DeepEqual(results[i].Interface(), refResults[i].Interface()) {
			t.Error(test.DiffMessage(results[i].Interface(), refResults[i].Interface(), "B8: result"))
		}
	}
}

func TestNewWSHandlerPlan_HTTPTypePanics(t *testing.T) {
	cases := []struct {
		method string
		want   string
	}{
		{"HTTPContext", "func(*ctx.HTTPContext)"},
		{"Body", "func(ctx.Body)"},
		{"QueryPipe", "func(core.recQueryPipe)"},
		{"Request", "func(func(string))"},
	}
	for _, c := range cases {
		rec := planPanicOf(func() {
			recv, m := planMethod(t, planWSBadCtl{}, c.method)
			newWSHandlerPlan(recv, m, nil, planCtr)
		})
		want := planUnresolved(c.want, 0)
		if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
			t.Error(test.DiffMessage(rec, want, "B2: an HTTP-only type in a WS handler panics at build ("+c.method+")"))
		}
	}
}

func TestNewWSHandlerPlan_UnknownTypePanics(t *testing.T) {
	rec := planPanicOf(func() {
		recv, m := planMethod(t, planWSBadCtl{}, "Unknown")
		newWSHandlerPlan(recv, m, nil, planCtr)
	})
	want := planUnresolved("func(*ctx.WSContext, int)", 1)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "B2: an unknown type panics at build with the func type and the index"))
	}
}

func TestWSHandlerPlan_PayloadPipe(t *testing.T) {
	planResetState(t)
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WPipe", nil).invoke(c, &trace.Tracer{})

	if len(planPipeCalls) != 1 {
		t.Fatal(test.DiffMessage(len(planPipeCalls), 1, "B10: Transform runs once per message"))
	}
	call := planPipeCalls[0]
	if !planSameMap(call.Src, c.WSPayload()) {
		t.Error(test.DiffMessage(call.Src, c.WSPayload(), "B10: the pipe's source is c.WSPayload()"))
	}
	if call.Meta.ParamType != common.WSPayloadPipeableKey {
		t.Error(test.DiffMessage(call.Meta.ParamType, common.WSPayloadPipeableKey, "B10: ParamType"))
	}
	if call.Meta.ContextType != common.ContextTypeWS {
		t.Error(test.DiffMessage(call.Meta.ContextType, common.ContextTypeWS, "B10 (0005 Q3): ContextType of a WS pipe"))
	}
	if got, _ := h.got[0].(recWSPayloadPipe); got.Kind != "wsPayload" {
		t.Error(test.DiffMessage(h.got, "Kind wsPayload", "B10: the handler receives what Transform returned"))
	}
}

func TestWSHandlerPlan_TransformReturnsNil(t *testing.T) {
	h := &planWSCtl{}
	plan := planWS(t, h, "WNilPipe", nil)
	rec := planPanicOf(func() { plan.invoke(planWSContext(), &trace.Tracer{}) })
	planWantTransformFailure(t, rec, "core.nilWSPipe", "nil")
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B11: the handler is not called"))
	}
}

func TestWSHandlerPlan_PipeEmitsStagePipe(t *testing.T) {
	planResetState(t)
	ev := &trace.Tracer{}
	log := planListen(ev)
	c := planWSContext()
	planWS(t, &planWSCtl{}, "WPipe", nil).invoke(c, ev)

	got := log.byStage(trace.StagePipe)
	if len(got) != 1 {
		t.Fatal(test.DiffMessage(len(got), 1, "B13: one pipe event for one pipe"))
	}
	e := got[0]
	if e.ID != c.ID() || e.ID == "" {
		t.Error(test.DiffMessage(e.ID, c.ID(), "B13: ID"))
	}
	if want := reflect.TypeOf(recWSPayloadPipe{}).String(); e.Name != want {
		t.Error(test.DiffMessage(e.Name, want, "B13: Name is the parameter type"))
	}
	if e.Transport != trace.TransportWS {
		t.Error(test.DiffMessage(e.Transport, trace.TransportWS, "B13: Transport"))
	}
}

func TestWSHandlerPlan_PipePanicStillEmitsStagePipe(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	plan := planWS(t, &planWSCtl{}, "WPanicPipe", nil)
	rec := planPanicOf(func() { plan.invoke(planWSContext(), ev) })
	if rec != "ws pipe exploded" {
		t.Fatal(test.DiffMessage(rec, "ws pipe exploded", "B9: the pipe's panic propagates unchanged"))
	}
	got := log.byStage(trace.StagePipe)
	if len(got) != 1 || got[0].Transport != trace.TransportWS || got[0].Name != reflect.TypeOf(panicWSPipe{}).String() {
		t.Fatal(test.DiffMessage(got, "one WS pipe event for panicWSPipe", "B13: the panicking pipe also emits its event"))
	}
	if got[0].Duration < time.Millisecond {
		t.Error(test.DiffMessage(got[0].Duration, ">= 1ms", "B13: Duration covers the Transform up to the panic"))
	}
}

func TestWSHandlerPlan_EmitsNoStageHandler(t *testing.T) {
	ev := &trace.Tracer{}
	log := planListen(ev)
	planWS(t, &planWSCtl{}, "WOne", nil).invoke(planWSContext(), ev)
	if got := log.all(); len(got) != 0 {
		t.Error(test.DiffMessage(got, "no events", "B15: a WS handler emits no handler event"))
	}
}

func TestWSHandlerPlan_NoResult(t *testing.T) {
	h := &planWSCtl{}
	got := planWS(t, h, "WNone", nil).invoke(planWSContext(), &trace.Tracer{})
	if len(got) != 0 || h.calls != 1 {
		t.Error(test.DiffMessage([]any{len(got), h.calls}, []any{0, 1}, "B7: no results, one call"))
	}
}

func TestWSHandlerPlan_ReturnsResult(t *testing.T) {
	got := planWS(t, &planWSCtl{}, "WOne", nil).invoke(planWSContext(), &trace.Tracer{})
	if len(got) != 1 || got[0].String() != "one" {
		t.Error(test.DiffMessage(got, []any{"one"}, "B7: one result"))
	}
}
