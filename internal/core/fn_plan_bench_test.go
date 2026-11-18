package core

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/trace"
)

func BenchmarkHTTPHandlerPlan_Invoke_NoArgs(b *testing.B) {
	plan := benchHTTPPlan(b, "NoArgs")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_Param(b *testing.B) {
	plan := benchHTTPPlan(b, "Param")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_QueryPipe(b *testing.B) {
	plan := benchHTTPPlan(b, "QueryPipe")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_BodyPipeWithProvider(b *testing.B) {
	plan := benchHTTPPlan(b, "BodyPipeWithProvider")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_FiveBuiltins(b *testing.B) {
	plan := benchHTTPPlan(b, "FiveBuiltins")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkHTTPHandlerPlan_Invoke_QueryPipe_Traced(b *testing.B) {
	plan := benchHTTPPlan(b, "QueryPipe")
	c := benchPlanContext()
	ev := &trace.Tracer{}
	ev.Listen(func(trace.Event) {})
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkWSHandlerPlan_Invoke_Payload(b *testing.B) {
	plan := benchWSPlan(b, "Payload")
	c := benchWSContext()
	ev := &trace.Tracer{}
	b.ReportAllocs()
	for b.Loop() {
		plan.invoke(c, ev)
	}
}

func BenchmarkWSHandlerPlan_Invoke_PayloadPipe(b *testing.B) {
	plan := benchWSPlan(b, "PayloadPipe")
	c := benchWSContext()
	ev := &trace.Tracer{}
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
		newHTTPHandlerPlan(recv, m, providers, newContainer())
	}
}
