package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPages_FormFlow(t *testing.T) {
	app := newApp(web, false)

	rec := send(t, app, http.MethodGet, "/bookings/new", nil)
	page := wantHTML(t, "form page", rec, http.StatusOK,
		`<form method="post" action="/bookings/new" novalidate>`,
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
	rec = send(t, app, http.MethodPost, "/bookings/new", url.Values{
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

	rec = send(t, app, http.MethodPost, "/bookings/new", url.Values{})
	wantHTML(t, "empty post", rec, http.StatusUnprocessableEntity,
		"Enter the guest&#39;s name", "Enter an email address", "Choose an arrival date", "Enter the number of nights")

	// A valid form is stored and the browser goes to the list.
	rec = send(t, app, http.MethodPost, "/bookings/new", url.Values{
		"guest":  {"Ann <Lee>"},
		"email":  {"ann@example.com"},
		"date":   {"2026-12-01"},
		"nights": {"3"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/bookings" {
		t.Fatalf("valid post: status %d, Location %q, want 303 /bookings", rec.Code, rec.Header().Get("Location"))
	}

	wantHTML(t, "list", send(t, app, http.MethodGet, "/bookings", nil), http.StatusOK,
		`<a href="/bookings/1" data-ok-typography data-ok-link`,
		"<td>Ann &lt;Lee&gt;</td>",
		"<td>1 Dec 2026</td>",
		`<td data-ok-align="right">3</td>`,
	)
	wantHTML(t, "detail", send(t, app, http.MethodGet, "/bookings/1", nil), http.StatusOK,
		"<title>Booking #1 · Owlkei pages</title>",
		"Arrives on Tuesday, 1 December 2026 for 3 nights.",
	)

	// An unknown booking is 4xx.html, inside the layout.
	wantHTML(t, "unknown id", send(t, app, http.MethodGet, "/bookings/99", nil), http.StatusNotFound,
		"<title>Not Found · Owlkei pages</title>",
		`<div data-ok-alert-message>No booking has this number</div>`,
	)
}

func TestPages_Assets(t *testing.T) {
	app := newApp(web, false)
	page := send(t, app, http.MethodGet, "/bookings/new", nil).Body.String()
	var rec *httptest.ResponseRecorder

	// The layout's own CSS is linked by {{pageAssets}} and served with its hash for good.
	layoutCSS := layoutLink.FindStringSubmatch(page)
	if layoutCSS == nil {
		t.Fatalf("form page: no link to the layout's CSS from {{pageAssets}}\n%s", page)
	}
	rec = send(t, app, http.MethodGet, layoutCSS[1], nil)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("layout.css: status %d, %d bytes, Content-Type %q, want 200, a body and text/css", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("layout.css: Cache-Control %q, want immutable", cc)
	}
	if got := strings.Index(page, layoutCSS[1]); got < strings.Index(page, "/assets/owlky.min.css") {
		t.Fatalf("form page: the layout's CSS must come after the shared stylesheet\n%s", page)
	}

	// public/robots.txt is served at the root.
	rec = send(t, app, http.MethodGet, "/robots.txt", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("robots.txt: status %d, %d bytes, Content-Type %q, want 200, a body and text/plain", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" || rec.Header().Get("ETag") == "" {
		t.Fatalf("robots.txt: Cache-Control %q, ETag %q, want no-cache and an ETag", cc, rec.Header().Get("ETag"))
	}

	// owlky.js is linked with a version and cached for good.
	src := scriptSrc.FindStringSubmatch(page)
	if src == nil {
		t.Fatalf("form page: no owlky.js script with a version\n%s", page)
	}
	rec = send(t, app, http.MethodGet, src[1], nil)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("owlky.js: status %d, %d bytes, want 200 and a body", rec.Code, rec.Body.Len())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("owlky.js: Cache-Control %q, want immutable", cc)
	}
}
