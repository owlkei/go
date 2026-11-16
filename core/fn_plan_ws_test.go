package core

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/trace"
	"golang.org/x/net/websocket"
)

type panicWSPipe struct{}

func (panicWSPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any {
	time.Sleep(time.Millisecond)
	panic("ws pipe exploded")
}

type planWSCtl struct {
	calls int
	got   []any
}

func (h *planWSCtl) WCtx(c *ctx.WSContext)    { h.calls++; h.got = []any{c} }
func (h *planWSCtl) WConn(c *websocket.Conn)  { h.calls++; h.got = []any{c} }
func (h *planWSCtl) WPayload(p ctx.WSPayload) { h.calls++; h.got = []any{p} }
func (h *planWSCtl) WNext(n ctx.Next)         { h.calls++; n() }
func (h *planWSCtl) WPublisher(p common.Publisher) {
	h.calls++
	h.got = []any{p}
}
func (h *planWSCtl) WPipe(p recWSPayloadPipe) { h.calls++; h.got = []any{p} }
func (h *planWSCtl) WNilPipe(p nilWSPipe)     { h.calls++; h.got = []any{p} }
func (h *planWSCtl) WPanicPipe(p panicWSPipe) { h.calls++ }
func (h *planWSCtl) WNone()                   { h.calls++ }
func (h *planWSCtl) WOne() string             { h.calls++; return "one" }
func (h *planWSCtl) WAll(c *ctx.WSContext, conn *websocket.Conn, payload ctx.WSPayload, next ctx.Next, pub common.Publisher, p recWSPayloadPipe) (int, string) {
	h.calls++
	planNextCalls = 0
	next()
	h.got = []any{c, conn, payload, planNextCalls, pub, p}
	return 7, "ws"
}

type planWSBadCtl struct{}

func (planWSBadCtl) HTTPContext(c *ctx.HTTPContext)  {}
func (planWSBadCtl) Body(b ctx.Body)                 {}
func (planWSBadCtl) Request(r ctx.Redirect)          {}
func (planWSBadCtl) QueryPipe(p recQueryPipe)        {}
func (planWSBadCtl) Unknown(c *ctx.WSContext, n int) {}

func planWSContext() *ctx.WSContext {
	c := ctx.NewWSContext()
	c.Init(&websocket.Conn{})
	c.SetWSPayload(ctx.WSPayload{"k": "v"})
	return c
}

func planWS(t testing.TB, controller any, name string, providers map[string]Provider) *wsHandlerPlan {
	t.Helper()
	recv, m := planMethod(t, controller, name)
	return newWSHandlerPlan(recv, m, providers, planCtr)
}

func TestWSHandlerPlan_InjectsWSContext(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WCtx", nil).invoke(c, event.NewEvent())
	if len(h.got) != 1 || h.got[0] != any(c) {
		t.Error(test.DiffMessage(h.got, []any{c}, "B1, B8: *ctx.WSContext is the message's context"))
	}
}

func TestWSHandlerPlan_InjectsConn(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WConn", nil).invoke(c, event.NewEvent())
	if len(h.got) != 1 || h.got[0] != any(c.Conn) {
		t.Error(test.DiffMessage(h.got, []any{c.Conn}, "B1, B8: *websocket.Conn is c.Conn"))
	}
}

func TestWSHandlerPlan_InjectsWSPayload(t *testing.T) {
	h := &planWSCtl{}
	c := planWSContext()
	planWS(t, h, "WPayload", nil).invoke(c, event.NewEvent())
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
	c.Next = func() { planNextCalls++ }
	planWS(t, h, "WNext", nil).invoke(c, event.NewEvent())
	if planNextCalls != 1 {
		t.Error(test.DiffMessage(planNextCalls, 1, "B1, B8: ctx.Next acts on c.Next"))
	}
}

func TestWSHandlerPlan_InjectsPublisher(t *testing.T) {
	pub := planInstallPublisher(t)
	h := &planWSCtl{}
	planWS(t, h, "WPublisher", nil).invoke(planWSContext(), event.NewEvent())
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
	c.Next = func() { planNextCalls++ }

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
			args[i] = reflect.ValueOf(c.Next)
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
	results := plan.invoke(c, event.NewEvent())

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
	planWS(t, h, "WPipe", nil).invoke(c, event.NewEvent())

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
	rec := planPanicOf(func() { plan.invoke(planWSContext(), event.NewEvent()) })
	planWantTransformFailure(t, rec, "core.nilWSPipe", "nil")
	if h.calls != 0 {
		t.Error(test.DiffMessage(h.calls, 0, "B11: the handler is not called"))
	}
}

func TestWSHandlerPlan_PipeEmitsStagePipe(t *testing.T) {
	planResetState(t)
	ev := event.NewEvent()
	log := planListen(ev)
	c := planWSContext()
	planWS(t, &planWSCtl{}, "WPipe", nil).invoke(c, ev)

	got := log.byStage(trace.StagePipe)
	if len(got) != 1 {
		t.Fatal(test.DiffMessage(len(got), 1, "B13: one pipe event for one pipe"))
	}
	e := got[0]
	if e.ID != c.GetID() || e.ID == "" {
		t.Error(test.DiffMessage(e.ID, c.GetID(), "B13: ID"))
	}
	if want := reflect.TypeOf(recWSPayloadPipe{}).String(); e.Name != want {
		t.Error(test.DiffMessage(e.Name, want, "B13: Name is the parameter type"))
	}
	if e.Transport != trace.TransportWS {
		t.Error(test.DiffMessage(e.Transport, trace.TransportWS, "B13: Transport"))
	}
}

func TestWSHandlerPlan_PipePanicStillEmitsStagePipe(t *testing.T) {
	ev := event.NewEvent()
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
	ev := event.NewEvent()
	log := planListen(ev)
	planWS(t, &planWSCtl{}, "WOne", nil).invoke(planWSContext(), ev)
	if got := log.all(); len(got) != 0 {
		t.Error(test.DiffMessage(got, "no events", "B15: a WS handler emits no handler event"))
	}
}

func TestWSHandlerPlan_NoResult(t *testing.T) {
	h := &planWSCtl{}
	got := planWS(t, h, "WNone", nil).invoke(planWSContext(), event.NewEvent())
	if len(got) != 0 || h.calls != 1 {
		t.Error(test.DiffMessage([]any{len(got), h.calls}, []any{0, 1}, "B7: no results, one call"))
	}
}

func TestWSHandlerPlan_ReturnsResult(t *testing.T) {
	got := planWS(t, &planWSCtl{}, "WOne", nil).invoke(planWSContext(), event.NewEvent())
	if len(got) != 1 || got[0].String() != "one" {
		t.Error(test.DiffMessage(got, []any{"one"}, "B7: one result"))
	}
}
