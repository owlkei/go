package aggregation

import (
	"testing"
)

var benchNoop = func(data any) any { return data }
var benchPredicate = func(data any) bool { return true }
var benchFalse = func(data any) bool { return false }

func BenchmarkNewAggregation(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewAggregation()
	}
}

func BenchmarkAggregate_NoOperators(b *testing.B) {
	a := NewAggregation()
	a.SetMainData("data")
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkAggregate_Transform(b *testing.B) {
	a := NewAggregation()
	a.SetMainData("data")
	a.Pipe(a.Transform(benchNoop))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkAggregate_AllOperators(b *testing.B) {
	a := NewAggregation()
	a.SetMainData("data")
	a.Pipe(a.Transform(benchNoop), a.Tap(benchNoop), a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkPipe_Two(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewAggregation()
		a.Pipe(a.Transform(benchNoop), a.Tap(benchNoop))
	}
}

func BenchmarkAggregate_Filter(b *testing.B) {
	a := NewAggregation()
	a.SetMainData(10)
	a.Pipe(a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkAggregate_Filter_False(b *testing.B) {
	a := NewAggregation()
	a.SetMainData(10)
	a.Pipe(a.Filter(benchFalse))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkAggregate_MultipleFilters(b *testing.B) {
	a := NewAggregation()
	a.SetMainData(30)
	a.Pipe(a.Filter(benchPredicate), a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkAggregate_Filter_And_Transform(b *testing.B) {
	a := NewAggregation()
	a.SetMainData("data")
	a.Pipe(a.Filter(benchPredicate), a.Transform(benchNoop))
	b.ReportAllocs()
	for b.Loop() {
		a.Aggregate()
	}
}

func BenchmarkInterceptorCycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewAggregation()
		a.Pipe(a.Transform(benchNoop))
		a.SetMainData("data")
		a.Aggregate()
	}
}
