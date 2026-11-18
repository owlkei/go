package helmet

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
	"github.com/dangduoc08/ginject/internal/test"
)

const (
	nonceDefaultPolicy = "default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self' 'nonce-%s'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests"
	nonceForeign       = "default-src 'none'; img-src data:"
)

var (
	nonceHexValue = regexp.MustCompile(`^[0-9a-f]{32}$`)
	nonceInHeader = regexp.MustCompile(`'nonce-([^']*)'`)
	nonceInBody   = regexp.MustCompile(`nonce="([^"]*)"`)
)

type noncePages struct {
	common.Page
	common.Middleware
}

func (c noncePages) NewController() core.Controller { return c }

func (c noncePages) READ_nonce() common.View { return common.View{Name: "nonce"} }
func (c noncePages) READ_plain() common.View { return common.View{Name: "plain"} }
func (c noncePages) READ_boom() common.View  { panic(exception.NotFoundException("missing")) }

func (c noncePages) READ_foreign(h *ctx.HTTPContext) common.View {
	h.ResponseWriter.Header().Set("Content-Security-Policy", nonceForeign)
	return common.View{Name: "nonce"}
}

type nonceLaterMiddleware struct{}

func (nonceLaterMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("Content-Security-Policy", nonceForeign)
	next()
}

type nonceLaterPages struct {
	common.Page
	common.Middleware
}

func (c nonceLaterPages) NewController() core.Controller {
	c.BindMiddleware(nonceLaterMiddleware{})
	return c
}

func (c nonceLaterPages) READ_later() common.View { return common.View{Name: "nonce"} }

type nonceAPI struct{ common.HTTP }

func (c nonceAPI) NewController() core.Controller { return c }
func (c nonceAPI) READ_api() string               { return "api" }

func nonceFiles(errorPage bool) fstest.MapFS {
	files := fstest.MapFS{
		"nonce.html":     {Data: []byte(`<script nonce="{{nonce}}"></script><style nonce="{{nonce}}"></style><i nonce="{{nonce}}"></i>`)},
		"plain.html":     {Data: []byte(`<p>plain</p>`)},
		"assets/app.css": {Data: []byte(`a{}`)},
	}
	if errorPage {
		files["error.html"] = &fstest.MapFile{Data: []byte(`<script nonce="{{nonce}}"></script><b>{{.Code}}</b>`)}
	}
	return files
}

func nonceApp(t testing.TB, h Helmet, errorPage bool) *core.App {
	t.Helper()
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(h)
	app.EnableRender(&core.RenderOptions{FS: nonceFiles(errorPage), Dir: "."})
	app.Create(core.NewModuleBuilder().Controllers(noncePages{}, nonceLaterPages{}, nonceAPI{}).Build())
	return app
}

func nonceServe(app *core.App, target string, headers ...string) *httptest.ResponseRecorder {
	return serve(app, request(http.MethodGet, target, headers...))
}

func directives(policy string) map[string]string {
	out := map[string]string{}
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		name, value, _ := strings.Cut(d, " ")
		out[name] = value
	}
	return out
}

func directiveOrder(policy string) []string {
	var names []string
	for _, d := range strings.Split(policy, ";") {
		if name, _, _ := strings.Cut(strings.TrimSpace(d), " "); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func bodyNonce(t testing.TB, body string) string {
	t.Helper()
	found := nonceInBody.FindAllStringSubmatch(body, -1)
	if len(found) == 0 {
		t.Fatal(test.DiffMessage(body, "nonce attributes", "R33: the page carries nonces"))
	}
	for _, m := range found {
		if m[1] != found[0][1] {
			t.Error(test.DiffMessage(m[1], found[0][1], "R33: the same nonce at every use in the body"))
		}
	}
	return found[0][1]
}

func TestNonce_DefaultPolicyHeader(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	w := nonceServe(app, "/nonce")
	n := bodyNonce(t, w.Body.String())
	want := strings.ReplaceAll(nonceDefaultPolicy, "%s", n)
	if got := w.Header().Get("Content-Security-Policy"); got != want {
		t.Error(test.DiffMessage(got, want, "R31: the exact default header"))
	}
	if n := len(w.Header().Values("Content-Security-Policy")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R31: one CSP header"))
	}
}

func TestNonce_SameInHeaderAndBody(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	w := nonceServe(app, "/nonce")
	body := bodyNonce(t, w.Body.String())
	if !nonceHexValue.MatchString(body) {
		t.Error(test.DiffMessage(body, "32 lowercase hexadecimal characters", "R33: format"))
	}
	headers := nonceInHeader.FindAllStringSubmatch(w.Header().Get("Content-Security-Policy"), -1)
	if len(headers) != 2 {
		t.Fatal(test.DiffMessage(len(headers), 2, "R33: a nonce in script-src and in style-src"))
	}
	for _, m := range headers {
		if m[1] != body {
			t.Error(test.DiffMessage(m[1], body, "R33: the header nonce equals the body nonce"))
		}
	}
	if strings.Count(w.Body.String(), body) != 3 {
		t.Error(test.DiffMessage(strings.Count(w.Body.String(), body), 3, "R33: three uses in one page"))
	}
}

func TestNonce_UniquePerResponse(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		w := nonceServe(app, "/nonce")
		n := bodyNonce(t, w.Body.String())
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'") {
			t.Fatal(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'", "R33: header and body agree"))
		}
		seen[n] = true
	}
	if len(seen) != 1000 {
		t.Error(test.DiffMessage(len(seen), 1000, "R33: 1,000 responses give 1,000 nonces"))
	}
}

func TestNonce_Concurrent(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				w := nonceServe(app, "/nonce")
				found := nonceInBody.FindStringSubmatch(w.Body.String())
				if found == nil || strings.Count(w.Header().Get("Content-Security-Policy"), "'nonce-"+found[1]+"'") != 2 {
					t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "the body's nonce twice", "R33: concurrent header and body"))
					return
				}
				mu.Lock()
				seen[found[1]] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 640 {
		t.Error(test.DiffMessage(len(seen), 640, "R33: 64 goroutines never share a nonce"))
	}
}

func TestNonce_OnlyWhenUsed(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	for _, target := range []string{"/plain", "/api", "/assets/app.css", "/missing"} {
		w := nonceServe(app, target)
		if got := w.Header().Get("Content-Security-Policy"); got != defaultCSP {
			t.Error(test.DiffMessage(got, defaultCSP, "R30: the static policy, byte for byte: "+target))
		}
		if strings.Contains(w.Header().Get("Content-Security-Policy"), "nonce") {
			t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "no nonce", "R30: "+target))
		}
	}
	if got := nonceServe(app, "/nonce").Header().Get("Content-Security-Policy"); got == defaultCSP {
		t.Error(test.DiffMessage(got, "a policy with a nonce", "R30: a page that uses {{nonce}} differs"))
	}
	if got := nonceServe(app, "/plain").Header().Get("Content-Security-Policy"); got != defaultCSP {
		t.Error(test.DiffMessage(got, defaultCSP, "R30: the shared header is not changed by an earlier nonce response"))
	}
}

func TestNonce_ErrorPageSet(t *testing.T) {
	app := nonceApp(t, Helmet{}, true)

	w := nonceServe(app, "/boom")
	if w.Code != http.StatusNotFound {
		t.Fatal(test.DiffMessage(w.Code, http.StatusNotFound, "R30: the error page answers"))
	}
	n := bodyNonce(t, w.Body.String())
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'") {
		t.Error(test.DiffMessage(w.Header().Get("Content-Security-Policy"), "'nonce-"+n+"'", "R30: an error page that uses {{nonce}} gets its nonce in the header"))
	}

	if got := nonceServe(app, "/api").Header().Get("Content-Security-Policy"); got != defaultCSP {
		t.Error(test.DiffMessage(got, defaultCSP, "R30: an API route stays static with an error page that has a nonce"))
	}
}

func TestNonce_WithoutHelmet(t *testing.T) {
	app := core.New()
	app.SetLogger(silentLogger{})
	app.EnableRender(&core.RenderOptions{FS: nonceFiles(false), Dir: "."})
	app.Create(core.NewModuleBuilder().Controllers(noncePages{}, nonceAPI{}).Build())

	w := nonceServe(app, "/nonce")
	if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
		t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R34: {{nonce}} still renders a nonce"))
	}
	if len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Header().Values("Content-Security-Policy"), []string{}, "R34: no CSP header is added"))
	}

	disabled := nonceApp(t, Helmet{ContentSecurityPolicy: Disabled}, false)
	w = nonceServe(disabled, "/nonce")
	if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
		t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R34: a disabled policy still renders a nonce"))
	}
	if len(w.Header().Values("Content-Security-Policy")) != 0 {
		t.Error(test.DiffMessage(w.Header().Values("Content-Security-Policy"), []string{}, "R34: a disabled policy adds no header"))
	}
}

func TestNonce_KeepsForeignCSP(t *testing.T) {
	app := nonceApp(t, Helmet{}, false)

	for _, target := range []string{"/foreign", "/later"} {
		w := nonceServe(app, target)
		if got := w.Header().Values("Content-Security-Policy"); !reflect.DeepEqual(got, []string{nonceForeign}) {
			t.Error(test.DiffMessage(got, []string{nonceForeign}, "R35: a CSP set after helmet is left as it is: "+target))
		}
		if !nonceHexValue.MatchString(bodyNonce(t, w.Body.String())) {
			t.Error(test.DiffMessage(w.Body.String(), "a nonce", "R35: the body still renders: "+target))
		}
	}
}

func nonceHeader(t *testing.T, policy string) (string, string) {
	t.Helper()
	app := nonceApp(t, Helmet{ContentSecurityPolicy: policy}, false)
	w := nonceServe(app, "/nonce")
	return w.Header().Get("Content-Security-Policy"), bodyNonce(t, w.Body.String())
}

func TestHelmet_NonceDirectives(t *testing.T) {
	got, n := nonceHeader(t, "script-src 'self'; style-src 'self' https://cdn.test; img-src 'self'")
	want := "script-src 'self' 'nonce-" + n + "'; style-src 'self' https://cdn.test 'nonce-" + n + "'; img-src 'self'"
	if got != want {
		t.Error(test.DiffMessage(got, want, "R32: present directives get the nonce appended"))
	}

	policy := "script-src 'self' 'unsafe-inline'; style-src 'none'; img-src 'self'"
	if got, _ := nonceHeader(t, policy); got != policy {
		t.Error(test.DiffMessage(got, policy, "R32: 'unsafe-inline' and 'none' directives are unchanged"))
	}

	got, n = nonceHeader(t, "default-src 'self' data:; script-src 'self'")
	d := directives(got)
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'self' data: 'nonce-"+n+"'" || d["default-src"] != "'self' data:" {
		t.Error(test.DiffMessage(got, "script-src with the nonce and style-src copied from default-src plus the nonce", "R32: an absent style-src follows default-src"))
	}
	if order := directiveOrder(got); len(order) < 2 || order[0] != "default-src" || order[1] != "style-src" {
		t.Error(test.DiffMessage(order, "style-src right after default-src", "R32: the inserted directive follows default-src"))
	}

	if got, _ := nonceHeader(t, "img-src 'self'; font-src 'self'"); got != "img-src 'self'; font-src 'self'" {
		t.Error(test.DiffMessage(got, "img-src 'self'; font-src 'self'", "R32: no default-src and no directive, unchanged"))
	}
	if got, _ := nonceHeader(t, "default-src 'none'; img-src 'self'"); got != "default-src 'none'; img-src 'self'" {
		t.Error(test.DiffMessage(got, "default-src 'none'; img-src 'self'", "R32: default-src 'none' is unchanged"))
	}
	if got, _ := nonceHeader(t, "default-src 'self' 'unsafe-inline'; img-src 'self'"); got != "default-src 'self' 'unsafe-inline'; img-src 'self'" {
		t.Error(test.DiffMessage(got, "default-src 'self' 'unsafe-inline'; img-src 'self'", "R32: default-src 'unsafe-inline' is unchanged"))
	}
}

func TestHelmet_NonceDirectivesElem(t *testing.T) {
	got, n := nonceHeader(t, "default-src 'self'; script-src-elem 'self'; style-src-elem 'self' 'unsafe-inline'")
	d := directives(got)
	if d["script-src-elem"] != "'self' 'nonce-"+n+"'" {
		t.Error(test.DiffMessage(d["script-src-elem"], "'self' 'nonce-"+n+"'", "R32: script-src-elem gets the nonce"))
	}
	if d["style-src-elem"] != "'self' 'unsafe-inline'" {
		t.Error(test.DiffMessage(d["style-src-elem"], "'self' 'unsafe-inline'", "R32: style-src-elem with 'unsafe-inline' is unchanged"))
	}
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'self' 'nonce-"+n+"'" {
		t.Error(test.DiffMessage(got, "script-src and style-src inserted from default-src", "R32: absent script-src and style-src follow default-src"))
	}

	got, n = nonceHeader(t, "script-src 'self'; script-src-elem 'self'; style-src 'none'; style-src-elem 'none'")
	d = directives(got)
	if d["script-src"] != "'self' 'nonce-"+n+"'" || d["script-src-elem"] != "'self' 'nonce-"+n+"'" || d["style-src"] != "'none'" || d["style-src-elem"] != "'none'" {
		t.Error(test.DiffMessage(got, "nonce on both script directives, 'none' style directives unchanged", "R32: -elem directives follow the same rule"))
	}
}

func TestHelmet_SnapshotNonceDirectives(t *testing.T) {
	for policy, want := range map[string][]string{
		"":                                    {"script-src", "style-src"},
		"script-src 'self'; style-src 'none'": {"script-src"},
		"default-src 'self'; script-src-elem 'self'; style-src-elem 'self'": {"script-src", "style-src", "script-src-elem", "style-src-elem"},
		"default-src 'none'": {},
		"img-src 'self'":     {},
		Disabled:             {},
	} {
		mw := Helmet{ContentSecurityPolicy: policy}.NewMiddleware()
		snapshotter, ok := mw.(inspect.Snapshotter)
		if !ok {
			t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "R44: helmet implements Snapshotter"))
		}
		snap := snapshotter.Snapshot()
		got, ok := field(snap, "nonce_directives").([]string)
		if !ok {
			t.Fatal(test.DiffMessage(field(snap, "nonce_directives"), "a []string", "R44: the field is present: "+policy))
		}
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !sameSet(got, want) {
			t.Error(test.DiffMessage(got, want, "R44: the directives that receive a nonce: "+policy))
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}
