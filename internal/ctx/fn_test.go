package ctx

import (
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestGetTagParamIndex_NegativeIndexFallsBackToZero(t *testing.T) {
	idx, field := GetTagParamIndex("items.-1")
	if idx != 0 || field != "items" {
		t.Error(test.DiffMessage([2]any{idx, field}, [2]any{0, "items"}, "a negative parsed index should fall back to 0"))
	}
}

func TestGetTagParams_TrimsAndDropsEmptyParts(t *testing.T) {
	got := GetTagParams(" name , ,limit=3,")
	if len(got) != 2 || got[0] != "name" || got[1] != "limit=3" {
		t.Error(test.DiffMessage(got, []string{"name", "limit=3"}, "GetTagParams should trim parts and drop empty ones"))
	}
}

func TestGetTagParamIndex_WithoutDot(t *testing.T) {
	idx, field := GetTagParamIndex("name")
	if idx != 0 || field != "name" {
		t.Error(test.DiffMessage([2]any{idx, field}, [2]any{0, "name"}, "a tag without a dot should have index 0"))
	}
}

func TestGetTagParamIndex_WithIndex(t *testing.T) {
	idx, field := GetTagParamIndex("integers_1.3")
	if idx != 3 || field != "integers_1" {
		t.Error(test.DiffMessage([2]any{idx, field}, [2]any{3, "integers_1"}, "a tag with a numeric suffix should split into index and field"))
	}
}
