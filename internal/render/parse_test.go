package render

import (
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

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
