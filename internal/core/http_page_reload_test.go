package core

import (
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

type rlSwapFS struct {
	mu    sync.RWMutex
	files fstest.MapFS
}

func (f *rlSwapFS) Open(name string) (fs.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.files.Open(name)
}

func (f *rlSwapFS) set(files fstest.MapFS) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = files
}

type rlOne struct{ common.HTTP }

func (c rlOne) NewController() Controller { return c }
func (c rlOne) RENDER_a()                 {}

type rlTwo struct{ common.HTTP }

func (c rlTwo) NewController() Controller { return c }
func (c rlTwo) RENDER_a()                 {}
func (c rlTwo) RENDER_p()                 {}

type rlX struct{ common.HTTP }

func (c rlX) NewController() Controller { return c }
func (c rlX) RENDER_x()                 {}

type rlPageOnly struct{ common.HTTP }

func (c rlPageOnly) NewController() Controller { return c }
func (c rlPageOnly) RENDER_page()              {}

func rlApp(t *testing.T, fsys fs.FS, isReload bool, controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{rlOne{}}
	}
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: fsys, Dir: ".", IsReload: isReload})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func rlFile(content string, mod time.Time) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content), ModTime: mod}
}

var rlEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestRender_NoReloadIgnoresChanges(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("old", rlEpoch)}}
	app, _ := rlApp(t, site, false)

	site.set(fstest.MapFS{
		"a.html": rlFile("new and longer", rlEpoch.Add(time.Hour)),
		"b.html": rlFile("added", rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "old" {
		t.Error(test.DiffMessage(got, "old", "R14: without IsReload a changed file is never read"))
	}
	if w := createGet(app, "/b"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "R14: an added file has no route"))
	}
}

func TestRender_ReloadSeesChanges(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("old", rlEpoch),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	}}
	app, _ := rlApp(t, site, true, rlTwo{})
	if got := createGet(app, "/a").Body.String(); got != "[old]" {
		t.Fatal(test.DiffMessage(got, "[old]", "R14: the first render"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("new text", rlEpoch),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "[new text]" {
		t.Error(test.DiffMessage(got, "[new text]", "R14: a page whose size changed"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("[{{template \"content\" .}}]", rlEpoch),
		"partials/field.html": rlFile("p1", rlEpoch),
		"a.html":              rlFile("NEW TEXT", rlEpoch.Add(time.Minute)),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "[NEW TEXT]" {
		t.Error(test.DiffMessage(got, "[NEW TEXT]", "R14: a page whose modification time changed"))
	}

	site.set(fstest.MapFS{
		"layout.html":         rlFile("<p>{{template \"content\" .}}</p>", rlEpoch),
		"partials/field.html": rlFile("p2-longer", rlEpoch),
		"a.html":              rlFile("NEW TEXT", rlEpoch.Add(time.Minute)),
		"p.html":              rlFile(`{{template "partials/field" .}}`, rlEpoch),
	})
	if got := createGet(app, "/a").Body.String(); got != "<p>NEW TEXT</p>" {
		t.Error(test.DiffMessage(got, "<p>NEW TEXT</p>", "R14: a changed layout"))
	}
	if got := createGet(app, "/p").Body.String(); got != "<p>p2-longer</p>" {
		t.Error(test.DiffMessage(got, "<p>p2-longer</p>", "R14: a changed partial"))
	}
}

func TestRender_ReloadRebinds(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("old form", rlEpoch)}}
	app, _ := rlApp(t, site, true, rlX{})
	if got := createGet(app, "/x").Body.String(); got != "old form" {
		t.Fatal(test.DiffMessage(got, "old form", "P10: the first render"))
	}

	site.set(fstest.MapFS{"x/index.html": rlFile("directory form", rlEpoch)})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "directory form" {
		t.Error(test.DiffMessage(w.Body.String(), "directory form", "P10: x.html moved to x/index.html, the route is bound again"))
	}
}

func TestRender_ReloadMissingTemplateIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("here", rlEpoch)}}
	app, logger := rlApp(t, site, true, rlX{})

	site.set(fstest.MapFS{"other.txt": rlFile("nothing", rlEpoch)})
	w := createGet(app, "/x")
	pgExpectHTMLFixed500(t, "P10: the template of a route is gone", w)
	if strings.Contains(w.Body.String(), "x.html") {
		t.Error(test.DiffMessage(w.Body.String(), "no file name", "P10: the cause is never in the response"))
	}
	entries := logger.named("RenderReloadFailed")
	if len(entries) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one RenderReloadFailed", "P10: logged with file and cause"))
	}
	if entries[0].field("file") == nil || entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(entries[0], "file and cause fields", "P10: RenderReloadFailed fields"))
	}

	site.set(fstest.MapFS{"x.html": rlFile("x", rlEpoch), "x/index.html": rlFile("twice", rlEpoch)})
	pgExpectHTMLFixed500(t, "P10: both templates exist", createGet(app, "/x"))

	site.set(fstest.MapFS{"x.html": rlFile("restored", rlEpoch.Add(time.Minute))})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "restored" {
		t.Error(test.DiffMessage(w.Body.String(), "restored", "P10: the page answers once the file is back"))
	}
}

func TestRender_ReloadIgnoresNewUnboundFile(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"x.html": rlFile("x", rlEpoch)}}
	app, logger := rlApp(t, site, true, rlX{})

	site.set(fstest.MapFS{
		"x.html":     rlFile("x", rlEpoch),
		"stray.html": rlFile("stray", rlEpoch),
		"orphan.css": rlFile("a{}", rlEpoch),
	})
	if w := createGet(app, "/x"); w.Code != http.StatusOK || w.Body.String() != "x" {
		t.Error(test.DiffMessage(w.Body.String(), "x", "P10: a file without a route is ignored until the restart"))
	}
	if w := createGet(app, "/stray"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P10: it has no route"))
	}
	if n := len(logger.named("RenderReloadFailed")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "P9, P30 are not repeated on reload"))
	}
}

func TestRender_ReloadLostPageAssetsCallIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{
		"layout.html": rlFile(`{{pageAssets}}{{template "content" .}}`, rlEpoch),
		"x.html":      rlFile("x", rlEpoch),
		"x.css":       rlFile("a{}", rlEpoch),
	}}
	app, _ := rlApp(t, site, true, rlX{})
	if got := createGet(app, "/x").Body.String(); got != `<link rel="stylesheet" href="/assets/_pages/x.css">x` {
		t.Fatal(test.DiffMessage(got, "a link without ?v=", "P29: IsReload tags have no version"))
	}
	site.set(fstest.MapFS{
		"layout.html": rlFile(`{{template "content" .}}`, rlEpoch.Add(time.Minute)),
		"x.html":      rlFile("x", rlEpoch),
		"x.css":       rlFile("a{}", rlEpoch),
	})
	pgExpectHTMLFixed500(t, "P10, P32: the layout lost its {{pageAssets}}", createGet(app, "/x"))
}

func TestRender_ReloadFailureIs500(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("fine", rlEpoch)}}
	app, logger := rlApp(t, site, true)

	site.set(fstest.MapFS{"a.html": rlFile("{{.Broken SECRETDETAIL", rlEpoch.Add(time.Minute))})
	for range 2 {
		w := createGet(app, "/a")
		pgExpectHTMLFixed500(t, "R14: a reload that fails to parse", w)
		if strings.Contains(w.Body.String(), "SECRETDETAIL") || strings.Contains(w.Body.String(), "a.html") {
			t.Error(test.DiffMessage(w.Body.String(), "no parse error text", "R14: the error is never in the response"))
		}
	}
	entries := logger.named("RenderReloadFailed")
	if len(entries) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one RenderReloadFailed", "R14: the failure is logged"))
	}
	if entries[0].level != "error" {
		t.Error(test.DiffMessage(entries[0].level, "error", "R14: RenderReloadFailed is an Error"))
	}
	if got := entries[0].field("file"); got != "a.html" {
		t.Error(test.DiffMessage(got, "a.html", "R14: the file field"))
	}
	if entries[0].field("cause") == nil {
		t.Error(test.DiffMessage(nil, "a cause", "R14: the cause field"))
	}

	site.set(fstest.MapFS{"a.html": rlFile("fixed again", rlEpoch.Add(2*time.Minute))})
	if w := createGet(app, "/a"); w.Code != http.StatusOK || w.Body.String() != "fixed again" {
		t.Error(test.DiffMessage(w.Body.String(), "fixed again", "R14: the page answers again once the file is fixed"))
	}
}

func TestRender_ReloadConcurrent(t *testing.T) {
	site := &rlSwapFS{files: fstest.MapFS{"a.html": rlFile("state-A", rlEpoch)}}
	app, _ := rlApp(t, site, true)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				site.set(fstest.MapFS{"a.html": rlFile("state-B-longer", rlEpoch.Add(time.Duration(i)*time.Second))})
			} else {
				site.set(fstest.MapFS{"a.html": rlFile("state-A", rlEpoch.Add(time.Duration(i)*time.Second))})
			}
		}
	}()

	var renderers sync.WaitGroup
	var mu sync.Mutex
	var bad []string
	for g := 0; g < 32; g++ {
		renderers.Add(1)
		go func() {
			defer renderers.Done()
			for range 40 {
				w := createGet(app, "/a")
				if w.Code != http.StatusOK || (w.Body.String() != "state-A" && w.Body.String() != "state-B-longer") {
					mu.Lock()
					bad = append(bad, w.Body.String())
					mu.Unlock()
				}
			}
		}()
	}
	renderers.Wait()
	close(stop)
	wg.Wait()
	if len(bad) != 0 {
		t.Error(test.DiffMessage(bad[0], "state-A or state-B-longer", "R14: every concurrent render sees one whole state"))
	}
}
