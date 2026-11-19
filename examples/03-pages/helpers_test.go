package main

import (
	"io"
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
	nonceAttr  = regexp.MustCompile(`<script nonce="([0-9a-f]{32})">`)
	scriptSrc  = regexp.MustCompile(`<script src="(/assets/owlky\.js\?v=([0-9a-f]{16}))"`)
	layoutLink = regexp.MustCompile(`<link rel="stylesheet" href="(/assets/_pages/layout\.css\?v=[0-9a-f]{16})">`)
)
