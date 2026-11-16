package core

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/trace"
	"golang.org/x/net/websocket"
)

type benchQueryDTO struct {
	Name string
	Page int
}

func (d benchQueryDTO) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type benchProviderDTO struct {
	P    fnTestProvider
	Name string
}

func (d benchProviderDTO) Transform(ctx.Body, common.ArgumentMetadata) any { return d }

type benchWSDTO struct {
	Topic string
}

func (d benchWSDTO) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return d }

type benchPlanCtl struct{}

func (benchPlanCtl) NoArgs() string { return "ok" }
func (benchPlanCtl) Param(ctx.Param) ctx.Map {
	return nil
}
func (benchPlanCtl) QueryPipe(benchQueryDTO) ctx.Map               { return nil }
func (benchPlanCtl) BodyPipeWithProvider(benchProviderDTO) ctx.Map { return nil }
func (benchPlanCtl) FiveBuiltins(*ctx.HTTPContext, *http.Request, http.ResponseWriter, ctx.Query, ctx.Header) {
}
func (benchPlanCtl) Payload(ctx.WSPayload)  {}
func (benchPlanCtl) PayloadPipe(benchWSDTO) {}

func benchPlanContext() *ctx.HTTPContext {
	c := benchHTTPContext("/?name=ann&page=2")
	c.SetID()
	c.ParamKeys = map[string][]int{"id": {0}}
	c.ParamValues = []string{"7"}
	return c
}

func benchHTTPPlan(b *testing.B, name string) *httpHandlerPlan {
	b.Helper()
	recv, m := planMethod(b, benchPlanCtl{}, name)
	return newHTTPHandlerPlan(recv, m, planProviders())
}

func BenchmarkHTTPHandlerPlan_Invoke_NoArgs(b *testing.B) {
	plan := benchHTTPPlan(b, "NoArgs")
	c := benchPlanContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_Param(b *testing.B) {
	plan := benchHTTPPlan(b, "Param")
	c := benchPlanContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_QueryPipe(b *testing.B) {
	plan := benchHTTPPlan(b, "QueryPipe")
	c := benchPlanContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider(b *testing.B) {
	plan := benchHTTPPlan(b, "BodyPipeWithProvider")
	c := benchPlanContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins(b *testing.B) {
	plan := benchHTTPPlan(b, "FiveBuiltins")
	c := benchPlanContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced(b *testing.B) {
	plan := benchHTTPPlan(b, "QueryPipe")
	c := benchPlanContext()
	ev := event.NewEvent()
	ev.On(trace.EventName, func(args ...any) {})
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func benchWSPlan(b *testing.B, name string) *wsHandlerPlan {
	b.Helper()
	recv, m := planMethod(b, benchPlanCtl{}, name)
	return newWSHandlerPlan(recv, m, nil)
}

func benchWSContext() *ctx.WSContext {
	c := ctx.NewWSContext()
	c.Init(&websocket.Conn{})
	c.SetWSPayload(ctx.WSPayload{"topic": "chat"})
	return c
}

func BenchmarkWSHandlerPlan_Invoke_Payload(b *testing.B) {
	plan := benchWSPlan(b, "Payload")
	c := benchWSContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkWSHandlerPlan_Invoke_PayloadPipe(b *testing.B) {
	plan := benchWSPlan(b, "PayloadPipe")
	c := benchWSContext()
	ev := event.NewEvent()
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkNewHTTPHandlerPlan(b *testing.B) {
	recv, m := planMethod(b, benchPlanCtl{}, "QueryPipe")
	providers := planProviders()
	b.ReportAllocs()
	for b.Loop() {
		newHTTPHandlerPlan(recv, m, providers)
	}
}

func BenchmarkReturnHTTP_String(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf("hello world")
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Int(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(123456)
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Float(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(3.14159)
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_Map(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(ctx.Map{"message": "ok", "count": 3})
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_PointerToStruct(b *testing.B) {
	c := newHTTPContext()
	v := reflect.ValueOf(&returnUser{Name: "ann"})
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkReturnHTTP_InterfaceString(b *testing.B) {
	c := newHTTPContext()
	v := anyValue("hello world")
	b.ReportAllocs()
	for b.Loop() {
		returnHTTP(c, v)
	}
}

func BenchmarkGetPkgFromControllerKey(b *testing.B) {
	key := "[123456789]github.com/dangduoc08/ginject/sample/modules/user.UserController"
	b.ReportAllocs()
	for b.Loop() {
		getPkgFromControllerKey(key)
	}
}

func BenchmarkGenFieldKey(b *testing.B) {
	t := reflect.TypeOf(&mockProvider{})
	b.ReportAllocs()
	for b.Loop() {
		genFieldKey(t)
	}
}

func BenchmarkGenControllerKey(b *testing.B) {
	m := ModuleBuilder().Build()
	c := &mockController{}
	b.ReportAllocs()
	for b.Loop() {
		genControllerKey(m, c)
	}
}

func BenchmarkIsDynamicModule(b *testing.B) {
	s := "func(pkg.Provider) *core.Module"
	b.ReportAllocs()
	for b.Loop() {
		isDynamicModule(s)
	}
}

func BenchmarkToUniqueControllers(b *testing.B) {
	m := ModuleBuilder().Build()
	c1 := &mockController{}
	c2 := &mockController{}
	b.ReportAllocs()
	for b.Loop() {
		controllers := []Controller{c1, c2, c1, c2, c1}
		toUniqueControllers(m, &controllers)
	}
}

func BenchmarkTraceHTTPHandler_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	h := traceHTTPHandler(ev, trace.StageGuard, "bench.Guard", func(c *ctx.HTTPContext) {})
	b.ReportAllocs()
	for b.Loop() {
		h(c)
	}
}

func BenchmarkTraceHTTPHandler_WithListener(b *testing.B) {
	ev := event.NewEvent()
	ev.On(trace.EventName, func(args ...any) {})
	c := newHTTPContext()
	h := traceHTTPHandler(ev, trace.StageGuard, "bench.Guard", func(c *ctx.HTTPContext) {})
	b.ReportAllocs()
	for b.Loop() {
		h(c)
	}
}

func BenchmarkTraceHTTPCatch_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	ex := exception.BadRequestException("bad")
	fn := traceHTTPCatch(ev, "bench.Filter", func(c *ctx.HTTPContext, ex *exception.Exception) {})
	b.ReportAllocs()
	for b.Loop() {
		fn(c, &ex)
	}
}

func BenchmarkBuildUseMiddleware_NoListener(b *testing.B) {
	ev := event.NewEvent()
	c := newHTTPContext()
	c.Next = func() {}
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "bench.Middleware", trace.TransportHTTP)
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}

func BenchmarkBuildUseMiddleware_WithListener(b *testing.B) {
	ev := event.NewEvent()
	ev.On(trace.EventName, func(args ...any) {})
	c := newHTTPContext()
	c.Next = func() {}
	mw := buildUseMiddleware(func(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }, ev, "bench.Middleware", trace.TransportHTTP)
	b.ReportAllocs()
	for b.Loop() {
		mw(c)
	}
}
