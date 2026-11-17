package common

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/routing"
)

func buildBenchHTTP(n int) *HTTP {
	r := &HTTP{
		patternToFuncNameMap: make(map[string]string, n),
		funcNameToPatternMap: make(map[string]string, n),
	}
	for i := range n {
		fn := fmt.Sprintf("READ_resource%d", i)
		route := fmt.Sprintf("/resource%d/", i)
		p := routing.MethodRouteVersionToPattern("GET", route, "")
		HTTPPatternToFuncNameMap(r)[p] = fn
		HTTPFuncNameToPatternMap(r)[fn] = p
	}
	return r
}

func buildBenchWS(n int) *WS {
	ws := &WS{
		funcNameByEvent: make(map[string]string, n),
	}
	for i := range n {
		event := fmt.Sprintf("event%d", i)
		fn := fmt.Sprintf("ON_event%d", i)
		ws.funcNameByEvent[event] = fn
	}
	return ws
}

var benchCB = func(_ int, _ reflect.Type, _ reflect.Value, _ reflect.Value) {}

func BenchmarkNormalizeRecovered_Exception(b *testing.B) {
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(ex)
	}
}

func BenchmarkNormalizeRecovered_Pointer(b *testing.B) {
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(&ex)
	}
}

func BenchmarkNormalizeRecovered_Error(b *testing.B) {
	err := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(err)
	}
}
