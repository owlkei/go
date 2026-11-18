package render

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func rdHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

func rdRender(t testing.TB, e *Engine, route string, data any) string {
	t.Helper()
	b, err := e.Render(route, data)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Release(b)
	return string(b.Bytes())
}

func rdRenderError(t testing.TB, e *Engine, code int, data any) string {
	t.Helper()
	b, err := e.RenderError(code, data)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Release(b)
	return string(b.Bytes())
}

func TestPageAssets_Tags(t *testing.T) {
	files := map[string]string{
		"layout.html":          `<head>{{pageAssets}}</head>{{template "content" .}}`,
		"layout.css":           "L{}",
		"bookings/new.html":    "N",
		"bookings/new.css":     "n{}",
		"bookings/new.form.js": "1",
	}
	e := rdEngine(t, files, false)
	want := `<head><link rel="stylesheet" href="/assets/_pages/layout.css?v=` + rdHash("L{}") + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/bookings/new.css?v=` + rdHash("n{}") + `">` + "\n" +
		`<script src="/assets/_pages/bookings/new.form.js?v=` + rdHash("1") + `" defer></script></head>N`
	if got := rdRender(t, e, "/bookings/new", nil); got != want {
		t.Error(test.DiffMessage(got, want, "P29: the layout's files, then the page's, CSS as link and JS as deferred script"))
	}
}

func TestPageAssets_GroupsSortByFileName(t *testing.T) {
	files := map[string]string{
		"layout.html": `{{pageAssets}}|{{template "content" .}}`,
		"layout.js":   "l1",
		"layout.css":  "l2",
		"p.html":      "P",
		"p.z.js":      "3",
		"p.b.css":     "2",
		"p.css":       "1",
	}
	e := rdEngine(t, files, false)
	want := `<link rel="stylesheet" href="/assets/_pages/layout.css?v=` + rdHash("l2") + `">` + "\n" +
		`<script src="/assets/_pages/layout.js?v=` + rdHash("l1") + `" defer></script>` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/p.b.css?v=` + rdHash("2") + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/p.css?v=` + rdHash("1") + `">` + "\n" +
		`<script src="/assets/_pages/p.z.js?v=` + rdHash("3") + `" defer></script>|P`
	if got := rdRender(t, e, "/p", nil); got != want {
		t.Error(test.DiffMessage(got, want, "P29: each group is sorted by file name in byte order, a .css and a .js mixed"))
	}
}

func TestPageAssets_LongestOwnerWins(t *testing.T) {
	files := map[string]string{
		"layout.html":  `{{pageAssets}}{{template "content" .}}`,
		"x.html":       "X",
		"x.print.html": "XP",
		"x.css":        "a",
		"x.other.css":  "b",
		"x.print.css":  "c",
		"x.print.js":   "d",
	}
	e := rdEngine(t, files, false)
	got := rdRender(t, e, "/x", nil)
	for _, owned := range []string{"x.css", "x.other.css"} {
		if !strings.Contains(got, "/assets/_pages/"+owned+"?v=") {
			t.Error(test.DiffMessage(got, "a tag for "+owned, "P28: it belongs to x.html"))
		}
	}
	if strings.Contains(got, "x.print") {
		t.Error(test.DiffMessage(got, "no x.print files", "P28: x.print.css belongs to x.print.html, the longer template name"))
	}
	got = rdRender(t, e, "/x.print", nil)
	if !strings.Contains(got, "/assets/_pages/x.print.css?v=") || !strings.Contains(got, "/assets/_pages/x.print.js?v=") || strings.Contains(got, "x.other") || strings.Contains(got, "/x.css") {
		t.Error(test.DiffMessage(got, "only the x.print files", "P28: the longest template name owns its files"))
	}
}

func TestPageAssets_LayoutAndIndexOwners(t *testing.T) {
	files := map[string]string{
		"layout.html":       `[{{pageAssets}}]{{template "content" .}}`,
		"layout.css":        "root",
		"admin/layout.html": `<{{pageAssets}}>{{template "content" .}}`,
		"admin/layout.css":  "admin",
		"admin/p.html":      "P",
		"index.html":        "I",
		"index.css":         "index",
	}
	e := rdEngine(t, files, false)
	got := rdRender(t, e, "/admin/p", nil)
	if !strings.Contains(got, "/assets/_pages/admin/layout.css?v="+rdHash("admin")) || strings.Contains(got, "/assets/_pages/layout.css") {
		t.Error(test.DiffMessage(got, "only the nearest layout's files", "P28, P29: admin/layout.css belongs to admin/layout.html and layouts do not nest"))
	}
	got = rdRender(t, e, "/", nil)
	if !strings.Contains(got, "/assets/_pages/layout.css?v="+rdHash("root")) || !strings.Contains(got, "/assets/_pages/index.css?v="+rdHash("index")) {
		t.Error(test.DiffMessage(got, "layout.css and index.css", "P28: index.css belongs to index.html"))
	}
}

func TestPageAssets_EncodedNames(t *testing.T) {
	files := map[string]string{
		"layout.html":               `{{pageAssets}}{{template "content" .}}`,
		"bookings/{id}.html":        "B",
		"bookings/{id}.css":         "1",
		"bookings/{id}.print.css":   "2",
		"bookings/p.html":           "P",
		"bookings/p.my file.css":    "3",
		"bookings/{id}/sub.html":    "S",
		"bookings/{id}/sub.form.js": "4",
	}
	e := rdEngine(t, files, false)
	got := rdRender(t, e, "/bookings/{id}", nil)
	for _, want := range []string{
		`href="/assets/_pages/bookings/%7Bid%7D.css?v=` + rdHash("1") + `"`,
		`href="/assets/_pages/bookings/%7Bid%7D.print.css?v=` + rdHash("2") + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Error(test.DiffMessage(got, "a tag holding "+want, "P29: every path segment is percent-encoded"))
		}
	}
	if got := rdRender(t, e, "/bookings/p", nil); !strings.Contains(got, `href="/assets/_pages/bookings/p.my%20file.css?v=`+rdHash("3")+`"`) {
		t.Error(test.DiffMessage(got, "p.my%20file.css", "P29: a space is encoded"))
	}
	if got := rdRender(t, e, "/bookings/{id}/sub", nil); !strings.Contains(got, `src="/assets/_pages/bookings/%7Bid%7D/sub.form.js?v=`+rdHash("4")+`"`) {
		t.Error(test.DiffMessage(got, "%7Bid%7D/sub.form.js", "P29: the directory segment is encoded, the slashes are kept"))
	}
}

func TestPageAssets_NoFilesWritesNothing(t *testing.T) {
	files := map[string]string{
		"layout.html": `[{{pageAssets}}]{{template "content" .}}`,
		"p.html":      "P",
	}
	e := rdEngine(t, files, false)
	if got := rdRender(t, e, "/p", nil); got != "[]P" {
		t.Error(test.DiffMessage(got, "[]P", "P29: a set that owns no file writes nothing"))
	}
}

func TestPageAssets_NoNonce(t *testing.T) {
	files := map[string]string{
		"layout.html": `{{pageAssets}}{{template "content" .}}`,
		"p.html":      "P",
		"p.css":       "p{}",
		"p.js":        "1",
	}
	e := rdEngine(t, files, false)
	before := e.NonceDraws()
	b, err := e.Render("/p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() != "" || e.NonceDraws() != before {
		t.Error(test.DiffMessage([]any{b.Nonce(), e.NonceDraws()}, []any{"", before}, "P31: page files are same-origin: no nonce is drawn"))
	}
	e.Release(b)
}

func TestPageAssets_ReloadHasNoVersion(t *testing.T) {
	files := map[string]string{
		"layout.html": `{{pageAssets}}{{template "content" .}}`,
		"p.html":      "P",
		"p.css":       "p{}",
	}
	e := rdEngine(t, files, true)
	want := `<link rel="stylesheet" href="/assets/_pages/p.css">P`
	if got := rdRender(t, e, "/p", nil); got != want {
		t.Error(test.DiffMessage(got, want, "P29: with IsReload the URL carries no ?v="))
	}
}

func TestPageAssets_ReloadSeesNewFiles(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"layout.html": `{{pageAssets}}{{template "content" .}}`, "p.html": "P"})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Bind("/p"); err != nil {
		t.Fatal(err)
	}
	if got := rdRender(t, e, "/p", nil); got != "P" {
		t.Fatal(test.DiffMessage(got, "P", "P29: no files yet"))
	}
	swap.set(map[string]string{"layout.html": `{{pageAssets}}{{template "content" .}}`, "p.html": "P", "p.css": "p{}"})
	if got := rdRender(t, e, "/p", nil); got != `<link rel="stylesheet" href="/assets/_pages/p.css">P` {
		t.Error(test.DiffMessage(got, "a tag for p.css", "P29: the string is built again at each reload"))
	}
}

func TestPageAssets_ErrorPageSet(t *testing.T) {
	files := map[string]string{
		"layout.html": `<head>{{pageAssets}}</head>{{template "content" .}}`,
		"layout.css":  "l",
		"404.html":    "E{{.Code}}",
		"404.css":     "e",
	}
	e := rdEngine(t, files, false)
	want := `<head><link rel="stylesheet" href="/assets/_pages/layout.css?v=` + rdHash("l") + `">` + "\n" +
		`<link rel="stylesheet" href="/assets/_pages/404.css?v=` + rdHash("e") + `"></head>E404`
	if got := rdRenderError(t, e, 404, map[string]any{"Code": 404}); got != want {
		t.Error(test.DiffMessage(got, want, "P28, P29: an error page owns its files and the root layout's"))
	}
}

func TestLoad_OrphanPageAssetFails(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a typo":         {"layout.html": `{{pageAssets}}{{template "content" .}}`, "bookings/new.html": "N", "bookings/nw.css": "x"},
		"under partials": {"partials/field.html": "F", "partials/field.css": "x"},
		"no template":    {"orphan.js": "x"},
		"prefix only":    {"bookings/new.html": "N", "bookings/ne.js": "x"},
	} {
		err := rdLoad(files)
		if err == nil || !strings.Contains(err.Error(), "belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/") {
			t.Error(test.DiffMessage(err, "<file> belongs to no template: …", "P30: "+name))
		}
	}
	if err := rdLoad(map[string]string{"assets/app.css": "x", "public/site.css": "x", "notes.txt": "x"}); err != nil {
		t.Error(test.DiffMessage(err, nil, "P30: files under assets and public, and other extensions, are not page files"))
	}
	err := rdLoad(map[string]string{"bookings/new.html": "N", "bookings/nw.css": "x"})
	if err == nil || !strings.HasPrefix(err.Error(), "bookings/nw.css belongs to no template") {
		t.Error(test.DiffMessage(err, "bookings/nw.css belongs to no template…", "P30: the message starts with the file"))
	}
}

func TestLoad_MissingPageAssetsCallFails(t *testing.T) {
	withLayout := map[string]string{"layout.html": `{{template "content" .}}`, "bookings/new.html": "N", "bookings/new.css": "x"}
	want := "bookings/new.html has bookings/new.css, but no template of the page calls {{pageAssets}}: add it to layout.html"
	if err := rdLoad(withLayout); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "P32: the page owns a file, the layout never calls pageAssets"))
	}
	layoutOwns := map[string]string{"layout.html": `{{template "content" .}}`, "layout.css": "x", "p.html": "P"}
	want = "p.html has layout.css, but no template of the page calls {{pageAssets}}: add it to layout.html"
	if err := rdLoad(layoutOwns); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "P32: the layout owns the file"))
	}
	without := map[string]string{"bookings/new.html": "N", "bookings/new.css": "x"}
	want = "bookings/new.html has bookings/new.css, but no template of the page calls {{pageAssets}}: add it to the page"
	if err := rdLoad(without); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "P32: without a layout the page is told to call it"))
	}
}

func TestLoad_PageAssetsCallThroughPartialCounts(t *testing.T) {
	files := map[string]string{
		"layout.html":        `{{template "partials/head" .}}{{template "content" .}}`,
		"partials/head.html": `{{pageAssets}}`,
		"p.html":             "P",
		"p.css":              "x",
	}
	if err := rdLoad(files); err != nil {
		t.Error(test.DiffMessage(err, nil, "P32: a call inside a reached partial counts"))
	}
	files["partials/head.html"] = "head"
	if err := rdLoad(files); err == nil {
		t.Error(test.DiffMessage(nil, "an error", "P32: a partial without the call does not count"))
	}
	files = map[string]string{
		"layout.html":        `{{template "content" .}}`,
		"partials/head.html": `{{pageAssets}}`,
		"p.html":             `{{template "partials/head" .}}`,
		"p.css":              "x",
	}
	if err := rdLoad(files); err != nil {
		t.Error(test.DiffMessage(err, nil, "P32: the page may call it through a partial too"))
	}
}

func TestLoad_PageWithoutFilesNeedsNoCall(t *testing.T) {
	if err := rdLoad(map[string]string{"layout.html": `{{template "content" .}}`, "p.html": "P", "q.html": "Q", "q.css": "x"}); err == nil {
		t.Error(test.DiffMessage(nil, "an error for q.css", "P32: only the set that owns a file is checked"))
	}
	if err := rdLoad(map[string]string{"layout.html": `{{template "content" .}}`, "p.html": "P"}); err != nil {
		t.Error(test.DiffMessage(err, nil, "P32: a set that owns no file needs no call"))
	}
}

func rdErrorFiles(names ...string) map[string]string {
	files := map[string]string{"layout.html": `<main>{{template "content" .}}</main>`}
	for _, name := range names {
		files[name] = strings.TrimSuffix(name, ".html") + `:{{.Code}}`
	}
	return files
}

func TestRenderError_ByStatus(t *testing.T) {
	const none = ""
	cases := []struct {
		note  string
		files []string
		want  map[int]string
	}{
		{"none", nil, map[int]string{404: none, 422: none, 500: none, 503: none}},
		{"404 only", []string{"404.html"}, map[int]string{404: "404", 422: none, 500: none, 503: none}},
		{"all three", []string{"404.html", "4xx.html", "5xx.html"}, map[int]string{404: "404", 422: "4xx", 500: "5xx", 503: "5xx"}},
		{"503 and 5xx", []string{"503.html", "5xx.html"}, map[int]string{404: none, 422: none, 500: "5xx", 503: "503"}},
	}
	for _, tc := range cases {
		e := rdEngine(t, rdErrorFiles(tc.files...), false)
		for code, name := range tc.want {
			if got := e.ErrorTemplate(code); got != name {
				t.Error(test.DiffMessage(got, name, "P17: "+tc.note+", the template of "+sprint(code)))
			}
			b, err := e.RenderError(code, map[string]any{"Code": code})
			if name == none {
				if !errors.Is(err, ErrNoErrorPage) || b != nil {
					t.Error(test.DiffMessage(err, ErrNoErrorPage, "P17: "+tc.note+", "+sprint(code)+" falls to the built-in page"))
				}
				continue
			}
			if err != nil {
				t.Error(test.DiffMessage(err, nil, "P17: "+tc.note+", "+sprint(code)))
				continue
			}
			if want := "<main>" + name + ":" + sprint(code) + "</main>"; string(b.Bytes()) != want {
				t.Error(test.DiffMessage(string(b.Bytes()), want, "P18: "+tc.note+", the page inside the root layout"))
			}
			e.Release(b)
		}
	}
}

func sprint(v any) string { return fmt.Sprint(v) }

func TestRenderError_OutsideErrorStatusesHasNoPage(t *testing.T) {
	e := rdEngine(t, rdErrorFiles("404.html", "4xx.html", "5xx.html"), false)
	for _, code := range []int{0, 99, 200, 302, 399, 600, 999, -1} {
		if _, err := e.RenderError(code, nil); !errors.Is(err, ErrNoErrorPage) {
			t.Error(test.DiffMessage(err, ErrNoErrorPage, "P17: no page serves "+sprint(code)))
		}
		if got := e.ErrorTemplate(code); got != "" {
			t.Error(test.DiffMessage(got, "", "P17: no template name for "+sprint(code)))
		}
	}
}

func TestRenderError_FailureDoesNotTryNextFile(t *testing.T) {
	files := rdErrorFiles("4xx.html")
	files["404.html"] = `{{index .Code 0}}`
	e := rdEngine(t, files, false)
	b, err := e.RenderError(404, map[string]any{"Code": 404})
	if err == nil || b != nil || errors.Is(err, ErrNoErrorPage) {
		t.Error(test.DiffMessage(err, "an execution error", "P19: a failing 404.html is an error, the engine does not try 4xx.html"))
	}
	if _, failures := e.Counters(); failures != 1 {
		t.Error(test.DiffMessage(failures, 1, "P19: the failure is counted"))
	}
}

func TestRenderError_EscapesData(t *testing.T) {
	e := rdEngine(t, map[string]string{"4xx.html": `<p title="{{.Message}}">{{.Message}}</p>`}, false)
	got := rdRenderError(t, e, 400, map[string]any{"Message": `<b>"x"&</b>`})
	want := `<p title="&lt;b&gt;&#34;x&#34;&amp;&lt;/b&gt;">&lt;b&gt;&#34;x&#34;&amp;&lt;/b&gt;</p>`
	if got != want {
		t.Error(test.DiffMessage(got, want, "P18: an error page is escaped by context"))
	}
}

func TestRender_ReloadRebindsRoutes(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"x.html": "old"})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	if name, err := e.Bind("/x"); err != nil || name != "x" {
		t.Fatal(test.DiffMessage([]any{name, err}, []any{"x", nil}, "P2: the first binding"))
	}
	if got := rdRender(t, e, "/x", nil); got != "old" {
		t.Fatal(test.DiffMessage(got, "old", "P10: the first render"))
	}
	swap.set(map[string]string{"x/index.html": "moved"})
	if got := rdRender(t, e, "/x", nil); got != "moved" {
		t.Error(test.DiffMessage(got, "moved", "P10: a reload re-binds the route: x.html became x/index.html"))
	}
}

func TestRender_ReloadMissingOrDoubledTemplateFails(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"x.html": "ok", "y.html": "y"})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/x", "/y"} {
		if _, err := e.Bind(route); err != nil {
			t.Fatal(err)
		}
	}

	swap.set(map[string]string{"y.html": "y"})
	for _, route := range []string{"/x", "/y"} {
		_, err := e.Render(route, nil)
		var fe *FileError
		if !errors.As(err, &fe) || fe.File == "" {
			t.Error(test.DiffMessage(err, "a FileError", "P10: a bound route lost its template, so the whole set fails: "+route))
		}
	}
	swap.set(map[string]string{"x.html": "ok", "x/index.html": "twice", "y.html": "y"})
	if _, err := e.Render("/y", nil); err == nil {
		t.Error(test.DiffMessage(nil, "an error", "P10: a doubled template fails the reload"))
	}
	swap.set(map[string]string{"x.html": "ok", "y.html": "y"})
	if got := rdRender(t, e, "/x", nil); got != "ok" {
		t.Error(test.DiffMessage(got, "ok", "P10: the site answers again once the files are fixed"))
	}
}

func TestRender_ReloadIgnoresNewUnboundFile(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"x.html": "ok"})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Bind("/x"); err != nil {
		t.Fatal(err)
	}
	swap.set(map[string]string{"x.html": "ok", "stray.html": "s", "nope/y.css": "orphan css"})
	if got := rdRender(t, e, "/x", nil); got != "ok" {
		t.Error(test.DiffMessage(got, "ok", "P10: a file without a route, or an orphan asset, is ignored until the restart"))
	}
	if _, err := e.Render("/stray", nil); !errors.Is(err, ErrNoTemplate) {
		t.Error(test.DiffMessage(err, ErrNoTemplate, "P10: the stray file has no route"))
	}
}

func TestRender_ReloadLostPageAssetsCallFails(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"layout.html": `{{pageAssets}}{{template "content" .}}`, "p.html": "P", "p.css": "x"})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Bind("/p"); err != nil {
		t.Fatal(err)
	}
	swap.set(map[string]string{"layout.html": `{{template "content" .}}`, "p.html": "P", "p.css": "x"})
	if _, err := e.Render("/p", nil); err == nil || !strings.Contains(err.Error(), "{{pageAssets}}") {
		t.Error(test.DiffMessage(err, "an error naming {{pageAssets}}", "P10: a set that lost its call fails the reload"))
	}
}

func TestSnapshot_PagesAndErrorTemplates(t *testing.T) {
	files := map[string]string{
		"layout.html":        `{{pageAssets}}{{template "content" .}}`,
		"layout.css":         "l",
		"bookings/new.html":  "N",
		"bookings/new.css":   "n",
		"bookings/{id}.html": "B",
		"404.html":           "e",
		"5xx.html":           "e",
		"partials/p.html":    "p",
		"assets/a.css":       "a",
	}
	e := rdEngine(t, files, false)
	snap := e.Snapshot()
	byName := map[string]Page{}
	for _, p := range snap.Pages {
		byName[p.Name] = p
	}
	if len(snap.Pages) != 2 {
		t.Fatal(test.DiffMessage(len(snap.Pages), 2, "R41: one entry per bound page; error pages are not pages"))
	}
	n := byName["bookings/new"]
	if n.Route != "/bookings/new" || n.Layout != "layout.html" {
		t.Error(test.DiffMessage([]string{n.Route, n.Layout}, []string{"/bookings/new", "layout.html"}, "R43: Name, Route and Layout"))
	}
	if got, want := strings.Join(n.Assets, ","), "layout.css,bookings/new.css"; got != want {
		t.Error(test.DiffMessage(got, want, "R43: the files of the page set, layout first"))
	}
	if b := byName["bookings/{id}"]; b.Route != "/bookings/{id}" || len(b.Assets) != 1 {
		t.Error(test.DiffMessage(b, "route /bookings/{id} and the layout's file", "R43: a parameter page"))
	}
	if got, want := strings.Join(snap.ErrorTemplates, ","), "404.html,5xx.html"; got != want {
		t.Error(test.DiffMessage(got, want, "R43: error_templates are file names, sorted"))
	}
	if got := strings.Join(snap.Partials, ","); got != "partials/p" {
		t.Error(test.DiffMessage(got, "partials/p", "R43: partials"))
	}
}

func TestSnapshot_NoErrorTemplatesWhenBuiltIn(t *testing.T) {
	e := rdEngine(t, map[string]string{"a.html": "a"}, false)
	if got := e.Snapshot().ErrorTemplates; len(got) != 0 {
		t.Error(test.DiffMessage(got, []string(nil), "R43: absent when only the built-in page exists"))
	}
}
