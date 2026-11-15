package bench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func checkScenarios(t *testing.T, name string, handler http.Handler) {
	for _, s := range Scenarios {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, s.Request())
		if w.Code != s.Status {
			t.Error(test.DiffMessage(w.Code, s.Status, name+" "+s.Name+" status, body "+w.Body.String()))
		}
		if s.Contains != "" && !strings.Contains(w.Body.String(), s.Contains) {
			t.Error(test.DiffMessage(w.Body.String(), s.Contains, name+" "+s.Name+" body"))
		}
	}
}

func TestScenarios_Ginject(t *testing.T) { checkScenarios(t, "ginject", NewGinject()) }
