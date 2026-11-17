package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func allocsOfServe(app *App, method, target string) float64 {
	r := httptest.NewRequest(method, target, nil)
	w := &benchDiscardWriter{header: http.Header{}}
	return testing.AllocsPerRun(200, func() { app.ServeHTTP(w, r) })
}

func TestServeHTTP_NotFoundAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodGet, "/does-not-exist"); got > 10 {
		t.Error(test.DiffMessage(got, "<= 10", "P7: a 404 writes shared bytes (today 23 allocations, about 5 expected)"))
	}
}

func TestServeHTTP_MethodNotAllowedAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodDelete, "/bench"); got > 10 {
		t.Error(test.DiffMessage(got, "<= 10", "P7: a 405 writes shared bytes (about 5 expected)"))
	}
}

func TestServeHTTP_StaticRouteAllocations(t *testing.T) {
	app := benchApp(benchPlainController{})

	if got := allocsOfServe(app, http.MethodGet, "/bench"); got > 7 {
		t.Error(test.DiffMessage(got, "<= 7", "P1, P3: a static route without body or middleware (today 9 allocations, 6 expected)"))
	}
}
