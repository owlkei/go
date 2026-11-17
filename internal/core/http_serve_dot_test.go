package core

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

type serveDotController struct {
	common.HTTP
}

func (c serveDotController) NewController() Controller { return c }
func (c serveDotController) SERVE_ANY_OF_dot() string  { return "." }

func TestServe_DotRootServesFilesUnderWorkingDirectory(t *testing.T) {
	file, err := os.CreateTemp(".", "serve_dot_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	if _, err := file.WriteString("DOT-ROOT-CONTENT"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	hidden, err := os.CreateTemp(".", ".serve_dot_hidden_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(hidden.Name()) })
	if _, err := hidden.WriteString("DOT-ROOT-SECRET"); err != nil {
		t.Fatal(err)
	}
	_ = hidden.Close()

	app := New()
	app.Create(NewModuleBuilder().Controllers(serveDotController{}).Build())

	w := serveGet(app, "/dot/"+strings.TrimPrefix(file.Name(), "./"))
	if w.Code != http.StatusOK || w.Body.String() != "DOT-ROOT-CONTENT" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "DOT-ROOT-CONTENT"}, "L9: a static route whose handler returns \".\" serves files under the working directory"))
	}

	w = serveGet(app, "/dot/"+strings.TrimPrefix(hidden.Name(), "./"))
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "DOT-ROOT-SECRET") {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "L9: a dotfile under the working directory still answers 404"))
	}

	w = serveGet(app, "/dot/../go.mod")
	if w.Code == http.StatusOK {
		t.Error(test.DiffMessage(w.Code, "not 200", "L9: a path leaving the working directory is refused"))
	}
}
