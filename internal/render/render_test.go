package render

import (
	"errors"
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

func rdEngine(t testing.TB, files map[string]string, isReload bool) *Engine {
	t.Helper()
	e := New(rdFS(files), isReload)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRender_RendersPageInLayout(t *testing.T) {
	e := rdEngine(t, map[string]string{
		"layout.html": `[{{template "content" .}}]`,
		"a/b.html":    `{{.}}`,
	}, false)
	b, err := e.Render("a/b", "hi")
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

func TestRender_UnknownPage(t *testing.T) {
	e := rdEngine(t, map[string]string{"layout.html": `{{template "content" .}}`, "partials/p.html": "p", "a.html": "a"}, false)
	for _, name := range []string{"", "layout", "partials/p", "error", "../a", "a.html"} {
		if _, err := e.Render(name, nil); !errors.Is(err, ErrUnknownPage) {
			t.Error(test.DiffMessage(err, ErrUnknownPage, "R9: unknown name "+name))
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
