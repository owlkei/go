package cors

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

const (
	appOrigin       = "https://app.example.com"
	defaultMethods  = "GET,HEAD,PUT,PATCH,POST,DELETE"
	defaultHeaders  = "Content-Type,Authorization"
	originRefusal   = `{"code":403,"error":"Forbidden","message":"Origin not allowed"}`
	optionsRouteOut = "options-route"
)

var listed = CORS{AllowOrigins: []string{appOrigin}}

func compact(s string) string { return strings.ReplaceAll(s, " ", "") }

func headerTokens(h http.Header, key string) []string {
	var tokens []string
	for _, value := range h.Values(key) {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func hasToken(h http.Header, key, token string) bool {
	for _, t := range headerTokens(h, key) {
		if t == token {
			return true
		}
	}
	return false
}

func corsRequest(method, target, origin string, headers ...string) *http.Request {
	r := request(method, target, headers...)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func preflight(origin string, headers ...string) *http.Request {
	return corsRequest(http.MethodOptions, "/ok", origin, append([]string{"Access-Control-Request-Method", "POST"}, headers...)...)
}

func expectNoCORSHeaders(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	for key := range w.Header() {
		if strings.HasPrefix(key, "Access-Control-") {
			t.Error(test.DiffMessage(key, "<no CORS header>", desc))
		}
	}
}

func TestNewCORS_Panics_EmptyOrigins(t *testing.T) {
	for _, c := range []CORS{{}, {AllowOrigins: []string{}}, {IsAllowCredentials: true}} {
		text, ok := panicText(func() { c.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid cors: AllowOrigins is empty") {
			t.Error(test.DiffMessage(text, "invalid cors: AllowOrigins is empty", "C1, G4: the zero value allows nothing and says so at Create"))
		}
	}
}

func TestNewCORS_Panics_WildcardWithOthers(t *testing.T) {
	text, ok := panicText(func() { CORS{AllowOrigins: []string{"*", appOrigin}}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid cors:") {
		t.Error(test.DiffMessage(text, "invalid cors: ...", "C1: * alone"))
	}
	text, ok = panicText(func() { CORS{AllowOrigins: []string{appOrigin, "*"}}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid cors:") {
		t.Error(test.DiffMessage(text, "invalid cors: ...", "C1: * alone, last"))
	}
}

func TestNewCORS_Panics_WildcardWithCredentials(t *testing.T) {
	text, ok := panicText(func() { CORS{AllowOrigins: []string{"*"}, IsAllowCredentials: true}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid cors:") {
		t.Error(test.DiffMessage(text, "invalid cors: ...", "C1: * never with credentials"))
	}
}

func TestNewCORS_Panics_InvalidOrigin(t *testing.T) {
	for _, origin := range []string{
		"example.com", "https://example.com/", "https://example.com/path", "HTTPS://example.com", "https://Example.com", "https://*",
		"https://*.", "https://a.*.example.com", "*.example.com", "null", "https://", "", "https://example.com?x=1", "https://user@example.com",
		"http://*example.com", "https://*.example.com/", "https://**.example.com", "https://example.com#x", "https://exa mple.com", "ftp",
	} {
		text, ok := panicText(func() { CORS{AllowOrigins: []string{origin}}.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid cors:") {
			t.Error(test.DiffMessage(text, "invalid cors: ...", fmt.Sprintf("C1, G4: %q is not scheme://host[:port] in lower case or scheme://*.host[:port]", origin)))
		}
	}
}

func TestNewCORS_Panics_TooManyOrigins(t *testing.T) {
	origins := make([]string, 257)
	for i := range origins {
		origins[i] = fmt.Sprintf("https://app%d.example.com", i)
	}
	text, ok := panicText(func() { CORS{AllowOrigins: origins}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid cors:") {
		t.Error(test.DiffMessage(text, "invalid cors: ...", "C1: more than 256 origins"))
	}
	if text, ok := panicText(func() { CORS{AllowOrigins: origins[:256]}.NewMiddleware() }); ok {
		t.Error(test.DiffMessage(text, "<no panic>", "C1: 256 origins are allowed"))
	}
}

func TestNewCORS_Panics_TooManyListEntries(t *testing.T) {
	names := make([]string, 65)
	for i := range names {
		names[i] = fmt.Sprintf("X-Name-%d", i)
	}
	for field, c := range map[string]CORS{
		"AllowMethods":  {AllowOrigins: []string{appOrigin}, AllowMethods: names},
		"AllowHeaders":  {AllowOrigins: []string{appOrigin}, AllowHeaders: names},
		"ExposeHeaders": {AllowOrigins: []string{appOrigin}, ExposeHeaders: names},
	} {
		text, ok := panicText(func() { c.NewMiddleware() })
		if !ok || !strings.Contains(text, "invalid cors:") {
			t.Error(test.DiffMessage(text, "invalid cors: ...", "C1: more than 64 entries in "+field))
		}
	}
	ok := CORS{AllowOrigins: []string{appOrigin}, AllowMethods: names[:64], AllowHeaders: names[:64], ExposeHeaders: names[:64]}
	if text, panicked := panicText(func() { ok.NewMiddleware() }); panicked {
		t.Error(test.DiffMessage(text, "<no panic>", "C1: 64 entries are allowed"))
	}
}

func TestNewCORS_Panics_NotAToken(t *testing.T) {
	for _, name := range []string{"GET POST", "", "GE\nT", "Bad Header", "X-A,X-B", "X:Y", "(x)", "X\tY", "é", "X;Y", "a/b"} {
		for field, c := range map[string]CORS{
			"AllowMethods":  {AllowOrigins: []string{appOrigin}, AllowMethods: []string{name}},
			"AllowHeaders":  {AllowOrigins: []string{appOrigin}, AllowHeaders: []string{name}},
			"ExposeHeaders": {AllowOrigins: []string{appOrigin}, ExposeHeaders: []string{name}},
		} {
			text, ok := panicText(func() { c.NewMiddleware() })
			if !ok || !strings.Contains(text, "invalid cors:") {
				t.Error(test.DiffMessage(text, "invalid cors: ...", fmt.Sprintf("C1: %q in %s is not an RFC 9110 token", name, field)))
			}
		}
	}
}

func TestNewCORS_ValidOptions(t *testing.T) {
	for _, c := range []CORS{
		{AllowOrigins: []string{"*"}},
		{AllowOrigins: []string{appOrigin, "https://*.example.com", "http://localhost:3000", "https://*.example.com:8443"}, IsAllowCredentials: true},
		{AllowOrigins: []string{appOrigin}, AllowMethods: []string{"GET", "X-Custom_1"}, AllowHeaders: []string{"X-Requested-With"}, ExposeHeaders: []string{"X-Total-Count"}},
		{AllowOrigins: []string{"https://[::1]:8080"}},
	} {
		if text, ok := panicText(func() { c.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", fmt.Sprintf("C1: valid %+v", c)))
		}
	}
}

type varyBefore struct{ Value string }

func (v varyBefore) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Add("Vary", v.Value)
	next()
}

func TestCORS_VaryOnEveryResponse(t *testing.T) {
	app := newApp(t, listed)
	requests := map[string]*http.Request{
		"no Origin":               corsRequest(http.MethodGet, "/ok", ""),
		"matching origin":         corsRequest(http.MethodGet, "/ok", appOrigin),
		"origin not matching":     corsRequest(http.MethodGet, "/ok", "https://evil.example"),
		"preflight":               preflight(appOrigin),
		"preflight not matching":  preflight("https://evil.example"),
		"OPTIONS without request": corsRequest(http.MethodOptions, "/ok", appOrigin),
		"no route":                corsRequest(http.MethodGet, "/missing", appOrigin),
		"POST":                    corsRequest(http.MethodPost, "/ok", appOrigin),
		"long origin":             corsRequest(http.MethodGet, "/ok", "https://"+strings.Repeat("a", 300)+".example.com"),
		"null origin":             corsRequest(http.MethodGet, "/ok", "null"),
	}
	for name, r := range requests {
		w := serve(app, r)
		if !hasToken(w.Header(), "Vary", "Origin") {
			t.Error(test.DiffMessage(w.Header().Values("Vary"), "<Origin>", "C3: Vary: Origin on "+name))
		}
	}
}

func TestCORS_Vary_AddedToExisting(t *testing.T) {
	app := newApp(t, varyBefore{Value: "Accept-Encoding"}, listed)
	w := serve(app, corsRequest(http.MethodGet, "/ok", appOrigin))
	for _, token := range []string{"Accept-Encoding", "Origin"} {
		if !hasToken(w.Header(), "Vary", token) {
			t.Error(test.DiffMessage(w.Header().Values("Vary"), "<"+token+">", "C3: Origin is added to an existing Vary"))
		}
	}
	w = serve(app, corsRequest(http.MethodGet, "/ok", ""))
	if !hasToken(w.Header(), "Vary", "Accept-Encoding") || !hasToken(w.Header(), "Vary", "Origin") {
		t.Error(test.DiffMessage(w.Header().Values("Vary"), "<Accept-Encoding, Origin>", "C3: also without an Origin header"))
	}
}

func TestCORS_NoOrigin(t *testing.T) {
	app := newApp(t, listed)
	w := serve(app, corsRequest(http.MethodGet, "/ok", ""))
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Body.String(), "ok", "C4: a request without Origin reaches its route"))
	}
	expectNoCORSHeaders(t, w, "C4: no CORS header without an Origin")
	w = serve(app, corsRequest(http.MethodOptions, "/ok", "", "Access-Control-Request-Method", "POST"))
	if w.Body.String() != optionsRouteOut {
		t.Error(test.DiffMessage(w.Body.String(), optionsRouteOut, "C5: Access-Control-Request-Method without Origin is not a preflight"))
	}
}

func TestCORS_Actual_Match(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin}, ExposeHeaders: []string{"X-Total", "X-Page"}})
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		w := serve(app, corsRequest(method, "/ok", appOrigin))
		if w.Code >= 400 || w.Body.String() != "ok" {
			t.Error(test.DiffMessage(w.Code, "<2xx>", "C6: the route runs: "+method))
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != appOrigin {
			t.Error(test.DiffMessage(got, appOrigin, "C6: Access-Control-Allow-Origin is the request's origin: "+method))
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Error(test.DiffMessage(got, "", "C6: no credentials unless asked: "+method))
		}
		if got := compact(w.Header().Get("Access-Control-Expose-Headers")); got != "X-Total,X-Page" {
			t.Error(test.DiffMessage(got, "X-Total,X-Page", "C6: Access-Control-Expose-Headers when set: "+method))
		}
		for _, key := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Max-Age"} {
			if w.Header().Get(key) != "" {
				t.Error(test.DiffMessage(w.Header().Get(key), "", "C6: "+key+" is for preflights: "+method))
			}
		}
	}
}

func TestCORS_Actual_NoExposeHeadersUnlessSet(t *testing.T) {
	app := newApp(t, listed)
	w := serve(app, corsRequest(http.MethodGet, "/ok", appOrigin))
	if got := w.Header().Get("Access-Control-Expose-Headers"); got != "" {
		t.Error(test.DiffMessage(got, "", "C6: no Expose-Headers when none are set"))
	}
}

func TestCORS_Actual_NoMatch_StillServed(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin}, IsAllowCredentials: true, ExposeHeaders: []string{"X-Total"}})
	w := serve(app, corsRequest(http.MethodPost, "/ok", "https://evil.example"))
	if w.Code >= 400 || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Code, "<2xx>", "C6: CORS is not access control, the request is served"))
	}
	expectNoCORSHeaders(t, w, "C6: no CORS header for an origin that does not match")
}

func TestCORS_Wildcard(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{"https://*.example.com", "http://localhost:3000", "https://exact.example.org"}})
	match := []string{"https://a.example.com", "https://a.b.example.com", "https://a-b.example.com", "https://a1.example.com", "http://localhost:3000", "https://exact.example.org", "https://" + strings.Repeat("a", 60) + ".example.com"}
	for _, origin := range match {
		if got := serve(app, corsRequest(http.MethodGet, "/ok", origin)).Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Error(test.DiffMessage(got, origin, "C2: matches"))
		}
	}
	noMatch := []string{
		"https://example.com", "http://a.example.com", "https://a.example.com.evil.example", "https://evil.example/.example.com",
		"https://evil.example\\.example.com", "https://a_b.example.com", "https://A.example.com", "https://a.example.com:8443",
		"https://a.example.org", "https://exact.example.org/", "http://localhost:3001", "http://localhost", "https://EXACT.example.org",
		"https://a@evil.example.example.com", "https://evil.example?.example.com", "https://a.example.com.", "null", "https://",
		"https://" + strings.Repeat("a", 300) + ".example.com", "https://evil.example#.example.com", "https://a b.example.com",
		"https://evil.example:80@a.example.com", "https://a.example.comx", "xhttps://a.example.com", "https://a.example.com/x",
	}
	for _, origin := range noMatch {
		if got := serve(app, corsRequest(http.MethodGet, "/ok", origin)).Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Error(test.DiffMessage(got, "", "C2: does not match "+origin))
		}
	}
}

func TestCORS_Wildcard_WithPort(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{"https://*.example.com:8443"}})
	if got := serve(app, corsRequest(http.MethodGet, "/ok", "https://a.example.com:8443")).Header().Get("Access-Control-Allow-Origin"); got != "https://a.example.com:8443" {
		t.Error(test.DiffMessage(got, "https://a.example.com:8443", "C2: the suffix includes the port"))
	}
	if got := serve(app, corsRequest(http.MethodGet, "/ok", "https://a.example.com")).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Error(test.DiffMessage(got, "", "C2: another port does not match"))
	}
}

func TestCORS_Star(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{"*"}})
	for _, origin := range []string{appOrigin, "null", "http://localhost", "https://evil.example"} {
		w := serve(app, corsRequest(http.MethodGet, "/ok", origin))
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Error(test.DiffMessage(got, "*", "C2, C6: * matches every origin and is sent as *: "+origin))
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error(test.DiffMessage(w.Header().Get("Access-Control-Allow-Credentials"), "", "C1: no credentials with *"))
		}
	}
	w := serve(app, preflight(appOrigin))
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Access-Control-Allow-Origin")}, []any{204, "*"}, "C7: preflight with *"))
	}
	w = serve(app, corsRequest(http.MethodGet, "/ok", ""))
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Body.String() != "ok" {
		t.Error(test.DiffMessage(w.Header(), "<no ACAO>", "C4: a request without Origin gets no CORS header with * either"))
	}
}

func TestCORS_Preflight_Exact(t *testing.T) {
	app := newApp(t, listed)
	w := serve(app, preflight(appOrigin))
	if w.Code != http.StatusNoContent {
		t.Error(test.DiffMessage(w.Code, http.StatusNoContent, "C7: preflight status"))
	}
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "C7: preflight has no body, next() is not called"))
	}
	for key, want := range map[string]string{
		"Access-Control-Allow-Origin":  appOrigin,
		"Access-Control-Allow-Methods": defaultMethods,
		"Access-Control-Allow-Headers": defaultHeaders,
		"Access-Control-Max-Age":       "600",
		"Content-Length":               "0",
	} {
		if got := compact(w.Header().Get(key)); got != want {
			t.Error(test.DiffMessage(got, want, "C7: "+key))
		}
	}
	if !hasToken(w.Header(), "Vary", "Origin") {
		t.Error(test.DiffMessage(w.Header().Values("Vary"), "<Origin>", "C3: a preflight also varies on Origin"))
	}
	for _, key := range []string{"Access-Control-Allow-Credentials", "Access-Control-Allow-Private-Network", "Access-Control-Expose-Headers"} {
		if w.Header().Get(key) != "" {
			t.Error(test.DiffMessage(w.Header().Get(key), "", "C7: "+key+" only when configured"))
		}
	}
}

func TestCORS_Preflight_CustomOptions(t *testing.T) {
	app := newApp(t, CORS{
		AllowOrigins: []string{appOrigin}, AllowMethods: []string{"GET", "POST"}, AllowHeaders: []string{"X-Custom", "Content-Type"},
		ExposeHeaders: []string{"X-Total"}, MaxAge: 2 * time.Hour, IsAllowCredentials: true,
	})
	w := serve(app, preflight(appOrigin))
	for key, want := range map[string]string{
		"Access-Control-Allow-Methods":     "GET,POST",
		"Access-Control-Allow-Headers":     "X-Custom,Content-Type",
		"Access-Control-Max-Age":           "7200",
		"Access-Control-Expose-Headers":    "X-Total",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Origin":      appOrigin,
	} {
		if got := compact(w.Header().Get(key)); got != want {
			t.Error(test.DiffMessage(got, want, "C7: "+key))
		}
	}
}

func TestCORS_Preflight_MaxAgeInWholeSeconds(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin}, MaxAge: 90 * time.Second})
	if got := serve(app, preflight(appOrigin)).Header().Get("Access-Control-Max-Age"); got != "90" {
		t.Error(test.DiffMessage(got, "90", "C7: Max-Age is in seconds"))
	}
}

func TestCORS_OptionsWithoutRequestMethod_ReachesRoute(t *testing.T) {
	app := newApp(t, listed)
	w := serve(app, corsRequest(http.MethodOptions, "/ok", appOrigin))
	if w.Body.String() != optionsRouteOut {
		t.Error(test.DiffMessage(w.Body.String(), optionsRouteOut, "C5: an OPTIONS without Access-Control-Request-Method reaches its route"))
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != appOrigin {
		t.Error(test.DiffMessage(got, appOrigin, "C6: it is an actual request, so ACAO is set"))
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "" || w.Header().Get("Access-Control-Max-Age") != "" {
		t.Error(test.DiffMessage(w.Header(), "<no preflight headers>", "C6: an actual request carries no preflight headers"))
	}
	if !hasToken(w.Header(), "Vary", "Origin") {
		t.Error(test.DiffMessage(w.Header().Values("Vary"), "<Origin>", "C3: Vary on a real OPTIONS"))
	}
}

func TestCORS_Preflight_NoMatch(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin}, IsAllowCredentials: true, IsAllowPrivateNetwork: true})
	w := serve(app, preflight("https://evil.example", "Access-Control-Request-Private-Network", "true"))
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.Len()}, []any{204, 0}, "C8: 204, next() not called"))
	}
	expectNoCORSHeaders(t, w, "C8: no CORS header for an origin that does not match")
	if !hasToken(w.Header(), "Vary", "Origin") {
		t.Error(test.DiffMessage(w.Header().Values("Vary"), "<Origin>", "C8: Vary only"))
	}
}

func TestCORS_Credentials(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin}, IsAllowCredentials: true})
	for name, r := range map[string]*http.Request{"actual": corsRequest(http.MethodGet, "/ok", appOrigin), "preflight": preflight(appOrigin)} {
		w := serve(app, r)
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Error(test.DiffMessage(got, "true", "C6: credentials, "+name))
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != appOrigin {
			t.Error(test.DiffMessage(got, appOrigin, "C1: with credentials the origin is echoed, never *: "+name))
		}
	}
	for name, r := range map[string]*http.Request{"actual": corsRequest(http.MethodGet, "/ok", "https://evil.example"), "preflight": preflight("https://evil.example")} {
		if got := serve(app, r).Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Error(test.DiffMessage(got, "", "C6: no credentials for an origin that does not match: "+name))
		}
	}
}

func TestCORS_PrivateNetwork(t *testing.T) {
	on := newApp(t, CORS{AllowOrigins: []string{appOrigin}, IsAllowPrivateNetwork: true})
	off := newApp(t, listed)
	const key = "Access-Control-Allow-Private-Network"
	if got := serve(on, preflight(appOrigin, "Access-Control-Request-Private-Network", "true")).Header().Get(key); got != "true" {
		t.Error(test.DiffMessage(got, "true", "C7: option on and the request asks"))
	}
	if got := serve(on, preflight(appOrigin)).Header().Get(key); got != "" {
		t.Error(test.DiffMessage(got, "", "C7: option on but the request does not ask"))
	}
	if got := serve(on, preflight(appOrigin, "Access-Control-Request-Private-Network", "false")).Header().Get(key); got != "" {
		t.Error(test.DiffMessage(got, "", "C7: the request header must be true"))
	}
	if got := serve(off, preflight(appOrigin, "Access-Control-Request-Private-Network", "true")).Header().Get(key); got != "" {
		t.Error(test.DiffMessage(got, "", "C7: the request asks but the option is off"))
	}
	if got := serve(on, corsRequest(http.MethodGet, "/ok", appOrigin, "Access-Control-Request-Private-Network", "true")).Header().Get(key); got != "" {
		t.Error(test.DiffMessage(got, "", "C7: an actual request never gets it"))
	}
}

func TestCORS_RequestHeadersNeverEchoed(t *testing.T) {
	app := newApp(t, listed)
	w := serve(app, preflight(appOrigin, "Access-Control-Request-Headers", "X-Evil, Authorization, X-Other"))
	if got := compact(w.Header().Get("Access-Control-Allow-Headers")); got != defaultHeaders {
		t.Error(test.DiffMessage(got, defaultHeaders, "C7: Allow-Headers is the configured list, never the request's"))
	}
	if hasToken(w.Header(), "Vary", "Access-Control-Request-Headers") {
		t.Error(test.DiffMessage(w.Header().Values("Vary"), "<no Access-Control-Request-Headers>", "C7: the request's header list is not read"))
	}
	for key, values := range w.Header() {
		for _, value := range values {
			if strings.Contains(value, "X-Evil") {
				t.Error(test.DiffMessage(key+": "+value, "<no echo>", "C7: nothing from Access-Control-Request-Headers is echoed"))
			}
		}
	}
}

func TestCORS_LongOrigin(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin, "https://*.example.com"}})
	long := "https://" + strings.Repeat("a", 260) + ".example.com"
	for name, r := range map[string]*http.Request{"actual": corsRequest(http.MethodGet, "/ok", long), "preflight": preflight(long)} {
		w := serve(app, r)
		expectNoCORSHeaders(t, w, "C2: an Origin longer than 256 bytes matches nothing: "+name)
	}
}

func TestCORS_WebSocketOrigin(t *testing.T) {
	app := newApp(t, CORS{AllowOrigins: []string{appOrigin, "https://*.example.com"}})
	upgrade := func(origin string) *http.Request {
		return corsRequest(http.MethodGet, "/ok", origin, "Upgrade", "websocket", "Connection", "Upgrade")
	}
	w := serve(app, upgrade("https://evil.example"))
	expectRefusal(t, w, http.StatusForbidden, originRefusal, "a WebSocket upgrade from an origin that does not match")
	w = serve(app, corsRequest(http.MethodGet, "/ok", "https://evil.example", "Upgrade", "WebSocket"))
	expectRefusal(t, w, http.StatusForbidden, originRefusal, "the Upgrade value is case-insensitive")
	for _, origin := range []string{appOrigin, "https://a.example.com", ""} {
		w = serve(app, upgrade(origin))
		if w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "C9: a matching or absent origin continues: "+origin))
		}
		expectNoCORSHeaders(t, w, "C9: no CORS header on a WebSocket upgrade: "+origin)
	}
	w = serve(app, corsRequest(http.MethodGet, "/ok", "https://evil.example", "Upgrade", "h2c"))
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "C9: only a websocket upgrade is refused"))
	}
}

func TestCORS_WebSocketHandshake(t *testing.T) {
	dial := func(allowed []string, origin string) error {
		app := core.New()
		app.SetLogger(silentLogger{})
		app.EnableWS(&core.WSConfig{Path: "/ws"}, CORS{AllowOrigins: allowed})
		app.Create(core.ModuleBuilder().Build())
		server := httptest.NewServer(app)
		defer server.Close()
		conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", origin)
		if err == nil {
			_ = conn.Close()
		}
		return err
	}
	if err := dial([]string{appOrigin}, appOrigin); err != nil {
		t.Error(test.DiffMessage(err, nil, "G5, C9: a matching origin completes the handshake"))
	}
	if err := dial([]string{appOrigin}, "https://evil.example"); err == nil {
		t.Error(test.DiffMessage(nil, "a refused handshake", "G5, C9: another origin refuses the handshake"))
	}
}

func TestCORS_RefusalIsHTTPLayerResponse(t *testing.T) {
	app := newApp(t, listed)
	r := corsRequest(http.MethodGet, "/ok", "https://evil.example", "Upgrade", "websocket")
	w := serve(app, r)
	expectRefusal(t, w, http.StatusForbidden, originRefusal, "a refused WebSocket origin")
	if strings.Contains(w.Body.String(), "evil") {
		t.Error(test.DiffMessage(w.Body.String(), "<no origin>", "G1: nothing from the request is in the body"))
	}
}

func TestCORS_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, listed, probeMiddleware{Name: "after"})
	for name, r := range map[string]*http.Request{
		"no origin": corsRequest(http.MethodGet, "/ok", ""),
		"actual":    corsRequest(http.MethodGet, "/ok", appOrigin),
		"no match":  corsRequest(http.MethodGet, "/ok", "https://evil.example"),
		"options":   corsRequest(http.MethodOptions, "/ok", appOrigin),
	} {
		probes.Delete("after")
		serve(app, r)
		before, after := probeOf("before"), probeOf("after")
		if before.request == nil || before.request != after.request {
			t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request: "+name))
		}
		if before.context != after.context {
			t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context(): "+name))
		}
	}
}

func TestCORS_DoesNotReadBody(t *testing.T) {
	app := newApp(t, listed)
	for _, origin := range []string{"", appOrigin, "https://evil.example"} {
		for _, method := range []string{http.MethodPost, http.MethodOptions} {
			r := corsRequest(method, "/ok", origin, "Access-Control-Request-Method", "POST")
			r.Body = failBody{t}
			serve(app, r)
		}
	}
}

func TestCORS_Snapshot(t *testing.T) {
	mw := CORS{AllowOrigins: []string{appOrigin, "https://*.example.com"}, ExposeHeaders: []string{"X-Total"}, IsAllowCredentials: true, IsAllowPrivateNetwork: true}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled cors implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "cors" {
		t.Error(test.DiffMessage(snap.Name, "cors", "I4: the snapshot name"))
	}
	lists := map[string]string{
		"allow_origins":  appOrigin + ",https://*.example.com",
		"allow_methods":  defaultMethods,
		"allow_headers":  defaultHeaders,
		"expose_headers": "X-Total",
	}
	for key, want := range lists {
		got, _ := field(snap, key).([]string)
		if compact(strings.Join(got, ",")) != want {
			t.Error(test.DiffMessage(got, want, "I4: every option after defaults: "+key))
		}
	}
	if got := field(snap, "max_age"); got != 10*time.Minute {
		t.Error(test.DiffMessage(got, 10*time.Minute, "I4: max_age after defaults"))
	}
	if field(snap, "allow_credentials") != true || field(snap, "allow_private_network") != true {
		t.Error(test.DiffMessage(snap.Fields, "credentials and private network true", "I4: booleans"))
	}
	if field(snap, "preflights") != int64(0) || field(snap, "refused") != int64(0) {
		t.Error(test.DiffMessage(snap.Fields, "preflights 0, refused 0", "I4: counters start at zero"))
	}

	w := &discardWriter{header: http.Header{}}
	mw.Use(preflight(appOrigin), w, noop)
	mw.Use(preflight(appOrigin), w, noop)
	mw.Use(preflight("https://evil.example"), w, noop)
	mw.Use(corsRequest(http.MethodGet, "/ok", "https://evil.example"), w, noop)
	mw.Use(corsRequest(http.MethodGet, "/ok", appOrigin), w, noop)
	after := s.Snapshot()
	if got := field(after, "preflights"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: preflights answered for a matching origin"))
	}
	if got := field(after, "refused"); got != int64(2) {
		t.Error(test.DiffMessage(got, int64(2), "I4: origins refused"))
	}
}

func TestCORS_Allocations(t *testing.T) {
	mw := listed.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	reset := func() { clear(w.header) }

	none := corsRequest(http.MethodGet, "/ok", "")
	if got := testing.AllocsPerRun(200, func() { reset(); mw.Use(none, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "C4: a request without Origin costs no allocation"))
	}
	actual := corsRequest(http.MethodGet, "/ok", appOrigin)
	if got := testing.AllocsPerRun(200, func() { reset(); mw.Use(actual, w, noop) }); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "C6: an actual request for a listed origin costs at most one allocation"))
	}
	pre := preflight(appOrigin)
	if got := testing.AllocsPerRun(200, func() { reset(); mw.Use(pre, w, noop) }); got > 1 {
		t.Error(test.DiffMessage(got, "<= 1", "C7: a preflight costs at most one allocation"))
	}
	refused := corsRequest(http.MethodGet, "/ok", "https://evil.example")
	if got := testing.AllocsPerRun(200, func() { reset(); mw.Use(refused, w, noop) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "C6: an origin that does not match costs no allocation"))
	}
}
