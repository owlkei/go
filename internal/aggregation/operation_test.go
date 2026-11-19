package aggregation

import (
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestTransform_RegisteredAndApplied(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "original")
	called := false
	a.Pipe(a.Transform(func(data any) any {
		called = true
		return "transformed"
	}))
	result := Aggregate(a)
	if !called {
		t.Error(test.DiffMessage(called, true, "Transform operator must be called"))
	}
	if result != "transformed" {
		t.Error(test.DiffMessage(result, "transformed", "Transform must update mainData"))
	}
}

func TestTransform_ReturnsRunnableOperator(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	op := a.Transform(func(data any) any { return data.(int) + 1 })
	a.Pipe(op)
	if got := Aggregate(a); got != 2 {
		t.Error(test.DiffMessage(got, 2, "A1: the returned Transform operator must run when piped"))
	}
}

func TestTap_CalledButDoesNotTransform(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "original")
	called := false
	a.Pipe(a.Tap(func(data any) any {
		called = true
		return "tap-result"
	}))
	result := Aggregate(a)
	if !called {
		t.Error(test.DiffMessage(called, true, "Tap operator must be called"))
	}
	if result != "original" {
		t.Error(test.DiffMessage(result, "original", "Tap must not change mainData"))
	}
}

func TestFilter_PredicateTrue(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 10)
	called := false
	a.Pipe(
		a.Filter(func(data any) bool {
			called = true
			return data.(int) > 5
		}),
	)

	result := Aggregate(a)

	if !called {
		t.Error(test.DiffMessage(called, true, "predicate must be called"))
	}
	if result != 10 {
		t.Error(test.DiffMessage(result, 10, "filter must return data when predicate is true"))
	}
}

func TestFilter_PredicateFalse(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 3)
	called := false
	a.Pipe(
		a.Filter(func(data any) bool {
			called = true
			return data.(int) > 5
		}),
	)

	result := Aggregate(a)

	if !called {
		t.Error(test.DiffMessage(called, true, "predicate must be called"))
	}
	if result != nil {
		t.Error(test.DiffMessage(result, nil, "filter must return nil when predicate is false"))
	}
}

func TestFilter_ReturnsRunnableOperator(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 1)
	op := a.Filter(func(data any) bool { return false })
	a.Pipe(op)
	if got := Aggregate(a); got != nil {
		t.Error(test.DiffMessage(got, nil, "A1: the returned Filter operator must run when piped"))
	}
}

func TestFilter_StopsTransform(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 3)
	transformCalled := false

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) > 5
		}),
		a.Transform(func(data any) any {
			transformCalled = true
			return data
		}),
	)

	result := Aggregate(a)

	if transformCalled {
		t.Error(test.DiffMessage(transformCalled, false, "Transform must not be called after filter returns nil"))
	}
	if result != nil {
		t.Error(test.DiffMessage(result, nil, "result must be nil when filter fails"))
	}
}

func TestFilter_MultipleFilters(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 30)

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) >= 18
		}),
		a.Filter(func(data any) bool {
			return data.(int) <= 65
		}),
	)

	result := Aggregate(a)

	if result != 30 {
		t.Error(test.DiffMessage(result, 30, "both filters should pass"))
	}
}

func TestFilter_MultipleFilters_FailsSecond(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 70)

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) >= 18
		}),
		a.Filter(func(data any) bool {
			return data.(int) <= 65
		}),
	)

	result := Aggregate(a)

	if result != nil {
		t.Error(test.DiffMessage(result, nil, "second filter should fail"))
	}
}

func TestFilter_WithTap(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 10)
	tapCalled := false

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) > 5
		}),
		a.Tap(func(data any) any {
			tapCalled = true
			return nil
		}),
	)

	result := Aggregate(a)

	if !tapCalled {
		t.Error(test.DiffMessage(tapCalled, true, "Tap must be called after filter succeeds"))
	}
	if result != 10 {
		t.Error(test.DiffMessage(result, 10, "Tap must not change data"))
	}
}

func TestFilter_WithTap_FilterFails(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 3)
	tapCalled := false

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) > 5
		}),
		a.Tap(func(data any) any {
			tapCalled = true
			return nil
		}),
	)

	result := Aggregate(a)

	if tapCalled {
		t.Error(test.DiffMessage(tapCalled, false, "Tap must not be called when filter fails"))
	}
	if result != nil {
		t.Error(test.DiffMessage(result, nil, "result must be nil"))
	}
}

func TestFilter_ThenTransform(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 10)

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) > 5
		}),
		a.Transform(func(data any) any {
			return data.(int) * 2
		}),
	)

	result := Aggregate(a)

	if result != 20 {
		t.Error(test.DiffMessage(result, 20, "filter pass, transform should execute"))
	}
}

func TestFilter_WithNilData(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, nil)

	a.Pipe(
		a.Filter(func(data any) bool {
			return data == nil
		}),
	)

	result := Aggregate(a)

	if result != nil {
		t.Error(test.DiffMessage(result, nil, "filter with nil input should handle gracefully"))
	}
}

func TestFilter_ZeroValue(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, 0)

	a.Pipe(
		a.Filter(func(data any) bool {
			return data.(int) == 0
		}),
	)

	result := Aggregate(a)

	if result != 0 {
		t.Error(test.DiffMessage(result, 0, "filter should distinguish zero from nil"))
	}
}

func TestFilter_EmptyString(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, "")

	a.Pipe(
		a.Filter(func(data any) bool {
			return data == ""
		}),
	)

	result := Aggregate(a)

	if result != "" {
		t.Error(test.DiffMessage(result, "", "filter should handle empty string"))
	}
}

func TestFilter_NilDataPredicateTrueContinues(t *testing.T) {
	a := NewAggregation()
	SetMainData(a, nil)
	a.Pipe(
		a.Filter(isTrue),
		a.Transform(func(d any) any { return "wrapped" }),
	)
	if got := Aggregate(a); got != "wrapped" {
		t.Error(test.DiffMessage(got, "wrapped", "A3: nil data with a true predicate must continue"))
	}
}

func TestFilter_NilDataPredicateFalseStops(t *testing.T) {
	a := NewAggregation()
	called := false
	a.Pipe(
		a.Filter(func(d any) bool { return false }),
		a.Tap(func(d any) any { called = true; return nil }),
	)
	if got := Aggregate(a); got != nil || called {
		t.Error(test.DiffMessage([]any{got, called}, []any{nil, false}, "A3: a false predicate stops the chain"))
	}
}
