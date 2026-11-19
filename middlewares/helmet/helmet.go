package helmet

import (
	"net/http"
	"strings"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/httphook"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/tracecontext"
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
	contentSecurityPolicyHeader      = "Content-Security-Policy"
	defaultSrc                       = "default-src"
	nonceSource                      = " 'nonce-"
)

var nonceTargets = [...]struct {
	name       string
	isInserted bool
}{
	{"script-src", true},
	{"style-src", true},
	{"script-src-elem", false},
	{"style-src-elem", false},
}

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
	headers         []header
	hsts            []string
	csp             *httphook.CSP
	nonceDirectives []string
	effective       []inspect.Field
}

func (instance Helmet) NewMiddleware() common.MiddlewareFn {
	return loadHelmetOptions(&instance)
}

func (instance Helmet) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadHelmetOptions(h *Helmet) *compiledHelmet {
	c := &compiledHelmet{nonceDirectives: []string{}}
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
		compiled := header{key: http.CanonicalHeaderKey(key), value: []string{value}}
		if key == contentSecurityPolicyHeader {
			c.csp, c.nonceDirectives = compileNonce(compiled.value)
		}
		c.headers = append(c.headers, compiled)
	}
	for _, fixed := range alwaysSent {
		c.headers = append(c.headers, header{key: http.CanonicalHeaderKey(fixed[0]), value: []string{fixed[1]}})
	}
	return c
}

func compileNonce(value []string) (*httphook.CSP, []string) {
	var directives []string
	for d := range strings.SplitSeq(value[0], ";") {
		if d = strings.TrimSpace(d); d != "" {
			directives = append(directives, d)
		}
	}
	indexByName := map[string]int{}
	for i, d := range directives {
		name, _, _ := strings.Cut(d, " ")
		name = strings.ToLower(name)
		if _, ok := indexByName[name]; !ok {
			indexByName[name] = i
		}
	}

	names := []string{}
	isNonced := make([]bool, len(directives))
	var inserted []string
	defaultIndex, hasDefault := indexByName[defaultSrc]
	defaultSources := ""
	if hasDefault {
		defaultSources = sourcesOf(directives[defaultIndex])
	}
	for _, target := range nonceTargets {
		if i, ok := indexByName[target.name]; ok {
			if allowsNonce(sourcesOf(directives[i])) {
				isNonced[i] = true
				names = append(names, target.name)
			}
			continue
		}
		if target.isInserted && hasDefault && allowsNonce(defaultSources) {
			inserted = append(inserted, target.name+" "+defaultSources)
			names = append(names, target.name)
		}
	}
	if len(names) == 0 {
		return nil, names
	}

	var parts []string
	var b strings.Builder
	cut := func() {
		b.WriteString(nonceSource)
		parts = append(parts, b.String())
		b.Reset()
		b.WriteByte('\'')
	}
	for i, d := range directives {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(d)
		if isNonced[i] {
			cut()
		}
		if hasDefault && i == defaultIndex {
			for _, extra := range inserted {
				b.WriteString("; ")
				b.WriteString(extra)
				cut()
			}
		}
	}
	parts = append(parts, b.String())
	return &httphook.CSP{Value: value, Parts: parts}, names
}

func sourcesOf(directive string) string {
	_, sources, _ := strings.Cut(directive, " ")
	return strings.TrimSpace(sources)
}

func allowsNonce(sources string) bool {
	if sources == "" {
		return false
	}
	for source := range strings.FieldsSeq(sources) {
		if strings.EqualFold(source, "'unsafe-inline'") || strings.EqualFold(source, "'none'") {
			return false
		}
	}
	return true
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
	if c.csp != nil {
		if cw, ok := w.(httphook.CSPWriter); ok {
			cw.SetCSP(c.csp)
		}
	}
	next()
}

func (c *compiledHelmet) Snapshot() inspect.Snapshot {
	fields := append(make([]inspect.Field, 0, len(c.effective)+1), c.effective...)
	fields = append(fields, inspect.Field{Key: "nonce_directives", Value: append([]string{}, c.nonceDirectives...)})
	return inspect.Snapshot{Name: "helmet", Fields: fields}
}
