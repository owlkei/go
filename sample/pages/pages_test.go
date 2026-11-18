package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func send(t *testing.T, app http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Accept", "text/html")
	// csrf.CSRF refuses unsafe requests that a browser did not mark as same-origin.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func wantHTML(t *testing.T, step string, rec *httptest.ResponseRecorder, code int, fragments ...string) string {
	t.Helper()

	body := rec.Body.String()
	if rec.Code != code {
		t.Fatalf("%s: status %d, want %d\n%s", step, rec.Code, code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("%s: Content-Type %q, want text/html; charset=utf-8", step, ct)
	}
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			t.Fatalf("%s: body lacks %q\n%s", step, fragment, body)
		}
	}
	return body
}

var (
	nonceAttr = regexp.MustCompile(`<script nonce="([0-9a-f]{32})">`)
	scriptSrc = regexp.MustCompile(`<script src="(/assets/owlky\.js\?v=([0-9a-f]{16}))"`)
)

func TestPages_FormFlow(t *testing.T) {
	app := newApp(web, false)

	// The empty form.
	rec := send(t, app, http.MethodGet, "/bookings/new", nil)
	page := wantHTML(t, "form page", rec, http.StatusOK,
		`<form method="post" action="/bookings" novalidate>`,
		`<input id="field-guest" name="guest" type="text" data-ok-text-field-input placeholder=" " value="">`,
		`name="nights" type="number" data-ok-text-field-input aria-describedby="field-nights-helper-text" placeholder=" " value="1">`,
		`<link rel="stylesheet" href="/assets/owlky.min.css?v=`,
	)

	// The CSP header holds the nonce of the page's inline script.
	nonce := nonceAttr.FindStringSubmatch(page)
	if nonce == nil {
		t.Fatalf("form page: no inline script with a nonce\n%s", page)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' 'nonce-"+nonce[1]+"'") {
		t.Fatalf("form page: Content-Security-Policy %q lacks the page's nonce %s", csp, nonce[1])
	}

	// An invalid form is shown again with 422, its messages and the submitted values, escaped.
	rec = send(t, app, http.MethodPost, "/bookings", url.Values{
		"guest":  {`"><script>alert(1)</script>`},
		"email":  {"nope"},
		"date":   {"tomorrow"},
		"nights": {"0"},
	})
	body := wantHTML(t, "invalid post", rec, http.StatusUnprocessableEntity,
		`<div data-ok-alert-message>Check the fields marked below.</div>`,
		`value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`,
		`value="nope"`,
		`value="tomorrow"`,
		`<div data-ok-text-field-helper id="field-email-helper-text">Enter an email address such as ann@example.com</div>`,
		`<div data-ok-text-field-helper id="field-date-helper-text">Enter the date as YYYY-MM-DD</div>`,
		`<div data-ok-text-field-helper id="field-nights-helper-text">Enter a number of nights from 1 to 30</div>`,
		`data-ok-error data-ok-full-width`,
		`aria-invalid="true"`,
	)
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatal("invalid post: the submitted script reached the page unescaped")
	}

	// Required fields.
	rec = send(t, app, http.MethodPost, "/bookings", url.Values{})
	wantHTML(t, "empty post", rec, http.StatusUnprocessableEntity,
		"Enter the guest&#39;s name", "Enter an email address", "Choose an arrival date", "Enter the number of nights")

	// A valid form is stored and the browser goes to the list.
	rec = send(t, app, http.MethodPost, "/bookings", url.Values{
		"guest":  {"Ann <Lee>"},
		"email":  {"ann@example.com"},
		"date":   {"2026-12-01"},
		"nights": {"3"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/bookings" {
		t.Fatalf("valid post: status %d, Location %q, want 303 /bookings", rec.Code, rec.Header().Get("Location"))
	}

	// The list shows the booking.
	wantHTML(t, "list", send(t, app, http.MethodGet, "/bookings", nil), http.StatusOK,
		`<a href="/bookings/1" data-ok-typography data-ok-link`,
		"<td>Ann &lt;Lee&gt;</td>",
		"<td>1 Dec 2026</td>",
		`<td data-ok-align="right">3</td>`,
	)
	wantHTML(t, "detail", send(t, app, http.MethodGet, "/bookings/1", nil), http.StatusOK,
		"<title>Booking #1 · Ginject pages</title>",
		"Arrives on Tuesday, 1 December 2026 for 3 nights.",
	)

	// An unknown booking is the HTML error page, inside the layout.
	wantHTML(t, "unknown id", send(t, app, http.MethodGet, "/bookings/99", nil), http.StatusNotFound,
		"<title>Not Found · Ginject pages</title>",
		`<div data-ok-alert-message>No booking has this number</div>`,
	)

	// owlky.js with its hash is cached for good.
	src := scriptSrc.FindStringSubmatch(page)
	if src == nil {
		t.Fatalf("form page: no owlky.js script with a version\n%s", page)
	}
	js, err := fs.ReadFile(web, "web/assets/owlky.js")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(js)
	if want := hex.EncodeToString(sum[:8]); src[2] != want {
		t.Fatalf("asset version %s, want the first 8 bytes of SHA-256 %s", src[2], want)
	}
	rec = send(t, app, http.MethodGet, src[1], nil)
	if rec.Code != http.StatusOK || rec.Body.Len() != len(js) {
		t.Fatalf("owlky.js: status %d, %d bytes, want 200 and %d bytes", rec.Code, rec.Body.Len(), len(js))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("owlky.js: Cache-Control %q, want immutable", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("owlky.js: Content-Type %q", ct)
	}
}
