package helmet

import (
	"net/http"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

const Disabled = "-"

const (
	defaultContentSecurityPolicy     = "default-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests"
	defaultCrossOriginEmbedderPolicy = Disabled
	defaultCrossOriginOpenerPolicy   = "same-origin"
	defaultCrossOriginResourcePolicy = "same-origin"
	defaultFrameOptions              = "DENY"
	defaultPermissionsPolicy         = "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), browsing-topics=()"
	defaultReferrerPolicy            = "no-referrer"
	defaultStrictTransportSecurity   = "max-age=63072000; includeSubDomains"
	strictTransportSecurityHeader    = "Strict-Transport-Security"
)

var alwaysSent = [][2]string{
	{"X-Content-Type-Options", "nosniff"},
	{"X-XSS-Protection", "0"},
	{"X-DNS-Prefetch-Control", "off"},
	{"X-Permitted-Cross-Domain-Policies", "none"},
	{"Origin-Agent-Cluster", "?1"},
}

type Helmet struct {
	ContentSecurityPolicy     string
	CrossOriginEmbedderPolicy string
	CrossOriginOpenerPolicy   string
	CrossOriginResourcePolicy string
	FrameOptions              string
	PermissionsPolicy         string
	ReferrerPolicy            string
	StrictTransportSecurity   string
}

type header struct {
	key   string
	value []string
}

type compiledHelmet struct {
	headers   []header
	hsts      []string
	effective []inspect.Field
}

func (instance Helmet) NewMiddleware() common.MiddlewareFn {
	return loadHelmetOptions(&instance)
}

func (instance Helmet) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadHelmetOptions(h *Helmet) *compiledHelmet {
	c := &compiledHelmet{}
	configurable := [][3]string{
		{"Content-Security-Policy", h.ContentSecurityPolicy, defaultContentSecurityPolicy},
		{"Cross-Origin-Embedder-Policy", h.CrossOriginEmbedderPolicy, defaultCrossOriginEmbedderPolicy},
		{"Cross-Origin-Opener-Policy", h.CrossOriginOpenerPolicy, defaultCrossOriginOpenerPolicy},
		{"Cross-Origin-Resource-Policy", h.CrossOriginResourcePolicy, defaultCrossOriginResourcePolicy},
		{"X-Frame-Options", h.FrameOptions, defaultFrameOptions},
		{"Permissions-Policy", h.PermissionsPolicy, defaultPermissionsPolicy},
		{"Referrer-Policy", h.ReferrerPolicy, defaultReferrerPolicy},
		{strictTransportSecurityHeader, h.StrictTransportSecurity, defaultStrictTransportSecurity},
	}
	for _, field := range configurable {
		key, value := field[0], field[1]
		if hasControlCharacter(value) {
			panic(color.FmtRed("invalid helmet: the %s value has a control character", key))
		}
		if value == "" {
			value = field[2]
		}
		c.effective = append(c.effective, inspect.Field{Key: key, Value: value})
		if value == Disabled {
			continue
		}
		if key == strictTransportSecurityHeader {
			c.hsts = []string{value}
			continue
		}
		c.headers = append(c.headers, header{key: http.CanonicalHeaderKey(key), value: []string{value}})
	}
	for _, fixed := range alwaysSent {
		c.headers = append(c.headers, header{key: http.CanonicalHeaderKey(fixed[0]), value: []string{fixed[1]}})
	}
	return c
}

func hasControlCharacter(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func (c *compiledHelmet) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	h := w.Header()
	for i := range c.headers {
		h[c.headers[i].key] = c.headers[i].value
	}
	if c.hsts != nil && tracecontext.IsHTTPS(r) {
		h[strictTransportSecurityHeader] = c.hsts
	}
	next()
}

func (c *compiledHelmet) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "helmet", Fields: append([]inspect.Field(nil), c.effective...)}
}
