package owlky

import (
	"net/http"
	"net/netip"

	"go.owlky.com/owlky/internal/core"
	"go.owlky.com/owlky/internal/tracecontext"
)

const (
	ProxyHeaderXForwarded = core.ProxyHeaderXForwarded
	ProxyHeaderForwarded  = core.ProxyHeaderForwarded
)

const (
	KB  = core.KB
	MB  = core.MB
	GB  = core.GB
	KiB = core.KiB
	MiB = core.MiB
	GiB = core.GiB
)

func OptionsFromEnv() *AppOptions {
	return core.OptionsFromEnv()
}

func ClientIP(r *http.Request) netip.Addr {
	return tracecontext.ClientIP(r)
}

func ClientScheme(r *http.Request) string {
	if tracecontext.IsHTTPS(r) {
		return "https"
	}
	return "http"
}

func ClientHost(r *http.Request) string {
	return tracecontext.Host(r)
}
