package aggregation

import (
	"slices"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestNewAggregation(t *testing.T) {
	a := NewAggregation()
	if a == nil {
		t.Fatal(test.DiffMessage(nil, "*Aggregation", "NewAggregation must not return nil"))
	}
	if IsMainHandlerCalled(a) {
		t.Error(test.DiffMessage(IsMainHandlerCalled(a), false, "IsMainHandlerCalled must start false"))
	}
	if a.mainData != nil {
		t.Error(test.DiffMessage(a.mainData, nil, "mainData must start nil"))
	}
}

func TestSetMainData(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "hello")
	if a.mainData != "hello" {
		t.Error(test.DiffMessage(a.mainData, "hello", "SetMainData must store value"))
	}
	SetMainData(a, 42)
	if a.mainData != 42 {
		t.Error(test.DiffMessage(a.mainData, 42, "SetMainData must overwrite previous value"))
	}
}

func TestSetMainData_Nil(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "something")
	SetMainData(a, nil)
	if a.mainData != nil {
		t.Error(test.DiffMessage(a.mainData, nil, "SetMainData with nil must clear value"))
	}
}

func TestPipe_SetsIsMainHandlerCalled(t *testing.T) {
	a := NewAggregation()
	if IsMainHandlerCalled(a) {
		t.Error(test.DiffMessage(IsMainHandlerCalled(a), false, "must start false"))
	}
	a.Pipe()
	if !IsMainHandlerCalled(a) {
		t.Error(test.DiffMessage(IsMainHandlerCalled(a), true, "Pipe must set IsMainHandlerCalled"))
	}
}

func TestPipe_ReturnsNil(t *testing.T) {
	a := NewAggregation()
	got := a.Pipe()
	if got != nil {
		t.Error(test.DiffMessage(got, nil, "Pipe must return nil"))
	}
}

func TestPipe_WithFilterTransformTap(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 10)
	result := a.Pipe(
		a.Filter(func(d any) bool { return d.(int) > 5 }),
		a.Transform(func(d any) any { return d.(int) * 2 }),
		a.Tap(func(d any) any { return nil }),
	)
	if result != nil {
		t.Error(test.DiffMessage(result, nil, "Pipe must return nil"))
	}
	if !IsMainHandlerCalled(a) {
		t.Error(test.DiffMessage(IsMainHandlerCalled(a), true, "Pipe must set IsMainHandlerCalled"))
	}
}

func TestAggregate_NoOperators(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "data")
	result := Aggregate(a)
	if result != "data" {
		t.Error(test.DiffMessage(result, "data", "no operators must return mainData unchanged"))
	}
}

func TestAggregate_NilData(t *testing.T) {
	a := NewAggregation()
	result := Aggregate(a)
	if result != nil {
		t.Error(test.DiffMessage(result, nil, "nil mainData must be returned as nil"))
	}
}

func TestAggregate_TransformAndTap_Order(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 0)
	order := []string{}
	a.Pipe(
		a.Transform(func(data any) any {
			order = append(order, "transform")
			return data.(int) + 1
		}),
		a.Tap(func(data any) any {
			order = append(order, "tap")
			return data
		}),
	)
	result := Aggregate(a)
	if result != 1 {
		t.Error(test.DiffMessage(result, 1, "Transform must increment value"))
	}
	if len(order) != 2 || order[0] != "transform" || order[1] != "tap" {
		t.Error(test.DiffMessage(order, []string{"transform", "tap"}, "operators must run in registration order"))
	}
}

func TestAggregate_MultipleTransforms(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 0)
	a.Pipe(
		a.Transform(func(data any) any { return data.(int) + 1 }),
		a.Transform(func(data any) any { return data.(int) * 3 }),
	)
	result := Aggregate(a)
	if result != 3 {
		t.Error(test.DiffMessage(result, 3, "(0+1)*3 = 3"))
	}
}

func TestPipe_RunsOnlyPipedOperators(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	ran := false
	a.Transform(func(d any) any { ran = true; return 100 })
	a.Tap(func(d any) any { ran = true; return nil })
	a.Filter(func(d any) bool { ran = true; return false })
	a.Pipe(a.Transform(addOne))
	got := Aggregate(a)
	if ran {
		t.Error(test.DiffMessage(ran, false, "A1/A2: an operator created but not piped must not run"))
	}
	if got != 2 {
		t.Error(test.DiffMessage(got, 2, "A1: only the piped operator runs"))
	}
}

func TestPipe_WithoutPipeNothingRuns(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	a.Transform(addOne)
	if got := Aggregate(a); got != 1 {
		t.Error(test.DiffMessage(got, 1, "A2: creating an operator registers nothing"))
	}
}

func TestPipe_OperatorFromAnotherAggregation(t *testing.T) {
	maker := NewAggregation()
	op := maker.Transform(addOne)
	a := NewAggregation()
	SetMainData(a, 1)
	a.Pipe(op)
	if got := Aggregate(a); got != 2 {
		t.Error(test.DiffMessage(got, 2, "A8: an operator from another aggregation runs where it is piped"))
	}
	SetMainData(maker, 10)
	if got := Aggregate(maker); got != 10 {
		t.Error(test.DiffMessage(got, 10, "A8: the creating aggregation registers nothing"))
	}
}

func TestPipe_RunsInArgumentOrder(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	t2 := a.Transform(timesTwo)
	t1 := a.Transform(addOne)
	a.Pipe(t1, t2)
	if got := Aggregate(a); got != 4 {
		t.Error(test.DiffMessage(got, 4, "A2: (1+1)*2 means argument order, not creation order"))
	}
}

func TestPipe_SkipsNilFunction(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	var nilFn func(any) any
	var nilPred func(any) bool
	a.Pipe(a.Transform(nilFn), a.Tap(nil), a.Filter(nilPred), a.Transform(addOne))
	got := Aggregate(a)
	if got != 2 {
		t.Error(test.DiffMessage(got, 2, "A4: operators with a nil function are skipped"))
	}
}

func TestPipe_SkipsZeroOperator(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	a.Pipe(Operator{}, a.Transform(addOne), Operator{})
	got := Aggregate(a)
	if got != 2 {
		t.Error(test.DiffMessage(got, 2, "A4: the zero Operator is skipped"))
	}
}

func TestPipe_ReplacesPreviousOperators(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	a.Pipe(a.Transform(addOne))
	a.Pipe(a.Transform(timesTwo))
	if got := Aggregate(a); got != 2 {
		t.Error(test.DiffMessage(got, 2, "A5: a second Pipe replaces the first one's operators"))
	}
	a.Pipe()
	if got := Aggregate(a); got != 2 {
		t.Error(test.DiffMessage(got, 2, "A5: Pipe with no operators clears them and data stays"))
	}
}

func TestPipe_ReturnsNilWithOperators(t *testing.T) {
	a := NewAggregation()
	if got := a.Pipe(a.Transform(addOne)); got != nil {
		t.Error(test.DiffMessage(got, nil, "A5: Pipe returns nil with operators"))
	}
}

func TestAggregate_TransformReturningNilContinues(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	seen := any("unset")
	a.Pipe(
		a.Transform(func(d any) any { return nil }),
		a.Tap(func(d any) any { seen = d; return nil }),
		a.Transform(func(d any) any {
			if d == nil {
				return "after-nil"
			}
			return "unexpected"
		}),
	)
	got := Aggregate(a)
	if seen != nil {
		t.Error(test.DiffMessage(seen, nil, "A6: a nil transform result is the data of the next operator"))
	}
	if got != "after-nil" {
		t.Error(test.DiffMessage(got, "after-nil", "A6: the chain goes on after a nil result"))
	}
}

func TestAggregate_FilterFalseStopsAndReturnsNil(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	called := false
	a.Pipe(
		a.Filter(func(d any) bool { return false }),
		a.Transform(func(d any) any { called = true; return 5 }),
	)
	if got := Aggregate(a); got != nil {
		t.Error(test.DiffMessage(got, nil, "A6: a false filter returns nil"))
	}
	if called {
		t.Error(test.DiffMessage(called, false, "A6: operators after a false filter must not run"))
	}
}

func TestAggregate_TapResultIgnored(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "keep")
	a.Pipe(a.Tap(func(d any) any { return "ignored" }), a.Tap(tapNothing))
	if got := Aggregate(a); got != "keep" {
		t.Error(test.DiffMessage(got, "keep", "A6: Tap result is ignored"))
	}
}

func TestAggregate_FinalDataKeptAsMainData(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	a.Pipe(a.Transform(addOne))
	Aggregate(a)
	if a.mainData != 2 {
		t.Error(test.DiffMessage(a.mainData, 2, "A6: the final data is kept as the main data"))
	}
}

func TestNewAggregation_Fresh(t *testing.T) {
	a := NewAggregation()
	if InterceptorData(a) != nil || Name(a) != "" || IsMainHandlerCalled(a) || a.mainData != nil {
		t.Error(test.DiffMessage(*a, Aggregation{}, "A7: a new aggregation is empty"))
	}
	if got := Aggregate(a); got != nil {
		t.Error(test.DiffMessage(got, nil, "A7: no operators and no data gives nil"))
	}
}

func TestPipe_FourOperatorsNoAllocs(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	allocs := testing.AllocsPerRun(100, func() {
		a.Pipe(a.Transform(addOne), a.Tap(tapNothing), a.Filter(isTrue), a.Transform(timesTwo))
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "A9: Pipe with four operators must not allocate"))
	}
}

func TestPipe_MoreThanFourOperators(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 0)
	order := []int{}
	mk := func(n int) Operator {
		return a.Tap(func(d any) any { order = append(order, n); return nil })
	}
	a.Pipe(mk(1), mk(2), mk(3), mk(4), mk(5), a.Transform(addOne))
	got := Aggregate(a)
	if !slices.Equal(order, []int{1, 2, 3, 4, 5}) {
		t.Error(test.DiffMessage(order, []int{1, 2, 3, 4, 5}, "A9: six operators keep their order"))
	}
	if got != 1 {
		t.Error(test.DiffMessage(got, 1, "A9: the sixth operator runs"))
	}
}

func TestHooks_NameAndInterceptorData(t *testing.T) {
	a := NewAggregation()
	if Name(a) != "" || InterceptorData(a) != nil {
		t.Error(test.DiffMessage([2]any{Name(a), InterceptorData(a)}, [2]any{"", nil}, "B18: a new aggregation has no name and no interceptor data"))
	}
	SetName(a, "x")
	SetInterceptorData(a, 7)
	if Name(a) != "x" || InterceptorData(a) != 7 {
		t.Error(test.DiffMessage([2]any{Name(a), InterceptorData(a)}, [2]any{"x", 7}, "B18: SetName and SetInterceptorData are read back"))
	}
}

func TestHooks_AggregateEqualsChainedSetMainData(t *testing.T) {
	a := NewAggregation()
	a.Pipe(a.Transform(func(v any) any { return v.(int) * 2 }))
	SetMainData(a, 21)
	if got := Aggregate(a); got != 42 {
		t.Error(test.DiffMessage(got, 42, "B18: Aggregate after SetMainData gives the transformed data"))
	}
	if !IsMainHandlerCalled(a) {
		t.Error(test.DiffMessage(IsMainHandlerCalled(a), true, "B18: Pipe sets IsMainHandlerCalled"))
	}
}
