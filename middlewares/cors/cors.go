package cors

import (
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

const (
	defaultMaxAge   = 10 * time.Minute
	anyOrigin       = "*"
	maxOrigins      = 256
	maxListEntries  = 64
	maxOriginLength = 256

	originHeader              = "Origin"
	varyHeader                = "Vary"
	upgradeHeader             = "Upgrade"
	requestMethodHeader       = "Access-Control-Request-Method"
	requestPrivateHeader      = "Access-Control-Request-Private-Network"
	allowOriginHeader         = "Access-Control-Allow-Origin"
	allowCredentialsHeader    = "Access-Control-Allow-Credentials"
	exposeHeadersHeader       = "Access-Control-Expose-Headers"
	allowMethodsHeader        = "Access-Control-Allow-Methods"
	allowHeadersHeader        = "Access-Control-Allow-Headers"
	maxAgeHeader              = "Access-Control-Max-Age"
	allowPrivateNetworkHeader = "Access-Control-Allow-Private-Network"
	contentLengthHeader       = "Content-Length"
	varyOriginToken           = "Origin"
	trueValue                 = "true"
	websocketUpgradeToken     = "websocket"
)

var (
	defaultAllowMethods = []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodPost,
		http.MethodDelete,
	}
	defaultAllowHeaders = []string{"Content-Type", "Authorization"}

	originNotAllowed = exception.ForbiddenException("Origin not allowed")

	varyOrigin    = []string{varyOriginToken}
	anyOriginList = []string{anyOrigin}
	trueList      = []string{trueValue}
	zeroLength    = []string{"0"}
)

type CORS struct {
	AllowOrigins          []string
	AllowMethods          []string
	AllowHeaders          []string
	ExposeHeaders         []string
	MaxAge                time.Duration
	IsAllowCredentials    bool
	IsAllowPrivateNetwork bool
}

type wildcardOrigin struct {
	prefix string
	suffix string
}

type compiledCORS struct {
	isAnyOrigin           bool
	isAllowCredentials    bool
	isAllowPrivateNetwork bool
	origins               map[string]struct{}
	wildcards             []wildcardOrigin

	allowOrigins  []string
	allowMethods  []string
	allowHeaders  []string
	exposeHeaders []string
	maxAge        time.Duration

	exposeValue  []string
	methodsValue []string
	headersValue []string
	maxAgeValue  []string

	preflights atomic.Int64
	refused    atomic.Int64
}

func (instance CORS) NewMiddleware() common.MiddlewareFn {
	return loadCORSOptions(&instance)
}

func (instance CORS) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadCORSOptions(cors *CORS) *compiledCORS {
	if len(cors.AllowOrigins) == 0 {
		panic(color.FmtRed("invalid cors: AllowOrigins is empty"))
	}
	if len(cors.AllowOrigins) > maxOrigins {
		panic(color.FmtRed("invalid cors: AllowOrigins has %d entries, at most %d are allowed", len(cors.AllowOrigins), maxOrigins))
	}
	c := &compiledCORS{
		isAllowCredentials:    cors.IsAllowCredentials,
		isAllowPrivateNetwork: cors.IsAllowPrivateNetwork,
		origins:               make(map[string]struct{}, len(cors.AllowOrigins)),
		allowOrigins:          append([]string(nil), cors.AllowOrigins...),
		allowMethods:          loadList("AllowMethods", cors.AllowMethods, defaultAllowMethods),
		allowHeaders:          loadList("AllowHeaders", cors.AllowHeaders, defaultAllowHeaders),
		exposeHeaders:         loadList("ExposeHeaders", cors.ExposeHeaders, nil),
		maxAge:                cors.MaxAge,
	}
	for _, origin := range cors.AllowOrigins {
		if origin == anyOrigin {
			if len(cors.AllowOrigins) > 1 {
				panic(color.FmtRed("invalid cors: \"*\" must be the only entry of AllowOrigins"))
			}
			if cors.IsAllowCredentials {
				panic(color.FmtRed("invalid cors: \"*\" cannot be combined with IsAllowCredentials (browsers reject it, and echoing the origin would let any site use the credentials)"))
			}
			c.isAnyOrigin = true
			continue
		}
		c.addOrigin(origin)
	}
	if c.maxAge <= 0 {
		c.maxAge = defaultMaxAge
	}
	if len(c.exposeHeaders) > 0 {
		c.exposeValue = []string{strings.Join(c.exposeHeaders, ", ")}
	}
	c.methodsValue = []string{strings.Join(c.allowMethods, ", ")}
	c.headersValue = []string{strings.Join(c.allowHeaders, ", ")}
	c.maxAgeValue = []string{strconv.FormatInt(int64(c.maxAge/time.Second), 10)}
	return c
}

func (c *compiledCORS) addOrigin(origin string) {
	scheme, host, ok := strings.Cut(origin, "://")
	if !ok || !isScheme(scheme) || len(origin) > maxOriginLength {
		panicOrigin(origin)
	}
	if rest, isWildcard := strings.CutPrefix(host, "*."); isWildcard {
		if !isOriginHost(rest) {
			panicOrigin(origin)
		}
		c.wildcards = append(c.wildcards, wildcardOrigin{prefix: scheme + "://", suffix: "." + rest})
		return
	}
	if !isOriginHost(host) {
		panicOrigin(origin)
	}
	c.origins[origin] = struct{}{}
}

func panicOrigin(origin string) {
	panic(color.FmtRed("invalid cors: AllowOrigins entry %q must be scheme://host[:port] in lower case, scheme://*.host[:port] or \"*\" alone", origin))
}

func isScheme(scheme string) bool {
	if scheme == "" || scheme[0] < 'a' || scheme[0] > 'z' {
		return false
	}
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func isOriginHost(host string) bool {
	name, port, hasPort, ok := tracecontext.SplitHostPort(host)
	if !ok || (hasPort && !tracecontext.IsPort(port)) {
		return false
	}
	if name[0] == '[' {
		ip, err := netip.ParseAddr(name[1 : len(name)-1])
		return err == nil && ip.Is6() && ip.Zone() == ""
	}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i < len(name) && name[i] != '.' {
			ch := name[i]
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
			continue
		}
		if i == start {
			return false
		}
		start = i + 1
	}
	return true
}

func loadList(field string, values, defaults []string) []string {
	if len(values) == 0 {
		return append([]string(nil), defaults...)
	}
	if len(values) > maxListEntries {
		panic(color.FmtRed("invalid cors: %s has %d entries, at most %d are allowed", field, len(values), maxListEntries))
	}
	for _, value := range values {
		if !isToken(value) {
			panic(color.FmtRed("invalid cors: %s entry %q is not an RFC 9110 token", field, value))
		}
	}
	return append([]string(nil), values...)
}

func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		if strings.IndexByte("!#$%&'*+-.^_`|~", c) < 0 {
			return false
		}
	}
	return true
}

func (c *compiledCORS) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	origin, hasOrigin := headerValue(r.Header, originHeader)
	if !hasOrigin {
		if !c.isAnyOrigin {
			addVaryOrigin(w.Header())
		}
		next()
		return
	}
	if isWebSocketUpgrade(r.Header) {
		if !c.matches(origin) {
			c.refused.Add(1)
			panic(&originNotAllowed)
		}
		next()
		return
	}
	h := w.Header()
	if !c.isAnyOrigin {
		addVaryOrigin(h)
	}
	isPreflight := r.Method == http.MethodOptions && len(r.Header[requestMethodHeader]) != 0
	if !c.matches(origin) {
		c.refused.Add(1)
		if isPreflight {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next()
		return
	}
	if c.isAnyOrigin {
		h[allowOriginHeader] = anyOriginList
	} else {
		h[allowOriginHeader] = []string{origin}
	}
	if c.isAllowCredentials {
		h[allowCredentialsHeader] = trueList
	}
	if c.exposeValue != nil {
		h[exposeHeadersHeader] = c.exposeValue
	}
	if !isPreflight {
		next()
		return
	}
	c.preflights.Add(1)
	h[allowMethodsHeader] = c.methodsValue
	h[allowHeadersHeader] = c.headersValue
	h[maxAgeHeader] = c.maxAgeValue
	if c.isAllowPrivateNetwork {
		if value, _ := headerValue(r.Header, requestPrivateHeader); value == trueValue {
			h[allowPrivateNetworkHeader] = trueList
		}
	}
	h[contentLengthHeader] = zeroLength
	w.WriteHeader(http.StatusNoContent)
}

func (c *compiledCORS) matches(origin string) bool {
	if c.isAnyOrigin {
		return true
	}
	if len(origin) > maxOriginLength {
		return false
	}
	if _, ok := c.origins[origin]; ok {
		return true
	}
	for _, wildcard := range c.wildcards {
		if len(origin) <= len(wildcard.prefix)+len(wildcard.suffix) ||
			!strings.HasPrefix(origin, wildcard.prefix) || !strings.HasSuffix(origin, wildcard.suffix) {
			continue
		}
		if isWildcardPart(origin[len(wildcard.prefix) : len(origin)-len(wildcard.suffix)]) {
			return true
		}
	}
	return false
}

func isWildcardPart(part string) bool {
	for i := 0; i < len(part); i++ {
		c := part[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func headerValue(h http.Header, key string) (string, bool) {
	if values := h[key]; len(values) > 0 {
		return values[0], true
	}
	return "", false
}

func isWebSocketUpgrade(h http.Header) bool {
	for _, value := range h[upgradeHeader] {
		if strings.EqualFold(value, websocketUpgradeToken) {
			return true
		}
	}
	return false
}

func addVaryOrigin(h http.Header) {
	existing, ok := h[varyHeader]
	if !ok {
		h[varyHeader] = varyOrigin
		return
	}
	for _, value := range existing {
		for token := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), varyOriginToken) {
				return
			}
		}
	}
	h[varyHeader] = append(existing, varyOriginToken)
}

func (c *compiledCORS) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "cors", Fields: []inspect.Field{
		{Key: "allow_origins", Value: append([]string(nil), c.allowOrigins...)},
		{Key: "allow_methods", Value: append([]string(nil), c.allowMethods...)},
		{Key: "allow_headers", Value: append([]string(nil), c.allowHeaders...)},
		{Key: "expose_headers", Value: append([]string(nil), c.exposeHeaders...)},
		{Key: "max_age", Value: c.maxAge},
		{Key: "allow_credentials", Value: c.isAllowCredentials},
		{Key: "allow_private_network", Value: c.isAllowPrivateNetwork},
		{Key: "preflights", Value: c.preflights.Load()},
		{Key: "refused", Value: c.refused.Load()},
	}}
}
