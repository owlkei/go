package hostcheck

import (
	"net/http"
	"net/netip"
	"sync/atomic"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

const (
	maxHosts      = 256
	maxHostLength = 255
	anyPort       = ""
)

var (
	unknownHost = exception.MisdirectedRequestException("Unknown host")
	invalidHost = exception.BadRequestException("Invalid host")
)

type HostCheck struct {
	Hosts []string
}

type compiledHostCheck struct {
	hosts         []string
	portsByName   map[string][]string
	portsBySuffix map[string][]string
	refused       atomic.Int64
}

func (instance HostCheck) NewMiddleware() common.MiddlewareFn {
	return loadHostCheckOptions(&instance)
}

func (instance HostCheck) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func loadHostCheckOptions(h *HostCheck) *compiledHostCheck {
	if len(h.Hosts) == 0 {
		panic(color.FmtRed("invalid hostcheck: Hosts is empty"))
	}
	if len(h.Hosts) > maxHosts {
		panic(color.FmtRed("invalid hostcheck: Hosts has %d entries, at most %d are allowed", len(h.Hosts), maxHosts))
	}
	c := &compiledHostCheck{
		hosts:         append([]string(nil), h.Hosts...),
		portsByName:   make(map[string][]string),
		portsBySuffix: make(map[string][]string),
	}
	for _, entry := range h.Hosts {
		name, port, isWildcard, ok := parseEntry(entry)
		if !ok {
			panic(color.FmtRed("invalid hostcheck: %q is not a host name, *.-pattern or IP literal with an optional port", entry))
		}
		if isWildcard {
			c.portsBySuffix[name] = append(c.portsBySuffix[name], port)
			continue
		}
		c.portsByName[name] = append(c.portsByName[name], port)
	}
	return c
}

func parseEntry(entry string) (name, port string, isWildcard, ok bool) {
	if entry == "" || len(entry) > maxHostLength {
		return "", "", false, false
	}
	name, port, hasPort, ok := tracecontext.SplitHostPort(entry)
	if !ok || (hasPort && !tracecontext.IsPort(port)) {
		return "", "", false, false
	}
	name = trimDot(lower(name))
	if name == "" {
		return "", "", false, false
	}
	if name[0] == '[' {
		ip, err := netip.ParseAddr(name[1 : len(name)-1])
		return name, port, false, err == nil && ip.Is6() && ip.Zone() == ""
	}
	if len(name) > 2 && name[0] == '*' && name[1] == '.' {
		name = name[2:]
		isWildcard = true
	}
	return name, port, isWildcard, isHostName(name)
}

func isHostName(name string) bool {
	if name == "" {
		return false
	}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i < len(name) && name[i] != '.' {
			c := name[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
			continue
		}
		if i == start || name[start] == '-' || name[i-1] == '-' {
			return false
		}
		start = i + 1
	}
	return true
}

func lower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func (c *compiledHostCheck) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	host := tracecontext.Host(r)
	if host == "" || len(host) > maxHostLength {
		c.refused.Add(1)
		panic(&invalidHost)
	}
	if !c.allows(host) {
		c.refused.Add(1)
		panic(&unknownHost)
	}
	next()
}

func (c *compiledHostCheck) allows(host string) bool {
	name, port, hasPort, ok := tracecontext.SplitHostPort(host)
	if !ok || (hasPort && !tracecontext.IsPort(port)) {
		return false
	}
	name = trimDot(name)
	for i := 0; i < len(name); i++ {
		if ch := name[i]; ch >= 'A' && ch <= 'Z' {
			var buf [maxHostLength]byte
			n := copy(buf[:], name)
			for j := i; j < n; j++ {
				if buf[j] >= 'A' && buf[j] <= 'Z' {
					buf[j] += 'a' - 'A'
				}
			}
			return matchName(c, buf[:n], port)
		}
	}
	return matchName(c, name, port)
}

func matchName[T string | []byte](c *compiledHostCheck, name T, port string) bool {
	if hasPort(c.portsByName[string(name)], port) {
		return true
	}
	for i := 1; i < len(name); i++ {
		if name[i] != '.' {
			continue
		}
		if ports, ok := c.portsBySuffix[string(name[i+1:])]; ok && isLabelRun(name[:i]) && hasPort(ports, port) {
			return true
		}
	}
	return false
}

func isLabelRun[T string | []byte](prefix T) bool {
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func hasPort(ports []string, port string) bool {
	for _, p := range ports {
		if p == anyPort || p == port {
			return true
		}
	}
	return false
}

func trimDot(name string) string {
	if len(name) > 1 && name[len(name)-1] == '.' {
		return name[:len(name)-1]
	}
	return name
}

func (c *compiledHostCheck) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "hostcheck", Fields: []inspect.Field{
		{Key: "hosts", Value: append([]string(nil), c.hosts...)},
		{Key: "refused", Value: c.refused.Load()},
	}}
}
