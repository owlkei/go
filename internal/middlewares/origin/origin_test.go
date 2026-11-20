package origin_test

import (
	"fmt"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/middlewares/origin"
	"go.owlky.com/owlky/internal/test"
)

func TestParse_EmptyListMatchesNothing(t *testing.T) {
	set := mustParse(t)
	if set.IsAny() {
		t.Error(test.DiffMessage(set.IsAny(), false, "P4: an empty list is not *"))
	}
	expectMatch(t, set, "https://a.com", false, "P4: an empty set matches no origin")
	expectMatch(t, set, "", false, "P4: an empty set does not match the empty origin")
}

func TestParse_NilListMatchesNothing(t *testing.T) {
	set, err := origin.Parse("Origins", nil)
	if err != nil || set == nil {
		t.Fatal(test.DiffMessage(err, nil, "P4: a nil list parses"))
	}
	expectMatch(t, set, "https://a.com", false, "P4: a nil list matches no origin")
}

func TestParse_ExactEntry(t *testing.T) {
	set := mustParse(t, "https://app.example.com")
	expectMatch(t, set, "https://app.example.com", true, "P4: an exact entry matches itself")
	expectMatch(t, set, "http://app.example.com", false, "P4: the scheme is part of the origin")
	expectMatch(t, set, "https://app.example.com:8443", false, "P4: the port is part of the origin")
	expectMatch(t, set, "https://other.example.com", false, "P4: another host")
	expectMatch(t, set, "https://app.example.com.evil.com", false, "P4: the entry as a prefix of another host")
	expectMatch(t, set, "https://evil.com/https://app.example.com", false, "P4: the entry as a suffix of another origin")
}

func TestParse_EntryWithPort(t *testing.T) {
	set := mustParse(t, "http://localhost:5173")
	expectMatch(t, set, "http://localhost:5173", true, "P4: host with port")
	expectMatch(t, set, "http://localhost", false, "P4: the same host without the port")
	expectMatch(t, set, "http://localhost:5174", false, "P4: the same host on another port")
}

func TestParse_PortBoundaries(t *testing.T) {
	mustParse(t, "https://a.com:1", "https://a.com:65535")
	expectRefused(t, "https://a.com:65536", "A3: a port over 65535")
	expectRefused(t, "https://a.com:", "A3: an empty port")
	expectRefused(t, "https://a.com:http", "A3: a port that is not a number")
	expectRefused(t, "https://a.com:-1", "A3: a negative port")
}

func TestParse_IPv6Entry(t *testing.T) {
	set := mustParse(t, "http://[::1]:3000", "https://[2001:db8::1]")
	expectMatch(t, set, "http://[::1]:3000", true, "P4: an IPv6 host with a port")
	expectMatch(t, set, "https://[2001:db8::1]", true, "P4: an IPv6 host")
	expectMatch(t, set, "http://[::1]", false, "P4: the IPv6 host without its port")
}

func TestParse_IPv6Refusals(t *testing.T) {
	expectRefused(t, "http://[fe80::1%eth0]", "A3: an IPv6 zone")
	expectRefused(t, "http://[127.0.0.1]", "A3: an IPv4 address in brackets")
	expectRefused(t, "http://::1", "A3: an IPv6 address without brackets")
	expectRefused(t, "http://[::1", "A3: an unclosed bracket")
	expectRefused(t, "http://[]", "A3: empty brackets")
}

func TestParse_IPv4Entry(t *testing.T) {
	set := mustParse(t, "http://127.0.0.1:8080")
	expectMatch(t, set, "http://127.0.0.1:8080", true, "P4: an IPv4 host with a port")
}

func TestParse_UpperCaseRefused(t *testing.T) {
	expectRefused(t, "https://App.example.com", "A3: an upper-case host")
	expectRefused(t, "HTTPS://app.example.com", "A3: an upper-case scheme")
	expectRefused(t, "https://*.Example.com", "A3: an upper-case wildcard host")
}

func TestParse_TrailingSlashRefused(t *testing.T) {
	expectRefused(t, "https://app.example.com/", "A3: a trailing slash")
	expectRefused(t, "https://*.example.com/", "A3: a trailing slash on a wildcard")
}

func TestParse_PathQueryFragmentUserinfoRefused(t *testing.T) {
	expectRefused(t, "https://app.example.com/path", "A3: a path")
	expectRefused(t, "https://app.example.com?x=1", "A3: a query")
	expectRefused(t, "https://app.example.com#x", "A3: a fragment")
	expectRefused(t, "https://user@app.example.com", "A3: userinfo")
	expectRefused(t, "https://app.example.com ", "A3: a trailing space")
	expectRefused(t, " https://app.example.com", "A3: a leading space")
}

func TestParse_MissingSchemeOrHostRefused(t *testing.T) {
	expectRefused(t, "", "A3: an empty entry")
	expectRefused(t, "app.example.com", "A3: no scheme")
	expectRefused(t, "//app.example.com", "A3: an empty scheme")
	expectRefused(t, "https://", "A3: no host")
	expectRefused(t, "https:app.example.com", "A3: no //")
	expectRefused(t, "1https://app.example.com", "A3: a scheme that starts with a digit")
	expectRefused(t, "null", "A3: null is not an origin entry")
}

func TestParse_EmptyHostLabelRefused(t *testing.T) {
	expectRefused(t, "https://.example.com", "A3: a leading dot")
	expectRefused(t, "https://app..example.com", "A3: an empty label")
	expectRefused(t, "https://app.example.com.", "A3: a trailing dot")
	expectRefused(t, "https://app_1.example.com", "A3: an underscore in the host")
}

func TestParse_WildcardEntry(t *testing.T) {
	set := mustParse(t, "https://*.example.com")
	expectMatch(t, set, "https://app.example.com", true, "P4: a subdomain matches the wildcard")
	expectMatch(t, set, "https://a-1.example.com", true, "P4: a subdomain with a hyphen and a digit")
	expectMatch(t, set, "https://example.com", false, "P4: the apex is not a subdomain")
	expectMatch(t, set, "https://.example.com", false, "P4: an empty subdomain")
	expectMatch(t, set, "http://app.example.com", false, "P4: the wildcard keeps its scheme")
	expectMatch(t, set, "https://app.example.com:8443", false, "P4: the wildcard keeps its port")
	expectMatch(t, set, "https://evilexample.com", false, "P4: the suffix starts at a label boundary")
	expectMatch(t, set, "https://app.example.com.evil.com", false, "P4: the wildcard host as a prefix of another host")
}

func TestParse_WildcardWithPort(t *testing.T) {
	set := mustParse(t, "https://*.example.com:8443")
	expectMatch(t, set, "https://app.example.com:8443", true, "P4: a wildcard with a port")
	expectMatch(t, set, "https://app.example.com", false, "P4: the wildcard's port is required")
	expectMatch(t, set, "https://app.example.com:8444", false, "P4: another port")
}

func TestMatches_WildcardHostileOrigins(t *testing.T) {
	set := mustParse(t, "https://*.example.com")
	expectMatch(t, set, "https://evil.com/.example.com", false, "P4: a path before the suffix")
	expectMatch(t, set, "https://evil.com#.example.com", false, "P4: a fragment before the suffix")
	expectMatch(t, set, "https://evil.com?.example.com", false, "P4: a query before the suffix")
	expectMatch(t, set, "https://evil.com:443.example.com", false, "P4: a port before the suffix")
	expectMatch(t, set, "https://user@evil.example.com", false, "P4: userinfo before the suffix")
	expectMatch(t, set, "https://evil.com\\.example.com", false, "P4: a backslash before the suffix")
	expectMatch(t, set, "https://a b.example.com", false, "P4: a space before the suffix")
	expectMatch(t, set, "https://a\x00.example.com", false, "P4: a NUL before the suffix")
}

func TestParse_WildcardRefusals(t *testing.T) {
	expectRefused(t, "https://*", "A3: a wildcard without a host")
	expectRefused(t, "https://*.", "A3: a wildcard with an empty host")
	expectRefused(t, "https://*example.com", "A3: a wildcard without the dot")
	expectRefused(t, "https://app.*.com", "A3: a wildcard in the middle")
	expectRefused(t, "https://*.*.example.com", "A3: two wildcards")
	expectRefused(t, "https://app.example.*", "A3: a wildcard at the end")
	expectRefused(t, "*://app.example.com", "A3: a wildcard scheme")
	expectRefused(t, "https://app.example.com:*", "A3: a wildcard port")
}

func TestParse_StarAlone(t *testing.T) {
	set := mustParse(t, "*")
	if !set.IsAny() {
		t.Error(test.DiffMessage(set.IsAny(), true, "P4: * alone is any origin"))
	}
	expectMatch(t, set, "https://anything.example", true, "P4: * matches every origin")
	expectMatch(t, set, "null", true, "P4: * matches null")
}

func TestParse_StarWithOtherEntriesRefused(t *testing.T) {
	want := `"*" must be the only entry of Origins`
	if got := parseError("*", "https://a.com"); got != want {
		t.Error(test.DiffMessage(got, want, "A3: * first"))
	}
	if got := parseError("https://a.com", "*"); got != want {
		t.Error(test.DiffMessage(got, want, "A3: * last"))
	}
	if got := parseError("*", "*"); got != want {
		t.Error(test.DiffMessage(got, want, "A3: * twice"))
	}
}

func TestParse_EntryCountBoundary(t *testing.T) {
	entries := make([]string, 257)
	for i := range entries {
		entries[i] = fmt.Sprintf("https://h%d.example.com", i)
	}
	if got := parseError(entries[:256]...); got != "" {
		t.Error(test.DiffMessage(got, "", "A3: 256 entries are allowed"))
	}
	want := "Origins has 257 entries, at most 256 are allowed"
	if got := parseError(entries...); got != want {
		t.Error(test.DiffMessage(got, want, "A3: 257 entries are refused"))
	}
}

func TestParse_EntryLengthBoundary(t *testing.T) {
	prefix, suffix := "https://", ".com"
	atLimit := prefix + strings.Repeat("a", origin.MaxLength-len(prefix)-len(suffix)) + suffix
	set := mustParse(t, atLimit)
	expectMatch(t, set, atLimit, true, "A3: an entry of 256 bytes is allowed and matches")
	expectRefused(t, prefix+strings.Repeat("a", origin.MaxLength-len(prefix)-len(suffix)+1)+suffix, "A3: an entry of 257 bytes")
}

func TestParse_ErrorNamesTheField(t *testing.T) {
	_, err := origin.Parse("OK_ORIGINS", []string{"bad"})
	want := `OK_ORIGINS entry "bad" must be scheme://host[:port] in lower case, scheme://*.host[:port] or "*" alone`
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "A3: the message names the field"))
	}
}

func TestParse_OneBadEntryRefusesTheList(t *testing.T) {
	set, err := origin.Parse("Origins", []string{"https://a.com", "https://B.com"})
	if err == nil || set != nil {
		t.Error(test.DiffMessage([]any{set, err}, "<nil set, an error>", "A3: one bad entry refuses the whole list"))
	}
}

func TestParse_CopiesEntries(t *testing.T) {
	entries := []string{"https://a.com"}
	set := mustParse(t, entries...)
	entries[0] = "https://evil.com"
	expectMatch(t, set, "https://a.com", true, "A2: the set keeps the entry it parsed")
	expectMatch(t, set, "https://evil.com", false, "A2: a later write to the caller's slice changes nothing")
	if got := set.Entries(); len(got) != 1 || got[0] != "https://a.com" {
		t.Error(test.DiffMessage(got, []string{"https://a.com"}, "A2: Entries reports the parsed entries"))
	}
}

func TestEntries_ReturnsACopy(t *testing.T) {
	set := mustParse(t, "https://a.com", "https://*.b.com")
	got := set.Entries()
	got[0] = "https://evil.com"
	if again := set.Entries(); len(again) != 2 || again[0] != "https://a.com" || again[1] != "https://*.b.com" {
		t.Error(test.DiffMessage(again, []string{"https://a.com", "https://*.b.com"}, "A2: Entries returns a copy, in order"))
	}
}

func TestMatches_TooLongOriginRefused(t *testing.T) {
	set := mustParse(t, "https://*.example.com")
	long := "https://" + strings.Repeat("a", 300) + ".example.com"
	expectMatch(t, set, long, false, "W5: an origin over 256 bytes never matches")
}

func TestMatches_NullAndEmptyRefused(t *testing.T) {
	set := mustParse(t, "https://a.com", "https://*.b.com")
	expectMatch(t, set, "null", false, "W5: null is not in the set")
	expectMatch(t, set, "", false, "W5: the empty origin is not in the set")
	expectMatch(t, set, "*", false, "W5: a literal * origin is not in the set")
	expectMatch(t, set, "https://*.b.com", false, "W5: the wildcard entry itself is not an origin")
}

func TestMatches_IsCaseSensitive(t *testing.T) {
	set := mustParse(t, "https://a.com", "https://*.b.com")
	expectMatch(t, set, "https://A.com", false, "P4: Matches compares the lower-cased origin its caller passes")
	expectMatch(t, set, "https://X.b.com", false, "P4: an upper-case subdomain is not a wildcard part")
}

func TestMatches_ConcurrentReads(t *testing.T) {
	set := mustParse(t, "https://a.com", "https://*.b.com")
	done := make(chan bool, 8)
	for range 8 {
		go func() {
			ok := true
			for range 200 {
				ok = ok && set.Matches("https://a.com") && set.Matches("https://x.b.com") && !set.Matches("https://evil.com")
			}
			done <- ok
		}()
	}
	for range 8 {
		if !<-done {
			t.Error(test.DiffMessage(false, true, "P4: the set is read-only after Parse"))
		}
	}
}
