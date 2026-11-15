package crypto

import (
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

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

func TestRandomHex_Length(t *testing.T) {
	for _, n := range []int{1, 8, 16, 17, 33} {
		got := RandomHex(n)
		if len(got) != n {
			t.Error(test.DiffMessage(len(got), n, "K2: RandomHex length"))
		}
	}
}

func TestRandomHex_OnlyHex(t *testing.T) {
	for _, n := range []int{1, 8, 16, 17, 33, 4096} {
		got := RandomHex(n)
		if !isHex(got) {
			t.Error(test.DiffMessage(got, "0-9a-f only", "K2: RandomHex alphabet"))
		}
	}
}

func TestRandomHex_ZeroAndNegative(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		got := RandomHex(n)
		if got != "" {
			t.Error(test.DiffMessage(got, "", "K2: RandomHex of non-positive length"))
		}
	}
}

func TestAppendRandomHex_AppendsAfterPrefix(t *testing.T) {
	for _, n := range []int{1, 8, 17} {
		dst := []byte("id-")
		got := AppendRandomHex(dst, n)
		if len(got) != 3+n {
			t.Fatal(test.DiffMessage(len(got), 3+n, "K3: appended length"))
		}
		if string(got[:3]) != "id-" {
			t.Error(test.DiffMessage(string(got[:3]), "id-", "K3: prefix unchanged"))
		}
		if !isHex(string(got[3:])) {
			t.Error(test.DiffMessage(string(got[3:]), "0-9a-f only", "K3: appended alphabet"))
		}
	}
}

func TestAppendRandomHex_NoAllocWithCapacity(t *testing.T) {
	buf := make([]byte, 0, 64)
	allocs := testing.AllocsPerRun(100, func() {
		buf = AppendRandomHex(buf[:0], 8)
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "K3: allocations with enough capacity"))
	}
	if len(buf) != 8 {
		t.Error(test.DiffMessage(len(buf), 8, "K3: length after append"))
	}
}

func TestAppendRandomHex_ZeroAndNegative(t *testing.T) {
	for _, n := range []int{0, -1} {
		dst := []byte("abc")
		got := AppendRandomHex(dst, n)
		if string(got) != "abc" {
			t.Error(test.DiffMessage(string(got), "abc", "K3: non-positive length returns dst"))
		}
	}
	if got := AppendRandomHex(nil, 0); len(got) != 0 {
		t.Error(test.DiffMessage(len(got), 0, "K3: nil dst with n = 0"))
	}
}

func TestRandomHex_Distinct(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		seen[RandomHex(16)] = struct{}{}
	}
	if len(seen) != 10000 {
		t.Error(test.DiffMessage(len(seen), 10000, "K4: distinct RandomHex(16)"))
	}
}

func TestRandomHex_EveryDigitAtEveryPosition(t *testing.T) {
	var seen [16][16]bool
	for i := 0; i < 10000; i++ {
		s := RandomHex(16)
		for pos := 0; pos < 16; pos++ {
			c := s[pos]
			d := 0
			if c >= 'a' {
				d = int(c-'a') + 10
			} else {
				d = int(c - '0')
			}
			seen[pos][d] = true
		}
	}
	for pos := 0; pos < 16; pos++ {
		for d := 0; d < 16; d++ {
			if !seen[pos][d] {
				t.Error(test.DiffMessage(false, true, "K4: digit never seen at a position"))
				return
			}
		}
	}
}

func TestRandomHex_ConcurrentNoRace(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 0, 16)
			for i := 0; i < 1000; i++ {
				s := RandomHex(8)
				if len(s) != 8 || !isHex(s) {
					t.Error(test.DiffMessage(s, "8 hex characters", "K5: RandomHex under concurrency"))
					return
				}
				buf = AppendRandomHex(buf[:0], 8)
				if len(buf) != 8 || !isHex(string(buf)) {
					t.Error(test.DiffMessage(string(buf), "8 hex characters", "K5: AppendRandomHex under concurrency"))
					return
				}
			}
		}()
	}
	wg.Wait()
}
