package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type dvrPage struct{ common.HTTP }

func (c dvrPage) NewController() Controller { return c }
func (c dvrPage) RENDER_dvrpage()           {}
func (c dvrPage) SUBMIT_dvrpage()           {}

type dvrAPI struct{ common.HTTP }

func (c dvrAPI) NewController() Controller { return c }
func (c dvrAPI) READ_dvrapi() string       { return "api" }

type dvrSnap struct{ common.HTTP }

func (c dvrSnap) NewController() Controller   { return c }
func (c dvrSnap) RENDER_a()                   {}
func (c dvrSnap) RENDER_c_OF_b()              {}
func (c dvrSnap) RENDER_nodata()              {}
func (c dvrSnap) RENDER_rows()                {}
func (c dvrSnap) RENDER_user_BY_id(ctx.Param) {}

func dvrDecode(t *testing.T, app *App) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(app.devtool.GetConfigurationResponse)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func dvrKinds(t *testing.T, snapshot map[string]any) map[string]any {
	t.Helper()
	controller, _ := snapshot["controller"].(map[string]any)
	entries, _ := controller["http"].([]any)
	kinds := map[string]any{}
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		route, _ := entry["route"].(string)
		method, _ := entry["http_method"].(string)
		kinds[method+" "+route] = entry["kind"]
	}
	return kinds
}

func dvrApp(t *testing.T, files map[string]string, controllers ...Controller) *App {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableDevtool()
	if files != nil {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

func dvrSnapFiles() map[string]string {
	return map[string]string{
		"layout.html":             `{{pageAssets}}<html>{{template "content" .}}</html>`,
		"layout.css":              `l{}`,
		"a.html":                  `A`,
		"a.css":                   `a{}`,
		"b/layout.html":           `{{pageAssets}}<div>{{template "content" .}}</div>`,
		"b/c.html":                `C`,
		"nodata.html":             `{{template "partials/p" .}}`,
		"rows.html":               `{{template "partials/q" (dict "odd")}}`,
		"user/{id}.html":          `U`,
		"partials/p.html":         `P`,
		"partials/q.html":         `Q`,
		"404.html":                `{{.Code}}`,
		"5xx.html":                `{{.Code}}`,
		"assets/app.css":          `a{}`,
		"assets/js/app.js":        `1`,
		"assets/.hidden/x.js":     `2`,
		"public/robots.txt":       `User-agent: *`,
		"public/site.webmanifest": `{}`,
	}
}

func TestDevtool_PageKind(t *testing.T) {
	app := dvrApp(t, map[string]string{"dvrpage.html": "A"}, dvrPage{}, dvrAPI{})

	kinds := dvrKinds(t, dvrDecode(t, app))
	var page, submit, api string
	for key := range kinds {
		switch {
		case strings.HasPrefix(key, "GET") && strings.Contains(key, "dvrpage"):
			page = key
		case strings.HasPrefix(key, "POST") && strings.Contains(key, "dvrpage"):
			submit = key
		case strings.Contains(key, "dvrapi"):
			api = key
		}
	}
	if page == "" || submit == "" || api == "" {
		t.Fatal(test.DiffMessage(kinds, "a RENDER_, a SUBMIT_ and an API route", "R42: all routes are in the snapshot"))
	}
	if kinds[page] != "page" {
		t.Error(test.DiffMessage(kinds[page], "page", "R42: a RENDER_ route has kind page"))
	}
	if kinds[submit] != "page" {
		t.Error(test.DiffMessage(kinds[submit], "page", "R42: a SUBMIT_ route has kind page"))
	}
	if kinds[api] != nil {
		t.Error(test.DiffMessage(kinds[api], nil, "R42: an API route has no kind"))
	}
}

func TestDevtool_NoRenderWithoutEnableRender(t *testing.T) {
	app := dvrApp(t, nil, dvrAPI{})

	snapshot := dvrDecode(t, app)
	if _, ok := snapshot["render"]; ok {
		t.Error(test.DiffMessage(snapshot["render"], "no render section", "R43: omitted without EnableRender"))
	}
	for route, kind := range dvrKinds(t, snapshot) {
		if kind != nil {
			t.Error(test.DiffMessage(kind, nil, "R42: no kind on an app without EnableRender: "+route))
		}
	}
}

func TestDevtool_RenderSnapshot(t *testing.T) {
	app := dvrApp(t, dvrSnapFiles(), dvrSnap{})

	for i := 0; i < 3; i++ {
		createGet(app, "/a")
	}
	if w := createGet(app, "/rows"); w.Code != http.StatusInternalServerError {
		t.Fatal(test.DiffMessage(w.Code, http.StatusInternalServerError, "R43: a failing render"))
	}

	snapshot := dvrDecode(t, app)
	render, ok := snapshot["render"].(map[string]any)
	if !ok {
		t.Fatal(test.DiffMessage(snapshot["render"], "a render section", "R43: present with EnableRender"))
	}
	if render["is_reload"] != nil && render["is_reload"] != false {
		t.Error(test.DiffMessage(render["is_reload"], false, "R43: is_reload"))
	}
	if _, old := render["error_template"]; old {
		t.Error(test.DiffMessage(render["error_template"], "no error_template key", "R43: error_template became error_templates"))
	}
	templates, _ := json.Marshal(render["error_templates"])
	if string(templates) != `["404.html","5xx.html"]` {
		t.Error(test.DiffMessage(string(templates), `["404.html","5xx.html"]`, "R43: error_templates are the file names"))
	}
	if render["assets"] != float64(2) {
		t.Error(test.DiffMessage(render["assets"], float64(2), "R43: assets counts the served files of assets/"))
	}
	if render["public"] != float64(2) {
		t.Error(test.DiffMessage(render["public"], float64(2), "R43: public counts the files of public/"))
	}

	pages, _ := render["pages"].([]any)
	type info struct {
		route, layout string
		assets        []string
	}
	got := map[string]info{}
	for _, p := range pages {
		entry, _ := p.(map[string]any)
		name, _ := entry["name"].(string)
		route, _ := entry["route"].(string)
		layout, _ := entry["layout"].(string)
		var assets []string
		list, _ := entry["assets"].([]any)
		for _, a := range list {
			assets = append(assets, a.(string))
		}
		got[name] = info{strings.Trim(route, "/"), layout, assets}
	}
	if len(got) != 5 {
		t.Fatal(test.DiffMessage(len(got), 5, "R41, R43: one entry per page route, named by its template path"))
	}
	want := map[string]info{
		"a":         {"a", "layout.html", []string{"layout.css", "a.css"}},
		"b/c":       {"b/c", "b/layout.html", nil},
		"nodata":    {"nodata", "layout.html", []string{"layout.css"}},
		"rows":      {"rows", "layout.html", []string{"layout.css"}},
		"user/{id}": {"user/{id}", "layout.html", []string{"layout.css"}},
	}
	for name, w := range want {
		g := got[name]
		if g.route != w.route || g.layout != w.layout || strings.Join(g.assets, ",") != strings.Join(w.assets, ",") {
			t.Error(test.DiffMessage(g, w, "R43: page "+name+" with its route, layout file and CSS and JS files"))
		}
	}
	partials, _ := json.Marshal(render["partials"])
	if string(partials) != `["partials/p","partials/q"]` {
		t.Error(test.DiffMessage(string(partials), `["partials/p","partials/q"]`, "R43: partials"))
	}
	renders, _ := render["renders"].(float64)
	if renders < 3 || render["failures"] != float64(1) {
		t.Error(test.DiffMessage([]any{render["renders"], render["failures"]}, []any{"3 or more", float64(1)}, "R43: counters"))
	}

	createGet(app, "/a")
	after, _ := dvrDecode(t, app)["render"].(map[string]any)
	if got, _ := after["renders"].(float64); got <= renders {
		t.Error(test.DiffMessage(got, "more than "+sprint(renders), "R43: renders counts live"))
	}
}

func TestDevtool_RenderSnapshotBuiltInError(t *testing.T) {
	app := dvrApp(t, map[string]string{"dvrpage.html": "A"}, dvrPage{})

	render, _ := dvrDecode(t, app)["render"].(map[string]any)
	if _, ok := render["error_templates"]; ok {
		t.Error(test.DiffMessage(render["error_templates"], "absent", "R43: error_templates is absent when only the built-in page exists"))
	}
	if render["assets"] != nil && render["assets"] != float64(0) {
		t.Error(test.DiffMessage(render["assets"], 0, "R43: no assets"))
	}
	if render["public"] != nil && render["public"] != float64(0) {
		t.Error(test.DiffMessage(render["public"], 0, "R43: no public files"))
	}
}
