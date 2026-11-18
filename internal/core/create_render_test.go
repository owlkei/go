package core

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type cpPageController struct{ common.HTTP }

func (c cpPageController) NewController() Controller { return c }
func (c cpPageController) RENDER_x()                 {}

type cpAnyResultController struct{ common.HTTP }

func (c cpAnyResultController) NewController() Controller { return c }
func (c cpAnyResultController) RENDER_x() any             { return 1 }
func (c cpAnyResultController) RENDER_s() string          { return "s" }
func (c cpAnyResultController) RENDER_n()                 {}
func (c cpAnyResultController) RENDER_i() (int, ctx.Map)  { return 422, nil }
func (c cpAnyResultController) RENDER_e() error           { return nil }
func (c cpAnyResultController) SERVE_files() string       { return "." }

type cpBookingParam struct{ common.HTTP }

func (c cpBookingParam) NewController() Controller       { return c }
func (c cpBookingParam) RENDER_bookings_BY_id(ctx.Param) {}

type cpPartialsPrefix struct{ common.HTTP }

func (c cpPartialsPrefix) NewController() Controller {
	c.Prefix("partials")
	return c
}
func (c cpPartialsPrefix) RENDER_app() {}

type cpAssetsPrefix struct{ common.HTTP }

func (c cpAssetsPrefix) NewController() Controller {
	c.Prefix("assets")
	return c
}
func (c cpAssetsPrefix) RENDER_app() {}

type cpPublicPath struct{ common.HTTP }

func (c cpPublicPath) NewController() Controller {
	c.Path("/public/app", c.RENDER_x)
	return c
}
func (c cpPublicPath) RENDER_x() {}

type cpAny struct{ common.HTTP }

func (c cpAny) NewController() Controller { return c }
func (c cpAny) RENDER_files_ANY()         {}

type cpVersion struct{ common.HTTP }

func (c cpVersion) NewController() Controller { return c }
func (c cpVersion) RENDER_x_VERSION_1()       {}

type cpFile struct{ common.HTTP }

func (c cpFile) NewController() Controller { return c }
func (c cpFile) RENDER_feed_xml_FILE()     {}

type cpSubmitAny struct{ common.HTTP }

func (c cpSubmitAny) NewController() Controller { return c }
func (c cpSubmitAny) RENDER_x()                 {}
func (c cpSubmitAny) SUBMIT_x_ANY()             {}

type cpSubmitVersion struct{ common.HTTP }

func (c cpSubmitVersion) NewController() Controller { return c }
func (c cpSubmitVersion) RENDER_x()                 {}
func (c cpSubmitVersion) SUBMIT_x_VERSION_1()       {}

type cpSubmitFile struct{ common.HTTP }

func (c cpSubmitFile) NewController() Controller { return c }
func (c cpSubmitFile) RENDER_x()                 {}
func (c cpSubmitFile) SUBMIT_x_FILE()            {}

type cpWildcardPath struct{ common.HTTP }

func (c cpWildcardPath) NewController() Controller {
	c.Path("/docs/*", c.RENDER_x)
	return c
}
func (c cpWildcardPath) RENDER_x() {}

type cpWildcardPrefix struct{ common.HTTP }

func (c cpWildcardPrefix) NewController() Controller {
	c.Prefix("docs/*")
	return c
}
func (c cpWildcardPrefix) RENDER_x() {}

type cpR404 struct{ common.HTTP }

func (c cpR404) NewController() Controller { return c }
func (c cpR404) RENDER_404()               {}

type cpR5xx struct{ common.HTTP }

func (c cpR5xx) NewController() Controller { return c }
func (c cpR5xx) RENDER_5xx()               {}

type cpPath404 struct{ common.HTTP }

func (c cpPath404) NewController() Controller {
	c.Path("/404", c.RENDER_x)
	return c
}
func (c cpPath404) RENDER_x() {}

type cpAdmin404 struct{ common.HTTP }

func (c cpAdmin404) NewController() Controller {
	c.Prefix("admin")
	return c
}
func (c cpAdmin404) RENDER_404() {}

type cpSubmitOnly struct{ common.HTTP }

func (c cpSubmitOnly) NewController() Controller { return c }
func (c cpSubmitOnly) SUBMIT_x()                 {}

type cpRenderElsewhere struct{ common.HTTP }

func (c cpRenderElsewhere) NewController() Controller { return c }
func (c cpRenderElsewhere) RENDER_x()                 {}

type cpSubmitPath struct{ common.HTTP }

func (c cpSubmitPath) NewController() Controller {
	c.Path("/y", c.SUBMIT_x)
	return c
}
func (c cpSubmitPath) RENDER_x() {}
func (c cpSubmitPath) SUBMIT_x() {}

type cpSubmitPrefix struct{ common.HTTP }

func (c cpSubmitPrefix) NewController() Controller {
	c.Prefix("p", c.SUBMIT_x)
	return c
}
func (c cpSubmitPrefix) RENDER_x() {}
func (c cpSubmitPrefix) SUBMIT_x() {}

type cpSubmitMovesWithRender struct{ common.HTTP }

func (c cpSubmitMovesWithRender) NewController() Controller {
	c.Path("/moved", c.RENDER_x)
	c.Prefix("p", c.RENDER_x)
	return c
}
func (c cpSubmitMovesWithRender) RENDER_x()                   {}
func (c cpSubmitMovesWithRender) SUBMIT_x(h *ctx.HTTPContext) { h.Redirect("/done") }

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

func cpCreateFiles(files map[string]string) func() {
	return cpCreate(&RenderOptions{FS: pgFiles(files), Dir: "."}, cpPageController{})
}

func cpCreateWith(files map[string]string, controllers ...Controller) func() {
	return cpCreate(&RenderOptions{FS: pgFiles(files), Dir: "."}, controllers...)
}

func TestCreate_RenderWithoutEnableRenderPanics(t *testing.T) {
	msExpectPanic(t, "P3", "invalid render: cpPageController.RENDER_x is a page route, call EnableRender before Create",
		cpCreate(nil, cpPageController{}))
	msExpectPanic(t, "P3", "invalid render: cpRenderElsewhere.RENDER_x is a page route, call EnableRender before Create",
		cpCreate(nil, cpRenderElsewhere{}))
}

func TestCreate_RenderResultMayBeAnyType(t *testing.T) {
	files := map[string]string{"x.html": "x", "s.html": "s", "n.html": "n", "i.html": "i", "e.html": "e"}
	if text, panicked := createPanicText(cpCreateWith(files, cpAnyResultController{})); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P6: a page handler may return any type or nothing; a SERVE handler on the same controller is accepted"))
	}
}

func TestCreate_RenderMissingTemplatePanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPageController.RENDER_x needs a template: x.html or x/index.html",
		cpCreateFiles(map[string]string{"layout.html": `{{template "content" .}}`}))
	msExpectPanic(t, "P4", "invalid render: cpBookingParam.RENDER_bookings_BY_id needs a template: bookings/{id}.html or bookings/{id}/index.html",
		cpCreateWith(map[string]string{"bookings/{slug}.html": "x"}, cpBookingParam{}))
	msExpectPanic(t, "P4", "invalid render: cpAdmin404.RENDER_404 needs a template: admin/404.html or admin/404/index.html",
		cpCreateWith(map[string]string{"layout.html": `{{template "content" .}}`}, cpAdmin404{}))
}

func TestCreate_RenderTwoTemplatesPanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPageController.RENDER_x has two templates: x.html and x/index.html",
		cpCreateFiles(map[string]string{"x.html": "a", "x/index.html": "b"}))
}

func TestCreate_RenderReservedDirPanics(t *testing.T) {
	msExpectPanic(t, "P4", "invalid render: cpPartialsPrefix.RENDER_app can have no template: partials/ is reserved",
		cpCreateWith(map[string]string{"partials/app.html": "x"}, cpPartialsPrefix{}))
	msExpectPanic(t, "P4", "invalid render: cpAssetsPrefix.RENDER_app can have no template: assets/ is reserved",
		cpCreateWith(map[string]string{"assets/app.html": "x"}, cpAssetsPrefix{}))
	msExpectPanic(t, "P4", "invalid render: cpPublicPath.RENDER_x can have no template: public/ is reserved",
		cpCreateWith(map[string]string{"public/app.html": "x"}, cpPublicPath{}))
}

func TestCreate_PageRouteWordsPanic(t *testing.T) {
	msExpectPanic(t, "P5", "invalid route: ANY in RENDER_files_ANY: a page route has no wildcard",
		cpCreateWith(map[string]string{"files.html": "x"}, cpAny{}))
	msExpectPanic(t, "P5", "invalid route: VERSION in RENDER_x_VERSION_1: a page route has no version",
		cpCreateWith(map[string]string{"x.html": "x"}, cpVersion{}))
	msExpectPanic(t, "P5", "invalid route: FILE in RENDER_feed_xml_FILE: a page route has no file extension",
		cpCreateWith(map[string]string{"feed.xml.html": "x"}, cpFile{}))
	msExpectPanic(t, "P5", "invalid route: ANY in SUBMIT_x_ANY: a page route has no wildcard",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitAny{}))
	msExpectPanic(t, "P5", "invalid route: VERSION in SUBMIT_x_VERSION_1: a page route has no version",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitVersion{}))
	msExpectPanic(t, "P5", "invalid route: FILE in SUBMIT_x_FILE: a page route has no file extension",
		cpCreateWith(map[string]string{"x.html": "x"}, cpSubmitFile{}))
}

func TestCreate_PageRouteWildcardPathPanics(t *testing.T) {
	files := map[string]string{"x.html": "x", "docs/x.html": "x"}
	msExpectPanic(t, "P5", "invalid route: cpWildcardPath.RENDER_x is a page route and its path", cpCreateWith(files, cpWildcardPath{}))
	msExpectPanic(t, "P5", "holds a wildcard", cpCreateWith(files, cpWildcardPath{}))
	msExpectPanic(t, "P5", "invalid route: cpWildcardPrefix.RENDER_x is a page route and its path", cpCreateWith(files, cpWildcardPrefix{}))
}

func TestCreate_UnboundTemplatePanics(t *testing.T) {
	msExpectPanic(t, "P9", "invalid render: bookings/detial.html has no RENDER_ route: add one, move it to partials/, or delete it",
		cpCreateFiles(map[string]string{"x.html": "x", "bookings/detail.html": "d", "bookings/detial.html": "d"}))
	msExpectPanic(t, "P9", "invalid render: error.html has no RENDER_ route: add one, move it to partials/, or delete it",
		cpCreateFiles(map[string]string{"x.html": "x", "error.html": "{{.Code}}"}))
	msExpectPanic(t, "P9", "has no RENDER_ route",
		cpCreateFiles(map[string]string{"x.html": "x", "layout.html": `{{template "content" .}}`, "admin/layout.html": `{{template "content" .}}`, "admin/stray.html": "s"}))

	files := map[string]string{
		"x.html":             "x",
		"layout.html":        `{{template "content" .}}`,
		"partials/p.html":    "p",
		"404.html":           "e",
		"4xx.html":           "e",
		"assets/page.html":   "{{ not parsed",
		"public/index.html":  "{{ not parsed",
		"public/docs/a.html": "{{ not parsed",
		"assets/app.css":     "a{}",
	}
	if text, panicked := createPanicText(cpCreateFiles(files)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P9: layouts, partials, error pages and files under assets and public are not strays"))
	}
}

func TestCreate_RenderReservedNamePanics(t *testing.T) {
	msExpectPanic(t, "P20", "invalid render: cpR404.RENDER_404 resolves to 404.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"404.html": "x"}, cpR404{}))
	msExpectPanic(t, "P20", "invalid render: cpR5xx.RENDER_5xx resolves to 5xx.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"5xx.html": "x"}, cpR5xx{}))
	msExpectPanic(t, "P20", "invalid render: cpPath404.RENDER_x resolves to 404.html, which is an error page, not a route",
		cpCreateWith(map[string]string{"404.html": "x"}, cpPath404{}))

	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"admin/404.html": "ADMIN-404"}), Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(cpAdmin404{}).Build())
	if w := createGet(app, "/admin/404"); w.Code != http.StatusOK || w.Body.String() != "ADMIN-404" {
		t.Error(test.DiffMessage(w.Body.String(), "ADMIN-404", "P20: under a prefix the name is an ordinary page"))
	}

	prefixed := New()
	prefixed.SetLogger(&lcRecordingLogger{})
	prefixed.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"site/404.html": "SITE-404"}), Dir: "."})
	module := NewModuleBuilder().Controllers(cpR404{}).Build()
	module.Prefix("site")
	mustCreate(t, prefixed, NewModuleBuilder().Imports(module).Build())
	if w := createGet(prefixed, "/site/404"); w.Code != http.StatusOK || w.Body.String() != "SITE-404" {
		t.Error(test.DiffMessage(w.Body.String(), "SITE-404", "P20: a module prefix removes the conflict"))
	}
}

func TestCreate_UnusableErrorNamePanics(t *testing.T) {
	for _, name := range []string{"200.html", "302.html", "3xx.html", "600.html"} {
		msExpectPanic(t, "P21", "invalid render: "+name+" is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html",
			cpCreateFiles(map[string]string{"x.html": "x", name: "x"}))
	}
}

func TestCreate_ReservedPagesDirPanics(t *testing.T) {
	msExpectPanic(t, "P34", "invalid render: assets/_pages is reserved for page CSS and JS",
		cpCreateFiles(map[string]string{"x.html": "x", "assets/_pages/x.css": "x"}))
	msExpectPanic(t, "P34", "invalid render: assets/_pages is reserved for page CSS and JS",
		cpCreateFiles(map[string]string{"x.html": "x", "assets/_pages": "x"}))
	msExpectPanic(t, "P34", "invalid render: x.html:",
		cpCreateFiles(map[string]string{"x.html": `{{asset "_pages/x.css"}}`, "x.css": "x"}))
}

func TestCreate_OrphanPageAssetPanics(t *testing.T) {
	const tail = "belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/"
	msExpectPanic(t, "P30", "invalid render: nw.css "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "nw.css": "x"}))
	msExpectPanic(t, "P30", "invalid render: partials/field.css "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "partials/field.html": "f", "partials/field.css": "x"}))
	msExpectPanic(t, "P30", "invalid render: sub/y.js "+tail,
		cpCreateFiles(map[string]string{"x.html": "x", "sub/y.js": "x"}))
}

func TestCreate_MissingPageAssetsCallPanics(t *testing.T) {
	msExpectPanic(t, "P32", "invalid render: x.html has x.css, but no template of the page calls {{pageAssets}}: add it to layout.html",
		cpCreateFiles(map[string]string{"layout.html": `{{template "content" .}}`, "x.html": "x", "x.css": "x"}))
	msExpectPanic(t, "P32", "invalid render: x.html has x.css, but no template of the page calls {{pageAssets}}: add it to the page",
		cpCreateFiles(map[string]string{"x.html": "x", "x.css": "x"}))

	partial := map[string]string{
		"layout.html":        `{{template "partials/head" .}}{{template "content" .}}`,
		"partials/head.html": `{{pageAssets}}`,
		"x.html":             "x",
		"x.css":              "x",
	}
	if text, panicked := createPanicText(cpCreateFiles(partial)); panicked {
		t.Error(test.DiffMessage(text, "no panic", "P32: the call inside a partial counts"))
	}
}

func TestCreate_TemplateParseErrorPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{"x.html": "{{.Open"}))
	msExpectPanic(t, "R4", "invalid render: sub/y.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/y.html": "{{if}}"}))
	msExpectPanic(t, "R4", "invalid render: layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "layout.html": "{{template \"content\" .}}{{end}}"}))
	msExpectPanic(t, "R4", "invalid render: sub/layout.html:", cpCreateFiles(map[string]string{"x.html": "ok", "sub/layout.html": "{{range}}"}))
	msExpectPanic(t, "R4", "invalid render: partials/p.html:", cpCreateFiles(map[string]string{"x.html": "ok", "partials/p.html": "{{if .A}}open"}))
	msExpectPanic(t, "P18", "invalid render: 404.html:", cpCreateFiles(map[string]string{"x.html": "ok", "404.html": "{{.Code"}))
	msExpectPanic(t, "P18", "invalid render: 4xx.html:", cpCreateFiles(map[string]string{"x.html": "ok", "4xx.html": "{{.Code"}))
	msExpectPanic(t, "P18", "invalid render: 5xx.html:", cpCreateFiles(map[string]string{"x.html": "ok", "5xx.html": "<a href='{{.Code}}"}))
}

func TestCreate_MissingPartialPanics(t *testing.T) {
	msExpectPanic(t, "R4", "invalid render: x.html:", cpCreateFiles(map[string]string{
		"x.html": `{{template "partials/nope" .}}`,
	}))
	msExpectPanic(t, "R4", "invalid render: sub/z.html:", cpCreateWith(map[string]string{
		"x.html":          `fine`,
		"layout.html":     `{{template "content" .}}`,
		"sub/z.html":      `{{template "partials/missing" .}}`,
		"partials/a.html": `a`,
	}, cpPageController{}, cpSubZ{}))
}

type cpSubZ struct{ common.HTTP }

func (c cpSubZ) NewController() Controller { return c }
func (c cpSubZ) RENDER_z_OF_sub()          {}

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

func TestCreate_SubmitWithoutRenderPanics(t *testing.T) {
	files := map[string]string{"x.html": "x"}
	msExpectPanic(t, "P13", "invalid handler: cpSubmitOnly.SUBMIT_x needs cpSubmitOnly.RENDER_x: a SUBMIT_ posts to the page that shows its form",
		cpCreateWith(files, cpSubmitOnly{}))
	msExpectPanic(t, "P13", "invalid handler: cpSubmitOnly.SUBMIT_x needs cpSubmitOnly.RENDER_x: a SUBMIT_ posts to the page that shows its form",
		cpCreateWith(files, cpSubmitOnly{}, cpRenderElsewhere{}))
}

func TestCreate_SubmitPathPanics(t *testing.T) {
	files := map[string]string{"x.html": "x", "y.html": "y", "p/x.html": "x"}
	msExpectPanic(t, "P15", `invalid route: Path("/y") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_`,
		cpCreateWith(files, cpSubmitPath{}))
	msExpectPanic(t, "P15", `invalid route: Prefix("p") is bound to 'SUBMIT_x': a SUBMIT_ uses the path of its RENDER_`,
		cpCreateWith(files, cpSubmitPrefix{}))
}

func TestSubmit_PathAndPrefixOfRenderMoveBoth(t *testing.T) {
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"p/moved.html": "FORM"}), Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(cpSubmitMovesWithRender{}).Build())

	if w := createGet(app, "/p/moved"); w.Body.String() != "FORM" {
		t.Error(test.DiffMessage(w.Body.String(), "FORM", "P15: Path and Prefix on the RENDER_ move the page"))
	}
	if w := createRequest(app, http.MethodPost, "/p/moved"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P15: and the SUBMIT_ with it"))
	}
	if w := createRequest(app, http.MethodPost, "/x"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P15: the SUBMIT_ has no path of its own"))
	}
}
