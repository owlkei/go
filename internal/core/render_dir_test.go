package core

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

//go:embed testdata/web
var rdEmbedded embed.FS

type rdX struct{ common.HTTP }

func (c rdX) NewController() Controller { return c }
func (c rdX) RENDER_x()                 {}

func rdApp(t *testing.T, opts *RenderOptions) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(opts)
	mustCreate(t, app, NewModuleBuilder().Controllers(rdX{}).Build())
	return app, logger
}

func TestRenderDir_DefaultIsWeb(t *testing.T) {
	files := fstest.MapFS{"web/x.html": {Data: []byte("FROMWEB")}, "x.html": {Data: []byte("FROMROOT")}}
	app, _ := rdApp(t, &RenderOptions{FS: files})
	if got := createGet(app, "/x").Body.String(); got != "FROMWEB" {
		t.Error(test.DiffMessage(got, "FROMWEB", "an empty Dir reads the web directory of FS"))
	}
}

func TestRenderDir_DotIsTheRoot(t *testing.T) {
	files := fstest.MapFS{"web/note.txt": {Data: []byte("FROMWEB")}, "x.html": {Data: []byte("FROMROOT")}}
	app, _ := rdApp(t, &RenderOptions{FS: files, Dir: "."})
	if got := createGet(app, "/x").Body.String(); got != "FROMROOT" {
		t.Error(test.DiffMessage(got, "FROMROOT", `Dir "." reads the root of FS`))
	}
}

func TestRenderDir_MissingDirectoryPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().EnableRender(&RenderOptions{FS: fstest.MapFS{"x.html": {Data: []byte("x")}}})
	})
	want := `invalid render: RenderOptions.FS has no "web" directory: embed it with //go:embed web, or set Dir`
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "a missing directory says how to fix it"))
	}
}

func TestRenderDir_EmbeddedFSWithReloadReadsDisk(t *testing.T) {
	dir := filepath.Join("testdata", "web")
	app, logger := rdApp(t, &RenderOptions{FS: rdEmbedded, Dir: dir, IsReload: true})
	if got := createGet(app, "/x").Body.String(); !strings.HasPrefix(got, "EMBEDDED") {
		t.Fatal(test.DiffMessage(got, "EMBEDDED …", "the page renders from the directory"))
	}
	path := filepath.Join(dir, "x.html")
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path, old, 0o644) })
	if err := os.WriteFile(path, []byte("ONDISK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := createGet(app, "/x").Body.String(); got != "ONDISK" {
		t.Error(test.DiffMessage(got, "ONDISK", "with an embed.FS, IsReload reads the same directory from disk"))
	}
	entries := logger.named("RenderReloadOn")
	if len(entries) != 1 || entries[0].level != "warn" {
		t.Error(test.DiffMessage(entries, "one Warn RenderReloadOn", "reload mode is visible in the log at start"))
	}
}

func TestRenderDir_ReloadWithoutDiskDirectoryPanics(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().EnableRender(&RenderOptions{FS: rdEmbedded, Dir: "no-such-dir", IsReload: true})
	})
	want := `invalid render: IsReload reads "no-such-dir" from disk, but there is no such directory under the working directory`
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "reload from a missing disk directory stops at start"))
	}
}

func TestRenderDir_NoReloadNoWarning(t *testing.T) {
	_, logger := rdApp(t, &RenderOptions{FS: fstest.MapFS{"web/x.html": {Data: []byte("x")}}})
	if n := len(logger.named("RenderReloadOn")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "no reload warning in production mode"))
	}
}
