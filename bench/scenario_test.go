package bench

import (
	"io"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestScenarios_Owlkei(t *testing.T) { checkScenarios(t, "owlkei", NewOwlkei()) }

func TestScenarios_NetHTTP(t *testing.T) { checkScenarios(t, "net/http", NewNetHTTP()) }

func TestScenarios_Gin(t *testing.T) { checkScenarios(t, "gin", NewGin()) }

func TestScenarios_Echo(t *testing.T) { checkScenarios(t, "echo", NewEcho()) }

func TestScenarios_Chi(t *testing.T) { checkScenarios(t, "chi", NewChi()) }

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
