package owlky

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestTraceTransport_DelegatesToTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Got", r.Header.Get("Traceparent"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	res, err := (&http.Client{Transport: TraceTransport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if got := res.Header.Get("X-Got"); got != "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01" {
		t.Error(test.DiffMessage(got, "the existing traceparent", "B20: a nil base uses the default transport and keeps an existing header"))
	}
}
