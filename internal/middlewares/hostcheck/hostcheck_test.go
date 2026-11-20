package hostcheck_test

import (
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/hostcheck"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/middlewares/proxy"
	"go.owlkei.com/owlkei/internal/test"
	"golang.org/x/net/websocket"
)

func TestNewHostCheck_Panics_Empty(t *testing.T) {
	for _, hosts := range [][]string{nil, {}} {
		text, ok := panicText(func() { HostCheck{Hosts: hosts}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid hostcheck: Hosts is empty") {
			t.Error(test.DiffMessage(text, "invalid hostcheck: Hosts is empty", "H1, G4: no hosts"))
		}
	}
}

func TestNewHostCheck_Panics_TooManyHosts(t *testing.T) {
	hosts := make([]string, 257)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("h%d.example.com", i)
	}
	text, ok := panicText(func() { HostCheck{Hosts: hosts}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid hostcheck:") {
		t.Error(test.DiffMessage(text, "invalid hostcheck: ...", "H1, G4: more than 256 entries"))
	}
}

func TestNewHostCheck_Panics_InvalidEntry(t *testing.T) {
	for _, entry := range []string{
		"", "exa mple.com", "http://example.com", "example.com/path", "*", "*example.com", "*.", "a@b.com",
		"example.com:abc", "[::1", "*.*.example.com", "example.com:", "evil.com\r\nx", "-example.com", "example..com",
	} {
		text, ok := panicText(func() { HostCheck{Hosts: []string{"ok.example.com", entry}}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid hostcheck:") {
			t.Error(test.DiffMessage(text, "invalid hostcheck: ...", fmt.Sprintf("H1, G4: %q is not a host name, *.-pattern or IP literal", entry)))
		}
	}
}

func TestNewHostCheck_ValidEntries(t *testing.T) {
	hosts := []string{"example.com", "api.example.com:8443", "*.example.com", "127.0.0.1", "[::1]", "[::1]:8080", "localhost", "a-b.example.com", "*.example.com:8443"}
	if text, ok := panicText(func() { HostCheck{Hosts: hosts}.NewMiddleware() }); ok {
		t.Error(test.DiffMessage(text, "<no panic>", "H1: the documented forms are valid"))
	}
	many := make([]string, 256)
	for i := range many {
		many[i] = fmt.Sprintf("h%d.example.com", i)
	}
	if text, ok := panicText(func() { HostCheck{Hosts: many}.NewMiddleware() }); ok {
		t.Error(test.DiffMessage(text, "<no panic>", "H1: 256 entries are allowed"))
	}
}

func TestHostCheck_Match(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com", "*.wild.example.org", "127.0.0.1", "[::1]", "Mixed.Example.NET"}})
	pass := []string{
		"example.com", "EXAMPLE.com", "Example.Com", "example.com.", "example.com:8080", "example.com:443",
		"a.wild.example.org", "a.b.wild.example.org", "a-b.wild.example.org", "A.WILD.example.org", "a.wild.example.org:9000", "a.wild.example.org.",
		"127.0.0.1", "127.0.0.1:3000", "[::1]", "[::1]:8080", "mixed.example.net", "MIXED.EXAMPLE.NET:81",
	}
	for _, host := range pass {
		if w := serve(app, hostRequest(host)); w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "H2: "+host+" matches"))
		}
	}
	refuse := []string{
		"evil.com", "example.com.evil.com", "notexample.com", "evilexample.com", "xexample.com", "example.org",
		"wild.example.org", "a_b.wild.example.org", "a b.wild.example.org", ".wild.example.org", "a.wild.example.org.evil.com",
		"evil.com/.wild.example.org", "a@wild.example.org", "a.wild.example.orgx", "128.0.0.1", "127.0.0.2", "[::2]",
		"example.com@evil.com", "mixed.example.net.evil.com",
	}
	for _, host := range refuse {
		if w := serve(app, hostRequest(host)); w.Code != http.StatusMisdirectedRequest && w.Code != http.StatusBadRequest {
			t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H2, H3: "+host+" does not match"))
		}
	}
}

func TestHostCheck_Match_PortRules(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"api.example.com:8443", "any.example.com"}})
	for host, want := range map[string]int{
		"api.example.com:8443": http.StatusOK,
		"API.example.com:8443": http.StatusOK,
		"api.example.com":      http.StatusMisdirectedRequest,
		"api.example.com:80":   http.StatusMisdirectedRequest,
		"api.example.com:8444": http.StatusMisdirectedRequest,
		"any.example.com":      http.StatusOK,
		"any.example.com:1":    http.StatusOK,
		"any.example.com:9999": http.StatusOK,
	} {
		if w := serve(app, hostRequest(host)); w.Code != want {
			t.Error(test.DiffMessage(w.Code, want, "H2: an entry with a port matches that port only, one without matches any: "+host))
		}
	}
}

func TestHostCheck_Match_WildcardWithPort(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"*.example.com:8443"}})
	if w := serve(app, hostRequest("a.example.com:8443")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "H2: a wildcard entry with a port matches that port"))
	}
	if w := serve(app, hostRequest("a.example.com:80")); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H2: another port does not match"))
	}
}

func TestHostCheck_Match_WildcardDoesNotMatchApex(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"*.example.com"}})
	for _, host := range []string{"example.com", "example.com:80", "EXAMPLE.COM"} {
		if w := serve(app, hostRequest(host)); w.Code != http.StatusMisdirectedRequest {
			t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H2: *.example.com does not match the apex "+host))
		}
	}
	deep := strings.Repeat("a.", 60) + "example.com"
	if w := serve(app, hostRequest(deep)); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "H2: any subdomain depth matches"))
	}
}

func TestHostCheck_Unknown_421(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	w := serve(app, hostRequest("evil.example"))
	expectRefusal(t, w, http.StatusMisdirectedRequest, unknownHostBody, "an unknown host")
}

func TestHostCheck_RefusalIsHTTPLayerResponse(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	w := serve(app, hostRequest("evil.example:8080"))
	expectRefusal(t, w, http.StatusMisdirectedRequest, unknownHostBody, "the refusal is the HTTP layer's error response")
	if strings.Contains(w.Body.String(), "evil") {
		t.Error(test.DiffMessage(w.Body.String(), "<no host>", "G1: nothing from the request is in the body"))
	}
	for _, values := range w.Header() {
		for _, value := range values {
			if strings.Contains(value, "evil") {
				t.Error(test.DiffMessage(value, "<no host>", "G1: nothing from the request is in a header"))
			}
		}
	}
}

func TestHostCheck_Empty_400(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	w := serve(app, hostRequest(""))
	expectRefusal(t, w, http.StatusBadRequest, invalidHostBody, "an empty host")
}

func TestHostCheck_TooLong_400(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	long := strings.Repeat("a", 252) + ".com"
	w := serve(app, hostRequest(long))
	expectRefusal(t, w, http.StatusBadRequest, invalidHostBody, "a host of 256 bytes")
	boundary := strings.Repeat("a", 251) + ".com"
	w = serve(app, hostRequest(boundary))
	expectRefusal(t, w, http.StatusMisdirectedRequest, unknownHostBody, "a host of 255 bytes is long enough to be looked up")
}

func TestHostCheck_UsesForwardedHostOnlyBehindProxy(t *testing.T) {
	app := newApp(t, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, HostCheck{Hosts: []string{"app.example.com"}})
	defaultApp := newApp(t, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}, HostCheck{Hosts: []string{"app.example.com"}})

	r := hostRequest("internal.local:8080")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	if w := serve(defaultApp, r); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "P9, H2: without IsHostForwarded r.Host is checked"))
	}

	r = hostRequest("internal.local:8080")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	if w := serve(app, r); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "H2: the forwarded host from a trusted proxy is checked"))
	}

	r = hostRequest("internal.local:8080")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Host", "evil.example")
	if w := serve(app, r); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H2: a forwarded host that is not listed is refused"))
	}

	r = hostRequest("internal.local:8080")
	r.RemoteAddr = "203.0.113.50:5000"
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	if w := serve(app, r); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H2: an untrusted peer's forwarded host is ignored, r.Host is checked"))
	}
}

func TestHostCheck_IgnoresForwardedHostWithoutProxy(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"app.example.com"}})
	r := hostRequest("evil.example")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	r.Header.Set("Forwarded", "host=app.example.com")
	if w := serve(app, r); w.Code != http.StatusMisdirectedRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "P2: with no proxy bound no forwarding header is read"))
	}
}

func TestHostCheck_LocalhostNotImplicit(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	for _, host := range []string{"localhost", "localhost:8080", "127.0.0.1", "127.0.0.1:8080", "[::1]"} {
		if w := serve(app, hostRequest(host)); w.Code != http.StatusMisdirectedRequest {
			t.Error(test.DiffMessage(w.Code, http.StatusMisdirectedRequest, "H4: "+host+" passes only when listed"))
		}
	}
	listed := newApp(t, HostCheck{Hosts: []string{"localhost"}})
	if w := serve(listed, hostRequest("localhost:8080")); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "H4: localhost passes when listed"))
	}
}

func TestHostCheck_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, HostCheck{Hosts: []string{"example.com"}}, probeMiddleware{Name: "after"})
	r := hostRequest("example.com")
	if w := serve(app, r); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: the request passes"))
	}
	before, after := probeOf("before"), probeOf("after")
	if before.request == nil || before.request != after.request {
		t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request"))
	}
	if before.context != after.context {
		t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context()"))
	}
}

func TestHostCheck_DoesNotReadBody(t *testing.T) {
	app := newApp(t, HostCheck{Hosts: []string{"example.com"}})
	for _, host := range []string{"example.com", "evil.example", ""} {
		r := request(http.MethodPost, "/ok")
		r.Body = failBody{t}
		r.Host = host
		serve(app, r)
	}
}

func TestHostCheck_WebSocketHandshake(t *testing.T) {
	build := func(hosts ...string) *httptest.Server {
		app := core.New(&core.AppOptions{Logger: silentLogger{}, WS: &core.WSOptions{Path: "/ws"}, Hosts: hosts})
		app.Create(owlkei.ModuleBuilder().Build())
		return httptest.NewServer(app)
	}
	dial := func(server *httptest.Server) error {
		conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
		if err == nil {
			_ = conn.Close()
		}
		return err
	}

	allowed := build("127.0.0.1")
	defer allowed.Close()
	if err := dial(allowed); err != nil {
		t.Error(test.DiffMessage(err, nil, "G5: a listed host completes the handshake"))
	}
	refused := build("example.com")
	defer refused.Close()
	if err := dial(refused); err == nil {
		t.Error(test.DiffMessage(nil, "a refused handshake", "G5: an unknown host refuses the handshake"))
	}
}

func TestHostCheck_Snapshot(t *testing.T) {
	mw := HostCheck{Hosts: []string{"example.com", "*.example.com:8443"}}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled hostcheck implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "hostcheck" {
		t.Error(test.DiffMessage(snap.Name, "hostcheck", "I4: the snapshot name"))
	}
	if got, _ := field(snap, "hosts").([]string); strings.Join(got, ",") != "example.com,*.example.com:8443" {
		t.Error(test.DiffMessage(got, []string{"example.com", "*.example.com:8443"}, "I4: the allowed hosts as configured"))
	}
	if got := field(snap, "refused"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "I4: nothing refused yet"))
	}
	refuse := func(host string) {
		defer func() { _ = recover() }()
		r := hostRequest(host)
		mw.Use(r, httptest.NewRecorder(), noop)
	}
	refuse("evil.example")
	refuse("other.example")
	refuse("example.com")
	if got := field(s.Snapshot(), "refused"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: refusals are counted, passes are not"))
	}
}

func TestHostCheck_Allocations(t *testing.T) {
	mw := HostCheck{Hosts: []string{"example.com", "*.example.org", "api.example.net:8443"}}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	for _, host := range []string{"example.com", "EXAMPLE.com", "example.com:8080", "a.example.org", "A.Example.ORG", "api.example.net:8443"} {
		r := hostRequest(host)
		if got := testing.AllocsPerRun(200, func() { mw.Use(r, w, noop) }); got != 0 {
			t.Error(test.DiffMessage(got, 0.0, "H2: a match costs no allocation: "+host))
		}
	}
}
