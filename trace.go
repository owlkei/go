package ginject

import (
	"net/http"

	"github.com/dangduoc08/ginject/internal/trace"
)

func TraceTransport(base http.RoundTripper) http.RoundTripper {
	return trace.Transport(base)
}
