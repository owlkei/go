package tracecontext

import (
	"net/http"
	"net/netip"
)

const maxHostLength = 255

func ClientIP(r *http.Request) netip.Addr {
	if s := ScopeOf(r.Context()); s != nil && s.IsClientSet {
		return s.ClientIP
	}
	return RemoteIP(r.RemoteAddr)
}

func IsHTTPS(r *http.Request) bool {
	if s := ScopeOf(r.Context()); s != nil && s.IsClientSet {
		return s.IsClientHTTPS
	}
	return r.TLS != nil
}

func Host(r *http.Request) string {
	if s := ScopeOf(r.Context()); s != nil && s.IsClientSet && s.ClientHost != "" {
		return s.ClientHost
	}
	return r.Host
}

func RemoteIP(remoteAddr string) netip.Addr {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().WithZone("").Unmap()
}

func IsValidHost(host string) bool {
	if len(host) == 0 || len(host) > maxHostLength {
		return false
	}
	name, port, hasPort, ok := SplitHostPort(host)
	if !ok || (hasPort && !IsPort(port)) {
		return false
	}
	if name[0] == '[' {
		ip, err := netip.ParseAddr(name[1 : len(name)-1])
		return err == nil && ip.Is6() && ip.Zone() == ""
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

func HostName(host string) string {
	name, _, _, ok := SplitHostPort(host)
	if !ok {
		return host
	}
	return name
}

func SplitHostPort(host string) (name, port string, hasPort, ok bool) {
	if host == "" {
		return "", "", false, false
	}
	if host[0] == '[' {
		end := -1
		for i := 1; i < len(host); i++ {
			if host[i] == ']' {
				end = i
				break
			}
		}
		if end < 2 {
			return "", "", false, false
		}
		rest := host[end+1:]
		if rest == "" {
			return host, "", false, true
		}
		if rest[0] != ':' {
			return "", "", false, false
		}
		return host[:end+1], rest[1:], true, true
	}
	colon := -1
	for i := 0; i < len(host); i++ {
		if host[i] == ':' {
			if colon >= 0 {
				return "", "", false, false
			}
			colon = i
		}
	}
	if colon < 0 {
		return host, "", false, true
	}
	if colon == 0 {
		return "", "", false, false
	}
	return host[:colon], host[colon+1:], true, true
}

func IsPort(port string) bool {
	if len(port) == 0 || len(port) > 5 {
		return false
	}
	n := 0
	for i := 0; i < len(port); i++ {
		c := port[i]
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n <= 65535
}
