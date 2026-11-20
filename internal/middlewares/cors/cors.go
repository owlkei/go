package cors

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/middlewares/origin"
)

const (
	defaultMaxAge  = 10 * time.Minute
	maxListEntries = 64

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
	anyOriginList = []string{origin.Any}
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

type compiledCORS struct {
	isAnyOrigin           bool
	isAllowCredentials    bool
	isAllowPrivateNetwork bool
	origins               *origin.Set

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
	if len(instance.AllowOrigins) == 0 {
		panic(color.FmtRed("invalid cors: AllowOrigins is empty"))
	}
	origins, err := origin.Parse("AllowOrigins", instance.AllowOrigins)
	if err == nil && origins.IsAny() && instance.IsAllowCredentials {
		err = errors.New(`"*" cannot be combined with IsAllowCredentials (browsers reject it, and echoing the origin would let any site use the credentials)`)
	}
	if err != nil {
		panic(color.FmtRed("invalid cors: " + err.Error()))
	}
	c, err := Compile(&instance, origins)
	if err != nil {
		panic(color.FmtRed("invalid cors: " + err.Error()))
	}
	return c
}

func (instance CORS) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func Compile(cors *CORS, origins *origin.Set) (*compiledCORS, error) {
	c := &compiledCORS{
		isAnyOrigin:           origins.IsAny(),
		isAllowCredentials:    cors.IsAllowCredentials,
		isAllowPrivateNetwork: cors.IsAllowPrivateNetwork,
		origins:               origins,
		maxAge:                cors.MaxAge,
	}
	var err error
	if c.allowMethods, err = loadList("AllowMethods", cors.AllowMethods, defaultAllowMethods); err != nil {
		return nil, err
	}
	if c.allowHeaders, err = loadList("AllowHeaders", cors.AllowHeaders, defaultAllowHeaders); err != nil {
		return nil, err
	}
	if c.exposeHeaders, err = loadList("ExposeHeaders", cors.ExposeHeaders, nil); err != nil {
		return nil, err
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
	return c, nil
}

func loadList(field string, values, defaults []string) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), defaults...), nil
	}
	if len(values) > maxListEntries {
		return nil, fmt.Errorf("%s has %d entries, at most %d are allowed", field, len(values), maxListEntries)
	}
	for _, value := range values {
		if !isToken(value) {
			return nil, fmt.Errorf("%s entry %q is not an RFC 9110 token", field, value)
		}
	}
	return append([]string(nil), values...), nil
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
	value, hasOrigin := headerValue(r.Header, originHeader)
	if !hasOrigin {
		if !c.isAnyOrigin {
			addVaryOrigin(w.Header())
		}
		next()
		return
	}
	if isWebSocketUpgrade(r.Header) {
		if !c.matches(value) {
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
	if !c.matches(value) {
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
		h[allowOriginHeader] = []string{value}
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
		if private, _ := headerValue(r.Header, requestPrivateHeader); private == trueValue {
			h[allowPrivateNetworkHeader] = trueList
		}
	}
	h[contentLengthHeader] = zeroLength
	w.WriteHeader(http.StatusNoContent)
}

func (c *compiledCORS) matches(value string) bool {
	return c.origins.Matches(value)
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
		{Key: "allow_origins", Value: c.origins.Entries()},
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
