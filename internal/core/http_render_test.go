package core

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type rnItem struct{ Name string }

type rnData struct{ common.HTTP }

func (c rnData) NewController() Controller { return c }

func (c rnData) RENDER_map() ctx.Map { return ctx.Map{"Name": rnHostile} }

func (c rnData) RENDER_structdata() rnItem { return rnItem{Name: rnHostile} }

func (c rnData) RENDER_ptr() *rnItem { return &rnItem{Name: "ptr"} }

func (c rnData) RENDER_slice() []rnItem { return []rnItem{{Name: "a"}, {Name: rnHostile}} }

func (c rnData) RENDER_text() string { return "plain " + rnHostile }

func (c rnData) RENDER_nilresult() any { return nil }

func (c rnData) RENDER_noresult() {}

func (c rnData) RENDER_last() (int, ctx.Map) { return http.StatusAccepted, ctx.Map{"Name": "last"} }

const rnHostile = `<script>alert("x")</script>`

func rnDataFiles() map[string]string {
	return map[string]string{
		"map.html":        `<p>{{.Name}}</p>`,
		"structdata.html": `<p>{{.Name}}</p>`,
		"ptr.html":        `<p>{{.Name}}</p>`,
		"slice.html":      `{{range .}}<i>{{.Name}}</i>{{end}}`,
		"text.html":       `<p>{{.}}</p>`,
		"nilresult.html":  `{{if .}}data{{else}}nil{{end}}`,
		"noresult.html":   `{{if .}}data{{else}}nil{{end}}`,
		"last.html":       `<p>{{.Name}}</p>`,
	}
}

type rnMixed struct{ common.HTTP }

func (c rnMixed) NewController() Controller { return c }

func (c rnMixed) READ_api(h *ctx.HTTPContext) ctx.Map {
	if h.Query().Get("k") == "boom" {
		panic(exception.NotFoundException("no such api thing"))
	}
	return ctx.Map{"ok": true}
}

func (c rnMixed) RENDER_dashboard(h *ctx.HTTPContext) ctx.Map {
	if h.Query().Get("k") == "boom" {
		panic(exception.NotFoundException("no such page thing"))
	}
	return ctx.Map{"Who": "me"}
}

func TestRender_IsGetPage(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{"nodata.html": "NODATA"}), pgPages{})

	w := createGet(app, "/nodata")
	if w.Code != http.StatusOK || w.Body.String() != "NODATA" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P1: GET answers the template, "+w.Body.String()))
	}
	h := createRequest(app, http.MethodHead, "/nodata")
	if h.Code != http.StatusOK || h.Body.Len() != 0 {
		t.Error(test.DiffMessage(h.Code, http.StatusOK, "P1, R15: HEAD answers without a body"))
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w := createRequest(app, method, "/nodata")
		if w.Code != http.StatusMethodNotAllowed {
			t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "P1: "+method+" on a page with no SUBMIT_ is 405"))
		}
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, "GET") || strings.Contains(allow, "POST") {
			t.Error(test.DiffMessage(allow, "GET without POST", "P1: Allow of "+method))
		}
	}
}

func TestRender_MixedController(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"dashboard.html": "hello {{.Who}}"}), rnMixed{})

	if w := createGet(app, "/dashboard"); w.Code != http.StatusOK || w.Body.String() != "hello me" {
		t.Error(test.DiffMessage(w.Body.String(), "hello me", "P1: a RENDER_ route in a controller that has a READ_ route"))
	}
	if w := createGet(app, "/api"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Error(test.DiffMessage(w.Body.String(), `{"ok":true}`, "P1: the READ_ route is JSON"))
	}
	page := createRequest(app, http.MethodGet, "/dashboard?k=boom", "Accept", "application/json")
	peExpectHTML(t, "R16: the page route of a mixed controller fails in HTML", page, http.StatusNotFound)
	api := createRequest(app, http.MethodGet, "/api?k=boom", "Accept", "text/html")
	peExpectJSON(t, "R17: the API route of the same controller fails in JSON", api, http.StatusNotFound)
}

type rnRoot struct{ common.HTTP }

func (c rnRoot) NewController() Controller { return c }
func (c rnRoot) RENDER() ctx.Map           { return ctx.Map{"Who": "root"} }

type rnBookings struct{ common.HTTP }

func (c rnBookings) NewController() Controller { return c }
func (c rnBookings) RENDER_bookings() ctx.Map  { return ctx.Map{"Who": "list"} }
func (c rnBookings) RENDER_bookings_BY_id(p ctx.Param) ctx.Map {
	return ctx.Map{"Who": "id=" + p.Get("id")}
}

type rnAdminUsers struct{ common.HTTP }

func (c rnAdminUsers) NewController() Controller {
	c.Prefix("admin")
	return c
}
func (c rnAdminUsers) RENDER_users() ctx.Map { return ctx.Map{"Who": "users"} }

type rnAdminRoot struct{ common.HTTP }

func (c rnAdminRoot) NewController() Controller {
	c.Prefix("admin")
	return c
}
func (c rnAdminRoot) RENDER() ctx.Map { return ctx.Map{"Who": "admin"} }

type rnKebab struct{ common.HTTP }

func (c rnKebab) NewController() Controller      { return c }
func (c rnKebab) RENDER_court_bookings() ctx.Map { return ctx.Map{"Who": "court"} }

type rnExact struct{ common.HTTP }

func (c rnExact) NewController() Controller {
	c.Path("/courts/layout", c.RENDER_lay)
	c.Path("/x/index", c.RENDER_idx)
	c.Path("/exact/page", c.RENDER_other)
	return c
}
func (c rnExact) RENDER_lay() ctx.Map   { return ctx.Map{"Who": "lay"} }
func (c rnExact) RENDER_idx() ctx.Map   { return ctx.Map{"Who": "idx"} }
func (c rnExact) RENDER_other() ctx.Map { return ctx.Map{"Who": "other"} }

type rnAssetsName struct{ common.HTTP }

func (c rnAssetsName) NewController() Controller { return c }
func (c rnAssetsName) RENDER_assets() ctx.Map    { return ctx.Map{"Who": "assets"} }

func rnServe(t *testing.T, files map[string]string, build func() *Module, target string) *httpResult {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	mustCreate(t, app, build())
	w := createGet(app, target)
	return &httpResult{code: w.Code, body: w.Body.String()}
}

type httpResult struct {
	code int
	body string
}

func rnModule(controllers ...Controller) func() *Module {
	return func() *Module { return NewModuleBuilder().Controllers(controllers...).Build() }
}

func TestRender_TemplateByRoute(t *testing.T) {
	const who = `{{.Who}}`
	cases := []struct {
		note   string
		files  map[string]string
		build  func() *Module
		target string
		want   string
	}{
		{"root renders index.html", map[string]string{"index.html": who}, rnModule(rnRoot{}), "/", "root"},
		{"the .html form", map[string]string{"bookings.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings", "list"},
		{"the index.html form", map[string]string{"bookings/index.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings", "list"},
		{"a parameter file", map[string]string{"bookings/index.html": who, "bookings/{id}.html": who}, rnModule(rnBookings{}), "/bookings/42", "id=42"},
		{"a parameter directory", map[string]string{"bookings/index.html": who, "bookings/{id}/index.html": who}, rnModule(rnBookings{}), "/bookings/7", "id=7"},
		{"a controller prefix", map[string]string{"admin/users.html": who}, rnModule(rnAdminUsers{}), "/admin/users", "users"},
		{"a controller prefix, directory form", map[string]string{"admin/users/index.html": who}, rnModule(rnAdminUsers{}), "/admin/users", "users"},
		{"a module prefix", map[string]string{"api/v2/admin/users.html": who}, func() *Module {
			m := NewModuleBuilder().Controllers(rnAdminUsers{}).Build()
			m.Prefix("api/v2")
			return m
		}, "/api/v2/admin/users", "users"},
		{"a prefix alone, .html", map[string]string{"admin.html": who}, rnModule(rnAdminRoot{}), "/admin", "admin"},
		{"a prefix alone, index.html", map[string]string{"admin/index.html": who}, rnModule(rnAdminRoot{}), "/admin", "admin"},
		{"kebab case", map[string]string{"court-bookings.html": who}, rnModule(rnKebab{}), "/court-bookings", "court"},
		{"as written", map[string]string{"court_bookings.html": who}, func() *Module {
			return NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(rnKebab{}).Build()
		}, "/court_bookings", "court"},
		{"Path", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/exact/page", "other"},
		{"layout as the last segment", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/courts/layout", "lay"},
		{"index as the last segment", map[string]string{"courts/layout/index.html": who, "x/index/index.html": who, "exact/page.html": who}, rnModule(rnExact{}), "/x/index", "idx"},
		{"assets.html at the root", map[string]string{"assets.html": who}, rnModule(rnAssetsName{}), "/assets", "assets"},
	}
	for _, tc := range cases {
		got := rnServe(t, tc.files, tc.build, tc.target)
		if got.code != http.StatusOK || got.body != tc.want {
			t.Error(test.DiffMessage([]any{got.code, got.body}, []any{http.StatusOK, tc.want}, "P2: "+tc.note))
		}
	}
}

func TestRender_TemplateByRouteIgnoresTheRequestPath(t *testing.T) {
	files := map[string]string{"bookings/index.html": `list`, "bookings/{id}.html": `id={{.Who}}`}
	app, _ := pgNew(t, pgFiles(files), rnBookings{})
	for _, target := range []string{"/bookings/%7Bid%7D", "/bookings/..%2findex", "/bookings/index"} {
		w := createGet(app, target)
		if w.Body.String() == "list" {
			t.Error(test.DiffMessage(w.Body.String(), "never the index template", "P2, Security: the request never picks the template: "+target))
		}
	}
}

func TestRender_DataIsResult(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	escaped := `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`
	cases := map[string]string{
		"/map":        `<p>` + escaped + `</p>`,
		"/structdata": `<p>` + escaped + `</p>`,
		"/ptr":        `<p>ptr</p>`,
		"/slice":      `<i>a</i><i>` + escaped + `</i>`,
		"/text":       `<p>plain ` + escaped + `</p>`,
		"/nilresult":  `nil`,
	}
	for target, want := range cases {
		w := createGet(app, target)
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Error(test.DiffMessage(w.Body.String(), want, "P6, R7: the handler's result is the data, escaped: "+target))
		}
	}
}

func TestRender_NoResult(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	if w := createGet(app, "/noresult"); w.Code != http.StatusOK || w.Body.String() != "nil" {
		t.Error(test.DiffMessage(w.Body.String(), "nil", "P6: a handler with no result renders with nil"))
	}
}

func TestRender_LastResultIsTheData(t *testing.T) {
	app, _ := pgNew(t, pgFiles(rnDataFiles()), rnData{})
	w := createGet(app, "/last")
	if w.Code != http.StatusAccepted || w.Body.String() != "<p>last</p>" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusAccepted, "<p>last</p>"}, "P6, P7: (int, data) renders the last result with the status"))
	}
}

func TestRender_VersionNeutral(t *testing.T) {
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{"nodata.html": "PAGE"}), Dir: "."})
	app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	mustCreate(t, app, NewModuleBuilder().Controllers(pgNodataOnly{}).Build())

	for name, headers := range map[string][]string{
		"a version":       {"X-Version", "2"},
		"another version": {"X-Version", "9"},
		"no version":      nil,
	} {
		w := createRequest(app, http.MethodGet, "/nodata", headers...)
		if w.Code != http.StatusOK || w.Body.String() != "PAGE" {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "PAGE"}, "P8: a page route answers "+name))
		}
	}
}

type pgNodataOnly struct{ common.HTTP }

func (c pgNodataOnly) NewController() Controller { return c }
func (c pgNodataOnly) RENDER_nodata()            {}

func TestRender_EmptyDirectoryServesNothingWithoutRoutes(t *testing.T) {
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: fstest.MapFS{"layout.html": {Data: []byte(`{{template "content" .}}`)}}, Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(rnMixedAPIOnly{}).Build())
	if w := createGet(app, "/layout"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P2: a layout is no route"))
	}
}

type rnMixedAPIOnly struct{ common.HTTP }

func (c rnMixedAPIOnly) NewController() Controller { return c }
func (c rnMixedAPIOnly) READ_x() string            { return "x" }
