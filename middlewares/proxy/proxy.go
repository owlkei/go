package proxy

import (
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

type Header int

const (
	XForwarded Header = iota
	Forwarded
)

const (
	maxTrustedProxies = 256
	maxHops           = 16
	maxEntries        = 16
	maxEntryLength    = 512

	forwardedForHeader   = "X-Forwarded-For"
	forwardedProtoHeader = "X-Forwarded-Proto"
	forwardedHostHeader  = "X-Forwarded-Host"
	forwardedHeader      = "Forwarded"
)

type Proxy struct {
	TrustedProxies []string
	TrustedHops    int
	Header         Header
}

type compiledProxy struct {
	prefixes  []netip.Prefix
	hops      int
	header    Header
	resolved  atomic.Int64
	untrusted atomic.Int64
}

func (instance Proxy) NewMiddleware() common.MiddlewareFn {
	return loadProxyOptions(&instance)
}

func (instance Proxy) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func ClientIP(r *http.Request) netip.Addr {
	return tracecontext.ClientIP(r)
}

func Scheme(r *http.Request) string {
	if tracecontext.IsHTTPS(r) {
		return "https"
	}
	return "http"
}

func Host(r *http.Request) string {
	return tracecontext.Host(r)
}

func loadProxyOptions(p *Proxy) *compiledProxy {
	hasProxies := len(p.TrustedProxies) > 0
	switch {
	case hasProxies && p.TrustedHops != 0:
		panic(color.FmtRed("invalid proxy: set TrustedProxies or TrustedHops, not both"))
	case !hasProxies && p.TrustedHops == 0:
		panic(color.FmtRed("invalid proxy: set TrustedProxies or TrustedHops"))
	case len(p.TrustedProxies) > maxTrustedProxies:
		panic(color.FmtRed("invalid proxy: TrustedProxies has %d entries, at most %d are allowed", len(p.TrustedProxies), maxTrustedProxies))
	case !hasProxies && (p.TrustedHops < 1 || p.TrustedHops > maxHops):
		panic(color.FmtRed("invalid proxy: TrustedHops is %d, it must be between 1 and %d", p.TrustedHops, maxHops))
	case p.Header != XForwarded && p.Header != Forwarded:
		panic(color.FmtRed("invalid proxy: unknown Header %d", int(p.Header)))
	}
	c := &compiledProxy{hops: p.TrustedHops, header: p.Header}
	for _, entry := range p.TrustedProxies {
		c.prefixes = append(c.prefixes, parseTrusted(entry))
	}
	return c
}

func parseTrusted(entry string) netip.Prefix {
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			panic(color.FmtRed("invalid proxy: TrustedProxies entry %q is neither an IP nor a CIDR", entry))
		}
		return prefix.Masked()
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil || addr.Zone() != "" {
		panic(color.FmtRed("invalid proxy: TrustedProxies entry %q is neither an IP nor a CIDR", entry))
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen())
}

func (c *compiledProxy) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if s := tracecontext.ScopeOf(r.Context()); s != nil {
		c.resolve(r, s)
	}
	next()
}

func (c *compiledProxy) resolve(r *http.Request, s *tracecontext.Scope) {
	s.IsClientSet = false
	var res result
	if c.hops > 0 {
		res = c.walkHops(r.Header)
	} else {
		if peer := tracecontext.RemoteIP(r.RemoteAddr); !peer.IsValid() || !c.isTrusted(peer) {
			c.untrusted.Add(1)
			return
		}
		res = c.walkTrusted(r.Header)
	}
	if res.position == 0 {
		return
	}
	c.resolved.Add(1)
	s.ClientIP = res.ip
	s.IsClientHTTPS = r.TLS != nil
	s.ClientHost = ""
	if scheme, ok := c.scheme(r.Header, &res); ok {
		s.IsClientHTTPS = scheme
	}
	if host := c.host(r.Header, &res); host != "" {
		s.ClientHost = host
	}
	s.IsClientSet = true
}

func (c *compiledProxy) isTrusted(ip netip.Addr) bool {
	for _, prefix := range c.prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

type result struct {
	ip       netip.Addr
	position int
	proto    string
	host     string
}

func (c *compiledProxy) walkTrusted(h http.Header) result {
	var res result
	it := c.entries(h)
	for position := 1; position <= maxEntries; position++ {
		e, ok := it.next()
		if !ok {
			break
		}
		ip, valid := c.parse(e, &res)
		if !valid {
			break
		}
		res.ip, res.position = ip, position
		if !c.isTrusted(ip) {
			break
		}
	}
	return res
}

func (c *compiledProxy) walkHops(h http.Header) result {
	var res result
	it := c.entries(h)
	for position := 1; position <= c.hops; position++ {
		e, ok := it.next()
		if !ok {
			return result{}
		}
		ip, valid := c.parse(e, &res)
		if !valid {
			return result{}
		}
		res.ip, res.position = ip, position
	}
	return res
}

func (c *compiledProxy) entries(h http.Header) entryIterator {
	if c.header == Forwarded {
		return newEntryIterator(h[forwardedHeader])
	}
	return newEntryIterator(h[forwardedForHeader])
}

func (c *compiledProxy) parse(entry string, res *result) (netip.Addr, bool) {
	if len(entry) > maxEntryLength {
		return netip.Addr{}, false
	}
	if c.header == XForwarded {
		return parseEntry(trimSpace(entry))
	}
	element, ok := parseElement(entry)
	if !ok {
		return netip.Addr{}, false
	}
	ip, ok := parseEntry(element.forValue)
	if !ok {
		return netip.Addr{}, false
	}
	if element.hasProto {
		res.proto = element.proto
	}
	if element.hasHost {
		res.host = element.host
	}
	return ip, true
}

func (c *compiledProxy) scheme(h http.Header, res *result) (bool, bool) {
	value := res.proto
	if c.header == XForwarded {
		value = selectValue(h[forwardedProtoHeader], res.position)
	}
	switch {
	case strings.EqualFold(value, "https"):
		return true, true
	case strings.EqualFold(value, "http"):
		return false, true
	}
	return false, false
}

func (c *compiledProxy) host(h http.Header, res *result) string {
	value := res.host
	if c.header == XForwarded {
		value = selectValue(h[forwardedHostHeader], res.position)
	}
	if !tracecontext.IsValidHost(value) {
		return ""
	}
	return value
}

func selectValue(lines []string, position int) string {
	it := newEntryIterator(lines)
	var value string
	for i := 0; i < position; i++ {
		e, ok := it.next()
		if !ok {
			break
		}
		if len(e) > maxEntryLength {
			return ""
		}
		value = e
	}
	return trimSpace(value)
}

type entryIterator struct {
	lines []string
	line  int
	end   int
}

func newEntryIterator(lines []string) entryIterator {
	it := entryIterator{lines: lines, line: len(lines) - 1}
	if it.line >= 0 {
		it.end = len(lines[it.line])
	}
	return it
}

func (it *entryIterator) next() (string, bool) {
	if it.line < 0 {
		return "", false
	}
	s := it.lines[it.line]
	low := max(0, it.end-maxEntryLength-1)
	comma := strings.LastIndexByte(s[low:it.end], ',')
	if comma < 0 && low > 0 {
		entry := s[low:it.end]
		it.line = -1
		return entry, true
	}
	start := low + comma + 1
	entry := s[start:it.end]
	if comma < 0 {
		it.line--
		if it.line >= 0 {
			it.end = len(it.lines[it.line])
		}
	} else {
		it.end = start - 1
	}
	return entry, true
}

func parseEntry(entry string) (netip.Addr, bool) {
	if entry == "" {
		return netip.Addr{}, false
	}
	if entry[0] == '[' && entry[len(entry)-1] == ']' {
		ip, err := netip.ParseAddr(entry[1 : len(entry)-1])
		return unmapped(ip, err == nil && ip.Is6())
	}
	if ip, err := netip.ParseAddr(entry); err == nil {
		return unmapped(ip, true)
	}
	ap, err := netip.ParseAddrPort(entry)
	return unmapped(ap.Addr(), err == nil)
}

func unmapped(ip netip.Addr, ok bool) (netip.Addr, bool) {
	if !ok || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

type element struct {
	forValue string
	proto    string
	host     string
	hasFor   bool
	hasProto bool
	hasHost  bool
}

func parseElement(s string) (element, bool) {
	var e element
	for s != "" {
		pair := s
		if i := strings.IndexByte(s, ';'); i >= 0 {
			pair, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		pair = trimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			return element{}, false
		}
		name := trimSpace(pair[:eq])
		value, ok := unquote(trimSpace(pair[eq+1:]))
		if !ok {
			return element{}, false
		}
		switch {
		case strings.EqualFold(name, "for"):
			e.forValue, e.hasFor = value, true
		case strings.EqualFold(name, "proto"):
			e.proto, e.hasProto = value, true
		case strings.EqualFold(name, "host"):
			e.host, e.hasHost = value, true
		}
	}
	return e, e.hasFor
}

func unquote(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if value[0] != '"' {
		return value, strings.IndexByte(value, '"') < 0
	}
	if len(value) < 2 || value[len(value)-1] != '"' {
		return "", false
	}
	inner := value[1 : len(value)-1]
	if strings.IndexByte(inner, '"') >= 0 || strings.IndexByte(inner, '\\') >= 0 {
		return "", false
	}
	return inner, true
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func (c *compiledProxy) Snapshot() inspect.Snapshot {
	mode, header := "proxies", "x-forwarded"
	if c.hops > 0 {
		mode = "hops"
	}
	if c.header == Forwarded {
		header = "forwarded"
	}
	trusted := make([]string, len(c.prefixes))
	for i, prefix := range c.prefixes {
		trusted[i] = prefix.String()
	}
	return inspect.Snapshot{Name: "proxy", Fields: []inspect.Field{
		{Key: "mode", Value: mode},
		{Key: "trusted", Value: trusted},
		{Key: "hops", Value: int64(c.hops)},
		{Key: "header", Value: header},
		{Key: "resolved", Value: c.resolved.Load()},
		{Key: "untrusted", Value: c.untrusted.Load()},
	}}
}
