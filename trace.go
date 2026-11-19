package owlky

import (
	"net/http"

	"go.owlky.com/owlky/internal/trace"
)

func TraceTransport(base http.RoundTripper) http.RoundTripper {
	return trace.Transport(base)
}
