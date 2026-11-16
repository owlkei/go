package csrf

import (
	"net/http"
	"sync/atomic"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
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
	trustedOrigins []string
	refused        atomic.Int64
}

func (instance CSRF) NewMiddleware() common.MiddlewareFn {
	return loadCSRFOptions(&instance)
}

func (instance CSRF) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadCSRFOptions(c *CSRF) *compiledCSRF {
	if len(c.TrustedOrigins) > maxTrustedOrigins {
		panic(color.FmtRed("invalid csrf: TrustedOrigins has %d entries, at most %d are allowed", len(c.TrustedOrigins), maxTrustedOrigins))
	}
	compiled := &compiledCSRF{
		protection:     http.NewCrossOriginProtection(),
		trustedOrigins: append([]string(nil), c.TrustedOrigins...),
	}
	for _, origin := range c.TrustedOrigins {
		if err := compiled.protection.AddTrustedOrigin(origin); err != nil {
			panic(color.FmtRed("invalid csrf: TrustedOrigins entry %q must be scheme://host[:port]", origin))
		}
	}
	return compiled
}

func (c *compiledCSRF) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		next()
		return
	}
	origin := headerValue(r.Header, originHeader)
	if len(origin) > maxOriginLength {
		c.refuse()
	}
	if origin != "" && len(r.Header[fetchSiteHeader]) == 0 && isForwardedHost(r) && originHost(origin) == tracecontext.Host(r) {
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

func originHost(origin string) string {
	for i := 0; i+2 < len(origin); i++ {
		if origin[i] == ':' && origin[i+1] == '/' && origin[i+2] == '/' {
			host := origin[i+3:]
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
