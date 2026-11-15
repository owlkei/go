package ctx

import (
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func TestContextID_SetID_SixteenLowercaseHex(t *testing.T) {
	var c contextID
	c.SetID()
	if len(c.id) != 16 {
		t.Error(test.DiffMessage(len(c.id), 16, "B24: SetID should give an id of 16 characters"))
	}
	if !isLowerHex(c.id) {
		t.Error(test.DiffMessage(c.id, "<16 lowercase hexadecimal characters>", "B24: SetID should give lowercase hexadecimal characters only"))
	}
}

func TestContextID_SetID_StillUnique(t *testing.T) {
	var c1, c2 contextID
	c1.SetID()
	c2.SetID()
	if c1.id == c2.id {
		t.Error(test.DiffMessage(c1.id, "<different id>", "ids should be unique across separate contexts"))
	}
}

func TestContextID_SetID_ManyIDsAreMostlyDistinct(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		var c contextID
		c.SetID()
		if len(c.id) != 16 || !isLowerHex(c.id) {
			t.Fatal(test.DiffMessage(c.id, "<16 lowercase hexadecimal characters>", "B24: every id should be well formed"))
		}
		seen[c.id] = struct{}{}
	}
	if len(seen) < n-10 {
		t.Error(test.DiffMessage(len(seen), n, "B24: 10,000 consecutive ids should hold at least 9,990 distinct values"))
	}
}

func TestContextID_SetID_IdempotentUntilReset(t *testing.T) {
	var c contextID
	c.SetID()
	first := c.id
	c.SetID()
	if c.id != first {
		t.Error(test.DiffMessage(c.id, first, "B24: SetID should keep its id on a second call"))
	}
	if c.GetID() != first {
		t.Error(test.DiffMessage(c.GetID(), first, "B24: GetID should return the id"))
	}
}

func TestContextID_SetID_NeverEmpty(t *testing.T) {
	for i := 0; i < 1000; i++ {
		var c contextID
		c.SetID()
		if c.id == "" {
			t.Fatal(test.DiffMessage(c.id, "<non-empty>", "B24: SetID should never return an empty id"))
		}
	}
}

func TestContextID_ConcurrentContexts_NoDataRace(t *testing.T) {
	const goroutines = 16
	ids := make([]string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			var c contextID
			c.SetID()
			ids[i] = c.GetID()
		}()
	}
	wg.Wait()
	seen := map[string]struct{}{}
	for _, id := range ids {
		if len(id) != 16 || !isLowerHex(id) {
			t.Error(test.DiffMessage(id, "<16 lowercase hexadecimal characters>", "B24: ids generated concurrently should be well formed"))
		}
		seen[id] = struct{}{}
	}
	if len(seen) != goroutines {
		t.Error(test.DiffMessage(len(seen), goroutines, "B24: ids generated concurrently should differ"))
	}
}
