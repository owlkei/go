package render

import (
	"bytes"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/test"
)

var nonceFormat = regexp.MustCompile(`^[0-9a-f]{32}$`)

const (
	nonceLayout = `<html><head><script nonce="{{nonce}}">var n = "{{nonce}}";</script><style nonce="{{nonce}}">a{content:"{{nonce}}"}</style></head><body>{{template "content" .}}</body></html>`
	noncePage   = `<a href="/x?n={{nonce}}" data-n="{{nonce}}">{{nonce}}</a>{{.}}`
)

type reloadFS struct {
	mu    sync.RWMutex
	files fstest.MapFS
}

func (f *reloadFS) Open(name string) (fs.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.files.Open(name)
}

func (f *reloadFS) set(files map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = rdFS(files)
}

func nonceFiles() map[string]string {
	return map[string]string{"layout.html": nonceLayout, "page.html": noncePage}
}

func directOutput(t testing.TB, nonce string, data any) string {
	t.Helper()
	funcs := template.FuncMap{"nonce": func() string { return nonce }}
	set, err := template.New("layout").Funcs(funcs).Parse(nonceLayout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.New("content").Parse(noncePage); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := set.Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestRender_NonceEqualsDirectValue(t *testing.T) {
	e := rdEngine(t, nonceFiles(), false)
	b, err := e.Render("page", "<data>")
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
	b, err := e.Render("plain", nil)
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

	b, err = e.Render("used", nil)
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
	for _, name := range []string{"viapartial", "deep/page"} {
		b, err := e.Render(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !nonceFormat.MatchString(b.Nonce()) || !strings.Contains(string(b.Bytes()), `nonce="`+b.Nonce()+`"`) {
			t.Error(test.DiffMessage(string(b.Bytes()), "a nonce attribute holding "+b.Nonce(), "R30: a nonce used by a partial or a layout is placed: "+name))
		}
		e.Release(b)
	}
	b, err := e.Render("vialayout", nil)
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
		"error.html":  `<script nonce="{{nonce}}"></script>{{.Code}}`,
	}, false)
	b, err := e.RenderError(map[string]any{"Code": 404})
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
		b, err := e.Render("page", nil)
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
	b, err := e.Render("page", submitted)
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
	b, err := e.Render("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Nonce() != "" {
		t.Error(test.DiffMessage(b.Nonce(), "", "R30: no nonce before the file uses it"))
	}
	e.Release(b)

	swap.set(map[string]string{"a.html": `<script nonce="{{nonce}}">1</script>`})
	b, err = e.Render("a", nil)
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
				b, err := e.Render("page", nil)
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
	probe, err := e.Render("page", nil)
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
		b, _ := e.Render("page", nil)
		e.Release(b)
	})
	if engine > direct+2 {
		t.Error(test.DiffMessage(engine, direct+2, "D10: the in-place overwrite adds at most 2 allocations to a set whose function returns the nonce"))
	}
}
