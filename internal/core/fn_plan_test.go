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

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	ctxlookalike "github.com/dangduoc08/ginject/internal/test/lookalike/ctx"
	"github.com/dangduoc08/ginject/internal/trace"
)

var (
	planPipeCalls     []planPipeCall
	planOrder         []string
	planTransformHits int
	planNextCalls     int
)

type planPipeCall struct {
	Kind string
	Src  any
	Meta common.ArgumentMetadata
}

type planPublisher struct{}

func (*planPublisher) Publish(string, ...any) error { return nil }

type planTraceLog struct {
	mu     sync.Mutex
	events []trace.Event
	stop   func()
}

func planListen(ev *trace.Tracer) *planTraceLog {
	l := &planTraceLog{}
	l.stop = ev.Listen(func(e trace.Event) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.events = append(l.events, e)
	})
	return l
}

func (l *planTraceLog) all() []trace.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]trace.Event(nil), l.events...)
}

func (l *planTraceLog) byStage(stage string) []trace.Event {
	var out []trace.Event
	for _, e := range l.all() {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

func planPanicOf(f func()) (rec any) {
	defer func() { rec = recover() }()
	f()
	return nil
}

func planExceptionOf(rec any) (exception.Exception, bool) {
	switch v := rec.(type) {
	case exception.Exception:
		return v, true
	case *exception.Exception:
		if v != nil {
			return *v, true
		}
	}
	return exception.Exception{}, false
}

func planUnresolved(funcType string, index int) string {
	return "dependency injection: can't resolve dependencies of the '" + funcType +
		"'. Please make sure that the argument dependency at index [" + strconv.Itoa(index) +
		"] is available in the handler"
}

func planSameMap(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

var planCtr = newContainer()

func planInstallPublisher(t testing.TB) *planPublisher {
	t.Helper()
	p := &planPublisher{}
	planCtr = newContainer()
	planCtr.addInterface(publisherKey, common.Publisher(p))
	t.Cleanup(func() { planCtr = newContainer() })
	return p
}

func planRemovePublisher(t testing.TB) {
	t.Helper()
	planCtr = newContainer()
	t.Cleanup(func() { planCtr = newContainer() })
}

func planResetState(t testing.TB) {
	t.Helper()
	planPipeCalls = nil
	planOrder = nil
	planTransformHits = 0
	planNextCalls = 0
	t.Cleanup(func() {
		planPipeCalls = nil
		planOrder = nil
		planTransformHits = 0
		planNextCalls = 0
	})
}

func planProviders() map[string]Provider {
	return map[string]Provider{
		genFieldKey(reflect.TypeOf(fnTestProvider{})): fnTestProvider{Tag: "injected"},
	}
}

func planMethod(t testing.TB, controller any, name string) (reflect.Value, reflect.Method) {
	t.Helper()
	m, ok := reflect.TypeOf(controller).MethodByName(name)
	if !ok {
		t.Fatalf("fixture: %T has no method %s", controller, name)
	}
	return reflect.ValueOf(controller), m
}

func planHTTP(t testing.TB, controller any, name string, providers map[string]Provider) *httpHandlerPlan {
	t.Helper()
	recv, m := planMethod(t, controller, name)
	return newHTTPHandlerPlan(recv, m, providers, planCtr)
}

func planRequestContext() *ctx.HTTPContext {
	r := httptest.NewRequest(http.MethodPost, "/items?q=1&q=2", strings.NewReader(`{"a":1}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Probe", "p")
	c := ctx.NewHTTPContext()
	ctx.HTTPInit(c, httptest.NewRecorder(), r)
	ctx.HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"7"})
	return c
}

func planInvoke(p *httpHandlerPlan, c *ctx.HTTPContext) []reflect.Value {
	return p.invoke(c, &trace.Tracer{})
}

type recCtxPipe struct{ Kind string }

func (d recCtxPipe) Transform(c *ctx.HTTPContext, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"context", c, m})
	d.Kind = "context"
	return d
}

type recBodyPipe struct{ Kind string }

func (d recBodyPipe) Transform(b ctx.Body, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"body", b, m})
	d.Kind = "body"
	return d
}

type provBodyPipe struct {
	P    fnTestProvider
	Kind string
	Name string
}

func (d provBodyPipe) Transform(b ctx.Body, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"body", b, m})
	d.Kind = "body"
	return d
}

type recFormPipe struct{ Kind string }

func (d recFormPipe) Transform(f ctx.Form, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"form", f, m})
	d.Kind = "form"
	return d
}

type recQueryPipe struct{ Kind string }

func (d recQueryPipe) Transform(q ctx.Query, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"query", q, m})
	d.Kind = "query"
	return d
}

type recHeaderPipe struct{ Kind string }

func (d recHeaderPipe) Transform(h ctx.Header, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"header", h, m})
	d.Kind = "header"
	return d
}

type recParamPipe struct{ Kind string }

func (d recParamPipe) Transform(p ctx.Param, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"param", p, m})
	d.Kind = "param"
	return d
}

type recFilePipe struct{ Kind string }

func (d recFilePipe) Transform(f ctx.File, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"file", f, m})
	d.Kind = "file"
	return d
}

type recWSPayloadPipe struct{ Kind string }

func (d recWSPayloadPipe) Transform(p ctx.WSPayload, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"wsPayload", p, m})
	d.Kind = "wsPayload"
	return d
}

type scalarPipe int

func (scalarPipe) Transform(p ctx.Param, _ common.ArgumentMetadata) any {
	planTransformHits++
	return scalarPipe(len(p) + 41)
}

type mapPipe map[string]int

func (mapPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return map[string]int{"a": 1}
}

type nilPipe struct{}

func (nilPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return nil }

type pointerResultPipe struct{}

func (pointerResultPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return &pointerResultPipe{}
}

type otherResultPipe struct{}

func (otherResultPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return "text" }

type nilWSPipe struct{}

func (nilWSPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return nil }

type countingPipe struct{}

func (countingPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	planTransformHits++
	return countingPipe{}
}

type seenPipe struct {
	Seen int
	Log  []string
}

func (d seenPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	d.Seen++
	d.Log = append(d.Log, "x")
	return d
}

type orderPipeA struct{}

func (d orderPipeA) Transform(ctx.Query, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "a")
	return d
}

type orderPipeB struct{}

func (d orderPipeB) Transform(ctx.Header, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "b")
	return d
}

type orderPipeC struct{}

func (d orderPipeC) Transform(ctx.Param, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "c")
	return d
}

type panicPipe struct{}

func (panicPipe) Transform(ctx.Header, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "boom")
	panic("pipe exploded")
}

type slowPipe struct{}

func (d slowPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	time.Sleep(20 * time.Millisecond)
	return d
}

type slowPipeB struct{}

func (d slowPipeB) Transform(ctx.Header, common.ArgumentMetadata) any {
	time.Sleep(20 * time.Millisecond)
	return d
}

type provQueryPipe struct{ P fnTestProvider }

func (d provQueryPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type ifaceQueryPipe struct{ Pub common.Publisher }

func (d ifaceQueryPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type unexportedFieldPipe struct{ hidden int }

func (d unexportedFieldPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return unexportedFieldPipe{hidden: d.hidden}
}

type unresolvedFieldPipe struct{ Missing unresolvableProviderType }

func (d unresolvedFieldPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type mutPipe struct{ Seen int }

func (d mutPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	d.Seen++
	return d
}

type planUnknownArg struct{}

type planCtl struct {
	calls int
	tag   string
	got   []any
}

func (h *planCtl) HCtx(c *ctx.HTTPContext) { h.calls++; h.got = []any{c} }
func (h *planCtl) HReq(r *http.Request)    { h.calls++; h.got = []any{r} }
func (h *planCtl) HWriter(w http.ResponseWriter) {
	h.calls++
	h.got = []any{w}
}
func (h *planCtl) HBody(b ctx.Body)     { h.calls++; h.got = []any{b} }
func (h *planCtl) HForm(f ctx.Form)     { h.calls++; h.got = []any{f} }
func (h *planCtl) HQuery(q ctx.Query)   { h.calls++; h.got = []any{q} }
func (h *planCtl) HHeader(x ctx.Header) { h.calls++; h.got = []any{x} }
func (h *planCtl) HParam(p ctx.Param)   { h.calls++; h.got = []any{p} }
func (h *planCtl) HFile(f ctx.File)     { h.calls++; h.got = []any{f} }
func (h *planCtl) HNext(n ctx.Next)     { h.calls++; n() }
func (h *planCtl) HRedirect(r ctx.Redirect) {
	h.calls++
	r("/elsewhere")
}
func (h *planCtl) HPublisher(p common.Publisher) { h.calls++; h.got = []any{p} }

func (h *planCtl) HNone()                     { h.calls++ }
func (h *planCtl) HOne() string               { h.calls++; return "one" }
func (h *planCtl) HTwo() (int, string)        { h.calls++; return 201, "two" }
func (h *planCtl) HThree() (int, string, any) { h.calls++; return 202, "a", "b" }
func (h *planCtl) HTag() string               { h.calls++; return h.tag }
func (h *planCtl) HPanics()                   { h.calls++; panic("handler exploded") }

func (h *planCtl) HCtxPipe(p recCtxPipe)       { h.calls++; h.got = []any{p} }
func (h *planCtl) HBodyPipe(p recBodyPipe)     { h.calls++; h.got = []any{p} }
func (h *planCtl) HFormPipe(p recFormPipe)     { h.calls++; h.got = []any{p} }
func (h *planCtl) HQueryPipe(p recQueryPipe)   { h.calls++; h.got = []any{p} }
func (h *planCtl) HHeaderPipe(p recHeaderPipe) { h.calls++; h.got = []any{p} }
func (h *planCtl) HParamPipe(p recParamPipe)   { h.calls++; h.got = []any{p} }
func (h *planCtl) HFilePipe(p recFilePipe)     { h.calls++; h.got = []any{p} }
func (h *planCtl) HScalarPipe(p scalarPipe)    { h.calls++; h.got = []any{p} }
func (h *planCtl) HMapPipe(p mapPipe)          { h.calls++; h.got = []any{p} }
func (h *planCtl) HNilPipe(p nilPipe)          { h.calls++; h.got = []any{p} }
func (h *planCtl) HPointerResult(p pointerResultPipe) {
	h.calls++
	h.got = []any{p}
}
func (h *planCtl) HOtherResult(p otherResultPipe) { h.calls++; h.got = []any{p} }
func (h *planCtl) HCountingPipe(p countingPipe)   { h.calls++; h.got = []any{p} }
func (h *planCtl) HSeenPipe(p seenPipe)           { h.calls++; h.got = []any{p.Seen, len(p.Log)} }
func (h *planCtl) HProvQuery(p provQueryPipe)     { h.calls++; h.got = []any{p} }
func (h *planCtl) HIfaceQuery(p ifaceQueryPipe)   { h.calls++; h.got = []any{p} }
func (h *planCtl) HUnexported(p unexportedFieldPipe) {
	h.calls++
}
func (h *planCtl) HUnresolved(p unresolvedFieldPipe) { h.calls++ }

func (h *planCtl) HOrder(a orderPipeA, b orderPipeB, c orderPipeC) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HPanicMid(a orderPipeA, b panicPipe, c orderPipeC) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HBodyThenPipe(b ctx.Body, a orderPipeA) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HSlow(p slowPipe)                 { h.calls++ }
func (h *planCtl) HTwoSlow(p slowPipe, q slowPipeB) { h.calls++ }
func (h *planCtl) HMixed(c *ctx.HTTPContext, p recQueryPipe, r *http.Request) {
	h.calls++
	h.got = []any{c, p, r}
}

func (h *planCtl) HAll(c *ctx.HTTPContext, r *http.Request, w http.ResponseWriter, body ctx.Body, form ctx.Form, query ctx.Query, header ctx.Header, param ctx.Param, file ctx.File, next ctx.Next, redirect ctx.Redirect, pub common.Publisher, cp recCtxPipe, bp provBodyPipe, fp recFormPipe, qp recQueryPipe, hp recHeaderPipe, pp recParamPipe, filep recFilePipe) (int, string) {
	h.calls++
	planNextCalls = 0
	next()
	nextSeen := planNextCalls
	redirect("/x")
	h.got = []any{c, r, w, body, form, query, header, param, file, nextSeen, ctx.HTTPCode(c), pub, cp, bp, fp, qp, hp, pp, filep}
	return 202, "all"
}

type planValueCtl struct{ tag string }

func (h planValueCtl) HTag() string { return h.tag }

type planConcurrentCtl struct{}

func (planConcurrentCtl) Run(p mutPipe, q ctx.Query) string {
	return strconv.Itoa(p.Seen) + ":" + q.Get("q")
}

type planBadCtl struct{}

func (planBadCtl) Unknown(c *ctx.HTTPContext, n int) string         { return "" }
func (planBadCtl) UnknownStruct(u planUnknownArg)                   {}
func (planBadCtl) Variadic(a ...int)                                {}
func (planBadCtl) Interface(e error)                                {}
func (planBadCtl) WSContext(c *ctx.WSContext)                       {}
func (planBadCtl) WSPayloadPipe(p recWSPayloadPipe)                 {}
func (planBadCtl) Lookalike(c *ctxlookalike.HTTPContext) string     { return "" }
func (planBadCtl) NeedsPublisher(p common.Publisher)                {}
func (planBadCtl) PointerPipe(p *recQueryPipe)                      {}
func (planBadCtl) CountedPipe(p countingPipe)                       {}
func (planBadCtl) Plain(c *ctx.HTTPContext, r *http.Request) string { return "" }

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

func planReferenceHTTP(t testing.TB, recv reflect.Value, name string, providers map[string]Provider, c *ctx.HTTPContext) []reflect.Value {
	t.Helper()
	handler := recv.MethodByName(name)
	ft := handler.Type()
	args := make([]reflect.Value, ft.NumIn())
	for i := range args {
		args[i] = planReferenceArg(t, ft.In(i), providers, c)
	}
	return handler.Call(args)
}

var (
	refHTTPContextT = reflect.TypeOf((*ctx.HTTPContext)(nil))
	refRequestT     = reflect.TypeOf((*http.Request)(nil))
	refWriterT      = reflect.TypeOf((*http.ResponseWriter)(nil)).Elem()
	refBodyT        = reflect.TypeOf(ctx.Body(nil))
	refFormT        = reflect.TypeOf(ctx.Form(nil))
	refQueryT       = reflect.TypeOf(ctx.Query(nil))
	refHeaderT      = reflect.TypeOf(ctx.Header(nil))
	refParamT       = reflect.TypeOf(ctx.Param(nil))
	refFileT        = reflect.TypeOf(ctx.File(nil))
	refNextT        = reflect.TypeOf(func() {})
	refRedirectT    = reflect.TypeOf(func(string) {})
	refPublisherT   = reflect.TypeOf((*common.Publisher)(nil)).Elem()
)

func planReferenceArg(t testing.TB, at reflect.Type, providers map[string]Provider, c *ctx.HTTPContext) reflect.Value {
	t.Helper()
	switch at {
	case refHTTPContextT:
		return reflect.ValueOf(c)
	case refRequestT:
		return reflect.ValueOf(c.Request)
	case refWriterT:
		return reflect.ValueOf(c.ResponseWriter)
	case refBodyT:
		return reflect.ValueOf(c.Body())
	case refFormT:
		return reflect.ValueOf(c.Form())
	case refQueryT:
		return reflect.ValueOf(c.Query())
	case refHeaderT:
		return reflect.ValueOf(c.Header())
	case refParamT:
		return reflect.ValueOf(c.Param())
	case refFileT:
		return reflect.ValueOf(c.File())
	case refNextT:
		return reflect.ValueOf(ctx.HTTPNext(c))
	case refRedirectT:
		return reflect.ValueOf(c.Redirect)
	case refPublisherT:
		p, _ := planCtr.interfaceFor(publisherKey)
		return reflect.ValueOf(p)
	}

	inst, err := injectDependencies(reflect.New(at).Elem().Interface(), "pipe", providers, planCtr)
	if err != nil {
		t.Fatal(err)
	}
	meta := func(kind string) common.ArgumentMetadata {
		return common.ArgumentMetadata{ContextType: common.ContextTypeHTTP, ParamType: kind}
	}
	var out any
	switch p := inst.Interface().(type) {
	case common.ContextPipeable:
		out = p.Transform(c, meta(common.ContextPipeableKey))
	case common.BodyPipeable:
		out = p.Transform(c.Body(), meta(common.BodyPipeableKey))
	case common.FormPipeable:
		out = p.Transform(c.Form(), meta(common.FormPipeableKey))
	case common.QueryPipeable:
		out = p.Transform(c.Query(), meta(common.QueryPipeableKey))
	case common.HeaderPipeable:
		out = p.Transform(c.Header(), meta(common.HeaderPipeableKey))
	case common.ParamPipeable:
		out = p.Transform(c.Param(), meta(common.ParamPipeableKey))
	case common.FilePipeable:
		out = p.Transform(c.File(), meta(common.FilePipeableKey))
	default:
		t.Fatalf("fixture: %v is not a pipe", at)
	}
	return reflect.ValueOf(out)
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

func planPipeKindCase(t *testing.T, method, kind string, src func(*ctx.HTTPContext) any, wantKind string) {
	t.Helper()
	planResetState(t)
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, method, nil), c)
	if len(planPipeCalls) != 1 {
		t.Fatal(test.DiffMessage(len(planPipeCalls), 1, "B10: Transform runs once per request"))
	}
	call := planPipeCalls[0]
	if call.Kind != wantKind {
		t.Error(test.DiffMessage(call.Kind, wantKind, "B10: Transform of the "+kind+" kind"))
	}
	want := src(c)
	if call.Src == nil || (reflect.ValueOf(want).Kind() == reflect.Map && !planSameMap(call.Src, want)) ||
		(reflect.ValueOf(want).Kind() == reflect.Pointer && call.Src != want) {
		t.Error(test.DiffMessage(call.Src, want, "B10: the "+kind+" pipe's source"))
	}
	if call.Meta.ParamType != kind {
		t.Error(test.DiffMessage(call.Meta.ParamType, kind, "B10: ParamType is the kind's key"))
	}
	if call.Meta.ContextType != common.ContextTypeHTTP {
		t.Error(test.DiffMessage(call.Meta.ContextType, common.ContextTypeHTTP, "B10: ContextType of an HTTP pipe"))
	}
	if len(h.got) != 1 || reflect.ValueOf(h.got[0]).FieldByName("Kind").String() != wantKind {
		t.Error(test.DiffMessage(h.got, wantKind, "B10: the handler receives what Transform returned"))
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

func planWantTransformFailure(t *testing.T, rec any, typeName, returned string) {
	t.Helper()
	ex, ok := planExceptionOf(rec)
	if !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception", "B11: a wrong Transform result panics with an exception"))
	}
	if ex.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(ex.Code(), http.StatusInternalServerError, "B11: code"))
	}
	want := "pipe " + typeName + ": Transform returned " + returned + ", want " + typeName
	if ex.Message() != want {
		t.Error(test.DiffMessage(ex.Message(), want, "B11: message"))
	}
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
