package httpsredirect_test

import (
	"crypto/tls"
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/httpsredirect"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/middlewares/proxy"
	"go.owlkei.com/owlkei/internal/test"
)

func TestHTTPSRedirect_HTTPS_Passes(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	r := request(http.MethodGet, "/ok")
	r.TLS = &tls.ConnectionState{}
	w := serve(app, r)
	if w.Code != http.StatusOK || w.Body.String() != "ok" || w.Header().Get("Location") != "" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String(), w.Header().Get("Location")}, []any{200, "ok", ""}, "R1: https calls next and nothing else"))
	}
}

func TestHTTPSRedirect_Redirects308_KeepsPathAndQuery(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	for _, target := range []string{"/", "/ok", "/a/b?x=1&y=%20z", "/a%20b/c?q=%E2%9C%93", "/ok?", "/deep/er/path/?a=b&a=c", "/a%2Fb"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
			r := plainRequest(method, "example.com", target)
			want := "https://example.com" + r.URL.RequestURI()
			w := serve(app, r)
			expectRedirect(t, w, want, method+" "+target)
		}
	}
}

func TestHTTPSRedirect_NextNotCalled(t *testing.T) {
	app := newApp(t, HTTPSRedirect{}, probeMiddleware{Name: "after-redirect"})
	probes.Delete("after-redirect")
	serve(app, plainRequest(http.MethodGet, "example.com", "/ok"))
	if _, called := probes.Load("after-redirect"); called {
		t.Error(test.DiffMessage(true, false, "R2: next() is not called after a redirect"))
	}
}

func TestHTTPSRedirect_FixedHost(t *testing.T) {
	app := newApp(t, HTTPSRedirect{Host: "www.example.com"})
	for _, host := range []string{"example.com", "evil.example", "example.com:8080", "evil.com/x", "a b"} {
		w := serve(app, plainRequest(http.MethodGet, host, "/ok?x=1"))
		expectRedirect(t, w, "https://www.example.com/ok?x=1", "a fixed Host replaces request host "+host)
	}
}

func TestHTTPSRedirect_Port(t *testing.T) {
	cases := []struct {
		port int
		want string
	}{
		{0, "https://example.com/x"},
		{443, "https://example.com/x"},
		{8443, "https://example.com:8443/x"},
		{65535, "https://example.com:65535/x"},
		{1, "https://example.com:1/x"},
	}
	for _, c := range cases {
		app := newApp(t, HTTPSRedirect{Port: c.port})
		expectRedirect(t, serve(app, plainRequest(http.MethodGet, "example.com", "/x")), c.want, fmt.Sprintf("Port %d", c.port))
	}
	app := newApp(t, HTTPSRedirect{Host: "www.example.com", Port: 8443})
	expectRedirect(t, serve(app, plainRequest(http.MethodGet, "example.com", "/x")), "https://www.example.com:8443/x", "Host and Port together")
}

func TestHTTPSRedirect_StripsRequestPort(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	expectRedirect(t, serve(app, plainRequest(http.MethodGet, "example.com:8080", "/x")), "https://example.com/x", "the request's port is dropped")
	expectRedirect(t, serve(app, plainRequest(http.MethodGet, "[::1]:8080", "/x")), "https://[::1]/x", "an IPv6 host keeps its brackets and loses its port")
	expectRedirect(t, serve(app, plainRequest(http.MethodGet, "127.0.0.1:3000", "/x")), "https://127.0.0.1/x", "an IPv4 host loses its port")
	withPort := newApp(t, HTTPSRedirect{Port: 8443})
	expectRedirect(t, serve(withPort, plainRequest(http.MethodGet, "[::1]:8080", "/x")), "https://[::1]:8443/x", "the configured port replaces the request's")
	expectRedirect(t, serve(withPort, plainRequest(http.MethodGet, "example.com:8080", "/x")), "https://example.com:8443/x", "the configured port replaces the request's (name)")
}

func TestHTTPSRedirect_NoOpenRedirect(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	targets := []string{
		"//evil.example/x", "///evil.example", "/\\evil.example", "/\\/evil.example", "/@evil.example", "/%2F%2Fevil.example",
		"/%0d%0aSet-Cookie:%20x=1", "/%0aLocation:%20http://evil.example", "/.evil.example", "//evil.example@example.com/", "/%09/evil.example", "/ok?next=//evil.example",
	}
	for _, target := range targets {
		r := plainRequest(http.MethodGet, "example.com", target)
		w := serve(app, r)
		location := w.Header().Get("Location")
		if w.Code != http.StatusPermanentRedirect {
			t.Error(test.DiffMessage(w.Code, http.StatusPermanentRedirect, "R3: "+target))
			continue
		}
		if !strings.HasPrefix(location, "https://example.com/") && location != "https://example.com" {
			t.Error(test.DiffMessage(location, "https://example.com/...", "R3: the location starts with https:// and the validated host: "+target))
		}
		if strings.ContainsAny(location, "\r\n \t") {
			t.Error(test.DiffMessage(location, "<no control character>", "R3: no control character in Location: "+target))
		}
		parsed, err := url.Parse(location)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "example.com" || parsed.User != nil {
			t.Error(test.DiffMessage(location, "a URL whose host is example.com", "R3: the path cannot change the target: "+target))
		}
		if len(w.Header().Values("Location")) != 1 || w.Header().Get("Set-Cookie") != "" {
			t.Error(test.DiffMessage(w.Header(), "one Location, no injected header", "R3: no header injection: "+target))
		}
	}
}

func TestHTTPSRedirect_InvalidHost_400(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	for _, host := range []string{"", "evil.com/x", "a b", "user@evil.com", "evil.com\r\nX-Injected: 1", strings.Repeat("a", 256), "evil.com?x", "evil.com#x", "[::1", "evil.com\\x", "ex_ample.com"} {
		w := serve(app, plainRequest(http.MethodGet, host, "/ok"))
		expectRefusal(t, w, http.StatusBadRequest, invalidHostBody, fmt.Sprintf("host %q", host))
		if w.Header().Get("Location") != "" {
			t.Error(test.DiffMessage(w.Header().Get("Location"), "", "R3: no location is built from an unvalidated host"))
		}
	}
}

func TestHTTPSRedirect_FixedHostSkipsRequestHostSyntax(t *testing.T) {
	app := newApp(t, HTTPSRedirect{Host: "www.example.com"})
	w := serve(app, plainRequest(http.MethodGet, "evil.com/x", "/ok"))
	expectRedirect(t, w, "https://www.example.com/ok", "R3: the request host is not used when Host is set")
}

func TestHTTPSRedirect_SchemeFromProxy(t *testing.T) {
	app := newApp(t, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}, HTTPSRedirect{})

	r := plainRequest(http.MethodGet, "internal.local:8080", "/ok")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Proto", "https")
	if w := serve(app, r); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R1: https forwarded by a trusted proxy passes"))
	}

	r = plainRequest(http.MethodGet, "internal.local:8080", "/ok")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	expectRedirect(t, serve(app, r), "https://internal.local/ok", "P9: without IsHostForwarded the redirect goes to the request's host")

	hostApp := newApp(t, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}, IsHostForwarded: true}, HTTPSRedirect{})
	r = plainRequest(http.MethodGet, "internal.local:8080", "/ok")
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	expectRedirect(t, serve(hostApp, r), "https://app.example.com/ok", "P9: with IsHostForwarded the redirect goes to the forwarded host")

	r = plainRequest(http.MethodGet, "example.com", "/ok")
	r.RemoteAddr = "203.0.113.50:5000"
	r.Header.Set("X-Forwarded-Proto", "https")
	expectRedirect(t, serve(app, r), "https://example.com/ok", "an untrusted peer cannot claim https")
}

func TestHTTPSRedirect_UnboundProxyIgnoresForwardedProto(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	r := plainRequest(http.MethodGet, "example.com", "/ok")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("Forwarded", "proto=https")
	expectRedirect(t, serve(app, r), "https://example.com/ok", "P2: with no proxy bound no forwarding header is read")
}

func TestNewHTTPSRedirect_Panics(t *testing.T) {
	cases := []HTTPSRedirect{
		{Host: "evil.com/x"}, {Host: "a b"}, {Host: "example.com:8080"}, {Host: "[::1]:8443"}, {Host: "*.example.com"},
		{Host: "user@example.com"}, {Host: strings.Repeat("a", 256)}, {Host: "https://example.com"},
		{Port: -1}, {Port: 65536}, {Port: 70000},
	}
	for _, c := range cases {
		text, ok := panicText(func() { c.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid httpsredirect:") {
			t.Error(test.DiffMessage(text, "invalid httpsredirect: ...", fmt.Sprintf("R4, G4: %+v", c)))
		}
	}
}

func TestNewHTTPSRedirect_ValidOptions(t *testing.T) {
	for _, c := range []HTTPSRedirect{{}, {Host: "example.com"}, {Host: "[::1]"}, {Host: "127.0.0.1"}, {Host: "www.example.com", Port: 8443}, {Port: 443}, {Port: 65535}} {
		if text, ok := panicText(func() { c.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", fmt.Sprintf("R4: valid %+v", c)))
		}
	}
}

func TestHTTPSRedirect_RefusalIsHTTPLayerResponse(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	w := serve(app, plainRequest(http.MethodGet, "evil.com/x", "/ok"))
	expectRefusal(t, w, http.StatusBadRequest, invalidHostBody, "an invalid host")
	if strings.Contains(w.Body.String(), "evil") {
		t.Error(test.DiffMessage(w.Body.String(), "<no host>", "G1: nothing from the request is in the body"))
	}
}

func TestHTTPSRedirect_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, HTTPSRedirect{}, probeMiddleware{Name: "after"})
	r := request(http.MethodGet, "/ok")
	r.TLS = &tls.ConnectionState{}
	if w := serve(app, r); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "fixture: the https request passes"))
	}
	before, after := probeOf("before"), probeOf("after")
	if before.request == nil || before.request != after.request {
		t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request"))
	}
	if before.context != after.context {
		t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context()"))
	}
}

func TestHTTPSRedirect_DoesNotReadBody(t *testing.T) {
	app := newApp(t, HTTPSRedirect{})
	r := plainRequest(http.MethodPost, "example.com", "/ok")
	r.Body = failBody{t}
	serve(app, r)
	r = plainRequest(http.MethodPost, "example.com", "/ok")
	r.Body = failBody{t}
	r.TLS = &tls.ConnectionState{}
	serve(app, r)
}

func TestHTTPSRedirect_Snapshot(t *testing.T) {
	mw := HTTPSRedirect{Host: "www.example.com", Port: 8443}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled httpsredirect implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "httpsredirect" || field(snap, "host") != "www.example.com" || field(snap, "port") != int64(8443) || field(snap, "redirected") != int64(0) {
		t.Error(test.DiffMessage(snap, "name httpsredirect, host www.example.com, port 8443, redirected 0", "I4: effective configuration"))
	}
	w := &discardWriter{header: http.Header{}}
	mw.Use(plainRequest(http.MethodGet, "example.com", "/a"), w, noop)
	mw.Use(plainRequest(http.MethodGet, "example.com", "/b"), w, noop)
	secure := request(http.MethodGet, "/c")
	secure.TLS = &tls.ConnectionState{}
	mw.Use(secure, w, noop)
	if got := field(s.Snapshot(), "redirected"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: redirects are counted, passes are not"))
	}
}

func TestHTTPSRedirect_Allocations(t *testing.T) {
	mw := HTTPSRedirect{}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	secure := request(http.MethodGet, "/ok")
	secure.TLS = &tls.ConnectionState{}
	if got := testing.AllocsPerRun(200, func() { mw.Use(secure, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "R1: the https path costs no allocation"))
	}
	plain := plainRequest(http.MethodGet, "example.com:8080", "/a/b?x=1")
	if got := testing.AllocsPerRun(200, func() {
		clear(w.header)
		mw.Use(plain, w, noop)
	}); got > 3 {
		t.Error(test.DiffMessage(got, "<= 3", "R2: a redirect builds one string"))
	}
}

func TestHTTPSRedirect_ForwardedHostIgnoredByDefault(t *testing.T) {
	app := newApp(t, proxy.Proxy{TrustedProxies: []string{"192.0.2.0/24"}}, HTTPSRedirect{})
	r := plainRequest(http.MethodGet, "example.com", "/x")
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 1.1.1.1")
	r.Header.Set("X-Forwarded-Host", "evil.example")
	expectRedirect(t, serve(app, r), "https://example.com/x", "M2: a forwarded host the proxy did not write is ignored")
}

func TestHTTPSRedirect_ClientProtoLeftOfProxyValueStillRedirects(t *testing.T) {
	app := newApp(t, proxy.Proxy{TrustedProxies: []string{"192.0.2.0/24"}}, HTTPSRedirect{})
	r := plainRequest(http.MethodGet, "example.com", "/x")
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 1.1.1.1")
	r.Header.Set("X-Forwarded-Proto", "https, http")
	expectRedirect(t, serve(app, r), "https://example.com/x", "M2: a client's https left of the proxy's http does not skip the redirect")
}
