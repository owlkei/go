package origin_test

import (
	"strconv"
	"testing"

	"go.owlky.com/owlky/internal/middlewares/origin"
	"go.owlky.com/owlky/internal/test"
)

func mustParse(t *testing.T, entries ...string) *origin.Set {
	t.Helper()
	set, err := origin.Parse("Origins", entries)
	if err != nil {
		t.Fatalf("fixture: Parse(%q) failed: %v", entries, err)
	}
	return set
}

func parseError(entries ...string) string {
	_, err := origin.Parse("Origins", entries)
	if err == nil {
		return ""
	}
	return err.Error()
}

func expectRefused(t *testing.T, entry, what string) {
	t.Helper()
	want := "Origins entry " + strconv.Quote(entry) + ` must be scheme://host[:port] in lower case, scheme://*.host[:port] or "*" alone`
	if got := parseError(entry); got != want {
		t.Error(test.DiffMessage(got, want, what))
	}
}

func expectMatch(t *testing.T, set *origin.Set, value string, want bool, what string) {
	t.Helper()
	if got := set.Matches(value); got != want {
		t.Error(test.DiffMessage(got, want, what))
	}
}
