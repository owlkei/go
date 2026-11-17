package aggregation

type operatorKind uint8

const (
	operatorNone operatorKind = iota
	operatorTransform
	operatorTap
	operatorFilter
)

type Operator struct {
	fn        func(any) any
	predicate func(any) bool
	kind      operatorKind
}

func (a *Aggregation) Transform(fn func(any) any) Operator {
	if fn == nil {
		return Operator{}
	}
	return Operator{kind: operatorTransform, fn: fn}
}

func (a *Aggregation) Tap(fn func(any) any) Operator {
	if fn == nil {
		return Operator{}
	}
	return Operator{kind: operatorTap, fn: fn}
}

func (a *Aggregation) Filter(predicate func(any) bool) Operator {
	if predicate == nil {
		return Operator{}
	}
	return Operator{kind: operatorFilter, predicate: predicate}
}
