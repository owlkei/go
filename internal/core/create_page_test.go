package core

import (
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

type cpBothController struct {
	common.HTTP
	common.Page
}

func (c cpBothController) NewController() Controller { return c }
func (c cpBothController) READ_x() common.View       { return common.View{Name: "x"} }

type cpPageController struct{ common.Page }

func (c cpPageController) NewController() Controller { return c }
func (c cpPageController) READ_x() common.View       { return common.View{Name: "x"} }

type cpAnyResultController struct{ common.Page }

func (c cpAnyResultController) NewController() Controller { return c }
func (c cpAnyResultController) READ_x() any               { return common.View{Name: "x"} }

type cpStringResultController struct{ common.Page }

func (c cpStringResultController) NewController() Controller { return c }
func (c cpStringResultController) READ_x() string            { return "x" }

type cpNoResultController struct{ common.Page }

func (c cpNoResultController) NewController() Controller { return c }
func (c cpNoResultController) READ_x()                   {}

type cpIntViewController struct{ common.Page }

func (c cpIntViewController) NewController() Controller { return c }
func (c cpIntViewController) READ_x() (int, common.View) {
	return 422, common.View{Name: "x"}
}

type cpServeController struct{ common.Page }

func (c cpServeController) NewController() Controller { return c }
func (c cpServeController) READ_x() common.View       { return common.View{Name: "x"} }
func (c cpServeController) SERVE_files() string       { return "." }

type cpHTTPViewController struct{ common.HTTP }

func (c cpHTTPViewController) NewController() Controller { return c }
func (c cpHTTPViewController) READ_x() common.View       { return common.View{Name: "x"} }

type cpHTTPIntViewController struct{ common.HTTP }

func (c cpHTTPIntViewController) NewController() Controller { return c }
func (c cpHTTPIntViewController) READ_x() (int, common.View) {
	return 200, common.View{Name: "x"}
}

func cpFS() fstest.MapFS {
	return fstest.MapFS{"x.html": {Data: []byte("x")}}
}

func cpCreate(opts *RenderOptions, controllers ...Controller) func() {
	return func() {
		app := New()
		if opts != nil {
			app.EnableRender(opts)
		}
		app.Create(NewModuleBuilder().Controllers(controllers...).Build())
	}
}

func TestCreate_PageAndHTTPPanics(t *testing.T) {
	msExpectPanic(t, "R2", "invalid controller: cpBothController embeds both ginject.HTTP and ginject.Page",
		cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpBothController{}))
}

func TestCreate_PageWithoutEnableRenderPanics(t *testing.T) {
	msExpectPanic(t, "R2", "invalid render: cpPageController embeds ginject.Page, call EnableRender before Create",
		cpCreate(nil, cpPageController{}))
}

func TestCreate_PageHandlerMustReturnView(t *testing.T) {
	opts := &RenderOptions{FS: cpFS(), Dir: "."}
	msExpectPanic(t, "R2", "invalid handler: cpAnyResultController.READ_x must return ginject.View",
		cpCreate(opts, cpAnyResultController{}))
	msExpectPanic(t, "R2", "invalid handler: cpStringResultController.READ_x must return ginject.View",
		cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpStringResultController{}))
	msExpectPanic(t, "R2", "invalid handler: cpNoResultController.READ_x must return ginject.View",
		cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpNoResultController{}))

	if text, panicked := createPanicText(cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpIntViewController{})); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R2: (int, ginject.View) is accepted"))
	}
	if text, panicked := createPanicText(cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpServeController{})); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R2: a SERVE handler on a page controller is accepted"))
	}
}

func TestCreate_ViewNeedsPage(t *testing.T) {
	msExpectPanic(t, "R2", "invalid handler: cpHTTPViewController.READ_x returns ginject.View, which needs a ginject.Page controller",
		cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpHTTPViewController{}))
	msExpectPanic(t, "R2", "invalid handler: cpHTTPIntViewController.READ_x returns ginject.View, which needs a ginject.Page controller",
		cpCreate(&RenderOptions{FS: cpFS(), Dir: "."}, cpHTTPIntViewController{}))
}

func TestEnableRender_NilPanics(t *testing.T) {
	msExpectPanic(t, "R3", "invalid render: EnableRender needs RenderOptions.FS", func() {
		New().EnableRender(nil)
	})
	msExpectPanic(t, "R3", "invalid render: EnableRender needs RenderOptions.FS", func() {
		New().EnableRender(&RenderOptions{})
	})
}

func TestEnableRender_TwicePanics(t *testing.T) {
	msExpectPanic(t, "R3", "invalid render: EnableRender called twice", func() {
		app := New()
		app.EnableRender(&RenderOptions{FS: cpFS(), Dir: "."})
		app.EnableRender(&RenderOptions{FS: cpFS(), Dir: "."})
	})
}

func TestEnableRender_AfterCreatePanics(t *testing.T) {
	app := msCreated()
	msExpectPanic(t, "R3", "invalid app: EnableRender must be called before Create", func() {
		app.EnableRender(&RenderOptions{FS: cpFS(), Dir: "."})
	})
}

func TestEnableRender_ReturnsApp(t *testing.T) {
	app := New()
	if got := app.EnableRender(&RenderOptions{FS: cpFS(), Dir: "."}); got != app {
		t.Error(test.DiffMessage(got, app, "R3: EnableRender returns the app"))
	}
}

func cpCreateFiles(files map[string]string) func() {
	return cpCreate(&RenderOptions{FS: pgFiles(files), Dir: "."}, cpPageController{})
}

func TestCreate_TemplateParseErrorPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{"x.html": "{{.Open"}))
	msExpectPanic(t, "R4", "invalid render: sub/y.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/y.html": "{{if}}"}))
	msExpectPanic(t, "R4", "invalid render: layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "layout.html": "{{template \"content\" .}}{{end}}"}))
	msExpectPanic(t, "R4", "invalid render: sub/layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/layout.html": "{{range}}"}))
	msExpectPanic(t, "R4", "invalid render: partials/p.html:", cpCreateFiles(map[string]string{"x.html": "ok", "partials/p.html": "{{if .A}}open"}))
	msExpectPanic(t, "R4", "invalid render: error.html:", cpCreateFiles(map[string]string{"x.html": "ok", "error.html": "{{.Code"}))
}

func TestCreate_MissingPartialPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{
		"x.html": `{{template "partials/nope" .}}`,
	}))
	msExpectPanic(t, "R4", "invalid render: sub/z.html:", cpCreateFiles(map[string]string{
		"x.html":          `fine`,
		"layout.html":     `{{template "content" .}}`,
		"sub/z.html":      `{{template "partials/missing" .}}`,
		"partials/a.html": `a`,
	}))
}

func TestCreate_EscapeErrorPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{
		"x.html": `<a href='{{.X}}`,
	}))
}

func TestCreate_IgnoresNonTemplateFilesAndHiddenPaths(t *testing.T) {
	files := map[string]string{
		"x.html":           "ok",
		"notes.txt":        "{{ broken",
		".secret.html":     "{{ broken",
		".git/config.html": "{{ broken",
		"assets/page.html": "{{ broken",
	}
	if text, panicked := createPanicText(cpCreateFiles(files)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "R4: files that are no templates are never parsed"))
	}
}
