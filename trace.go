package owlkei

import (
	"net/http"

	"go.owlkei.com/owlkei/internal/trace"
)

func TraceTransport(base http.RoundTripper) http.RoundTripper {
	return trace.Transport(base)
}
