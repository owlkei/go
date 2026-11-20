package trace

import (
	"net/http"

	"go.owlkei.com/owlkei/internal/tracecontext"
)

const traceparentHeader = "Traceparent"

type transport struct {
	base http.RoundTripper
}

func Transport(base http.RoundTripper) http.RoundTripper {
	return &transport{base: base}
}

func (t *transport) baseTransport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.baseTransport()
	s := tracecontext.ScopeOf(req.Context())
	if s == nil || s.Traceparent() == "" {
		return base.RoundTrip(req)
	}
	if _, ok := req.Header[traceparentHeader]; ok {
		return base.RoundTrip(req)
	}
	out := *req
	out.Header = req.Header.Clone()
	if out.Header == nil {
		out.Header = make(http.Header, 1)
	}
	out.Header[traceparentHeader] = []string{s.Traceparent()}
	return base.RoundTrip(&out)
}

func (t *transport) CloseIdleConnections() {
	if closer, ok := t.baseTransport().(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
