package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

const benchRows = 40

type benchData struct{ Rows []string }

func benchPage(nonce bool) map[string]string {
	script := ""
	if nonce {
		script = `<script nonce="{{nonce}}">1</script><style nonce="{{nonce}}"></style>`
	}
	return map[string]string{
		"layout.html": `<html><head>` + script + `</head><body>{{template "content" .}}</body></html>`,
		"list.html":   `<ul>{{range .Rows}}<li class="row">{{.}}</li>{{end}}</ul>`,
	}
}

func benchRowsData() benchData {
	rows := make([]string, benchRows)
	for i := range rows {
		rows[i] = strings.Repeat("row ", 20)
	}
	return benchData{Rows: rows}
}

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

func rdErrorFiles(names ...string) map[string]string {
	files := map[string]string{"layout.html": `<main>{{template "content" .}}</main>`}
	for _, name := range names {
		files[name] = strings.TrimSuffix(name, ".html") + `:{{.Code}}`
	}
	return files
}

func sprint(v any) string { return fmt.Sprint(v) }

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
