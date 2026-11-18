package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

type dvrPage struct{ common.Page }

func (c dvrPage) NewController() Controller { return c }
func (c dvrPage) READ_dvrpage() common.View { return common.View{Name: "a"} }

type dvrAPI struct{ common.HTTP }

func (c dvrAPI) NewController() Controller { return c }
func (c dvrAPI) READ_dvrapi() string       { return "api" }

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
		kinds[route] = entry["kind"]
	}
	return kinds
}

func dvrApp(t *testing.T, files map[string]string, withRender bool) *App {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableDevtool()
	if withRender {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
		mustCreate(t, app, NewModuleBuilder().Controllers(dvrPage{}, dvrAPI{}, pgPages{}).Build())
		return app
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(dvrAPI{}).Build())
	return app
}

func TestDevtool_PageKind(t *testing.T) {
	app := dvrApp(t, map[string]string{"a.html": "A"}, true)

	var pageRoute, apiRoute string
	for route := range dvrKinds(t, dvrDecode(t, app)) {
		switch {
		case strings.Contains(route, "dvrpage"):
			pageRoute = route
		case strings.Contains(route, "dvrapi"):
			apiRoute = route
		}
	}
	kinds := dvrKinds(t, dvrDecode(t, app))
	if pageRoute == "" || apiRoute == "" {
		t.Fatal(test.DiffMessage(kinds, "a page and an API route", "R42: both routes are in the snapshot"))
	}
	if kinds[pageRoute] != "page" {
		t.Error(test.DiffMessage(kinds[pageRoute], "page", "R42: a page route has kind page"))
	}
	if kinds[apiRoute] != nil {
		t.Error(test.DiffMessage(kinds[apiRoute], nil, "R42: an API route has no kind"))
	}
}

func TestDevtool_NoRenderWithoutEnableRender(t *testing.T) {
	app := dvrApp(t, nil, false)

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
	app := dvrApp(t, map[string]string{
		"layout.html":         `<html>{{template "content" .}}</html>`,
		"a.html":              `A`,
		"b/layout.html":       `<div>{{template "content" .}}</div>`,
		"b/c.html":            `C`,
		"nodata.html":         `{{template "partials/p" .}}`,
		"rows.html":           `{{.Nope}}`,
		"partials/p.html":     `P`,
		"partials/q.html":     `Q`,
		"error.html":          `{{.Code}}`,
		"assets/app.css":      `a{}`,
		"assets/js/app.js":    `1`,
		"assets/.hidden/x.js": `2`,
	}, true)

	for i := 0; i < 3; i++ {
		createGet(app, pgView("a", ""))
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
	if render["error_template"] != "error.html" {
		t.Error(test.DiffMessage(render["error_template"], "error.html", "R43: error_template"))
	}
	if render["assets"] != float64(2) {
		t.Error(test.DiffMessage(render["assets"], float64(2), "R43: assets counts the served files"))
	}
	pages, _ := render["pages"].([]any)
	layouts := map[string]string{}
	for _, p := range pages {
		entry, _ := p.(map[string]any)
		var name, layout string
		for _, v := range entry {
			if s, ok := v.(string); ok {
				switch s {
				case "a", "b/c", "nodata", "rows":
					name = s
				case "layout.html", "b/layout.html":
					layout = s
				}
			}
		}
		if name != "" {
			layouts[name] = layout
		}
	}
	if len(layouts) != 4 || layouts["a"] != "layout.html" || layouts["nodata"] != "layout.html" || layouts["b/c"] != "b/layout.html" {
		t.Error(test.DiffMessage(layouts, "a, nodata, rows under layout.html and b/c under b/layout.html", "R43: pages with their layout file"))
	}
	partials, _ := json.Marshal(render["partials"])
	if string(partials) != `["partials/p","partials/q"]` {
		t.Error(test.DiffMessage(string(partials), `["partials/p","partials/q"]`, "R43: partials"))
	}
	renders, _ := render["renders"].(float64)
	if renders < 3 || render["failures"] != float64(1) {
		t.Error(test.DiffMessage([]any{render["renders"], render["failures"]}, []any{"3 or more", float64(1)}, "R43: counters"))
	}

	createGet(app, pgView("a", ""))
	after, _ := dvrDecode(t, app)["render"].(map[string]any)
	if got, _ := after["renders"].(float64); got <= renders {
		t.Error(test.DiffMessage(got, "more than "+sprint(renders), "R43: renders counts live"))
	}
}

func TestDevtool_RenderSnapshotBuiltInError(t *testing.T) {
	app := dvrApp(t, map[string]string{"a.html": "A"}, true)

	render, _ := dvrDecode(t, app)["render"].(map[string]any)
	if render["error_template"] != "built-in" {
		t.Error(test.DiffMessage(render["error_template"], "built-in", "R43: error_template without error.html"))
	}
	if render["assets"] != nil && render["assets"] != float64(0) {
		t.Error(test.DiffMessage(render["assets"], 0, "R43: no assets"))
	}
}
