package bench

import (
	"io"
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

func TestScenarios_NetHTTP(t *testing.T) { checkScenarios(t, "net/http", NewNetHTTP()) }
func TestScenarios_Gin(t *testing.T)     { checkScenarios(t, "gin", NewGin()) }
func TestScenarios_Echo(t *testing.T)    { checkScenarios(t, "echo", NewEcho()) }
func TestScenarios_Chi(t *testing.T)     { checkScenarios(t, "chi", NewChi()) }

func TestScenarios_Fiber(t *testing.T) {
	app := NewFiber()
	for _, s := range Scenarios {
		response, err := app.Test(s.Request(), -1)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != s.Status {
			t.Error(test.DiffMessage(response.StatusCode, s.Status, "fiber "+s.Name+" status, body "+string(body)))
		}
		if s.Contains != "" && !strings.Contains(string(body), s.Contains) {
			t.Error(test.DiffMessage(string(body), s.Contains, "fiber "+s.Name+" body"))
		}
	}
}
