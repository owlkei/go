package httpsredirect

import (
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

const (
	locationHeader = "Location"
	defaultPort    = 443
	maxPort        = 65535
	hexDigits      = "0123456789ABCDEF"
)

var invalidHost = exception.BadRequestException("Invalid host")

type HTTPSRedirect struct {
	Host string
	Port int
}

type compiledHTTPSRedirect struct {
	host       string
	port       int
	portSuffix string
	redirected atomic.Int64
}

func (instance HTTPSRedirect) NewMiddleware() common.MiddlewareFn {
	return loadHTTPSRedirectOptions(&instance)
}

func (instance HTTPSRedirect) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadHTTPSRedirectOptions(h *HTTPSRedirect) *compiledHTTPSRedirect {
	if h.Host != "" && (!tracecontext.IsValidHost(h.Host) || tracecontext.HostName(h.Host) != h.Host) {
		panic(color.FmtRed("invalid httpsredirect: Host %q must be a host name or IP literal without a port", h.Host))
	}
	if h.Port < 0 || h.Port > maxPort {
		panic(color.FmtRed("invalid httpsredirect: Port %d is outside 0-%d", h.Port, maxPort))
	}
	c := &compiledHTTPSRedirect{host: h.Host, port: h.Port}
	if h.Port != 0 && h.Port != defaultPort {
		c.portSuffix = ":" + strconv.Itoa(h.Port)
	}
	return c
}

func (c *compiledHTTPSRedirect) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if tracecontext.IsHTTPS(r) {
		next()
		return
	}
	host := c.host
	if host == "" {
		requestHost := tracecontext.Host(r)
		if !tracecontext.IsValidHost(requestHost) {
			panic(&invalidHost)
		}
		host = tracecontext.HostName(requestHost)
	}
	c.redirected.Add(1)
	w.Header()[locationHeader] = []string{c.location(r, host)}
	w.WriteHeader(http.StatusPermanentRedirect)
}

func (c *compiledHTTPSRedirect) location(r *http.Request, host string) string {
	var stack [256]byte
	buf := append(stack[:0], "https://"...)
	buf = append(buf, host...)
	buf = append(buf, c.portSuffix...)
	path := r.URL.EscapedPath()
	if path == "" || path[0] != '/' {
		buf = append(buf, '/')
	}
	buf = append(buf, path...)
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		buf = append(buf, '?')
		buf = appendQuery(buf, r.URL.RawQuery)
	}
	return string(buf)
}

func appendQuery(buf []byte, query string) []byte {
	for i := 0; i < len(query); i++ {
		b := query[i]
		if b > ' ' && b < 0x7f && b != '#' {
			buf = append(buf, b)
			continue
		}
		buf = append(buf, '%', hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return buf
}

func (c *compiledHTTPSRedirect) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "httpsredirect", Fields: []inspect.Field{
		{Key: "host", Value: c.host},
		{Key: "port", Value: int64(c.port)},
		{Key: "redirected", Value: c.redirected.Load()},
	}}
}
