package crypto

import (
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestUUID_Format(t *testing.T) {
	id, err := UUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 {
		t.Fatal(test.DiffMessage(len(id), 36, "K1: UUID length"))
	}
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Error(test.DiffMessage(id, "8-4-4-4-12", "K1: UUID dashes"))
	}
	if !isHex(id[0:8] + id[9:13] + id[14:18] + id[19:23] + id[24:]) {
		t.Error(test.DiffMessage(id, "lower-case hex", "K1: UUID digits"))
	}
	if id[14] != '4' {
		t.Error(test.DiffMessage(id[14], byte('4'), "K1: UUID version nibble"))
	}
	if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Error(test.DiffMessage(v, "one of 8 9 a b", "K1: UUID variant nibble"))
	}
}

func TestUUID_Distinct(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id, err := UUID()
		if err != nil {
			t.Fatal(err)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "K1: distinct UUIDs"))
	}
}
