package aggregation

import (
	"testing"
)

func BenchmarkNewAggregation(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NewAggregation()
	}
}

func BenchmarkAggregate_NoOperators(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, "data")
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkAggregate_Transform(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, "data")
	a.Pipe(a.Transform(benchNoop))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkAggregate_AllOperators(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, "data")
	a.Pipe(a.Transform(benchNoop), a.Tap(benchNoop), a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
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
	SetMainData(a, 10)
	a.Pipe(a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkAggregate_Filter_False(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, 10)
	a.Pipe(a.Filter(benchFalse))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkAggregate_MultipleFilters(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, 30)
	a.Pipe(a.Filter(benchPredicate), a.Filter(benchPredicate))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkAggregate_Filter_And_Transform(b *testing.B) {
	a := NewAggregation()
	SetMainData(a, "data")
	a.Pipe(a.Filter(benchPredicate), a.Transform(benchNoop))
	b.ReportAllocs()
	for b.Loop() {
		Aggregate(a)
	}
}

func BenchmarkInterceptorCycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewAggregation()
		a.Pipe(a.Transform(benchNoop))
		SetMainData(a, "data")
		Aggregate(a)
	}
}
