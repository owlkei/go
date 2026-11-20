package origin

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/inspect"
	"go.owlky.com/owlky/internal/tracecontext"
)

const (
	Any          = "*"
	maxEntries   = 256
	MaxLength    = 256
	originHeader = "Origin"
	httpsScheme  = "https://"
	httpScheme   = "http://"
)

var notAllowed = exception.ForbiddenException("Origin not allowed")

type Set struct {
	isAny     bool
	exact     map[string]struct{}
	wildcards []wildcard
	entries   []string
}

type wildcard struct {
	prefix string
	suffix string
}

func Parse(field string, entries []string) (*Set, error) {
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("%s has %d entries, at most %d are allowed", field, len(entries), maxEntries)
	}
	s := &Set{exact: make(map[string]struct{}, len(entries)), entries: append([]string(nil), entries...)}
	for _, entry := range entries {
		if entry == Any {
			if len(entries) > 1 {
				return nil, fmt.Errorf("%q must be the only entry of %s", Any, field)
			}
			s.isAny = true
			continue
		}
		if !s.add(entry) {
			return nil, fmt.Errorf("%s entry %q must be scheme://host[:port] in lower case, scheme://*.host[:port] or %q alone", field, entry, Any)
		}
	}
	return s, nil
}

func (s *Set) add(entry string) bool {
	scheme, host, ok := strings.Cut(entry, "://")
	if !ok || !isScheme(scheme) || len(entry) > MaxLength {
		return false
	}
	if rest, isWildcard := strings.CutPrefix(host, "*."); isWildcard {
		if !isHost(rest) {
			return false
		}
		s.wildcards = append(s.wildcards, wildcard{prefix: scheme + "://", suffix: "." + rest})
		return true
	}
	if !isHost(host) {
		return false
	}
	s.exact[entry] = struct{}{}
	return true
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

func isHost(host string) bool {
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

func (s *Set) IsAny() bool {
	return s.isAny
}

func (s *Set) Entries() []string {
	return append([]string(nil), s.entries...)
}

func (s *Set) Matches(origin string) bool {
	if s.isAny {
		return true
	}
	if len(origin) > MaxLength {
		return false
	}
	if _, ok := s.exact[origin]; ok {
		return true
	}
	for _, w := range s.wildcards {
		if len(origin) <= len(w.prefix)+len(w.suffix) ||
			!strings.HasPrefix(origin, w.prefix) || !strings.HasSuffix(origin, w.suffix) {
			continue
		}
		if isWildcardPart(origin[len(w.prefix) : len(origin)-len(w.suffix)]) {
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

type Check struct {
	set     *Set
	refused atomic.Int64
}

func NewCheck(set *Set) *Check {
	return &Check{set: set}
}

func (c *Check) Use(r *http.Request, _ http.ResponseWriter, next ctx.Next) {
	values := r.Header[originHeader]
	if len(values) == 0 {
		next()
		return
	}
	value := values[0]
	if len(value) <= MaxLength {
		value = strings.ToLower(value)
		if isSameOrigin(r, value) || (c.set != nil && c.set.Matches(value)) {
			next()
			return
		}
	}
	c.refused.Add(1)
	panic(&notAllowed)
}

func isSameOrigin(r *http.Request, origin string) bool {
	scheme, port := httpScheme, ":80"
	if tracecontext.IsHTTPS(r) {
		scheme, port = httpsScheme, ":443"
	}
	rest, ok := strings.CutPrefix(origin, scheme)
	if !ok {
		return false
	}
	host := strings.ToLower(tracecontext.Host(r))
	return strings.TrimSuffix(rest, port) == strings.TrimSuffix(host, port)
}

func (c *Check) Snapshot() inspect.Snapshot {
	var entries []string
	if c.set != nil {
		entries = c.set.Entries()
	}
	return inspect.Snapshot{Name: "origin", Fields: []inspect.Field{
		{Key: "origins", Value: entries},
		{Key: "refused", Value: c.refused.Load()},
	}}
}
