package render

import (
	"errors"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/test"
)

func rdFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

var errorPageName = regexp.MustCompile(`^([0-9]{3}|[0-9]xx)\.html$`)

func rdRoutes(files map[string]string) []string {
	var routes []string
	for name := range files {
		if !strings.HasSuffix(name, ".html") || errorPageName.MatchString(name) {
			continue
		}
		if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "public/") || strings.HasPrefix(name, "partials/") || path.Base(name) == "layout.html" {
			continue
		}
		route := "/" + strings.TrimSuffix(name, ".html")
		if route == "/index" {
			route = "/"
		}
		routes = append(routes, route)
	}
	return routes
}

func rdEngine(t testing.TB, files map[string]string, isReload bool) *Engine {
	t.Helper()
	e := New(rdFS(files), isReload)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	for _, route := range rdRoutes(files) {
		if _, err := e.Bind(route); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.CheckBindings(); err != nil {
		t.Fatal(err)
	}
	return e
}

func rdLoad(files map[string]string) error {
	return New(rdFS(files), false).Load()
}

func rdBind(t testing.TB, files map[string]string, route string) (string, error) {
	t.Helper()
	e := New(rdFS(files), false)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e.Bind(route)
}

func TestRender_RendersPageInLayout(t *testing.T) {
	e := rdEngine(t, map[string]string{
		"layout.html": `[{{template "content" .}}]`,
		"a/b.html":    `{{.}}`,
	}, false)
	b, err := e.Render("/a/b", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b.Bytes()); got != "[hi]" {
		t.Error(test.DiffMessage(got, "[hi]", "R5: page inside its layout"))
	}
	if b.Len() != 4 {
		t.Error(test.DiffMessage(b.Len(), 4, "R5: buffer length"))
	}
	e.Release(b)
}

func TestRender_UnboundRouteIsNoTemplate(t *testing.T) {
	e := rdEngine(t, map[string]string{"layout.html": `{{template "content" .}}`, "partials/p.html": "p", "a.html": "a"}, false)
	for _, route := range []string{"", "/", "/layout", "/partials/p", "/error", "/../a", "/a.html", "/nope"} {
		if _, err := e.Render(route, nil); !errors.Is(err, ErrNoTemplate) {
			t.Error(test.DiffMessage(err, ErrNoTemplate, "P2, P10: a route that was never bound has no template: "+route))
		}
	}
}

func TestRender_RouteSpellingIsNormalized(t *testing.T) {
	e := rdEngine(t, map[string]string{"a/b.html": "ab"}, false)
	for _, route := range []string{"/a/b", "/a/b/", "a/b", "a/b/"} {
		b, err := e.Render(route, nil)
		if err != nil {
			t.Error(test.DiffMessage(err, nil, "P2: the outer slashes of a route are not part of its key: "+route))
			continue
		}
		e.Release(b)
	}
}

func TestBind_TemplateByRoute(t *testing.T) {
	cases := []struct {
		files map[string]string
		route string
		want  string
		note  string
	}{
		{map[string]string{"index.html": "x"}, "/", "index", "the root"},
		{map[string]string{"bookings.html": "x"}, "/bookings/", "bookings", "the .html form"},
		{map[string]string{"bookings/index.html": "x"}, "/bookings/", "bookings/index", "the index.html form"},
		{map[string]string{"bookings/{id}.html": "x"}, "/bookings/{id}/", "bookings/{id}", "a parameter file"},
		{map[string]string{"bookings/{id}/index.html": "x"}, "/bookings/{id}/", "bookings/{id}/index", "a parameter directory"},
		{map[string]string{"admin/users.html": "x"}, "/admin/users/", "admin/users", "a prefix"},
		{map[string]string{"admin.html": "x"}, "/admin/", "admin", "a prefix alone, .html"},
		{map[string]string{"admin/index.html": "x"}, "/admin/", "admin/index", "a prefix alone, index.html"},
		{map[string]string{"court-bookings.html": "x"}, "/court-bookings/", "court-bookings", "kebab case"},
		{map[string]string{"court_bookings/index.html": "x"}, "/court_bookings/", "court_bookings/index", "as written"},
		{map[string]string{"courts/layout/index.html": "x"}, "/courts/layout/", "courts/layout/index", "layout as the last segment uses the directory form"},
		{map[string]string{"x/index/index.html": "x"}, "/x/index/", "x/index/index", "index as the last segment uses the directory form"},
		{map[string]string{"assets.html": "x"}, "/assets/", "assets", "assets.html is a page"},
		{map[string]string{"partials.html": "x"}, "/partials/", "partials", "partials.html is a page"},
		{map[string]string{"public.html": "x"}, "/public/", "public", "public.html is a page"},
		{map[string]string{"admin/404.html": "x"}, "/admin/404/", "admin/404", "a reserved name under a prefix is a page"},
		{map[string]string{"admin/5xx.html": "x"}, "/admin/5xx/", "admin/5xx", "a reserved name under a prefix is a page"},
	}
	for _, tc := range cases {
		got, err := rdBind(t, tc.files, tc.route)
		if err != nil || got != tc.want {
			t.Error(test.DiffMessage([]any{got, err}, []any{tc.want, nil}, "P2: "+tc.note))
		}
	}
}

func TestBind_LayoutFileIsNoCandidateForItsOwnName(t *testing.T) {
	files := map[string]string{"courts/layout.html": "x"}
	_, err := rdBind(t, files, "/courts/layout/")
	if !errors.Is(err, ErrNoTemplate) || err.Error() != "needs a template: courts/layout/index.html" {
		t.Error(test.DiffMessage(err, "needs a template: courts/layout/index.html", "P2: courts/layout.html is a layout, not the page of /courts/layout"))
	}
	_, err = rdBind(t, map[string]string{"x/index.html": "x"}, "/x/index/")
	if !errors.Is(err, ErrNoTemplate) || err.Error() != "needs a template: x/index/index.html" {
		t.Error(test.DiffMessage(err, "needs a template: x/index/index.html", "P2: x/index.html belongs to /x/"))
	}
}

func TestBind_MissingTemplateNamesBothCandidates(t *testing.T) {
	_, err := rdBind(t, map[string]string{"other.html": "x"}, "/bookings/")
	if !errors.Is(err, ErrNoTemplate) || err.Error() != "needs a template: bookings.html or bookings/index.html" {
		t.Error(test.DiffMessage(err, "needs a template: bookings.html or bookings/index.html", "P4: both candidates are named"))
	}
	_, err = rdBind(t, map[string]string{"other.html": "x"}, "/")
	if !errors.Is(err, ErrNoTemplate) || err.Error() != "needs a template: index.html" {
		t.Error(test.DiffMessage(err, "needs a template: index.html", "P4: the root has one candidate"))
	}
	_, err = rdBind(t, map[string]string{"bookings/{slug}.html": "x"}, "/bookings/{id}/")
	if !errors.Is(err, ErrNoTemplate) || err.Error() != "needs a template: bookings/{id}.html or bookings/{id}/index.html" {
		t.Error(test.DiffMessage(err, "needs a template: bookings/{id}.html or bookings/{id}/index.html", "P4: a wrong parameter name in the file is a missing template"))
	}
}

func TestBind_TwoTemplates(t *testing.T) {
	_, err := rdBind(t, map[string]string{"bookings.html": "a", "bookings/index.html": "b"}, "/bookings/")
	if !errors.Is(err, ErrTwoTemplates) || err.Error() != "has two templates: bookings.html and bookings/index.html" {
		t.Error(test.DiffMessage(err, "has two templates: bookings.html and bookings/index.html", "P4: both candidates exist"))
	}
}

func TestBind_ReservedDirectory(t *testing.T) {
	for _, dir := range []string{"partials", "assets", "public"} {
		files := map[string]string{dir + "/x.html": "x"}
		_, err := rdBind(t, files, "/"+dir+"/x/")
		want := "can have no template: " + dir + "/ is reserved"
		if !errors.Is(err, ErrReservedDir) || err == nil || err.Error() != want {
			t.Error(test.DiffMessage(err, want, "P4: nothing below "+dir+" can be a page"))
		}
	}
}

func TestBind_ErrorPageNameIsNotARoute(t *testing.T) {
	for _, name := range []string{"404", "500", "4xx", "5xx"} {
		_, err := rdBind(t, map[string]string{name + ".html": "x"}, "/"+name+"/")
		want := "resolves to " + name + ".html, which is an error page, not a route"
		if !errors.Is(err, ErrErrorPageName) || err == nil || err.Error() != want {
			t.Error(test.DiffMessage(err, want, "P20: "+name))
		}
	}
	if _, err := rdBind(t, map[string]string{"40.html": "x"}, "/40/"); err != nil {
		t.Error(test.DiffMessage(err, nil, "P20: two digits are an ordinary page"))
	}
	if _, err := rdBind(t, map[string]string{"4x.html": "x"}, "/4x/"); err != nil {
		t.Error(test.DiffMessage(err, nil, "P20: 4x is an ordinary page"))
	}
}

func TestCheckBindings_StrayPage(t *testing.T) {
	files := map[string]string{
		"layout.html":          "{{template \"content\" .}}",
		"partials/p.html":      "p",
		"404.html":             "e",
		"4xx.html":             "e",
		"assets/x.html":        "{{ not parsed",
		"public/y.html":        "{{ not parsed",
		"bookings/detail.html": "d",
	}
	e := New(rdFS(files), false)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	want := "bookings/detail.html has no RENDER_ route: add one, move it to partials/, or delete it"
	if err := e.CheckBindings(); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "P9: a page without a route stops the boot; assets, public, layouts, partials and error pages do not"))
	}
	if _, err := e.Bind("/bookings/detail"); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckBindings(); err != nil {
		t.Error(test.DiffMessage(err, nil, "P9: nothing is left unbound"))
	}
}

func TestCheckBindings_ErrorDotHTMLIsStray(t *testing.T) {
	e := New(rdFS(map[string]string{"error.html": "e"}), false)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	want := "error.html has no RENDER_ route: add one, move it to partials/, or delete it"
	if err := e.CheckBindings(); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "P9: error.html has no role"))
	}
	if _, err := e.RenderError(404, nil); !errors.Is(err, ErrNoErrorPage) {
		t.Error(test.DiffMessage(err, ErrNoErrorPage, "P17: error.html is never an error page"))
	}
}

func TestLoad_UnusableErrorNames(t *testing.T) {
	for _, name := range []string{"200.html", "302.html", "399.html", "600.html", "000.html", "3xx.html", "1xx.html", "6xx.html"} {
		err := rdLoad(map[string]string{name: "x"})
		want := name + " is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html"
		if err == nil || err.Error() != want {
			t.Error(test.DiffMessage(err, want, "P21: "+name))
		}
	}
	for _, name := range []string{"400.html", "404.html", "599.html", "4xx.html", "5xx.html"} {
		if err := rdLoad(map[string]string{name: "x"}); err != nil {
			t.Error(test.DiffMessage(err, nil, "P21: "+name+" is an error page"))
		}
	}
}

func TestLoad_ReservedPagesAssetsDir(t *testing.T) {
	for _, files := range []map[string]string{
		{"assets/_pages": "x"},
		{"assets/_pages/x.css": "x"},
	} {
		err := rdLoad(files)
		want := "assets/_pages is reserved for page CSS and JS"
		if err == nil || err.Error() != want {
			t.Error(test.DiffMessage(err, want, "P34: a file or a directory named _pages"))
		}
	}
	if err := rdLoad(map[string]string{"assets/pages/x.css": "x", "assets/_other.css": "x"}); err != nil {
		t.Error(test.DiffMessage(err, nil, "P34: only _pages is reserved"))
	}
}

func TestRender_ParseErrorNamesFile(t *testing.T) {
	e := New(rdFS(map[string]string{"a/b.html": "{{if .X}}open"}), false)
	err := e.Load()
	var fe *FileError
	if !errors.As(err, &fe) || fe.File != "a/b.html" {
		t.Fatal(test.DiffMessage(err, "a FileError for a/b.html", "R4: the error names its file"))
	}
	if !strings.HasPrefix(err.Error(), "a/b.html: ") {
		t.Error(test.DiffMessage(err.Error(), "a/b.html: …", "R4: error text"))
	}
}

func TestRender_ErrorPageParseErrorNamesFile(t *testing.T) {
	for _, name := range []string{"404.html", "4xx.html", "5xx.html"} {
		err := rdLoad(map[string]string{name: "{{.Code"})
		var fe *FileError
		if !errors.As(err, &fe) || fe.File != name {
			t.Error(test.DiffMessage(err, "a FileError for "+name, "P18: an error page is parsed at Load"))
		}
	}
}

func TestRender_BufferLimit(t *testing.T) {
	b := &Buffer{}
	if _, err := b.Write(make([]byte, pageLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); !errors.Is(err, ErrPageTooLarge) {
		t.Error(test.DiffMessage(err, ErrPageTooLarge, "R11: a write past 8 MiB fails"))
	}
}

func TestRender_ReleaseDropsLargeBuffers(t *testing.T) {
	e := rdEngine(t, map[string]string{"a.html": "a"}, false)
	big := &Buffer{}
	_, _ = big.Write(make([]byte, retainLimit+1))
	e.Release(big)
	small := &Buffer{}
	_, _ = small.Write([]byte("x"))
	e.Release(small)
	if small.Len() != 0 {
		t.Error(test.DiffMessage(small.Len(), 0, "a released buffer is reset"))
	}
}

func TestRender_Dict(t *testing.T) {
	m, err := dict("a", 1, "b", "x")
	if err != nil || m["a"] != 1 || m["b"] != "x" {
		t.Error(test.DiffMessage(m, "a=1 b=x", "D6: dict builds a map"))
	}
	if _, err := dict("a"); !errors.Is(err, errDictPairs) {
		t.Error(test.DiffMessage(err, errDictPairs, "D6: odd arguments"))
	}
	if _, err := dict(1, 2); !errors.Is(err, errDictKey) {
		t.Error(test.DiffMessage(err, errDictKey, "D6: non-string key"))
	}
	if m, err := dict(); err != nil || len(m) != 0 {
		t.Error(test.DiffMessage(m, "empty map", "D6: no arguments"))
	}
}

func TestRender_AssetHidesHiddenAndDirs(t *testing.T) {
	files := map[string]string{"assets/a.css": "a", "assets/.env": "s", "assets/d/e.js": "e"}
	for _, reload := range []bool{false, true} {
		e := rdEngine(t, files, reload)
		if _, err := e.asset("a.css"); err != nil {
			t.Error(test.DiffMessage(err, nil, "R12: a known asset"))
		}
		if _, err := e.asset("d/e.js"); err != nil {
			t.Error(test.DiffMessage(err, nil, "R12: a nested asset"))
		}
		for _, name := range []string{".env", "d", "nope"} {
			if _, err := e.asset(name); err == nil {
				t.Error(test.DiffMessage(nil, "an error", "R12: asset "+name))
			}
		}
	}
}

func TestRender_ClassifyIgnoresHidden(t *testing.T) {
	if !hasHiddenSegment("a/.b/c.html") || hasHiddenSegment("a/b/c.html") {
		t.Error(test.DiffMessage(false, true, "hidden segments are detected"))
	}
}
