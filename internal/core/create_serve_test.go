package core

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

var svRoot string

type svNameAnyController struct{ common.HTTP }

func (c svNameAnyController) NewController() Controller { return c }
func (c svNameAnyController) SERVE_static_ANY() string  { return svRoot }

type svAnyOfController struct{ common.HTTP }

func (c svAnyOfController) NewController() Controller   { return c }
func (c svAnyOfController) SERVE_ANY_OF_static() string { return svRoot }

func svWrite(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func svApp(t *testing.T) *App {
	t.Helper()
	svRoot = filepath.Join(t.TempDir(), "public")
	svWrite(t, filepath.Join(svRoot, "a.txt"), "alpha")
	svWrite(t, filepath.Join(svRoot, "sub", "x.txt"), "nested")

	app := New()
	mustCreate(t, app, ModuleBuilder().Controllers(svNameAnyController{}).Build())
	return app
}

func TestServe_NameAnyServesSubtree(t *testing.T) {
	app := svApp(t)

	cases := []struct{ target, body string }{
		{"/static/a.txt", "alpha"},
		{"/static/sub/x.txt", "nested"},
	}
	for _, c := range cases {
		w := createGet(app, c.target)
		if w.Code != http.StatusOK {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "V3: status of GET "+c.target))
		}
		if w.Body.String() != c.body {
			t.Error(test.DiffMessage(w.Body.String(), c.body, "V3: body of GET "+c.target))
		}
	}
}

func TestServe_NameAnyDoesNotMatchPrefixSibling(t *testing.T) {
	app := svApp(t)

	for _, target := range []string{"/staticzzz", "/staticzzz/a.txt", "/other/a.txt"} {
		if w := createGet(app, target); w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "V3: GET "+target+" is not under /static/"))
		}
	}
}

func TestServe_NameAnyMissingFileIs404(t *testing.T) {
	app := svApp(t)

	if w := createGet(app, "/static/missing.txt"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "V3: a missing file under /static/ is 404"))
	}
}

func TestServe_NameAnyTraversalIsRefused(t *testing.T) {
	app := svApp(t)
	secret := filepath.Join(filepath.Dir(svRoot), "secret.txt")
	svWrite(t, secret, "secret")

	w := createGet(app, "/static/..%2fsecret.txt")
	if w.Code == http.StatusOK || w.Body.String() == "secret" {
		t.Error(test.DiffMessage(w.Code, "a refusal", "V3: a traversal never leaves the served directory"))
	}
}

func TestServe_NameAnyConflictsWithAnyOfName(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(ModuleBuilder().Controllers(svNameAnyController{}, svAnyOfController{}).Build())
	})

	want := "route conflict:"
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "V4: SERVE_static_ANY and SERVE_ANY_OF_static are the same route"))
	}
}
