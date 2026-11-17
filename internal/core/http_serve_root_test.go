package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestServe_DotfilesAre404(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, ".git", "config"), "SECRET=git")
	writeServeFile(t, filepath.Join(serveRoot, "css", ".secret"), "SECRET=css")
	writeServeFile(t, filepath.Join(serveRoot, ".hidden", "index.html"), "SECRET=index")
	for _, target := range []string{"/files/.env", "/files/%2eenv", "/files/.git/config", "/files/css/.secret", "/files/.hidden/", "/files/.hidden/index.html", "/files/.git/"} {
		w := serveGet(app, target)
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "a dotfile or dot directory under a served directory answers 404: "+target))
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Body.String(), "<no file contents>", "a dotfile is never read: "+target))
		}
	}
}

func TestServe_WellKnownIsServed(t *testing.T) {
	app := newServeApp(t)
	writeServeFile(t, filepath.Join(serveRoot, ".well-known", "security.txt"), "Contact: x")
	w := serveGet(app, "/files/.well-known/security.txt")
	if w.Code != http.StatusOK || w.Body.String() != "Contact: x" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, ".well-known (RFC 8615) stays served"))
	}
}

func TestServe_SymlinkOutOfRootIs404(t *testing.T) {
	app := newServeApp(t)
	outside := filepath.Dir(serveRoot)
	writeServeFile(t, filepath.Join(outside, "outside.txt"), "SECRET=file")
	writeServeFile(t, filepath.Join(outside, "outdir", "f.txt"), "SECRET=dir")
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(serveRoot, "link.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := os.Symlink(filepath.Join(outside, "outdir"), filepath.Join(serveRoot, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside.txt", filepath.Join(serveRoot, "css", "rel.txt")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/files/link.txt", "/files/linkdir/f.txt", "/files/css/rel.txt"} {
		w := serveGet(app, target)
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "a symlink leaving the served directory answers 404: "+target))
		}
		if strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Body.String(), "<no file contents>", "a file outside the served directory is never read: "+target))
		}
	}
}

func TestServe_SymlinkInsideRootIsServed(t *testing.T) {
	app := newServeApp(t)
	if err := os.Symlink("logo.png", filepath.Join(serveRoot, "alias.png")); err != nil {
		t.Skip("no symlinks:", err)
	}
	w := serveGet(app, "/files/alias.png")
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "a symlink that stays inside the served directory is served"))
	}
}

func TestServe_RangeAndConditionalStillWork(t *testing.T) {
	app := newServeApp(t)
	r := httptest.NewRequest(http.MethodGet, "/files/logo.png", nil)
	r.Header.Set("Range", "bytes=1-2")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "el" {
		t.Error(test.DiffMessage(w.Code, http.StatusPartialContent, "ranges are served"))
	}
	first := serveGet(app, "/files/logo.png")
	r = httptest.NewRequest(http.MethodGet, "/files/logo.png", nil)
	r.Header.Set("If-Modified-Since", first.Header().Get("Last-Modified"))
	w = httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Error(test.DiffMessage(w.Code, http.StatusNotModified, "conditional requests are answered"))
	}
}

func TestServe_NameAnyDotfileAndSymlinkAre404(t *testing.T) {
	app := svApp(t)
	svWrite(t, filepath.Join(svRoot, ".env"), "SECRET=1")
	outside := filepath.Join(filepath.Dir(svRoot), "outside.txt")
	svWrite(t, outside, "SECRET=2")
	if err := os.Symlink(outside, filepath.Join(svRoot, "out.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	for _, target := range []string{"/static/.env", "/static/out.txt"} {
		w := createGet(app, target)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "SECRET") {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "SERVE_<x>_ANY hides dotfiles and symlinks out of the root: "+target))
		}
	}
}
