package helmet

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

const (
	defaultCSP         = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests"
	defaultPermissions = "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()"
	defaultHSTS        = "max-age=63072000; includeSubDomains"
)

var defaultsOverHTTPS = map[string]string{
	"Content-Security-Policy":           defaultCSP,
	"Cross-Origin-Opener-Policy":        "same-origin",
	"Cross-Origin-Resource-Policy":      "same-origin",
	"Permissions-Policy":                defaultPermissions,
	"Referrer-Policy":                   "no-referrer",
	"Strict-Transport-Security":         defaultHSTS,
	"X-Frame-Options":                   "DENY",
	"X-Content-Type-Options":            "nosniff",
	"X-XSS-Protection":                  "0",
	"X-DNS-Prefetch-Control":            "off",
	"X-Permitted-Cross-Domain-Policies": "none",
	"Origin-Agent-Cluster":              "?1",
}

var fixedHeaders = []string{"X-Content-Type-Options", "X-XSS-Protection", "X-DNS-Prefetch-Control", "X-Permitted-Cross-Domain-Policies", "Origin-Agent-Cluster"}

func secure(target string) *http.Request {
	r := request(http.MethodGet, target)
	r.TLS = &tls.ConnectionState{}
	return r
}

func expectHeader(t *testing.T, w *httptest.ResponseRecorder, key, want, desc string) {
	t.Helper()
	got := w.Header().Values(key)
	if want == "" {
		if len(got) != 0 {
			t.Error(test.DiffMessage(got, []string{}, desc+": "+key+" is not sent"))
		}
		return
	}
	if len(got) != 1 || got[0] != want {
		t.Error(test.DiffMessage(got, []string{want}, desc+": "+key))
	}
}

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

type headerProbe struct{ Name string }

var seen sync.Map

func (p headerProbe) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	seen.Store(p.Name, w.Header().Clone())
	next()
}

type headerOverride struct{ Key, Value string }

func (o headerOverride) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set(o.Key, o.Value)
	next()
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
