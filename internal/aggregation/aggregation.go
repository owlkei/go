package aggregation

const inlineOperatorCount = 4

type Aggregation struct {
	InterceptorData     any
	mainData            any
	Name                string
	extraOperators      []Operator
	inlineOperators     [inlineOperatorCount]Operator
	operatorCount       int
	IsMainHandlerCalled bool
}

func NewAggregation() *Aggregation {
	return &Aggregation{}
}

func (a *Aggregation) Pipe(operators ...Operator) any {
	a.IsMainHandlerCalled = true
	a.operatorCount = len(operators)
	if len(operators) <= inlineOperatorCount {
		copy(a.inlineOperators[:], operators)
		a.extraOperators = nil
		return nil
	}

	a.extraOperators = append(a.extraOperators[:0], operators...)
	return nil
}

func (a *Aggregation) SetMainData(d any) *Aggregation {
	a.mainData = d
	return a
}

func (a *Aggregation) operators() []Operator {
	if a.operatorCount > inlineOperatorCount {
		return a.extraOperators
	}
	return a.inlineOperators[:a.operatorCount]
}

func (a *Aggregation) Aggregate() any {
	for _, operator := range a.operators() {
		switch operator.kind {
		case operatorTransform:
			a.mainData = operator.fn(a.mainData)
		case operatorTap:
			operator.fn(a.mainData)
		case operatorFilter:
			if !operator.predicate(a.mainData) {
				return nil
			}
		}
	}

	return a.mainData
}
