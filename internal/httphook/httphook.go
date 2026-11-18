package httphook

import (
	"net/http"
	"strings"
	"time"
)

const cspHeader = "Content-Security-Policy"

type Hook interface {
	Done(elapsed time.Duration)
}

type Writer interface {
	Inner() http.ResponseWriter
	SetInner(w http.ResponseWriter)
	OnDone(h Hook)
}

func Of(w http.ResponseWriter) Writer {
	hw, _ := w.(Writer)
	return hw
}

type CSP struct {
	Value []string
	Parts []string
}

func (p *CSP) Header(nonce string) []string {
	size := len(p.Parts[0])
	for _, part := range p.Parts[1:] {
		size += len(nonce) + len(part)
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteString(p.Parts[0])
	for _, part := range p.Parts[1:] {
		b.WriteString(nonce)
		b.WriteString(part)
	}
	return []string{b.String()}
}

func (p *CSP) IsSent(h http.Header) bool {
	v := h[cspHeader]
	return len(v) == 1 && &v[0] == &p.Value[0]
}

type CSPWriter interface {
	SetCSP(p *CSP)
}
