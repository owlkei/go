package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

const (
	wantNotFoundJSON         = `{"code":404,"error":"Not Found","message":"Not Found"}`
	wantMethodNotAllowedJSON = `{"code":405,"error":"Method Not Allowed","message":"Method Not Allowed"}`
	wantGoneJSON             = `{"code":410,"error":"Gone","message":"Deprecated URL usage"}`
	wantInternalErrorJSON    = `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`
	wantInvalidURLJSON       = `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`
)

var serveRoot string

type serveController struct {
	common.HTTP
}

func (c serveController) NewController() Controller  { return c }
func (c serveController) SERVE_ANY_OF_files() string { return serveRoot }
func (c serveController) SERVE_logo_png_FILE() string {
	return filepath.Join(serveRoot, "logo.png")
}
func (c serveController) SERVE_missing_png_FILE() string {
	return filepath.Join(serveRoot, "missing.png")
}
func (c serveController) SERVE_docs() string { return serveRoot }
func (c serveController) SERVE_number() int  { return 42 }

func newServeApp(t *testing.T) *App {
	t.Helper()
	serveRoot = filepath.Join(t.TempDir(), "public")
	if err := os.MkdirAll(filepath.Join(serveRoot, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeServeFile(t, filepath.Join(serveRoot, "logo.png"), "hello")
	writeServeFile(t, filepath.Join(serveRoot, "css", "app.css"), "body{}")
	writeServeFile(t, filepath.Join(serveRoot, ".env"), "SECRET=1")

	app := New()
	app.Create(ModuleBuilder().Controllers(serveController{}).Build())
	return app
}

func writeServeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func serveGet(app *App, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestServe_NonStringResultIs404(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/number")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S2: a handler result that is not a string answers 404"))
	}
	if w.Body.String() != wantNotFoundJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S2: the 404 body is N1's"))
	}
}

func TestServe_ExistingFile(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/logo.png")

	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "a SERVE route serves an existing file with 200"))
	}
	if w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "a SERVE route writes the file contents"))
	}
}

func TestServe_MissingFileIs404(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/missing.png")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S2: a missing file answers 404"))
	}
	if w.Body.String() != wantNotFoundJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S2: the 404 body is N1's"))
	}
}

func TestServe_WildcardServesFileUnderDirectory(t *testing.T) {
	app := newServeApp(t)
	w := serveGet(app, "/files/css/app.css")

	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "a wildcard SERVE route serves a file found under the directory"))
	}
	if w.Body.String() != "body{}" {
		t.Error(test.DiffMessage(w.Body.String(), "body{}", "a wildcard SERVE route writes the resolved file's contents"))
	}
}

func TestServe_TraversalIs400(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(filepath.Dir(serveRoot), "files", "secret"), "do-not-leak")

	w := serveGet(app, "/files/a/../../files/secret")

	if w.Code != http.StatusBadRequest {
		t.Error(test.DiffMessage(w.Code, http.StatusBadRequest, "S2: a resolved path outside the served directory answers 400"))
	}
	if w.Body.String() != wantInvalidURLJSON {
		t.Error(test.DiffMessage(w.Body.String(), wantInvalidURLJSON, "S2: the 400 body is fixed"))
	}
	if strings.Contains(w.Body.String(), "do-not-leak") {
		t.Error(test.DiffMessage(w.Body.String(), "<no leaked file contents>", "S2: the escaped file is never read"))
	}
}

func TestServe_DirectoryWithoutIndexIs404(t *testing.T) {
	app := newServeApp(t)

	for _, target := range []string{"/files/", "/files/css/", "/docs/"} {
		w := serveGet(app, target)

		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S1: a directory without index.html answers 404 for "+target))
		}
		if w.Body.String() != wantNotFoundJSON {
			t.Error(test.DiffMessage(w.Body.String(), wantNotFoundJSON, "S1: the 404 body is N1's for "+target))
		}
	}
}

func TestServe_NeverListsDirectory(t *testing.T) {
	app := newServeApp(t)

	for _, target := range []string{"/files/", "/files/css/", "/docs/"} {
		w := serveGet(app, target)
		body := w.Body.String()

		for _, leaked := range []string{".env", "logo.png", "app.css", "<a href", "<pre>"} {
			if strings.Contains(body, leaked) {
				t.Error(test.DiffMessage(body, "<no directory listing>", "S1: "+target+" must not list "+leaked))
			}
		}
	}
}

func TestServe_DirectoryWithIndex(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, "sub", "index.html"), "<h1>home</h1>")

	w := serveGet(app, "/files/sub/")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "S1: a directory with index.html answers that file"))
	}
	if w.Body.String() != "<h1>home</h1>" {
		t.Error(test.DiffMessage(w.Body.String(), "<h1>home</h1>", "S1: the body is the index.html contents"))
	}
}

func TestServe_DirectoryWithIndexRedirectsToSlash(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, "sub", "index.html"), "<h1>home</h1>")

	w := serveGet(app, "/files/sub")
	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "S1: a directory URL without the slash redirects as http.ServeFile does"))
	}
	if got := w.Header().Get("Location"); got != "sub/" {
		t.Error(test.DiffMessage(got, "sub/", "S1: the redirect points at the slash-terminated URL"))
	}
}
