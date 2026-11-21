package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/ctx"
)

var benchServeRoot string

type benchServeController struct{ common.HTTP }

func (c benchServeController) NewController() Controller { return c }

func (c benchServeController) SERVE_files_ANY() string { return benchServeRoot }

type benchAllowGuard struct{}

func (g benchAllowGuard) CanActivate(c *ctx.HTTPContext) bool { return true }

type benchGuardedPages struct {
	common.HTTP
	common.Guard
}

func (c benchGuardedPages) NewController() Controller {
	c.BindGuard(benchAllowGuard{})
	return c
}

func (c benchGuardedPages) RENDER_dash() {}

func BenchmarkServe_File(b *testing.B) {
	dir := b.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o750); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "guide.txt"), []byte("GUIDE"), 0o600); err != nil {
		b.Fatal(err)
	}
	benchServeRoot = dir
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, HTTP: &HTTPOptions{ServeDirs: []string{dir}}})
	app.Create(NewModuleBuilder().Controllers(benchServeController{}).Build())
	benchStatic(b, app, "/files/docs/guide.txt")
}

func BenchmarkAsset_PageGuarded(b *testing.B) {
	files := pgFiles(map[string]string{"dash.html": "{{pageAssets}}DASH", "dash.css": "DASH-CSS"})
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: files, Dir: "."}})
	app.Create(NewModuleBuilder().Controllers(benchGuardedPages{}).Build())
	benchStatic(b, app, "/assets/_pages/dash.css")
}

func BenchmarkPublic_File(b *testing.B) {
	files := pgFiles(map[string]string{"public/robots.txt": "User-agent: *\n"})
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, Render: &RenderOptions{FS: files, Dir: "."}})
	app.Create(NewModuleBuilder().Controllers(puAPI{}).Build())
	benchStatic(b, app, "/robots.txt")
}

func benchStatic(b *testing.B, app *App, target string) {
	b.Helper()
	if w := createGet(app, target); w.Code != http.StatusOK {
		b.Fatalf("GET %s answered %d", target, w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, target, nil)
	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(&benchDiscardWriter{header: http.Header{}}, r)
	}
}
