package render

import (
	"bytes"
	"errors"
	"html/template"
	"strings"
	"sync"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

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

func TestRender_NonceEqualsDirectValue(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	b, err := e.Render("/page", "<data>")
	if err != nil {
		t.Fatal(err)
	}
	nonce := b.Nonce()
	if !nonceFormat.MatchString(nonce) {
		t.Fatal(test.DiffMessage(nonce, "32 lowercase hexadecimal characters", "R33: the buffer reports the nonce it placed"))
	}
	if got, want := string(b.Bytes()), directOutput(t, nonce, "<data>"); got != want {
		t.Error(test.DiffMessage(got, want, "R33: the in-place overwrite equals a set whose function returns the nonce itself"))
	}
	if n := strings.Count(string(b.Bytes()), nonce); n != 7 {
		t.Error(test.DiffMessage(n, 7, "R33: every use of {{nonce}} holds the same value"))
	}
	e.Release(b)
}

func TestRender_NonceOnlyWhenUsed(t *testing.T) {
	e := rdEngine(t, map[string]string{
		"layout.html": `<html>{{template "content" .}}</html>`,
		"plain.html":  `<p>plain</p>`,
		"used.html":   `<script nonce="{{nonce}}"></script>`,
	}, false)

	before := e.NonceDraws()
	b, err := e.Render("/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() != "" {
		t.Error(test.DiffMessage(b.Nonce(), "", "R30: a page without {{nonce}} has no nonce"))
	}
	e.Release(b)
	if got := e.NonceDraws(); got != before {
		t.Error(test.DiffMessage(got, before, "R30: a page without {{nonce}} does not read crypto/rand"))
	}

	b, err = e.Render("/used", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !nonceFormat.MatchString(b.Nonce()) {
		t.Error(test.DiffMessage(b.Nonce(), "32 lowercase hexadecimal characters", "R30: a page with {{nonce}} has one"))
	}
	e.Release(b)
	if got := e.NonceDraws(); got != before+1 {
		t.Error(test.DiffMessage(got, before+1, "R30: one draw per response that uses {{nonce}}"))
	}
}

func TestRender_NonceReachedThroughPartialAndLayout(t *testing.T) {
	e := rdEngine(t, map[string]string{
		"layout.html":      `<main>{{template "content" .}}</main>`,
		"viapartial.html":  `{{template "partials/p" .}}`,
		"vialayout.html":   `x`,
		"deep/layout.html": `<script nonce="{{nonce}}"></script>{{template "content" .}}`,
		"deep/page.html":   `y`,
		"partials/p.html":  `<script nonce="{{nonce}}"></script>`,
	}, false)
	for _, name := range []string{"/viapartial", "/deep/page"} {
		b, err := e.Render(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !nonceFormat.MatchString(b.Nonce()) || !strings.Contains(string(b.Bytes()), `nonce="`+b.Nonce()+`"`) {
			t.Error(test.DiffMessage(string(b.Bytes()), "a nonce attribute holding "+b.Nonce(), "R30: a nonce used by a partial or a layout is placed: "+name))
		}
		e.Release(b)
	}
	b, err := e.Render("/vialayout", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() != "" {
		t.Error(test.DiffMessage(b.Nonce(), "", "R30: a set that never reaches {{nonce}} has none"))
	}
	e.Release(b)
}

func TestRender_NonceInErrorPage(t *testing.T) {
	e := rdEngine(t, map[string]string{
		"layout.html": `<main>{{template "content" .}}</main>`,
		"4xx.html":    `<script nonce="{{nonce}}"></script>{{.Code}}`,
	}, false)
	b, err := e.RenderError(404, map[string]any{"Code": 404})
	if err != nil {
		t.Fatal(err)
	}
	if !nonceFormat.MatchString(b.Nonce()) || !strings.Contains(string(b.Bytes()), `<script nonce="`+b.Nonce()+`"></script>404`) {
		t.Error(test.DiffMessage(string(b.Bytes()), "a script with nonce "+b.Nonce(), "R30: the error page set places its nonce"))
	}
	e.Release(b)
}

func TestRender_NonceUniquePerRender(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		b, err := e.Render("/page", nil)
		if err != nil {
			t.Fatal(err)
		}
		seen[b.Nonce()] = true
		e.Release(b)
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "R33: 1,000 renders give 1,000 nonces"))
	}
}

func TestRender_NonceNeverTakenFromData(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	submitted := strings.Repeat("ab", 16)
	b, err := e.Render("/page", submitted)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() == submitted || !strings.HasSuffix(string(b.Bytes()), submitted+"</body></html>") {
		t.Error(test.DiffMessage(string(b.Bytes()), "the submitted value kept as data", "R33: a value submitted by a client never becomes a nonce"))
	}
	e.Release(b)
}

func TestRender_NonceFollowsReload(t *testing.T) {
	swap := &reloadFS{}
	swap.set(map[string]string{"a.html": `<p>a</p>`})
	e := New(swap, true)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Bind("/a"); err != nil {
		t.Fatal(err)
	}
	b, err := e.Render("/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() != "" {
		t.Error(test.DiffMessage(b.Nonce(), "", "R30: no nonce before the file uses it"))
	}
	e.Release(b)

	swap.set(map[string]string{"a.html": `<script nonce="{{nonce}}">1</script>`})
	b, err = e.Render("/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !nonceFormat.MatchString(b.Nonce()) || !strings.Contains(string(b.Bytes()), `nonce="`+b.Nonce()+`"`) {
		t.Error(test.DiffMessage(string(b.Bytes()), "a nonce attribute", "R14, R30: a reloaded set places its nonce"))
	}
	e.Release(b)
}

func TestRender_NonceConcurrent(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				b, err := e.Render("/page", nil)
				if err != nil {
					t.Error(err)
					return
				}
				nonce := b.Nonce()
				if got, want := string(b.Bytes()), directOutput(t, nonce, nil); got != want {
					t.Error(test.DiffMessage(got, want, "R33: a concurrent render holds its own nonce at every use"))
				}
				mu.Lock()
				seen[nonce] = true
				mu.Unlock()
				e.Release(b)
			}
		}()
	}
	wg.Wait()
	if len(seen) != 64*20 {
		t.Error(test.DiffMessage(len(seen), 64*20, "R33: every concurrent render has its own nonce"))
	}
}

func TestRender_NonceAllocationsAgainstDirectValue(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	probe, err := e.Render("/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !nonceFormat.MatchString(probe.Nonce()) {
		t.Fatal(test.DiffMessage(probe.Nonce(), "32 lowercase hexadecimal characters", "R33: the render places a nonce"))
	}
	e.Release(probe)

	funcs := template.FuncMap{"nonce": func() string { return "0123456789abcdef0123456789abcdef" }}
	set, err := template.New("layout").Funcs(funcs).Parse(nonceLayout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.New("content").Parse(noncePage); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	direct := testing.AllocsPerRun(200, func() {
		out.Reset()
		_ = set.Execute(&out, nil)
	})
	engine := testing.AllocsPerRun(200, func() {
		b, _ := e.Render("/page", nil)
		e.Release(b)
	})
	if engine > direct+2 {
		t.Error(test.DiffMessage(engine, direct+2, "D10: the in-place overwrite adds at most 2 allocations to a set whose function returns the nonce"))
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
