package helmet_test

import (
	"fmt"
	. "go.owlky.com/owlky/internal/middlewares/helmet"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/middlewares/proxy"
	"go.owlky.com/owlky/internal/test"
)

func TestHelmet_Disabled_Constant(t *testing.T) {
	if Disabled != "-" {
		t.Error(test.DiffMessage(Disabled, "-", "M1: the value that turns a header off"))
	}
}

func TestHelmet_Defaults(t *testing.T) {
	app := newApp(t, Helmet{})
	w := serve(app, secure("/ok"))
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatal(test.DiffMessage(w.Body.String(), "ok", "fixture: the route runs"))
	}
	for key, want := range defaultsOverHTTPS {
		expectHeader(t, w, key, want, "M1, M3: default")
	}
	expectHeader(t, w, "Cross-Origin-Embedder-Policy", "", "M1: COEP is not sent by default")
	expectHeader(t, w, "X-Download-Options", "", "M3: X-Download-Options is no longer sent")
}

func TestHelmet_Defaults_OverHTTP(t *testing.T) {
	app := newApp(t, Helmet{})
	w := serve(app, request(http.MethodGet, "/ok"))
	for key, want := range defaultsOverHTTPS {
		if key == "Strict-Transport-Security" {
			expectHeader(t, w, key, "", "M2: HSTS is not sent over plain HTTP")
			continue
		}
		expectHeader(t, w, key, want, "M1, M3: default over HTTP")
	}
}

func TestHelmet_Disabled(t *testing.T) {
	fields := map[string]func(*Helmet){
		"Content-Security-Policy":      func(h *Helmet) { h.ContentSecurityPolicy = Disabled },
		"Cross-Origin-Embedder-Policy": func(h *Helmet) { h.CrossOriginEmbedderPolicy = Disabled },
		"Cross-Origin-Opener-Policy":   func(h *Helmet) { h.CrossOriginOpenerPolicy = Disabled },
		"Cross-Origin-Resource-Policy": func(h *Helmet) { h.CrossOriginResourcePolicy = Disabled },
		"X-Frame-Options":              func(h *Helmet) { h.FrameOptions = Disabled },
		"Permissions-Policy":           func(h *Helmet) { h.PermissionsPolicy = Disabled },
		"Referrer-Policy":              func(h *Helmet) { h.ReferrerPolicy = Disabled },
		"Strict-Transport-Security":    func(h *Helmet) { h.StrictTransportSecurity = Disabled },
	}
	for key, disable := range fields {
		var h Helmet
		disable(&h)
		w := serve(newApp(t, h), secure("/ok"))
		expectHeader(t, w, key, "", "M1: Disabled")
		for other, want := range defaultsOverHTTPS {
			if other == key {
				continue
			}
			expectHeader(t, w, other, want, "M1: disabling "+key+" leaves the others")
		}
	}
}

func TestHelmet_AllDisabled_KeepsFixedHeaders(t *testing.T) {
	h := Helmet{
		ContentSecurityPolicy: Disabled, CrossOriginEmbedderPolicy: Disabled, CrossOriginOpenerPolicy: Disabled, CrossOriginResourcePolicy: Disabled,
		FrameOptions: Disabled, PermissionsPolicy: Disabled, ReferrerPolicy: Disabled, StrictTransportSecurity: Disabled,
	}
	w := serve(newApp(t, h), secure("/ok"))
	for _, key := range fixedHeaders {
		expectHeader(t, w, key, defaultsOverHTTPS[key], "M3: fixed headers are always sent")
	}
	for _, key := range []string{"Content-Security-Policy", "Cross-Origin-Opener-Policy", "Cross-Origin-Resource-Policy", "Permissions-Policy", "Referrer-Policy", "Strict-Transport-Security", "X-Frame-Options", "Cross-Origin-Embedder-Policy"} {
		expectHeader(t, w, key, "", "M1: every configurable header off")
	}
}

func TestHelmet_Custom(t *testing.T) {
	h := Helmet{
		ContentSecurityPolicy: "default-src 'none'", CrossOriginEmbedderPolicy: "require-corp", CrossOriginOpenerPolicy: "same-origin-allow-popups",
		CrossOriginResourcePolicy: "cross-origin", FrameOptions: "SAMEORIGIN", PermissionsPolicy: "camera=(self)", ReferrerPolicy: "strict-origin-when-cross-origin",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains; preload",
	}
	w := serve(newApp(t, h), secure("/ok"))
	for key, want := range map[string]string{
		"Content-Security-Policy":      "default-src 'none'",
		"Cross-Origin-Embedder-Policy": "require-corp",
		"Cross-Origin-Opener-Policy":   "same-origin-allow-popups",
		"Cross-Origin-Resource-Policy": "cross-origin",
		"X-Frame-Options":              "SAMEORIGIN",
		"Permissions-Policy":           "camera=(self)",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains; preload",
	} {
		expectHeader(t, w, key, want, "M1: a custom value is sent as written")
	}
	plain := serve(newApp(t, h), request(http.MethodGet, "/ok"))
	expectHeader(t, plain, "Strict-Transport-Security", "", "M2: a custom HSTS is still HTTPS only")
}

func TestHelmet_HSTSOnlyOverHTTPS(t *testing.T) {
	trusted := proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}}
	behind := newApp(t, trusted, Helmet{})
	alone := newApp(t, Helmet{})

	direct := serve(alone, secure("/ok"))
	expectHeader(t, direct, "Strict-Transport-Security", defaultHSTS, "M2: direct TLS")

	forwarded := request(http.MethodGet, "/ok", "X-Forwarded-For", "198.51.100.1", "X-Forwarded-Proto", "https")
	forwarded.RemoteAddr = "10.0.0.1:80"
	expectHeader(t, serve(behind, forwarded), "Strict-Transport-Security", defaultHSTS, "M2: https forwarded by a trusted proxy")

	notHTTPS := request(http.MethodGet, "/ok", "X-Forwarded-For", "198.51.100.1", "X-Forwarded-Proto", "http")
	notHTTPS.RemoteAddr = "10.0.0.1:80"
	expectHeader(t, serve(behind, notHTTPS), "Strict-Transport-Security", "", "M2: http forwarded by a trusted proxy")

	spoofed := request(http.MethodGet, "/ok", "X-Forwarded-Proto", "https")
	spoofed.RemoteAddr = "203.0.113.50:80"
	expectHeader(t, serve(behind, spoofed), "Strict-Transport-Security", "", "M2: an untrusted peer cannot claim https")

	noProxy := request(http.MethodGet, "/ok", "X-Forwarded-Proto", "https", "Forwarded", "proto=https")
	expectHeader(t, serve(alone, noProxy), "Strict-Transport-Security", "", "P2: with no proxy bound no forwarding header is read")

	expectHeader(t, serve(alone, request(http.MethodGet, "/ok")), "Strict-Transport-Security", "", "M2: plain HTTP")
}

func TestHelmet_HSTS_NeverPreloadByDefault(t *testing.T) {
	w := serve(newApp(t, Helmet{}), secure("/ok"))
	if strings.Contains(w.Header().Get("Strict-Transport-Security"), "preload") {
		t.Error(test.DiffMessage(w.Header().Get("Strict-Transport-Security"), defaultHSTS, "M1: preload is an explicit request"))
	}
}

func TestHelmet_FixedHeadersOnEveryResponse(t *testing.T) {
	app := newApp(t, Helmet{})
	for name, r := range map[string]*http.Request{
		"route":    request(http.MethodGet, "/ok"),
		"no route": request(http.MethodGet, "/missing"),
		"post":     request(http.MethodPost, "/ok"),
		"405":      request(http.MethodPatch, "/missing"),
	} {
		w := serve(app, r)
		for _, key := range fixedHeaders {
			expectHeader(t, w, key, defaultsOverHTTPS[key], "M3: "+name)
		}
	}
}

func TestHelmet_HeadersSetBeforeNext(t *testing.T) {
	app := newApp(t, Helmet{}, headerProbe{Name: "after-helmet"})
	serve(app, secure("/ok"))
	value, _ := seen.Load("after-helmet")
	h, _ := value.(http.Header)
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != defaultCSP {
		t.Error(test.DiffMessage(h, "<helmet headers>", "M4: headers are set before next()"))
	}
}

func TestHelmet_LaterLayerMayOverwrite(t *testing.T) {
	app := newApp(t, Helmet{}, headerOverride{Key: "X-Frame-Options", Value: "SAMEORIGIN"}, headerOverride{Key: "Content-Security-Policy", Value: "default-src 'none'"})
	w := serve(app, secure("/ok"))
	expectHeader(t, w, "X-Frame-Options", "SAMEORIGIN", "M4: a later layer overwrites")
	expectHeader(t, w, "Content-Security-Policy", "default-src 'none'", "M4: a later layer overwrites the CSP")
	expectHeader(t, w, "Referrer-Policy", "no-referrer", "M4: the others stay")
}

func TestNewHelmet_Panics_ControlCharacter(t *testing.T) {
	for _, value := range []string{"a\nb", "a\x00b", "a\rb", "a\x7fb", "\x1b[0m", "x\r\nSet-Cookie: a=b"} {
		fields := map[string]Helmet{
			"ContentSecurityPolicy":     {ContentSecurityPolicy: value},
			"CrossOriginEmbedderPolicy": {CrossOriginEmbedderPolicy: value},
			"CrossOriginOpenerPolicy":   {CrossOriginOpenerPolicy: value},
			"CrossOriginResourcePolicy": {CrossOriginResourcePolicy: value},
			"FrameOptions":              {FrameOptions: value},
			"PermissionsPolicy":         {PermissionsPolicy: value},
			"ReferrerPolicy":            {ReferrerPolicy: value},
			"StrictTransportSecurity":   {StrictTransportSecurity: value},
		}
		for field, h := range fields {
			text, ok := panicText(func() { h.NewMiddleware() })
			if !ok || !strings.Contains(text, "invalid helmet:") {
				t.Error(test.DiffMessage(text, "invalid helmet: ...", fmt.Sprintf("M1, G4: %q in %s", value, field)))
			}
		}
	}
}

func TestNewHelmet_ValidValues(t *testing.T) {
	h := Helmet{ContentSecurityPolicy: "default-src 'self'; img-src https: data:", FrameOptions: "SAMEORIGIN", ReferrerPolicy: "origin"}
	if text, ok := panicText(func() { h.NewMiddleware() }); ok {
		t.Error(test.DiffMessage(text, "<no panic>", "M1: ordinary values"))
	}
}

func TestHelmet_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, Helmet{}, probeMiddleware{Name: "after"})
	serve(app, secure("/ok"))
	before, after := probeOf("before"), probeOf("after")
	if before.request == nil || before.request != after.request {
		t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request"))
	}
	if before.context != after.context {
		t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context()"))
	}
}

func TestHelmet_DoesNotReadBody(t *testing.T) {
	app := newApp(t, Helmet{})
	r := request(http.MethodPost, "/ok")
	r.Body = failBody{t}
	serve(app, r)
}

func TestHelmet_Snapshot(t *testing.T) {
	mw := Helmet{FrameOptions: "SAMEORIGIN", ReferrerPolicy: Disabled}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled helmet implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "helmet" {
		t.Error(test.DiffMessage(snap.Name, "helmet", "I4: the snapshot name"))
	}
	for key, want := range map[string]string{
		"Content-Security-Policy":      defaultCSP,
		"Cross-Origin-Embedder-Policy": Disabled,
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"X-Frame-Options":              "SAMEORIGIN",
		"Permissions-Policy":           defaultPermissions,
		"Referrer-Policy":              Disabled,
		"Strict-Transport-Security":    defaultHSTS,
	} {
		if got := field(snap, key); got != want {
			t.Error(test.DiffMessage(got, want, "I4: every header as sent, Disabled shown: "+key))
		}
	}
}

func TestHelmet_Allocations(t *testing.T) {
	mw := Helmet{}.NewMiddleware()
	w := &discardWriter{header: http.Header{}}
	for name, r := range map[string]*http.Request{"http": request(http.MethodGet, "/ok"), "https": secure("/ok")} {
		if got := testing.AllocsPerRun(200, func() { clear(w.header); mw.Use(r, w, noop) }); got != 0 {
			t.Error(test.DiffMessage(got, 0.0, "M1: helmet assigns prebuilt slices, no allocation: "+name))
		}
	}
}

func TestHelmet_AddingToAHeaderDoesNotChangeSharedValue(t *testing.T) {
	app := newApp(t, Helmet{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				w := serve(app, secure("/ok"))
				w.Header().Add("X-Frame-Options", "tampered")
				w.Header().Add("Content-Security-Policy", "tampered")
			}
		}()
	}
	wg.Wait()
	w := serve(app, secure("/ok"))
	expectHeader(t, w, "Content-Security-Policy", defaultCSP, "M1: Header.Add on a prebuilt value never writes the shared slice")
	expectHeader(t, w, "X-Frame-Options", "DENY", "M1: Header.Add on a prebuilt value never writes the shared slice (X-Frame-Options)")
}

func TestNonce_DefaultPolicyHeader(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	w := nonceServe(app, "/nonce")
	n := bodyNonce(t, w.Body.String())
	want := strings.ReplaceAll(nonceDefaultPolicy, "%s", n)
	if got := w.Header().Get("Content-Security-Policy"); got != want {
		t.Error(test.DiffMessage(got, want, "R31: the exact default header"))
	}
	if n := len(w.Header().Values("Content-Security-Policy")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R31: one CSP header"))
	}
}

func TestNonce_SameInHeaderAndBody(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	w := nonceServe(app, "/nonce")
	body := bodyNonce(t, w.Body.String())
	if !nonceHexValue.MatchString(body) {
		t.Error(test.DiffMessage(body, "32 lowercase hexadecimal characters", "R33: format"))
	}
	headers := nonceInHeader.FindAllStringSubmatch(w.Header().Get("Content-Security-Policy"), -1)
	if len(headers) != 2 {
		t.Fatal(test.DiffMessage(len(headers), 2, "R33: a nonce in script-src and in style-src"))
	}
	for _, m := range headers {
		if m[1] != body {
			t.Error(test.DiffMessage(m[1], body, "R33: the header nonce equals the body nonce"))
		}
	}
	if strings.Count(w.Body.String(), body) != 3 {
		t.Error(test.DiffMessage(strings.Count(w.Body.String(), body), 3, "R33: three uses in one page"))
	}
}

func TestNonce_UniquePerResponse(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		w := nonceServe(app, "/nonce")
		n := bodyNonce(t, w.Body.String())
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'") {
			t.Fatal(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'", "R33: header and body agree"))
		}
		seen[n] = true
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "R33: 1,000 responses give 1,000 nonces"))
	}
}

func TestNonce_Concurrent(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				w := nonceServe(app, "/nonce")
				found := nonceInBody.FindStringSubmatch(w.Body.String())
				if found == nil || strings.Count(w.Header().Get("Content-Security-Policy"), "'nonce-"+found[1]+"'") != 2 {
					t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "the body's nonce twice", "R33: concurrent header and body"))
					return
				}
				mu.Lock()
				seen[found[1]] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 640 {
		t.Error(test.DiffMessage(len(seen), 640, "R33: 64 goroutines never share a nonce"))
	}
}

func TestNonce_OnlyWhenUsed(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	for _, target := range []string{"/plain", "/api", "/assets/app.css", "/missing"} {
		w := nonceServe(app, target)
		if got := w.Header().Get("Content-Security-Policy"); got != defaultCSP {
			t.Error(test.DiffMessage(got, defaultCSP, "R30: the static policy, byte for byte: "+target))
		}
		if strings.Contains(w.Header().Get("Content-Security-Policy"), "nonce") {
			t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "no nonce", "R30: "+target))
		}
	}
	if got := nonceServe(app, "/nonce").Header().Get("Content-Security-Policy"); got == defaultCSP {
		t.Error(test.DiffMessage(got, "a policy with a nonce", "R30: a page that uses {{nonce}} differs"))
	}
	if got := nonceServe(app, "/plain").Header().Get("Content-Security-Policy"); got != defaultCSP {
		t.Error(test.DiffMessage(got, defaultCSP, "R30: the shared header is not changed by an earlier nonce response"))
	}
}

func TestNonce_ErrorPageSet(t *testing.T) {
	app := nonceApp(t, Helmet{}, true)

	w := nonceServe(app, "/boom")
	if w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "R30: the error page answers"))
	}
	n := bodyNonce(t, w.Body.String())
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'") {
		t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'", "R30: an error page that uses {{nonce}} gets its nonce in the header"))
	}

	if got := nonceServe(app, "/api").Header().Get("Content-Security-Policy"); got != defaultCSP {
		t.Error(test.DiffMessage(got, defaultCSP, "R30: an API route stays static with an error page that has a nonce"))
	}
}

func TestNonce_WithoutHelmet(t *testing.T) {
	app := core.New(&core.AppOptions{Logger: silentLogger{}, Render: &core.RenderOptions{FS: nonceFiles(false), Dir: "."}})
	app.Create(core.NewModuleBuilder().Controllers(noncePages{}, nonceLaterPages{}, nonceAPI{}).Build())

	w := nonceServe(app, "/nonce")
	if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
		t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R34: {{nonce}} still renders a nonce"))
	}
	if len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Header().Values("Content-Security-Policy"), []string{}, "R34: no CSP header is added"))
	}

	disabled := nonceApp(t, Helmet{ContentSecurityPolicy: Disabled}, false)
	w = nonceServe(disabled, "/nonce")
	if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
		t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R34: a disabled policy still renders a nonce"))
	}
	if len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Header().Values("Content-Security-Policy"), []string{}, "R34: a disabled policy adds no header"))
	}
}

func TestNonce_KeepsForeignCSP(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	for _, target := range []string{"/foreign", "/later"} {
		w := nonceServe(app, target)
		if got := w.Header().Values("Content-Security-Policy"); !reflect.DeepEqual(got, []string{nonceForeign}) {
			t.Error(test.DiffMessage(got, []string{nonceForeign}, "R35: a CSP set after helmet is left as it is: "+target))
		}
		if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
			t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R35: the body still renders: "+target))
		}
	}
}

func TestHelmet_NonceDirectives(t *testing.T) {
	got, n := nonceHeader(t, "script-src 'self'; style-src 'self' https://cdn.test; img-src 'self'")
	want := "script-src 'self' 'nonce-" + n + "'; style-src 'self' https://cdn.test 'nonce-" + n + "'; img-src 'self'"
	if got != want {
		t.Error(test.DiffMessage(got, want, "R32: present directives get the nonce appended"))
	}

	policy := "script-src 'self' 'unsafe-inline'; style-src 'none'; img-src 'self'"
	if got, _ := nonceHeader(t, policy); got != policy {
		t.Error(test.DiffMessage(got, policy, "R32: 'unsafe-inline' and 'none' directives are unchanged"))
	}

	got, n = nonceHeader(t, "default-src 'self' data:; script-src 'self'")
	d := directives(got)
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'self' data: 'nonce-"+n+"'" || d["default-src"] != "'self' data:" {
		t.Error(test.DiffMessage(got, "script-src with the nonce and style-src copied from default-src plus the nonce", "R32: an absent style-src follows default-src"))
	}
	if order := directiveOrder(got); len(order) < 2 || order[0] != "default-src" || order[1] != "style-src" {
		t.Error(test.DiffMessage(order, "style-src right after default-src", "R32: the inserted directive follows default-src"))
	}

	if got, _ := nonceHeader(t, "img-src 'self'; font-src 'self'"); got != "img-src 'self'; font-src 'self'" {
		t.Error(test.DiffMessage(got, "img-src 'self'; font-src 'self'", "R32: no default-src and no directive, unchanged"))
	}
	if got, _ := nonceHeader(t, "default-src 'none'; img-src 'self'"); got != "default-src 'none'; img-src 'self'" {
		t.Error(test.DiffMessage(got, "default-src 'none'; img-src 'self'", "R32: default-src 'none' is unchanged"))
	}
	if got, _ := nonceHeader(t, "default-src 'self' 'unsafe-inline'; img-src 'self'"); got != "default-src 'self' 'unsafe-inline'; img-src 'self'" {
		t.Error(test.DiffMessage(got, "default-src 'self' 'unsafe-inline'; img-src 'self'", "R32: default-src 'unsafe-inline' is unchanged"))
	}
}

func TestHelmet_NonceDirectivesElem(t *testing.T) {
	got, n := nonceHeader(t, "default-src 'self'; script-src-elem 'self'; style-src-elem 'self' 'unsafe-inline'")
	d := directives(got)
	if d["script-src-elem"] != "'self' 'nonce-"+n+"'" {
		t.Error(test.DiffMessage(d["script-src-elem"], "'self' 'nonce-"+n+"'", "R32: script-src-elem gets the nonce"))
	}
	if d["style-src-elem"] != "'self' 'unsafe-inline'" {
		t.Error(test.DiffMessage(d["style-src-elem"], "'self' 'unsafe-inline'", "R32: style-src-elem with 'unsafe-inline' is unchanged"))
	}
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'self' 'nonce-"+n+"'" {
		t.Error(test.DiffMessage(got, "script-src and style-src inserted from default-src", "R32: absent script-src and style-src follow default-src"))
	}

	got, n = nonceHeader(t, "script-src 'self'; script-src-elem 'self'; style-src 'none'; style-src-elem 'none'")
	d = directives(got)
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["script-src-elem"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'none'" || d["style-src-elem"] != "'none'" {
		t.Error(test.DiffMessage(got, "nonce on both script directives, 'none' style directives unchanged", "R32: -elem directives follow the same rule"))
	}
}

func TestHelmet_SnapshotNonceDirectives(t *testing.T) {
	for policy, want := range map[string][]string{
		"":                                    {"script-src", "style-src"},
		"script-src 'self'; style-src 'none'": {"script-src"},
		"default-src 'self'; script-src-elem 'self'; style-src-elem 'self'": {"script-src", "style-src", "script-src-elem", "style-src-elem"},
		"default-src 'none'": {},
		"img-src 'self'":     {},
		Disabled:             {},
	} {
		mw := Helmet{ContentSecurityPolicy: policy}.NewMiddleware()
		snapshotter, ok := mw.(inspect.Snapshotter)
		if !ok {
			t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "R44: helmet implements Snapshotter"))
		}
		snap := snapshotter.Snapshot()
		got, ok := field(snap, "nonce_directives").([]string)
		if !ok {
			t.Fatal(test.DiffMessage(field(snap, "nonce_directives"), "a []string", "R44: the field is present: "+policy))
		}
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !sameSet(got, want) {
			t.Error(test.DiffMessage(got, want, "R44: the directives that receive a nonce: "+policy))
		}
	}
}
