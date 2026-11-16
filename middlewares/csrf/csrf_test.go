package csrf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

const refusedBody = `{"code":403,"error":"Forbidden","message":"Cross-origin request refused"}`

var unsafeMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

func csrfRequest(method string, fetchSite, origin string) *http.Request {
	r := request(method, "/ok")
	if fetchSite != "" {
		r.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func expectPass(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	if w.Code >= http.StatusBadRequest || w.Body.String() == refusedBody {
		t.Error(test.DiffMessage(w.Code, "<2xx>", desc+": the request passes"))
	}
}

func expectRefused(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	if w.Code != http.StatusForbidden || w.Body.String() != refusedBody {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{403, refusedBody}, desc+": the request is refused"))
	}
}

func TestCSRF_Table(t *testing.T) {
	app := newApp(t, CSRF{})
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		for _, site := range []string{"", "cross-site", "same-site", "same-origin", "none", "junk"} {
			for _, origin := range []string{"", "https://evil.example", "null", "https://example.com"} {
				expectPass(t, serve(app, csrfRequest(method, site, origin)), fmt.Sprintf("S1: %s site=%q origin=%q", method, site, origin))
			}
		}
	}
	for _, method := range unsafeMethods {
		for _, site := range []string{"same-origin", "none"} {
			for _, origin := range []string{"", "https://evil.example", "null", "https://example.com"} {
				expectPass(t, serve(app, csrfRequest(method, site, origin)), fmt.Sprintf("S2: %s site=%q origin=%q", method, site, origin))
			}
		}
		for _, site := range []string{"cross-site", "same-site", "junk", "Cross-Site"} {
			for _, origin := range []string{"", "https://evil.example", "null", "https://example.com", "http://example.com"} {
				expectRefused(t, serve(app, csrfRequest(method, site, origin)), fmt.Sprintf("S3: %s site=%q origin=%q", method, site, origin))
			}
		}
		expectPass(t, serve(app, csrfRequest(method, "", "")), "S4: "+method+" without Sec-Fetch-Site and Origin")
		for _, origin := range []string{"https://example.com", "http://example.com", "HTTPS://example.com"} {
			expectPass(t, serve(app, csrfRequest(method, "", origin)), "S5: "+method+" origin "+origin+" on host example.com")
		}
		for _, origin := range []string{"https://evil.example", "null", "://bad", "https://example.com.evil.example", "not a url", "https://example.org", "https://sub.example.com", "https://example.com:8443", "\x00"} {
			expectRefused(t, serve(app, csrfRequest(method, "", origin)), "S6: "+method+" origin "+origin)
		}
	}
}

func TestCSRF_S5_HostWithPort(t *testing.T) {
	app := newApp(t, CSRF{})
	r := csrfRequest(http.MethodPost, "", "http://example.com:8080")
	r.Host = "example.com:8080"
	expectPass(t, serve(app, r), "S5: the origin's host and port equal the request's")
	r = csrfRequest(http.MethodPost, "", "http://example.com:9090")
	r.Host = "example.com:8080"
	expectRefused(t, serve(app, r), "S6: another port is another origin")
}

func TestCSRF_TrustedOrigin(t *testing.T) {
	app := newApp(t, CSRF{TrustedOrigins: []string{"https://app.example.com", "https://idp.example.org:8443"}})
	for _, method := range unsafeMethods {
		expectPass(t, serve(app, csrfRequest(method, "cross-site", "https://app.example.com")), "S3: "+method+" a trusted origin, cross-site")
		expectPass(t, serve(app, csrfRequest(method, "same-site", "https://app.example.com")), "S3: "+method+" a trusted origin, same-site")
		expectPass(t, serve(app, csrfRequest(method, "", "https://app.example.com")), "S6: "+method+" a trusted origin, no Sec-Fetch-Site")
		expectPass(t, serve(app, csrfRequest(method, "cross-site", "https://idp.example.org:8443")), "S3: "+method+" a trusted origin with a port")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "https://other.example.com")), "S3: "+method+" an origin that is not trusted")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "https://app.example.com:8443")), "S3: "+method+" another port of a trusted host")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "http://app.example.com")), "S3: "+method+" another scheme of a trusted host")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "https://app.example.com.evil.example")), "S3: "+method+" a look-alike of a trusted origin")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "")), "S3: "+method+" a trusted list does not admit a request without an Origin")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "null")), "S6: "+method+" null is never trusted")
	}
}

func TestCSRF_SameSiteRefused(t *testing.T) {
	app := newApp(t, CSRF{TrustedOrigins: []string{"https://app.example.com"}})
	for _, method := range unsafeMethods {
		expectRefused(t, serve(app, csrfRequest(method, "same-site", "https://sub.example.com")), "S3: "+method+" a sibling subdomain is cross-origin")
		expectRefused(t, serve(app, csrfRequest(method, "same-site", "")), "S3: "+method+" same-site without an Origin")
	}
}

func TestCSRF_NullOrigin(t *testing.T) {
	app := newApp(t, CSRF{TrustedOrigins: []string{"https://app.example.com"}})
	for _, method := range unsafeMethods {
		expectRefused(t, serve(app, csrfRequest(method, "", "null")), "S6: "+method+" Origin null without Sec-Fetch-Site")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", "null")), "S3: "+method+" Origin null cross-site")
	}
}

func TestCSRF_ForwardedHost(t *testing.T) {
	app := newApp(t, proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}, CSRF{})

	forwarded := func(origin, host, remote string) *http.Request {
		r := csrfRequest(http.MethodPost, "", origin)
		r.Host = "internal.local:8080"
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", "198.51.100.1")
		if host != "" {
			r.Header.Set("X-Forwarded-Host", host)
		}
		return r
	}
	expectPass(t, serve(app, forwarded("https://app.example.com", "app.example.com", "10.0.0.1:80")), "S8: the origin's host equals the forwarded host")
	expectRefused(t, serve(app, forwarded("https://other.example.com", "app.example.com", "10.0.0.1:80")), "S8: another origin than the forwarded host")
	expectRefused(t, serve(app, forwarded("https://app.example.com", "app.example.com", "203.0.113.50:80")), "S8: an untrusted peer's forwarded host is ignored")
	expectRefused(t, serve(app, forwarded("https://app.example.com", "", "10.0.0.1:80")), "S8: with no forwarded host the request's host is compared")
	expectPass(t, serve(app, forwarded("http://internal.local:8080", "", "10.0.0.1:80")), "S5: with no forwarded host an origin equal to r.Host passes")

	r := forwarded("https://app.example.com", "app.example.com", "10.0.0.1:80")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	expectRefused(t, serve(app, r), "S8: with Sec-Fetch-Site present the forwarded host is not used")
}

func TestCSRF_IgnoresForwardedHostWithoutProxy(t *testing.T) {
	app := newApp(t, CSRF{})
	r := csrfRequest(http.MethodPost, "", "https://app.example.com")
	r.Host = "internal.local"
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	r.Header.Set("Forwarded", "host=app.example.com")
	expectRefused(t, serve(app, r), "P2: with no proxy bound no forwarding header is read")
}

func TestCSRF_LongOrigin(t *testing.T) {
	app := newApp(t, CSRF{TrustedOrigins: []string{"https://app.example.com"}})
	long := "https://" + strings.Repeat("a", 250) + ".example.com"
	if len(long) <= 256 {
		t.Fatal("fixture: the origin is not longer than 256 bytes")
	}
	for _, method := range unsafeMethods {
		expectRefused(t, serve(app, csrfRequest(method, "", long)), "S9: "+method+" an origin longer than 256 bytes")
		expectRefused(t, serve(app, csrfRequest(method, "cross-site", long)), "S9: "+method+" an origin longer than 256 bytes, cross-site")
	}
	expectPass(t, serve(app, csrfRequest(http.MethodGet, "", long)), "S1: a safe method ignores the Origin length")
	same := csrfRequest(http.MethodPost, "", long)
	same.Host = strings.TrimPrefix(long, "https://")
	expectRefused(t, serve(app, same), "S9: even an origin equal to the host is refused when longer than 256 bytes")
}

func TestCSRF_SetsNoCookie(t *testing.T) {
	app := newApp(t, CSRF{})
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
		for _, site := range []string{"same-origin", "cross-site", ""} {
			w := serve(app, csrfRequest(method, site, ""))
			if len(w.Header().Values("Set-Cookie")) != 0 {
				t.Error(test.DiffMessage(w.Header().Values("Set-Cookie"), []string{}, "S11: no cookie is set: "+method+" "+site))
			}
			for key := range w.Header() {
				if strings.Contains(strings.ToLower(key), "csrf") || strings.Contains(strings.ToLower(key), "xsrf") {
					t.Error(test.DiffMessage(key, "<no token header>", "S11: no token is issued"))
				}
			}
		}
	}
}

func TestCSRF_RefusalIsHTTPLayerResponse(t *testing.T) {
	app := newApp(t, CSRF{})
	r := csrfRequest(http.MethodPost, "cross-site", "https://evil.example")
	w := serve(app, r)
	expectRefusal(t, w, http.StatusForbidden, refusedBody, "a cross-site request")
	if strings.Contains(w.Body.String(), "evil") {
		t.Error(test.DiffMessage(w.Body.String(), "<no origin>", "G1: nothing from the request is in the body"))
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Error(test.DiffMessage(w.Header(), "<no cookie>", "S11: a refusal sets no cookie"))
	}
}

func TestCSRF_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, CSRF{}, probeMiddleware{Name: "after"})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if w := serve(app, csrfRequest(method, "same-origin", "")); w.Code >= 400 {
			t.Fatal(test.DiffMessage(w.Code, "<2xx>", "fixture: the request passes"))
		}
		before, after := probeOf("before"), probeOf("after")
		if before.request == nil || before.request != after.request {
			t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request: "+method))
		}
		if before.context != after.context {
			t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context(): "+method))
		}
	}
}

func TestCSRF_DoesNotReadBody(t *testing.T) {
	app := newApp(t, CSRF{})
	for _, site := range []string{"same-origin", "cross-site", ""} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			r := csrfRequest(method, site, "")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Body = failBody{t}
			serve(app, r)
		}
	}
}

func TestNewCSRF_Panics_InvalidOrigin(t *testing.T) {
	for _, origin := range []string{"example.com", "https://example.com/path", "*", "", "https://", "https://example.com?x=1", "//example.com", "https://example.com#frag"} {
		text, ok := panicText(func() { CSRF{TrustedOrigins: []string{origin}}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid csrf:") {
			t.Error(test.DiffMessage(text, "invalid csrf: ...", fmt.Sprintf("S10, G4: %q is not scheme://host[:port]", origin)))
		}
	}
}

func TestNewCSRF_Panics_TooManyOrigins(t *testing.T) {
	origins := make([]string, 65)
	for i := range origins {
		origins[i] = fmt.Sprintf("https://app%d.example.com", i)
	}
	text, ok := panicText(func() { CSRF{TrustedOrigins: origins}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid csrf:") {
		t.Error(test.DiffMessage(text, "invalid csrf: ...", "S10, G4: more than 64 origins"))
	}
	if text, ok := panicText(func() { CSRF{TrustedOrigins: origins[:64]}.NewMiddleware() }); ok {
		t.Error(test.DiffMessage(text, "<no panic>", "S10: 64 origins are allowed"))
	}
}

func TestNewCSRF_ValidOrigins(t *testing.T) {
	for _, origins := range [][]string{nil, {"https://app.example.com"}, {"https://app.example.com:8443", "http://localhost:3000", "https://[::1]:8080"}} {
		if text, ok := panicText(func() { CSRF{TrustedOrigins: origins}.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", fmt.Sprintf("S10: %v is valid", origins)))
		}
	}
}

func TestCSRF_Snapshot(t *testing.T) {
	mw := CSRF{TrustedOrigins: []string{"https://app.example.com"}}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled csrf implements Snapshotter"))
	}
	snap := s.Snapshot()
	if got, _ := field(snap, "trusted_origins").([]string); snap.Name != "csrf" || strings.Join(got, ",") != "https://app.example.com" {
		t.Error(test.DiffMessage(snap, "csrf with trusted_origins", "I4: effective configuration"))
	}
	if got := field(snap, "refused"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "I4: nothing refused yet"))
	}
	use := func(r *http.Request) {
		defer func() { _ = recover() }()
		mw.Use(r, httptest.NewRecorder(), noop)
	}
	use(csrfRequest(http.MethodPost, "cross-site", "https://evil.example"))
	use(csrfRequest(http.MethodPost, "", "null"))
	use(csrfRequest(http.MethodPost, "same-origin", ""))
	use(csrfRequest(http.MethodGet, "cross-site", ""))
	if got := field(s.Snapshot(), "refused"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: refusals are counted, passes are not"))
	}
}

func TestCSRF_Allocations(t *testing.T) {
	mw := CSRF{}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}

	safe := csrfRequest(http.MethodGet, "cross-site", "https://evil.example")
	if got := testing.AllocsPerRun(200, func() { mw.Use(safe, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "S1: a safe method costs no allocation"))
	}
	fetch := csrfRequest(http.MethodPost, "same-origin", "https://evil.example")
	if got := testing.AllocsPerRun(200, func() { mw.Use(fetch, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "S2: a Sec-Fetch-Site decision costs no allocation"))
	}
	origin := csrfRequest(http.MethodPost, "", "https://example.com")
	if got := testing.AllocsPerRun(200, func() { mw.Use(origin, w, noop) }); got > 2 {
		t.Error(test.DiffMessage(got, "<= 2", "S5: an Origin against the host costs at most two allocations"))
	}
	bare := csrfRequest(http.MethodPost, "", "")
	if got := testing.AllocsPerRun(200, func() { mw.Use(bare, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "S4: a non-browser request costs no allocation"))
	}
}
