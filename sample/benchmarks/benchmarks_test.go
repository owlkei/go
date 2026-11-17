package benchmarks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject"
)

func TestPingRoute(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Imports(Module).Build())

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello, World!") {
		t.Fatalf("ping: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping?error=true", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ping error: %d, want 500", rec.Code)
	}
}
