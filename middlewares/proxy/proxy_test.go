package proxy

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/test"
)

func TestNewProxy_Panics_BothSet(t *testing.T) {
	text, ok := panicText(func() { Proxy{TrustedProxies: []string{"10.0.0.0/8"}, TrustedHops: 1}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid proxy: set TrustedProxies or TrustedHops, not both") {
		t.Error(test.DiffMessage(text, "invalid proxy: set TrustedProxies or TrustedHops, not both", "P1, G4: both modes set"))
	}
}

func TestNewProxy_Panics_NeitherSet(t *testing.T) {
	text, ok := panicText(func() { Proxy{}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid proxy: set TrustedProxies or TrustedHops") {
		t.Error(test.DiffMessage(text, "invalid proxy: set TrustedProxies or TrustedHops", "P1, G4: no mode set"))
	}
}

func TestNewProxy_Panics_InvalidEntry(t *testing.T) {
	for _, entry := range []string{"not-an-ip", "10.0.0.0/33", "999.1.1.1", "", "10.0.0.0/", "example.com", "10.0.0.0/8/8", "*", "::/129"} {
		text, ok := panicText(func() { Proxy{TrustedProxies: []string{entry}}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid proxy:") {
			t.Error(test.DiffMessage(text, "invalid proxy: ...", "P1, G4: entry "+entry+" is neither an IP nor a CIDR"))
		}
	}
}

func TestNewProxy_Panics_TooManyEntries(t *testing.T) {
	entries := make([]string, 257)
	for i := range entries {
		entries[i] = fmt.Sprintf("10.%d.%d.0/24", i/256, i%256)
	}
	text, ok := panicText(func() { Proxy{TrustedProxies: entries}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid proxy:") {
		t.Error(test.DiffMessage(text, "invalid proxy: ...", "P1, G4: more than 256 entries"))
	}
}

func TestNewProxy_Panics_HopsOutOfRange(t *testing.T) {
	for _, hops := range []int{-1, 17, 1000} {
		text, ok := panicText(func() { Proxy{TrustedHops: hops}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid proxy:") {
			t.Error(test.DiffMessage(text, "invalid proxy: ...", fmt.Sprintf("P1, G4: TrustedHops %d is outside 1-16", hops)))
		}
	}
}

func TestNewProxy_ValidConfigs(t *testing.T) {
	entries := make([]string, 256)
	for i := range entries {
		entries[i] = fmt.Sprintf("10.0.%d.0/24", i)
	}
	valid := []Proxy{
		{TrustedProxies: []string{"203.0.113.7"}},
		{TrustedProxies: []string{"10.0.0.0/8", "fd00::/8", "2001:db8::1", "::1"}},
		{TrustedProxies: entries},
		{TrustedHops: 1},
		{TrustedHops: 16},
		{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded},
		{TrustedHops: 2, Header: HeaderForwarded},
		{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true},
		{TrustedHops: 2, IsHostForwarded: true},
		{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded, IsHostForwarded: true},
		{TrustedHops: 1, Header: HeaderForwarded, IsHostForwarded: true},
	}
	for i, p := range valid {
		if text, ok := panicText(func() { p.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", fmt.Sprintf("P1: valid config %d", i)))
		}
	}
}

func TestProxy_HeaderConstants(t *testing.T) {
	if HeaderXForwarded != 0 || HeaderForwarded == HeaderXForwarded {
		t.Error(test.DiffMessage([]Header{HeaderXForwarded, HeaderForwarded}, "XForwarded is the zero value", "P11: the default family is X-Forwarded-*"))
	}
}

func TestProxy_Unbound_UsesConnection(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:4711"
	r.Host = "app.example.com"
	for k, v := range map[string]string{
		"X-Forwarded-For": "1.1.1.1", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example",
		"Forwarded": "for=2.2.2.2;proto=https;host=evil.example", "X-Real-IP": "3.3.3.3",
	} {
		r.Header.Set(k, v)
	}
	expectIP(t, ClientIP(r), "203.0.113.9", "P2: without proxy the client is the peer")
	if got := Scheme(r); got != "http" {
		t.Error(test.DiffMessage(got, "http", "P2: without proxy the scheme is the connection's"))
	}
	if got := Host(r); got != "app.example.com" {
		t.Error(test.DiffMessage(got, "app.example.com", "P2: without proxy the host is r.Host"))
	}
	r.TLS = &tls.ConnectionState{}
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "P2: r.TLS != nil is https"))
	}
}

func TestProxy_Unbound_ScopeWithoutProxy(t *testing.T) {
	r := scoped("[::ffff:192.0.2.5]:80", "X-Forwarded-For", "1.1.1.1")
	expectIP(t, ClientIP(r), "192.0.2.5", "P2: a scope nobody resolved reads the connection, IPv4-mapped unmapped")
}

func TestProxy_UnparsableRemoteAddr(t *testing.T) {
	for _, remote := range []string{"", "garbage", "10.0.0.1", "example.com:80"} {
		r := resolve(t, trustedPrivate, remote, "X-Forwarded-For", "1.1.1.1")
		if got := ClientIP(r); got.IsValid() {
			t.Error(test.DiffMessage(got, netip.Addr{}, "P2, P3: RemoteAddr "+remote+" is not ip:port, the zero Addr, headers ignored"))
		}
	}
}

func TestProxy_UntrustedPeer_IgnoresHeaders(t *testing.T) {
	headers := []string{
		"X-Forwarded-For", "1.1.1.1", "X-Forwarded-For", "2.2.2.2",
		"X-Forwarded-Proto", "https", "X-Forwarded-Proto", "https",
		"X-Forwarded-Host", "evil.example", "X-Forwarded-Host", "evil2.example",
		"Forwarded", "for=3.3.3.3;proto=https;host=evil.example", "Forwarded", "for=4.4.4.4;proto=https",
		"X-Real-IP", "5.5.5.5", "X-Client-IP", "6.6.6.6", "True-Client-IP", "7.7.7.7",
	}
	for _, header := range []Header{HeaderXForwarded, HeaderForwarded} {
		p := Proxy{TrustedProxies: []string{"10.0.0.0/8", "203.0.113.7"}, Header: header}
		r := resolve(t, p, "198.51.100.9:1234", headers...)
		expectIP(t, ClientIP(r), "198.51.100.9", fmt.Sprintf("P3: an untrusted peer's headers change nothing (family %d)", header))
		if got := Scheme(r); got != "http" {
			t.Error(test.DiffMessage(got, "http", "P3: scheme from an untrusted peer"))
		}
		if got := Host(r); got != "example.com" {
			t.Error(test.DiffMessage(got, "example.com", "P3: host from an untrusted peer"))
		}
	}
}

func TestProxy_TrustedPeerSingleIPEntry(t *testing.T) {
	p := Proxy{TrustedProxies: []string{"203.0.113.7"}}
	expectIP(t, ipFrom(t, p, "203.0.113.7:1", "X-Forwarded-For", "1.1.1.1"), "1.1.1.1", "P1: a single IP entry trusts exactly that address")
	expectIP(t, ipFrom(t, p, "203.0.113.8:1", "X-Forwarded-For", "1.1.1.1"), "203.0.113.8", "P3: its neighbor is not trusted")
}

func TestProxy_MappedPeerMatchesCIDR(t *testing.T) {
	expectIP(t, ipFrom(t, trustedPrivate, "[::ffff:10.0.0.1]:80", "X-Forwarded-For", "1.1.1.1"), "1.1.1.1", "P2: an IPv4-mapped peer is unmapped before matching a CIDR")
	p := Proxy{TrustedProxies: []string{"fd00::/8"}}
	expectIP(t, ipFrom(t, p, "[fd00::1]:80", "X-Forwarded-For", "2001:db8::5"), "2001:db8::5", "P4: IPv6 CIDRs match")
}

func TestProxy_RightmostUntrusted(t *testing.T) {
	cases := []struct {
		header, want, desc string
	}{
		{"8.8.8.8", "8.8.8.8", "one proxy"},
		{"8.8.8.8, 10.0.0.2", "8.8.8.8", "two proxies"},
		{"8.8.8.8, 10.0.0.3, 10.0.0.2", "8.8.8.8", "three proxies"},
		{"9.9.9.9, 8.8.8.8, 10.0.0.2", "8.8.8.8", "the rightmost untrusted wins over entries to its left"},
		{"8.8.8.8,10.0.0.2", "8.8.8.8", "no space after the comma"},
		{"8.8.8.8 ,   10.0.0.2  ", "8.8.8.8", "white space around entries"},
	}
	for _, c := range cases {
		expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", c.header), c.want, "P4: "+c.desc)
	}
}

func TestProxy_AllTrusted_Leftmost(t *testing.T) {
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "10.0.0.5, 10.0.0.6, 10.0.0.7"), "10.0.0.5", "P4: every entry trusted, the leftmost read")
}

func TestProxy_NoHeader_Peer(t *testing.T) {
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80"), "10.0.0.1", "P4: no entry, the peer")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", ""), "10.0.0.1", "P6: an empty header is no valid entry, the peer")
}

func TestProxy_MultipleHeaderLines(t *testing.T) {
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1", "X-Forwarded-For", "2.2.2.2, 10.0.0.2"), "2.2.2.2", "P4: repeated lines, the last line first")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "7.7.7.7, 10.0.0.9", "X-Forwarded-For", "10.0.0.8"), "7.7.7.7", "P4: the walk continues into the previous line")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "10.0.0.5", "X-Forwarded-For", "10.0.0.6"), "10.0.0.5", "P4: all trusted over two lines, the leftmost entry of the first line")
}

func TestProxy_Hops(t *testing.T) {
	one := Proxy{TrustedHops: 1}
	two := Proxy{TrustedHops: 2}
	expectIP(t, ipFrom(t, one, "198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, 2.2.2.2"), "2.2.2.2", "P5: n = 1 is the last entry")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, 2.2.2.2"), "1.1.1.1", "P5: n = 2 is the second from the right")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "9.9.9.9, 1.1.1.1, 2.2.2.2"), "1.1.1.1", "P5: entries left of n are never used")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "2.2.2.2"), "198.51.100.1", "P5: fewer entries than n, the peer")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80"), "198.51.100.1", "P5: no entry, the peer")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "9.9.9.9", "X-Forwarded-For", "1.1.1.1, 2.2.2.2"), "1.1.1.1", "P5: entries are counted across repeated lines")
}

func TestProxy_Hops_InvalidEntry(t *testing.T) {
	one := Proxy{TrustedHops: 1}
	two := Proxy{TrustedHops: 2}
	expectIP(t, ipFrom(t, one, "198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, unknown"), "198.51.100.1", "P6: an invalid entry at position n resolves to the peer")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "unknown, 2.2.2.2"), "198.51.100.1", "P6: an invalid entry at position n resolves to the peer (n = 2)")
	expectIP(t, ipFrom(t, two, "198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, unknown"), "198.51.100.1", "P6: an invalid entry right of position n resolves to the peer")
	expectIP(t, ipFrom(t, one, "198.51.100.1:80", "X-Forwarded-For", "unknown, 2.2.2.2"), "2.2.2.2", "P6: an invalid entry left of position n is never read")
}

func TestProxy_EntryForms(t *testing.T) {
	valid := map[string]string{
		"1.2.3.4":               "1.2.3.4",
		"1.2.3.4:5678":          "1.2.3.4",
		"[2001:db8::1]:5":       "2001:db8::1",
		"[2001:db8::1]":         "2001:db8::1",
		"2001:db8::1":           "2001:db8::1",
		"::ffff:1.2.3.4":        "1.2.3.4",
		"[::ffff:1.2.3.4]:80":   "1.2.3.4",
		"  1.2.3.4  ":           "1.2.3.4",
		"2001:DB8:0:0:0:0:0:1":  "2001:db8::1",
		"[2001:db8:0:0::1]:443": "2001:db8::1",
	}
	for entry, want := range valid {
		got := ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", entry)
		expectIP(t, got, want, "P6: entry form "+entry)
		if got.Is4In6() {
			t.Error(test.DiffMessage(got, "<unmapped>", "P6: IPv4-mapped addresses are unmapped: "+entry))
		}
	}
	invalid := []string{
		"unknown", "_hidden", "fe80::1%eth0", "[fe80::1%eth0]:80", "example.com", "1.2.3.4:", "1.2.3.4:abc", "300.1.1.1",
		"1.2.3", "1.2.3.4.5", "[1.2.3.4", "1.2.3.4]", "[]", "::g", "1.2.3.4/32", "0x7f.0.0.1", "1.2.3.4 5.6.7.8",
		"1.2.3.4\x00", "1.2.3.4\r\nX-Injected: 1", "\"1.2.3.4\"", strings.Repeat("a", 513), "1.2.3.4" + strings.Repeat(" ", 520),
	}
	for _, entry := range invalid {
		expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", entry), "10.0.0.1", fmt.Sprintf("P6: %q is not an entry: the peer", entry))
	}
}

func TestProxy_EntryForms_512ByteBoundary(t *testing.T) {
	entry := "1.2.3.4" + strings.Repeat(" ", 505)
	if len(entry) != 512 {
		t.Fatal("fixture: entry is not 512 bytes")
	}
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", entry), "1.2.3.4", "P6: an entry of exactly 512 bytes is still read")
}

func TestProxy_InvalidEntryStops(t *testing.T) {
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1, unknown, 10.0.0.2"), "10.0.0.2", "P6: the walk ends at the invalid entry, the last valid address read is the client")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "8.8.8.8, 10.0.0.2, unknown"), "10.0.0.1", "P6: an invalid first entry read leaves the peer")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1,,10.0.0.2"), "10.0.0.2", "P6: an empty entry ends the walk")
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1, 8.8.4.4, _hidden, 10.0.0.2"), "10.0.0.2", "P6: obfuscated identifiers end the walk")
}

func TestProxy_SeventeenEntries(t *testing.T) {
	allTrusted := joinEntries("10.0.1", 1, 17)
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", allTrusted), "10.0.1.2", "P7: at most 16 entries are read, the walk ends and the last read is the client")
	withClient := "8.8.8.8, " + joinEntries("10.0.1", 2, 17)
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", withClient), "10.0.1.2", "P7: a client entry beyond the 16th is never read")
	sixteen := "8.8.8.8, " + joinEntries("10.0.1", 3, 17)
	expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", sixteen), "8.8.8.8", "P7: the 16th entry is read")
}

func TestProxy_SeventeenEntries_Hops(t *testing.T) {
	p := Proxy{TrustedHops: 16}
	entries := "9.9.9.9, " + joinEntries("1.1.1", 1, 16)
	expectIP(t, ipFrom(t, p, "198.51.100.1:80", "X-Forwarded-For", entries), "1.1.1.1", "P5, P7: hops 16 reads the 16th entry from the right")
}

func TestProxy_SpoofedPrefixCannotMoveResult(t *testing.T) {
	real := "5.5.5.5"
	for _, prefix := range []string{"", "6.6.6.6", "6.6.6.6, 7.7.7.7", "10.0.0.99", "unknown", "127.0.0.1, ::1", "10.0.0.2, 10.0.0.3"} {
		header := real
		if prefix != "" {
			header = prefix + ", " + real
		}
		expectIP(t, ipFrom(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", header), real, "P4: entries a client wrote left of the real chain never move the result: "+prefix)
	}
	for _, prefix := range []string{"", "6.6.6.6", "6.6.6.6, 7.7.7.7"} {
		header := "1.1.1.1, 2.2.2.2"
		if prefix != "" {
			header = prefix + ", " + header
		}
		expectIP(t, ipFrom(t, Proxy{TrustedHops: 2}, "198.51.100.1:80", "X-Forwarded-For", header), "1.1.1.1", "P5: a spoofed prefix cannot move the hop: "+prefix)
	}
}

func TestProxy_Scheme_LastValue(t *testing.T) {
	cases := []struct {
		p       Proxy
		remote  string
		headers []string
		want    string
		desc    string
	}{
		{trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1, 10.0.0.2", "X-Forwarded-Proto", "https, http"}, "http", "the last value, not the one at the client's position"},
		{trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1, 10.0.0.2", "X-Forwarded-Proto", "http, https"}, "https", "the last value wins"},
		{Proxy{TrustedHops: 2}, "198.51.100.1:80", []string{"X-Forwarded-For", "1.1.1.1, 2.2.2.2", "X-Forwarded-Proto", "https, http"}, "http", "hops mode reads the last value too"},
		{Proxy{TrustedHops: 1}, "198.51.100.1:80", []string{"X-Forwarded-For", "1.1.1.1, 2.2.2.2", "X-Forwarded-Proto", "https, http"}, "http", "hops 1 reads the last value"},
		{trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "https", "X-Forwarded-Proto", "http"}, "http", "the last header line"},
		{trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "http", "X-Forwarded-Proto", "https"}, "https", "the last header line, https"},
		{trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "http, http", "X-Forwarded-Proto", "https , \thttps"}, "https", "spaces and tabs around the last value are trimmed"},
	}
	for _, c := range cases {
		r := resolve(t, c.p, c.remote, c.headers...)
		if got := Scheme(r); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "P8: "+c.desc))
		}
	}
}

func TestProxy_Scheme_SingleValue(t *testing.T) {
	for _, forwardedFor := range []string{"1.1.1.1, 10.0.0.2", "1.1.1.1", "9.9.9.9, 1.1.1.1", "9.9.9.9, 8.8.8.8, 1.1.1.1, 10.0.0.2"} {
		r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", forwardedFor, "X-Forwarded-Proto", "https")
		if got := Scheme(r); got != "https" {
			t.Error(test.DiffMessage(got, "https", "P8: one value written by the proxy, whatever the number of entries: "+forwardedFor))
		}
	}
}

func TestProxy_Scheme_CaseInsensitive(t *testing.T) {
	r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "HTTPS")
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "P8: any case is accepted and the result is lower case"))
	}
}

func TestProxy_Scheme_Invalid(t *testing.T) {
	for _, value := range []string{"ftp", "javascript", "", "https:", "https, ftp", "https, ", "https,", "http, ftp", "wss", "h ttps", "https\r\nX: y"} {
		r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", value)
		if got := Scheme(r); got != "http" {
			t.Error(test.DiffMessage(got, "http", fmt.Sprintf("P8: %q is not http or https, the connection's scheme is kept", value)))
		}
	}
	r := scoped("10.0.0.1:80", "X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "ftp")
	r.TLS = &tls.ConnectionState{}
	run(t, trustedPrivate, r)
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "P8: an invalid value keeps P2's scheme, https on a TLS connection"))
	}
}

func TestProxy_Host_Validated(t *testing.T) {
	trustedPrivate := Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}
	valid := []string{"example.com", "example.com:8443", "[::1]:80", "[2001:db8::1]", "a-b.example.com.", "127.0.0.1:80", "Example.COM", strings.Repeat("a", 255)}
	for _, host := range valid {
		r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1", "X-Forwarded-Host", host)
		if got := Host(r); got != host {
			t.Error(test.DiffMessage(got, host, "P9: a valid host is accepted"))
		}
	}
	invalid := []string{
		"evil.com/path", "a b", "user@evil.com", "", strings.Repeat("a", 256), "evil.com\r\nX-Injected: 1", "evil.com\nX: y",
		"ex_ample.com", "[::1", "evil.com:port", "evil.com?x=1", "evil.com#frag", "evil.com\\x", "evil.com:80:80", "http://evil.com", "évil.com",
	}
	for _, host := range invalid {
		r := scoped("10.0.0.1:80", "X-Forwarded-For", "1.1.1.1")
		r.Header["X-Forwarded-Host"] = []string{host}
		run(t, trustedPrivate, r)
		if got := Host(r); got != "example.com" {
			t.Error(test.DiffMessage(got, "example.com", fmt.Sprintf("P9: %q fails the syntax, r.Host is kept", host)))
		}
	}
}

func TestProxy_Host_LastValue(t *testing.T) {
	p := Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}
	r := resolve(t, p, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1, 10.0.0.2", "X-Forwarded-Host", "evil.example, app.example.com")
	if got := Host(r); got != "app.example.com" {
		t.Error(test.DiffMessage(got, "app.example.com", "P9: the last value is the nearest proxy's"))
	}
	r = resolve(t, p, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1, 10.0.0.2", "X-Forwarded-Host", "app.example.com, evil.example/x")
	if got := Host(r); got != "example.com" {
		t.Error(test.DiffMessage(got, "example.com", "P9: an invalid last value does not fall back to an earlier one"))
	}
}

func TestProxy_Host_OffByDefault(t *testing.T) {
	cases := []struct {
		name    string
		p       Proxy
		remote  string
		headers []string
	}{
		{"XForwarded trusted mode", trustedPrivate, "10.0.0.1:80", []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "app.example.com"}},
		{"XForwarded hops mode", Proxy{TrustedHops: 1}, "198.51.100.1:80", []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "app.example.com"}},
		{"Forwarded", Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded}, "10.0.0.1:80", []string{"Forwarded", "for=1.1.1.1;proto=https;host=app.example.com"}},
		{"Forwarded hops mode", Proxy{TrustedHops: 1, Header: HeaderForwarded}, "198.51.100.1:80", []string{"Forwarded", "for=1.1.1.1;proto=https;host=app.example.com"}},
	}
	for _, c := range cases {
		r := resolve(t, c.p, c.remote, c.headers...)
		if got := Host(r); got != "example.com" {
			t.Error(test.DiffMessage(got, "example.com", "P9: "+c.name+": the forwarded host is not read without IsHostForwarded"))
		}
		if got := Scheme(r); got != "https" {
			t.Error(test.DiffMessage(got, "https", "P8: "+c.name+": the scheme is still read"))
		}
	}
}

func TestProxy_XForwarded_ClientValuesLeftOfProxyValueIgnored(t *testing.T) {
	headers := []string{"X-Forwarded-For", "9.9.9.9, 1.1.1.1", "X-Forwarded-Host", "evil.example, app.example.com", "X-Forwarded-Proto", "http, https"}
	r := resolve(t, Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, "10.0.0.1:80", headers...)
	if got := Host(r); got != "app.example.com" {
		t.Error(test.DiffMessage(got, "app.example.com", "M2, P9: only the last host counts"))
	}
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "M2, P8: only the last proto counts"))
	}
	r = resolve(t, trustedPrivate, "10.0.0.1:80", headers...)
	if got := Host(r); got != "example.com" {
		t.Error(test.DiffMessage(got, "example.com", "M2, P9: without the option the client's host never counts"))
	}
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "M2, P8: the last proto counts"))
	}
}

func TestProxy_Scheme_SpoofedClientProtoIsNotLastWhenProxyAppends(t *testing.T) {
	r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "9.9.9.9, 1.1.1.1", "X-Forwarded-Proto", "https, http")
	if got := Scheme(r); got != "http" {
		t.Error(test.DiffMessage(got, "http", "M2, P8: a client's https is left of the proxy's http and never counts"))
	}
}

func TestProxy_Host_ClientForwardedHostNeverChoosesHostByDefault(t *testing.T) {
	for _, headers := range [][]string{
		{"X-Forwarded-For", "9.9.9.9, 1.1.1.1", "X-Forwarded-Host", "evil.example"},
		{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Host", "evil.example"},
		{"X-Forwarded-Host", "evil.example"},
		{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Host", "a.example", "X-Forwarded-Host", "evil.example"},
	} {
		r := resolve(t, trustedPrivate, "10.0.0.1:80", headers...)
		if got := Host(r); got != "example.com" {
			t.Error(test.DiffMessage(got, "example.com", fmt.Sprintf("M2, P9: %v: r.Host is kept", headers)))
		}
	}
}

func TestProxy_NoEntry_KeepsConnection(t *testing.T) {
	cases := []struct {
		name    string
		p       Proxy
		remote  string
		headers []string
	}{
		{"no X-Forwarded-For", Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, "10.0.0.1:80", nil},
		{"unknown entry", Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, "10.0.0.1:80", []string{"X-Forwarded-For", "unknown"}},
		{"hops 2 with one entry", Proxy{TrustedHops: 2, IsHostForwarded: true}, "198.51.100.1:80", []string{"X-Forwarded-For", "1.1.1.1"}},
		{"untrusted peer", Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, "198.51.100.1:80", []string{"X-Forwarded-For", "1.1.1.1"}},
	}
	for _, c := range cases {
		headers := append([]string{"X-Forwarded-Proto", "https", "X-Forwarded-Host", "app.example.com"}, c.headers...)
		r := resolve(t, c.p, c.remote, headers...)
		if got := Scheme(r); got != "http" {
			t.Error(test.DiffMessage(got, "http", "P13: "+c.name+": the scheme is the connection's"))
		}
		if got := Host(r); got != "example.com" {
			t.Error(test.DiffMessage(got, "example.com", "P13: "+c.name+": the host is r.Host"))
		}
	}
}

func TestProxy_Forwarded_Parse(t *testing.T) {
	p := Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded, IsHostForwarded: true}
	type result struct{ ip, scheme, host string }
	cases := []struct {
		lines []string
		want  result
		desc  string
	}{
		{[]string{"for=1.1.1.1"}, result{"1.1.1.1", "http", "example.com"}, "a bare for"},
		{[]string{`for="1.1.1.1"`}, result{"1.1.1.1", "http", "example.com"}, "a quoted value"},
		{[]string{`for="[2001:db8::1]:4711"`}, result{"2001:db8::1", "http", "example.com"}, "a quoted IPv6 with a port"},
		{[]string{"FOR=1.1.1.1;Proto=HTTPS;HOST=app.example.com"}, result{"1.1.1.1", "https", "app.example.com"}, "names are case-insensitive"},
		{[]string{"by=10.0.0.1;for=1.1.1.1;ext=x;proto=https"}, result{"1.1.1.1", "https", "example.com"}, "unknown names are ignored"},
		{[]string{"for=1.1.1.1;proto=https;host=app.example.com, for=10.0.0.2;proto=http"}, result{"1.1.1.1", "https", "app.example.com"}, "the client's element decides"},
		{[]string{"for=1.1.1.1, for=10.0.0.2;proto=https;host=front.example.com"}, result{"1.1.1.1", "https", "front.example.com"}, "the nearest element to the right that has the value"},
		{[]string{"for=1.1.1.1", "for=10.0.0.2"}, result{"1.1.1.1", "http", "example.com"}, "repeated lines, last first"},
		{[]string{"for=10.0.0.5, for=10.0.0.6"}, result{"10.0.0.5", "http", "example.com"}, "all trusted, the leftmost"},
		{[]string{"proto=https, for=10.0.0.2"}, result{"10.0.0.2", "http", "example.com"}, "an element without for is an invalid entry and ends the walk"},
		{[]string{"for=1.1.1.1, for=_hidden, for=10.0.0.2"}, result{"10.0.0.2", "http", "example.com"}, "an obfuscated identifier ends the walk"},
		{[]string{"for=unknown"}, result{"10.0.0.1", "http", "example.com"}, "unknown is not an address"},
		{[]string{"for=1.1.1.1;proto=ftp"}, result{"1.1.1.1", "http", "example.com"}, "an invalid proto keeps the connection's"},
		{[]string{"for=1.1.1.1;host=evil.com/x"}, result{"1.1.1.1", "http", "example.com"}, "an invalid host keeps r.Host"},
		{[]string{"for=1.1.1.1;;proto=https"}, result{"1.1.1.1", "https", "example.com"}, "an empty pair is skipped"},
		{[]string{`for="1.1.1.1`}, result{"10.0.0.1", "http", "example.com"}, "an unterminated quote does not parse"},
		{[]string{"for=1.1.1.1;proto"}, result{"10.0.0.1", "http", "example.com"}, "a pair without a value does not parse"},
	}
	for _, c := range cases {
		headers := make([]string, 0, 2*len(c.lines))
		for _, line := range c.lines {
			headers = append(headers, "Forwarded", line)
		}
		r := resolve(t, p, "10.0.0.1:80", headers...)
		got := result{ClientIP(r).String(), Scheme(r), Host(r)}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "P10: "+c.desc))
		}
	}
}

func TestProxy_Forwarded_Hops(t *testing.T) {
	p := Proxy{TrustedHops: 2, Header: HeaderForwarded}
	r := resolve(t, p, "198.51.100.1:80", "Forwarded", "for=9.9.9.9, for=1.1.1.1;proto=https, for=2.2.2.2")
	expectIP(t, ClientIP(r), "1.1.1.1", "P5, P10: hops count Forwarded elements")
	if got := Scheme(r); got != "https" {
		t.Error(test.DiffMessage(got, "https", "P8: Forwarded proto of the client's element"))
	}
}

func TestProxy_OtherFamilyIgnored(t *testing.T) {
	xff := []string{"X-Forwarded-For", "1.1.1.1", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "evil.example"}
	std := []string{"Forwarded", "for=2.2.2.2;proto=https;host=evil.example"}

	r := resolve(t, Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded, IsHostForwarded: true}, "10.0.0.1:80", xff...)
	expectIP(t, ClientIP(r), "10.0.0.1", "P11: with Header Forwarded, X-Forwarded-* changes nothing")
	if Scheme(r) != "http" || Host(r) != "example.com" {
		t.Error(test.DiffMessage([]string{Scheme(r), Host(r)}, []string{"http", "example.com"}, "P11: scheme and host ignore X-Forwarded-*"))
	}

	r = resolve(t, Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, "10.0.0.1:80", std...)
	expectIP(t, ClientIP(r), "10.0.0.1", "P11: with Header XForwarded, Forwarded changes nothing")
	if Scheme(r) != "http" || Host(r) != "example.com" {
		t.Error(test.DiffMessage([]string{Scheme(r), Host(r)}, []string{"http", "example.com"}, "P11: scheme and host ignore Forwarded"))
	}

	r = resolve(t, Proxy{TrustedHops: 1, IsHostForwarded: true}, "198.51.100.1:80", std...)
	expectIP(t, ClientIP(r), "198.51.100.1", "P11: hops mode reads one family only")
}

func TestProxy_NeverRefuses_NeverMutatesRequest(t *testing.T) {
	hostile := [][]string{
		{"X-Forwarded-For", strings.Repeat("1.1.1.1, ", 5000)},
		{"X-Forwarded-For", "\x00\xff\xfe", "X-Forwarded-Proto", strings.Repeat("h", 100000)},
		{"X-Forwarded-Host", "evil\r\nSet-Cookie: x=1", "Forwarded", "for=;;;;===,,,\""},
		{"Forwarded", strings.Repeat("for=1.1.1.1,", 3000)},
	}
	for _, header := range []Header{HeaderXForwarded, HeaderForwarded} {
		for _, headers := range hostile {
			r := scoped("10.0.0.1:80", headers...)
			before := r.Header.Clone()
			remote, host, requestURI, path, ctxBefore := r.RemoteAddr, r.Host, r.RequestURI, r.URL.Path, r.Context()
			w := run(t, Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: header}, r)
			if len(w.Header()) != 0 || w.Body.Len() != 0 || w.Code != http.StatusOK {
				t.Error(test.DiffMessage(w.Header(), http.Header{}, "P12: proxy writes no response header and no body"))
			}
			if r.RemoteAddr != remote || r.Host != host || r.RequestURI != requestURI || r.URL.Path != path {
				t.Error(test.DiffMessage([]string{r.RemoteAddr, r.Host, r.URL.Path}, []string{remote, host, path}, "P12: RemoteAddr, Host and URL are untouched"))
			}
			if !reflect.DeepEqual(r.Header, before) {
				t.Error(test.DiffMessage(r.Header, before, "P12: r.Header is untouched"))
			}
			if r.Context() != ctxBefore {
				t.Error(test.DiffMessage(r.Context(), ctxBefore, "G2: the request's context is the one proxy received"))
			}
		}
	}
}

func TestProxy_DoesNotReadBody(t *testing.T) {
	r := scoped("10.0.0.1:80", "X-Forwarded-For", "1.1.1.1")
	r.Body = failBodyCloser{failBody{t}}
	run(t, trustedPrivate, r)
}

func TestProxy_ResultIsParsedAddress_NeverHeaderText(t *testing.T) {
	r := resolve(t, trustedPrivate, "10.0.0.1:80", "X-Forwarded-For", "1.1.1.1\r\nX-Injected: 1")
	if got := ClientIP(r).String(); strings.ContainsAny(got, "\r\n ") {
		t.Error(test.DiffMessage(got, "<an address>", "P6: the result is a parsed address and cannot inject into logs"))
	}
}

func TestProxy_InApp_HandlerReadsClient(t *testing.T) {
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true})
	app.Create(owlky.ModuleBuilder().Controllers(whoController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/who", nil)
	r.RemoteAddr = "10.0.0.7:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.20, 10.0.0.2")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if got := w.Body.String(); got != "198.51.100.20|https|app.example.com" {
		t.Error(test.DiffMessage(got, "198.51.100.20|https|app.example.com", "P4, P8, P9: a handler reads the resolved client through the app"))
	}

	r = httptest.NewRequest(http.MethodGet, "/who", nil)
	r.RemoteAddr = "203.0.113.5:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.20")
	w = httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if got := w.Body.String(); got != "203.0.113.5|http|example.com" {
		t.Error(test.DiffMessage(got, "203.0.113.5|http|example.com", "P3: an untrusted peer resolves to the connection through the app"))
	}
}

func TestProxy_InApp_ResolutionDoesNotLeakToNextRequest(t *testing.T) {
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(Proxy{TrustedProxies: []string{"10.0.0.0/8"}})
	app.Create(owlky.ModuleBuilder().Controllers(whoController{}).Build())

	first := httptest.NewRequest(http.MethodGet, "/who", nil)
	first.RemoteAddr = "10.0.0.7:5000"
	first.Header.Set("X-Forwarded-For", "198.51.100.20")
	first.Header.Set("X-Forwarded-Proto", "https")
	app.ServeHTTP(httptest.NewRecorder(), first)

	second := httptest.NewRequest(http.MethodGet, "/who", nil)
	second.RemoteAddr = "10.0.0.7:5000"
	w := httptest.NewRecorder()
	app.ServeHTTP(w, second)
	if got := w.Body.String(); got != "10.0.0.7|http|example.com" {
		t.Error(test.DiffMessage(got, "10.0.0.7|http|example.com", "P12: a request without headers resolves to the peer, nothing is left from the previous one"))
	}
}

func TestProxy_Snapshot(t *testing.T) {
	mw := Proxy{TrustedProxies: []string{"10.0.0.0/8", "203.0.113.7"}, Header: HeaderForwarded}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled proxy implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "proxy" {
		t.Error(test.DiffMessage(snap.Name, "proxy", "I4: the snapshot name"))
	}
	for key, want := range map[string]any{"mode": "proxies", "header": "forwarded", "hops": int64(0)} {
		if got := field(snap, key); got != want {
			t.Error(test.DiffMessage(got, want, "I4: effective configuration "+key))
		}
	}
	if got, _ := field(snap, "trusted").([]string); !reflect.DeepEqual(got, []string{"10.0.0.0/8", "203.0.113.7/32"}) {
		t.Error(test.DiffMessage(got, []string{"10.0.0.0/8", "203.0.113.7/32"}, "I4: trusted prefixes"))
	}
	if got := field(snap, "resolved"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "I4: nothing resolved yet"))
	}
	if got := field(snap, "hostForwarded"); got != false {
		t.Error(test.DiffMessage(got, false, "I4, P9: hostForwarded is false by default"))
	}
	on := Proxy{TrustedHops: 1, IsHostForwarded: true}.NewMiddleware().(inspect.Snapshotter).Snapshot()
	if got := field(on, "hostForwarded"); got != true {
		t.Error(test.DiffMessage(got, true, "I4, P9: hostForwarded is true with the option"))
	}

	counted := Proxy{TrustedProxies: []string{"10.0.0.0/8"}}.NewMiddleware()
	cs := counted.(inspect.Snapshotter)
	call := func(remote string, headers ...string) {
		r := scoped(remote, headers...)
		counted.Use(r, httptest.NewRecorder(), noop)
	}
	call("10.0.0.1:80", "X-Forwarded-For", "1.1.1.1")
	call("10.0.0.1:80", "X-Forwarded-For", "2.2.2.2")
	call("198.51.100.1:80", "X-Forwarded-For", "3.3.3.3")
	after := cs.Snapshot()
	if got := field(after, "resolved"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: requests resolved from headers"))
	}
	if got := field(after, "untrusted"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "I4: requests from untrusted peers"))
	}
}

func TestProxy_Snapshot_Hops(t *testing.T) {
	mw := Proxy{TrustedHops: 2}.NewMiddleware()
	snap := mw.(inspect.Snapshotter).Snapshot()
	if field(snap, "mode") != "hops" || field(snap, "hops") != int64(2) || field(snap, "header") != "x-forwarded" {
		t.Error(test.DiffMessage(snap.Fields, "mode hops, hops 2, header x-forwarded", "I4: hops mode configuration"))
	}
}

func TestProxy_Snapshot_ConcurrentWithTraffic(t *testing.T) {
	mw := trustedPrivate.NewMiddleware()
	s := mw.(inspect.Snapshotter)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = s.Snapshot()
		}
	}()
	for i := 0; i < 200; i++ {
		mw.Use(scoped("10.0.0.1:80", "X-Forwarded-For", "1.1.1.1"), httptest.NewRecorder(), noop)
	}
	<-done
}

func TestProxy_Allocations(t *testing.T) {
	untrusted := trustedPrivate.NewMiddleware()
	r := scoped("198.51.100.9:1234", "X-Forwarded-For", "1.1.1.1")
	w := &discardWriter{header: http.Header{}}
	if got := testing.AllocsPerRun(200, func() { untrusted.Use(r, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P2: an untrusted peer costs no allocation"))
	}

	trusted := Proxy{TrustedProxies: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fd00::/8"}}.NewMiddleware()
	r = scoped("10.0.0.1:80", "X-Forwarded-For", "8.8.8.8, 10.0.0.3, 10.0.0.2", "X-Forwarded-Proto", "https", "X-Forwarded-Host", "app.example.com")
	if got := testing.AllocsPerRun(200, func() { trusted.Use(r, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P4: a trusted peer with three entries costs no allocation"))
	}

	hops := Proxy{TrustedHops: 2}.NewMiddleware()
	r = scoped("198.51.100.1:80", "X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := testing.AllocsPerRun(200, func() { hops.Use(r, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P5: hops mode costs no allocation"))
	}

	forwarded := Proxy{TrustedProxies: []string{"10.0.0.0/8"}, Header: HeaderForwarded}.NewMiddleware()
	r = scoped("10.0.0.1:80", "Forwarded", "for=8.8.8.8;proto=https, for=10.0.0.2")
	if got := testing.AllocsPerRun(200, func() { forwarded.Use(r, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P10: the Forwarded family costs no allocation"))
	}

	if got := testing.AllocsPerRun(200, func() { _ = ClientIP(r); _ = Scheme(r); _ = Host(r) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "ClientIP, Scheme and Host cost no allocation"))
	}
	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	bare.RemoteAddr = "203.0.113.9:1"
	if got := testing.AllocsPerRun(200, func() { _ = ClientIP(bare) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P2: an unresolved request parses RemoteAddr without allocating"))
	}
}
