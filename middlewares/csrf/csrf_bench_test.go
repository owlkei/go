package csrf

import (
	"net/http"
	"testing"
)

func BenchmarkGenerateCSRFToken(b *testing.B) {
	for b.Loop() {
		_, _ = GenerateCSRFToken(32)
	}
}

func BenchmarkCompareTokensSecurely_Equal(b *testing.B) {
	tok, _ := GenerateCSRFToken(32)
	for b.Loop() {
		CompareTokensSecurely(tok, tok)
	}
}

func BenchmarkCompareTokensSecurely_Unequal(b *testing.B) {
	a, _ := GenerateCSRFToken(32)
	c, _ := GenerateCSRFToken(32)
	for b.Loop() {
		CompareTokensSecurely(a, c)
	}
}

func BenchmarkCSRF_SafeMethod_NoCookie(b *testing.B) {
	mw := CSRF{}.NewMiddleware()
	for b.Loop() {
		c, _ := newCSRFContext(http.MethodGet, "", "")
		mw.Use(c.Request, c.ResponseWriter, noop)
	}
}

func BenchmarkCSRF_SafeMethod_WithCookie(b *testing.B) {
	mw := CSRF{}.NewMiddleware()
	for b.Loop() {
		c, _ := newCSRFContext(http.MethodGet, "", "existingtoken123456")
		mw.Use(c.Request, c.ResponseWriter, noop)
	}
}

func BenchmarkCSRF_POST_ValidHeader(b *testing.B) {
	mw := CSRF{}.NewMiddleware()
	for b.Loop() {
		c, _ := newCSRFContextWithHeader(http.MethodPost, csrfDefaultHeaderName, "tok", "tok")
		mw.Use(c.Request, c.ResponseWriter, noop)
	}
}

func BenchmarkLoadCSRFOptions(b *testing.B) {
	cfg := &CSRF{TokenLength: 32, CookieName: "_csrf", HeaderName: "X-CSRF-Token"}
	for b.Loop() {
		loadCSRFOptions(cfg)
	}
}
