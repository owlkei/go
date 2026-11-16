package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/trace"
)

func TestTransport_FromHandlerContext(t *testing.T) {
	var seen string
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Traceparent")
	}))
	defer upstream.Close()

	app := lcApp(t)
	lcSetHandler(func(c *ctx.HTTPContext) any {
		r, err := http.NewRequestWithContext(c.Context(), http.MethodGet, upstream.URL, nil)
		if err != nil {
			return err.Error()
		}
		res, err := (&http.Client{Transport: trace.Transport(nil)}).Do(r)
		if err != nil {
			return err.Error()
		}
		_ = res.Body.Close()
		return "ok"
	})
	w := lcServe(app, http.MethodGet, "/ok")
	requestID, traceID, _ := expectScopeHeaders(t, w, "handler calling upstream")

	got, parentID, _, ok := tracecontext.ParseTraceparent(seen)
	if !ok {
		t.Fatal(test.DiffMessage(seen, "<a valid traceparent>", "T6: the upstream receives a traceparent"))
	}
	if got != traceID {
		t.Error(test.DiffMessage(got, traceID, "T6: the upstream sees the response's trace ID"))
	}
	if parentID != requestID {
		t.Error(test.DiffMessage(parentID, requestID, "T3: the upstream's parent is the request's span"))
	}
}
