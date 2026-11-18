package core

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
)

var (
	nonceHex      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	nonceAttr     = regexp.MustCompile(`(?:nonce|data-n)="([^"]*)"`)
	nonceFilesMap = map[string]string{
		"used.html":  `<script nonce="{{nonce}}">var n="{{nonce}}";</script><style nonce="{{nonce}}"></style><i data-n="{{nonce}}"></i>`,
		"plain.html": `<p>plain</p>`,
	}
)

type nonceFailing struct{ common.HTTP }

func (c nonceFailing) NewController() Controller { return c }

func (c nonceFailing) RENDER_boom() { panic(exception.NotFoundException("nothing here")) }

type noncePages struct{ common.HTTP }

func (c noncePages) NewController() Controller { return c }
func (c noncePages) RENDER_used()              {}
func (c noncePages) RENDER_plain()             {}

func nonceOf(t *testing.T, body string) string {
	t.Helper()
	found := nonceAttr.FindAllStringSubmatch(body, -1)
	if len(found) != 3 {
		t.Fatal(test.DiffMessage(len(found), 3, "R33: three nonce attributes in "+body))
	}
	for _, m := range found {
		if m[1] != found[0][1] {
			t.Error(test.DiffMessage(m[1], found[0][1], "R33: the same nonce at every use"))
		}
	}
	if !nonceHex.MatchString(found[0][1]) {
		t.Error(test.DiffMessage(found[0][1], "32 lowercase hexadecimal characters", "R33: nonce format"))
	}
	if n := strings.Count(body, found[0][1]); n != 4 {
		t.Error(test.DiffMessage(n, 4, "R33: the script string holds the same value"))
	}
	return found[0][1]
}

func TestNonce_WithoutHelmet(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	w := createGet(app, "/used")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R34: the page renders"))
	}
	nonceOf(t, w.Body.String())
	if csp := w.Header().Values("Content-Security-Policy"); len(csp) != 0 {
		t.Error(test.DiffMessage(csp, []string{}, "R34: no CSP header is added without helmet"))
	}
}

func TestNonce_PlainPageHasNone(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	w := createGet(app, "/plain")
	if w.Body.String() != "<p>plain</p>" || len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "<p>plain</p>", "R30: a page without {{nonce}} is unchanged"))
	}
}

func TestNonce_UniquePerResponse(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		seen[nonceOf(t, createGet(app, "/used").Body.String())] = true
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "R33: 1,000 responses give 1,000 nonces"))
	}
}

func TestNonce_NeverFromClient(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})
	submitted := strings.Repeat("f", 32)

	w := createRequest(app, http.MethodGet, "/used?x="+submitted, "X-Nonce", submitted, "Content-Security-Policy", "script-src 'nonce-"+submitted+"'")
	if got := nonceOf(t, w.Body.String()); got == submitted {
		t.Error(test.DiffMessage(got, "a server drawn value", "R33: a value submitted by a client never becomes a nonce"))
	}
}

func TestNonce_Concurrent(t *testing.T) {
	app, _ := pgNew(t, pgFiles(nonceFilesMap), noncePages{})

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				body := createGet(app, "/used").Body.String()
				found := nonceAttr.FindAllStringSubmatch(body, -1)
				if len(found) != 3 || found[0][1] != found[1][1] || found[1][1] != found[2][1] || strings.Count(body, found[0][1]) != 4 {
					t.Error(test.DiffMessage(body, "one nonce at every use", "R33: concurrent responses"))
					return
				}
				mu.Lock()
				seen[found[0][1]] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 640 {
		t.Error(test.DiffMessage(len(seen), 640, "R33: 64 goroutines never share a nonce"))
	}
}

func TestNonce_ErrorPageSet(t *testing.T) {
	files := pgFiles(map[string]string{
		"boom.html": `boom`,
		"404.html":  `<script nonce="{{nonce}}"></script><style nonce="{{nonce}}"></style><i data-n="{{nonce}}"></i><b>{{.Code}}</b>`,
	})
	app, _ := pgNew(t, files, nonceFailing{})

	w := createGet(app, "/boom")
	if w.Code != http.StatusNotFound || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "R30: the error page answers"))
	}
	body := w.Body.String()
	found := nonceAttr.FindAllStringSubmatch(body, -1)
	if len(found) != 3 || found[0][1] != found[1][1] || found[1][1] != found[2][1] || !nonceHex.MatchString(found[0][1]) {
		t.Error(test.DiffMessage(body, "one nonce at every use", "R30: an error page that uses {{nonce}} gets one"))
	}
	again := nonceAttr.FindStringSubmatch(createGet(app, "/boom").Body.String())
	if again == nil || again[1] == found[0][1] {
		t.Error(test.DiffMessage(again, "another nonce", "R33: the error page draws a nonce per response"))
	}
}
