package aggregation

const inlineOperatorCount = 4

type Aggregation struct {
	interceptorData     any
	mainData            any
	name                string
	extraOperators      []Operator
	inlineOperators     [inlineOperatorCount]Operator
	operatorCount       int
	isMainHandlerCalled bool
}

func NewAggregation() *Aggregation {
	return &Aggregation{}
}

func (a *Aggregation) Pipe(operators ...Operator) any {
	a.isMainHandlerCalled = true
	a.operatorCount = len(operators)
	if len(operators) <= inlineOperatorCount {
		copy(a.inlineOperators[:], operators)
		a.extraOperators = nil
		return nil
	}

	a.extraOperators = append(a.extraOperators[:0], operators...)
	return nil
}

func SetMainData(a *Aggregation, d any) {
	a.mainData = d
}

func Name(a *Aggregation) string {
	return a.name
}

func SetName(a *Aggregation, name string) {
	a.name = name
}

func InterceptorData(a *Aggregation) any {
	return a.interceptorData
}

func SetInterceptorData(a *Aggregation, data any) {
	a.interceptorData = data
}

func IsMainHandlerCalled(a *Aggregation) bool {
	return a.isMainHandlerCalled
}

func (a *Aggregation) operators() []Operator {
	if a.operatorCount > inlineOperatorCount {
		return a.extraOperators
	}
	return a.inlineOperators[:a.operatorCount]
}

func Aggregate(a *Aggregation) any {
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
