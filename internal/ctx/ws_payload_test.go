package ctx

import (
	"reflect"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestWSPayload_Bind(t *testing.T) {
	p := WSPayload{"name": "joe", "age": float64(30)}
	result, fls := p.Bind(wsPayloadBindDTO{})

	dto, ok := result.(wsPayloadBindDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, wsPayloadBindDTO{}, "Bind should return a wsPayloadBindDTO"))
	}
	if dto.Name != "joe" || dto.Age != 30 {
		t.Error(test.DiffMessage(dto, wsPayloadBindDTO{Name: "joe", Age: 30}, "Bind should populate fields from the payload map"))
	}
	if len(fls) != 2 {
		t.Error(test.DiffMessage(len(fls), 2, "Bind should report a FieldLevel per bound field"))
	}
}

func TestWSPayload_BindSharesBodyPlan(t *testing.T) {
	data := map[string]any{"name": "joe", "age": float64(300), "page": float64(2)}
	type dto struct {
		Name string   `bind:"name"`
		Age  int8     `bind:"age"`
		Page namedInt `bind:"page"`
	}
	var fromPayload, fromBody any
	var flsPayload, flsBody []FieldLevel
	if !noPanic(t, "binding named types and out-of-range numbers", func() {
		fromPayload, flsPayload = WSPayload(data).Bind(dto{})
		fromBody, flsBody = Body(data).Bind(dto{})
	}) {
		return
	}
	if !reflect.DeepEqual(fromPayload, fromBody) || !reflect.DeepEqual(flsPayload, flsBody) {
		t.Error(test.DiffMessage(fromPayload, fromBody, "WSPayload.Bind should bind like Body.Bind"))
	}
	if got := fromPayload.(dto); got.Age != 0 || got.Page != 2 {
		t.Error(test.DiffMessage(got, dto{Name: "joe", Page: 2}, "B7, B1: WSPayload.Bind should apply the shared rules"))
	}
}

func TestWSPayload_BindValueEqualsBind(t *testing.T) {
	p := WSPayload{"name": "joe", "age": float64(30)}
	want, _ := p.Bind(bindValueDTO{})
	got := p.BindValue(bindValueDTO{})
	if !reflect.DeepEqual(got, want) || got.(bindValueDTO).Name != "joe" {
		t.Error(test.DiffMessage(got, want, "BindValue should return Bind's value"))
	}
}
