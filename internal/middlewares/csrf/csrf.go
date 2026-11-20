package csrf

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/middlewares/origin"
	"go.owlkei.com/owlkei/internal/tracecontext"
)

const (
	maxTrustedOrigins = 64
	maxOriginLength   = 256
	originHeader      = "Origin"
	fetchSiteHeader   = "Sec-Fetch-Site"
)

var refusal = exception.ForbiddenException("Cross-origin request refused")

type CSRF struct {
	TrustedOrigins []string
}

type compiledCSRF struct {
	protection     *http.CrossOriginProtection
	origins        *origin.Set
	trustedOrigins []string
	refused        atomic.Int64
}

func (instance CSRF) NewMiddleware() common.MiddlewareFn {
	c, err := Compile(&instance, nil)
	if err != nil {
		panic(color.FmtRed("invalid csrf: " + err.Error()))
	}
	return c
}

func (instance CSRF) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func Compile(c *CSRF, origins *origin.Set) (*compiledCSRF, error) {
	if len(c.TrustedOrigins) > maxTrustedOrigins {
		return nil, fmt.Errorf("TrustedOrigins has %d entries, at most %d are allowed", len(c.TrustedOrigins), maxTrustedOrigins)
	}
	compiled := &compiledCSRF{
		protection:     http.NewCrossOriginProtection(),
		origins:        origins,
		trustedOrigins: append([]string(nil), c.TrustedOrigins...),
	}
	for _, trusted := range c.TrustedOrigins {
		if err := compiled.protection.AddTrustedOrigin(trusted); err != nil {
			return nil, fmt.Errorf("TrustedOrigins entry %q must be scheme://host[:port]", trusted)
		}
	}
	if origins != nil {
		compiled.trustedOrigins = append(compiled.trustedOrigins, origins.Entries()...)
	}
	return compiled, nil
}

func (c *compiledCSRF) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		next()
		return
	}
	value := headerValue(r.Header, originHeader)
	if len(value) > maxOriginLength {
		c.refuse()
	}
	if value != "" && c.origins != nil && c.origins.Matches(value) {
		next()
		return
	}
	if value != "" && len(r.Header[fetchSiteHeader]) == 0 && isForwardedHost(r) && originHost(value) == tracecontext.Host(r) {
		next()
		return
	}
	if c.protection.Check(r) != nil {
		c.refuse()
	}
	next()
}

func (c *compiledCSRF) refuse() {
	c.refused.Add(1)
	panic(&refusal)
}

func headerValue(h http.Header, key string) string {
	if values := h[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func isForwardedHost(r *http.Request) bool {
	s := tracecontext.ScopeOf(r.Context())
	return s != nil && s.IsClientSet && s.ClientHost != ""
}

func originHost(value string) string {
	for i := 0; i+2 < len(value); i++ {
		if value[i] == ':' && value[i+1] == '/' && value[i+2] == '/' {
			host := value[i+3:]
			for j := 0; j < len(host); j++ {
				switch host[j] {
				case '/', '?', '#', '@', '\\':
					return ""
				}
			}
			return host
		}
	}
	return ""
}

func (c *compiledCSRF) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "csrf", Fields: []inspect.Field{
		{Key: "trusted_origins", Value: append([]string(nil), c.trustedOrigins...)},
		{Key: "refused", Value: c.refused.Load()},
	}}
}
