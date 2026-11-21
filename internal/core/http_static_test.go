package core

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/test"
)

func TestStaticName_Policy(t *testing.T) {
	refused := []string{
		"a\\b.txt", "x.txt::$DATA", "C:x.txt", "x.txt:stream", "main.txt.", "x.txt ", "x.txt...", "GIT~1/config", "SECRE~1.GO", "ENV~1", "a~1",
		"CON", "con.txt", "PRN", "AUX.html", "nul.txt", "COM1", "com9.css", "LPT1", "lpt9.css", "d/NUL", "CONIN$", "conout$.txt", "COM\u00b9", "lpt\u00b3.css", "Com\u00b2.txt", "a\x01b.txt", "a\x7fb.txt", "d/.hidden.txt", ".hidden/x.txt", "d/.x/y.txt",
	}
	accepted := []string{"a~b.txt", ".well-known/x.txt", "con-fig.txt", "a b.txt", "100%.png", "comx.txt", "lpt10.txt", "console.txt", "a.b.c.txt", "conin.txt", "conout.txt", "com\u2074.txt", "com\u00b9x.txt"}
	files := map[string]string{"page.html": "x"}
	for _, name := range refused {
		files["public/"+name] = secMarker
	}
	for _, name := range accepted {
		files["public/"+name] = "ACCEPTED"
	}
	logger := &lcRecordingLogger{}
	app := New(&AppOptions{Logger: logger, Render: &RenderOptions{FS: pgFiles(files), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(secPage{}).Build())

	for _, name := range refused {
		if isServableName(name, false) {
			t.Error(test.DiffMessage(true, false, "S1: "+strconv.Quote(name)+" is refused by the policy"))
		}
		want := http.StatusNotFound
		if hasControl(name) {
			want = http.StatusBadRequest
		}
		w := createGet(app, (&url.URL{Path: "/" + name}).EscapedPath())
		if w.Code != want || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, want, "S1, S2, N2: "+strconv.Quote(name)+" is refused (400 for a control character, else the fixed 404)"))
		}
	}
	for _, name := range accepted {
		if !isServableName(name, false) {
			t.Error(test.DiffMessage(false, true, "S1: "+strconv.Quote(name)+" is accepted by the policy"))
		}
		if strings.Contains(name, " ") {
			continue
		}
		w := createGet(app, (&url.URL{Path: "/" + name}).EscapedPath())
		if w.Code != http.StatusOK || w.Body.String() != "ACCEPTED" {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "ACCEPTED"}, "S1: "+strconv.Quote(name)+" is a legal name"))
		}
	}
	if len(logger.named("StaticFilesSkipped")) != 1 {
		t.Error(test.DiffMessage(len(logger.named("StaticFilesSkipped")), 1, "S3, P3: the refused names are reported once"))
	}
}

func TestStaticName_Forbidden(t *testing.T) {
	forbidden := []string{
		"main.go", "MAIN.GO", "a/b/Main.Go", "go.mod", "Go.Mod", "go.sum", "go.work", "go.work.sum", "prod.env", "PROD.ENV", ".env",
		"a.pem", "a.KEY", "a.p12", "a.pfx", "id_rsa", "ID_RSA", "id_dsa", "id_ecdsa", "id_ed25519",
		"index.html~", "a.swp", "a.swo", "a.bak", "a.orig", "a.rej", "a.old", "a.tmp", "#main.go#", "#x#",
		"d.sql", "d.sqlite", "d.sqlite3", "d.db", "app.js.map", "App.JS.MAP",
		"terraform.tfstate", "infra.TFSTATE.backup", "vault.kdbx", "app.jks", "release.keystore", "server.ppk",
		"credentials.json", "Credentials.JSON", "service-account.json", "service-account-prod.json", "Service-Account.JSON",
	}
	allowed := []string{"go.model", "key.txt", "mapfile", "sql.txt", "gopher", "main.gopher", "id_rsa.txt", "env", "a.pemx", "#x", "x#", "dbx", "ok.txt", "index.html", "a.map.txt", "tfstate", "a.tfstate.txt", "credentials.json.txt", "my-credentials.json", "service-account.txt", "service.json"}
	for _, name := range forbidden {
		if !isForbiddenFile(name) {
			t.Error(test.DiffMessage(false, true, "F1: "+name+" is forbidden"))
		}
	}
	for _, name := range allowed {
		if isForbiddenFile(name) {
			t.Error(test.DiffMessage(true, false, "F1: "+name+" is a near miss, not forbidden"))
		}
	}
	if allocs := testing.AllocsPerRun(100, func() {
		for _, name := range forbidden {
			_ = isForbiddenFile(name)
		}
		for _, name := range allowed {
			_ = isForbiddenFile(name)
		}
	}); allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "F2: isForbiddenFile does not allocate"))
	}
}

func TestStaticAttack_EncodedTraversal(t *testing.T) {
	secExpect(t, secApps(t), "EncodedTraversal", http.StatusNotFound,
		"%2e%2e/secret.txt", "..%2fsecret.txt", "%2e%2e%2f%2e%2e%2fetc/passwd", "a/../../secret.txt", "../../etc/passwd")
}

func TestStaticAttack_DoubleEncodedTraversal(t *testing.T) {
	secExpect(t, secApps(t), "DoubleEncodedTraversal", http.StatusNotFound,
		"%252e%252e/secret.txt", "%252e%252e%252fsecret.txt", "..%255c..%255csecret.txt")
}

func TestStaticAttack_Backslash(t *testing.T) {
	secExpect(t, secApps(t), "Backslash", http.StatusNotFound,
		"..%5c..%5csecret.txt", "sub%5c.env", "sub%5csecret.go", "sub%5c..%5c.git%5cconfig")
}

func TestStaticAttack_DriveLetter(t *testing.T) {
	secExpect(t, secApps(t), "DriveLetter", http.StatusNotFound,
		"C:/Windows/win.ini", "c:secret.txt", "x.txt::$DATA", "x.txt:stream", "/server/share/x")
}

func TestStaticAttack_NUL(t *testing.T) {
	apps := secApps(t)
	for _, app := range apps {
		for _, target := range []string{"/s/a.txt%00.png", "/s/%00", "/t/a%00/ok.txt", "/%00", "/assets/a.txt%00.png", "/assets/_pages/%00"} {
			w := createGet(app, target)
			if w.Code != http.StatusBadRequest || w.Body.String() != string(invalidURLBody) {
				t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusBadRequest, string(invalidURLBody)}, "NUL: "+target+" is the fixed 400 of N2"))
			}
		}
	}
}

func TestStaticAttack_TrailingDotSpace(t *testing.T) {
	secExpect(t, secApps(t), "TrailingDotSpace", http.StatusNotFound,
		"main.go.", "main.go%20", ".env.", "main.go....", "secret.key%20.")
}

func TestStaticAttack_MixedCase(t *testing.T) {
	secExpect(t, secApps(t), "MixedCase", http.StatusNotFound,
		"MAIN.GO", "Go.Mod", ".ENV", "Key.PEM", "APP.JS.MAP", ".GIT/config", "Tls.Key", "PROD.ENV")
	for _, app := range secApps(t) {
		for _, target := range []string{"/ASSETS/ok.txt", "/Assets/ok.txt", "/S/ok.txt", "/T/acme/ok.txt"} {
			if w := createGet(app, target); w.Code != http.StatusNotFound {
				t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "MixedCase, N5: routes are case-sensitive: "+target))
			}
		}
	}
}

func TestStaticAttack_ShortName(t *testing.T) {
	secExpect(t, secApps(t), "ShortName", http.StatusNotFound,
		"GIT~1/config", "SECRET~1.GO", "ENV~1", "PROGRA~1/x", "LONGNA~1.TXT")
	if runtime.GOOS != "windows" {
		t.Skip("8.3 short names exist only on Windows volumes; the name-policy half above ran")
	}
	_, _, env := secBuild(t, secMap, nil)
	if _, err := os.Stat(filepath.Join(env.serve, "LONGNA~1.TXT")); err != nil {
		t.Skip("this volume has no 8.3 names:", err)
	}
}

func TestStaticAttack_DeviceName(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		secExpect(t, secApps(t), "DeviceName", http.StatusNotFound, "CON", "nul.txt", "COM1", "aux.html", "LPT9.css", "sub/PRN")
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("DeviceName: a request hung")
	}
}

func TestStaticAttack_SymlinkOut(t *testing.T) {
	app, logger, env := secBuild(t, secMap, nil)
	secLink(t, filepath.Join(env.root, "secret.txt"), filepath.Join(env.serve, "out_file"))
	secLink(t, env.outside, filepath.Join(env.serve, "out_dir"))
	secLink(t, filepath.Join(env.up, "beta", "private.txt"), filepath.Join(env.up, "acme", "to_beta"))
	secLink(t, env.outside, filepath.Join(env.up, "evil"))
	for _, target := range []string{"/s/out_file", "/s/out_dir/x.txt", "/t/acme/to_beta", "/t/evil/x.txt", "/f?f=out_file", "/f?f=out_dir/x.txt"} {
		w := createGet(app, target)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, "a refusal", "SymlinkOut, D5: "+target))
		}
		if !strings.HasPrefix(target, "/f?") && (w.Code != http.StatusNotFound || w.Body.String() != wantNotFoundJSON) {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusNotFound, wantNotFoundJSON}, "SymlinkOut, D5: the fixed 404 for "+target))
		}
	}
	_ = logger

	disk, diskLogger, _ := secBuild(t, secDiskLinks, nil)
	for _, target := range []string{"/assets/link.txt", "/link.txt"} {
		w := createGet(disk, target)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "SymlinkOut, S6: a linked file of Render.FS is skipped at Create: "+target))
		}
	}
	if len(diskLogger.named("StaticFilesSkipped")) != 1 {
		t.Error(test.DiffMessage(len(diskLogger.named("StaticFilesSkipped")), 1, "S6, P3: the linked files are reported"))
	}
}

func TestStaticAttack_SymlinkOutEmbedReload(t *testing.T) {
	web := filepath.Join("testdata", "web")
	assets := filepath.Join(web, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(assets) })
	if err := os.WriteFile(filepath.Join(assets, "real.txt"), []byte("REAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(secTemp(t), "outside.txt")
	if err := os.WriteFile(outside, []byte(secMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	app, _ := rdApp(t, &RenderOptions{FS: rdEmbedded, Dir: web, IsReload: true})
	secLink(t, outside, filepath.Join(assets, "link.txt"))
	if w := createGet(app, "/assets/real.txt"); w.Code != http.StatusOK || w.Body.String() != "REAL" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "REAL"}, "S5: a plain file is read from disk"))
	}
	if w := createGet(app, "/assets/link.txt"); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), secMarker) {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S5: a link under web/ that leaves it is not followed"))
	}
}

func TestStaticAttack_SymlinkIn(t *testing.T) {
	app, _, env := secBuild(t, secMap, nil)
	secLink(t, "ok.txt", filepath.Join(env.serve, "alias.txt"))
	secLink(t, "ok.txt", filepath.Join(env.up, "acme", "alias.txt"))
	for _, target := range []string{"/s/alias.txt", "/t/acme/alias.txt", "/f?f=alias.txt"} {
		if w := createGet(app, target); w.Code != http.StatusOK || w.Body.String() != "OK-BODY" {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "OK-BODY"}, "SymlinkIn: "+target))
		}
	}
}

func TestStaticAttack_HiddenSegment(t *testing.T) {
	secExpect(t, secApps(t), "HiddenSegment", http.StatusNotFound,
		".env", ".git/config", "css/.secret", ".well-known/.env", ".DS_Store")
	for _, app := range secApps(t) {
		for _, target := range []string{"/s/.well-known/security.txt", "/.well-known/security.txt", "/assets/.well-known/security.txt"} {
			if w := createGet(app, target); w.Code != http.StatusOK || w.Body.String() != "WELL-KNOWN" {
				t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "WELL-KNOWN"}, "HiddenSegment: .well-known is served: "+target))
			}
		}
		for _, target := range []string{"/t/.git/config", "/t/.git/x"} {
			if w := createGet(app, target); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), secMarker) {
				t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "HiddenSegment, D6: a hidden tenant: "+target))
			}
		}
	}
}

func TestStaticAttack_SourceAndSecrets(t *testing.T) {
	names := []string{"main.go", "go.mod", "go.sum", "go.work", "prod.env", "server.pem", "tls.key", "id_rsa", "store.p12", "dump.sql", "app.db"}
	secExpect(t, secApps(t), "SourceAndSecrets", http.StatusNotFound, names...)

	files := map[string]string{"page.html": "x"}
	for _, name := range names[:5] {
		files["public/"+name] = secMarker
		files["assets/"+name] = secMarker
	}
	logger := &lcRecordingLogger{}
	app := New(&AppOptions{Logger: logger, Render: &RenderOptions{FS: pgFiles(files), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(secPage{}).Build())
	entries := logger.named("StaticFilesSkipped")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "S3, P3: one StaticFilesSkipped line"))
	}
	listed := lcText(entries[0].field("files"))
	for _, name := range names[:5] {
		for _, dir := range []string{"public/", "assets/"} {
			if !strings.Contains(listed, dir+name) {
				t.Error(test.DiffMessage(listed, dir+name, "P3: the skipped file is listed"))
			}
		}
	}
	if entries[0].level != "warn" || lcText(entries[0].field("count")) != "10" {
		t.Error(test.DiffMessage([]any{entries[0].level, lcText(entries[0].field("count"))}, []any{"warn", "10"}, "P3: Warn with the count"))
	}
	if note := lcText(entries[0].field("note")); note != "source, secret, backup, linked or oddly named files are never served" {
		t.Error(test.DiffMessage(note, "source, secret, backup, linked or oddly named files are never served", "P3: the note"))
	}
}

func TestStaticAttack_EditorBackup(t *testing.T) {
	secExpect(t, secApps(t), "EditorBackup", http.StatusNotFound,
		"index.html~", ".index.html.swp", "index.html.swp", "app.js.bak", "app.js.orig", "%23app.js%23", "config.old", "x.tmp", "x.rej")
}

func TestStaticAttack_SourceMap(t *testing.T) {
	prod, _, _ := secBuild(t, secMap, nil)
	for _, way := range secWays() {
		if w := createGet(prod, way.target("app.js.map")); w.Code == http.StatusOK || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, "a refusal", "F3: no source map in production: "+way.name))
		}
	}
	dev, _, _ := secBuild(t, secDiskReload, nil)
	for _, way := range secWays() {
		w := createGet(dev, way.target("app.js.map"))
		isAssets := way.name == "e: /assets/*"
		if isAssets && w.Code != http.StatusOK {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "F3: /assets/app.js.map is served with IsReload"))
		}
		if !isAssets && (w.Code == http.StatusOK || strings.Contains(w.Body.String(), secMarker)) {
			t.Error(test.DiffMessage(w.Code, "a refusal", "F3: only /assets/ serves maps: "+way.name))
		}
	}
}

func TestStaticAttack_DirectoryIndex(t *testing.T) {
	for _, app := range secApps(t) {
		for _, target := range []string{"/s/dir_noindex", "/s/dir_noindex/", "/s/dir_idxdir/", "/t/acme/dir_noindex/", "/assets/", "/assets/_pages/", "/assets/sub/", "/sub/", "/assets/dir_noindex/"} {
			if w := createGet(app, target); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "x.txt") {
				t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusNotFound, wantNotFoundJSON}, "DirectoryIndex, S4: "+target))
			}
		}
		w := createGet(app, "/s/dir_index/")
		if w.Code != http.StatusOK || w.Body.String() != "INDEX-BODY" || w.Header().Get("Content-Disposition") != "attachment" {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String(), w.Header().Get("Content-Disposition")}, []any{http.StatusOK, "INDEX-BODY", "attachment"}, "DirectoryIndex, I1: a directory's index.html is an active file"))
		}
		if w := createGet(app, "/s/dir_index"); strings.Contains(w.Body.String(), "<pre>") || strings.Contains(w.Body.String(), "index.html") {
			t.Error(test.DiffMessage(w.Body.String(), "no listing", "DirectoryIndex, S4: a directory is never listed"))
		}
	}
}

func TestStaticAttack_RootOutsideServeDirs(t *testing.T) {
	app, logger, env := secBuild(t, secMap, nil)
	refused := []string{
		"/q/x.txt?tenant=../../etc", "/q/x.txt?tenant=..", "/q/x.txt?abs=" + url.QueryEscape("/etc"), "/q/x.txt?tenant=" + url.QueryEscape("a/../../b"),
		"/q/x.txt?abs=" + url.QueryEscape(filepath.Join(env.root, "other")), "/q/x.txt?abs=" + url.QueryEscape(env.up+"2"), "/q/x.txt?abs=" + url.QueryEscape(env.root),
		"/q/x.txt?tenant=a%0ab",
	}
	for i, target := range refused {
		w := createGet(app, target)
		if w.Code != http.StatusInternalServerError || w.Body.String() != wantInternalErrorJSON || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusInternalServerError, wantInternalErrorJSON}, "D4: "+target))
		}
		if w.Header().Get("Cache-Control") != "" {
			t.Error(test.DiffMessage(w.Header().Get("Cache-Control"), "", "D4: no Cache-Control on the 500"))
		}
		if got := len(logger.named("ServeRootRefused")); got != i+1 {
			t.Error(test.DiffMessage(got, i+1, "D4: one ServeRootRefused line per refused request: "+target))
		}
	}
	logged := logger.named("ServeRootRefused")
	if len(logged) == 0 {
		t.Fatal(test.DiffMessage(0, len(refused), "D4: ServeRootRefused is logged"))
	}
	entry := logged[0]
	if entry.level != "error" || !strings.Contains(lcText(entry.field("route")), "SERVE_q_ANY") || lcText(entry.field("note")) != "the directory is not under HTTP.ServeDirs" {
		t.Error(test.DiffMessage(entry, "an Error line with route and note", "D4: the log line"))
	}
}

func TestStaticAttack_Range(t *testing.T) {
	for _, app := range secApps(t) {
		for _, way := range secWays() {
			get := func(rng string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, way.target("range.txt"), nil)
				r.Header.Set("Range", rng)
				w := httptest.NewRecorder()
				app.ServeHTTP(w, r)
				return w
			}
			if way.name == "e: /assets/_pages/*" {
				continue
			}
			many := make([]string, 2000)
			for i := range many {
				many[i] = strconv.Itoa(i%100) + "-" + strconv.Itoa(i%100)
			}
			for rng, want := range map[string]struct{ code, size int }{
				"bytes=0-9":                        {http.StatusPartialContent, 10},
				"bytes=0-0,2-2":                    {http.StatusOK, 100},
				"bytes=" + strings.Join(many, ","): {http.StatusOK, 100},
				"bytes=0-10,5-20":                  {http.StatusOK, 100},
				"bytes=-1":                         {http.StatusPartialContent, 1},
				"bytes=500-":                       {http.StatusRequestedRangeNotSatisfiable, -1},
			} {
				w := get(rng)
				desc := "M3: " + way.name + ", " + rng[:min(len(rng), 24)]
				if w.Code != want.code {
					t.Error(test.DiffMessage(w.Code, want.code, desc))
				}
				if want.size >= 0 && w.Body.Len() != want.size {
					t.Error(test.DiffMessage(w.Body.Len(), want.size, desc+": body size"))
				}
				if w.Body.Len() > 100 {
					t.Error(test.DiffMessage(w.Body.Len(), 100, desc+": never more than the file"))
				}
			}
		}
	}
}

func TestStaticAttack_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("sparse 1 GiB file: skipped with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("sparse files are not portable to Windows volumes")
	}
	app, _, env := secBuild(t, secMap, nil)
	f, err := os.Create(filepath.Join(env.serve, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil {
		_ = f.Close()
		t.Skip("no sparse files here:", err)
	}
	_ = f.Close()

	head := createRequest(app, http.MethodHead, "/s/big.bin")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(1<<30) {
		t.Error(test.DiffMessage([]any{head.Code, head.Body.Len()}, []any{http.StatusOK, 0}, "LargeFile: HEAD has no body"))
	}

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		app.ServeHTTP(w, r)
	}))
	defer srv.Close()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET /s/big.bin HTTP/1.1\r\nHost: x\r\n\r\n"))
	buf := make([]byte, 1024)
	_, _ = io.ReadFull(conn, buf)
	_ = conn.Close()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("LargeFile: the handler did not end after the client left")
	}
	runtime.ReadMemStats(&after)
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 256<<20 {
		t.Error(test.DiffMessage(grown, uint64(256<<20), "LargeFile: the file is streamed, not read into memory"))
	}
}

func TestStaticAttack_MIMESniffing(t *testing.T) {
	for _, kind := range []string{"no Headers stage", "Headers stage"} {
		mutate := func(o *AppOptions) {}
		if kind == "Headers stage" {
			mutate = func(o *AppOptions) { o.Headers = &HeadersOptions{IsEnabled: true} }
		}
		for _, k := range []secKind{secMap, secDiskReload} {
			app, _, _ := secBuild(t, k, mutate)
			for name, want := range map[string]string{"sniff/x": "application/octet-stream", "sniff/x.unknown": "application/octet-stream", "sniff/x.png": "image/png", "sniff/x.txt": "text/plain; charset=utf-8"} {
				for _, way := range secWays() {
					if way.name == "e: /assets/_pages/*" {
						continue
					}
					w := createGet(app, way.target(name))
					if w.Header().Get("Content-Type") != want || w.Header().Get("X-Content-Type-Options") != "nosniff" {
						t.Error(test.DiffMessage([]any{w.Header().Get("Content-Type"), w.Header().Get("X-Content-Type-Options")}, []any{want, "nosniff"}, "MIMESniffing, M1, M2: "+way.name+", "+name+", "+kind))
					}
				}
			}
		}
	}
}

func TestStaticAttack_UploadedActiveFile(t *testing.T) {
	for _, stage := range []bool{false, true} {
		app, _, _ := secBuild(t, secMap, func(o *AppOptions) {
			if stage {
				o.Headers = &HeadersOptions{IsEnabled: true}
			}
		})
		for _, name := range []string{"evil.html", "evil.svg", "evil.xml", "evil.js", "evil.xhtml", "EVIL.HTM", "evil.svgz"} {
			for _, target := range []string{"/s/" + name, "/t/acme/" + name, "/f?f=" + name} {
				w := createGet(app, target)
				if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "attachment" || w.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Content-Disposition"), w.Header().Get("Content-Security-Policy")}, []any{http.StatusOK, "attachment", "sandbox; default-src 'none'"}, "I2: "+target))
				}
			}
			w := createGet(app, "/inline/"+name)
			csp := w.Header().Get("Content-Security-Policy")
			if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "inline" || strings.Contains(csp, "sandbox") || (stage && !strings.Contains(csp, "default-src 'self'")) || (!stage && csp != "") {
				t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Content-Disposition"), csp}, "inline, the stage's CSP kept", "I3: opt-in for "+name))
			}
		}
	}
}

func TestStaticAttack_EmbedAll(t *testing.T) {
	files := map[string]string{
		"page.html": "x", ".env": secMarker, ".git/HEAD": secMarker, "assets/.secret": secMarker, "public/.htpasswd": secMarker,
		"notes/x.txt": secMarker, "assets/ok.txt": "OK", "public/ok.txt": "OK", ".well-known/x": secMarker,
	}
	logger := &lcRecordingLogger{}
	app := New(&AppOptions{Logger: logger, Render: &RenderOptions{FS: pgFiles(files), Dir: "."}})
	mustCreate(t, app, NewModuleBuilder().Controllers(secPage{}).Build())
	for _, target := range []string{"/.env", "/.git/HEAD", "/assets/.secret", "/.htpasswd", "/notes/x.txt", "/assets/notes/x.txt", "/assets/.env", "/.well-known/x"} {
		if w := createGet(app, target); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "EmbedAll: "+target))
		}
	}
	entries := logger.named("RenderHiddenFiles")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "P4: RenderHiddenFiles is logged"))
	}
	listed := lcText(entries[0].field("files"))
	for _, name := range []string{".env", ".git", "assets/.secret", "public/.htpasswd"} {
		if !strings.Contains(listed, name) {
			t.Error(test.DiffMessage(listed, name, "P4: the hidden entry is listed"))
		}
	}
}

func TestStaticAttack_PublicRouteSyntax(t *testing.T) {
	for _, name := range []string{"{id}.txt", "*.txt", "a/{x}/b.txt", "a}.txt"} {
		app := New(&AppOptions{Render: &RenderOptions{FS: pgFiles(map[string]string{"page.html": "x", "public/" + name: "x"}), Dir: "."}})
		want := "invalid render: public/" + name + ": a public file name can't hold '{', '}' or '*', which the router reads as a parameter or a wildcard"
		msExpectPanic(t, "P1", want, func() { app.Create(NewModuleBuilder().Controllers(secPage{}).Build()) })
	}
}

func TestServeDirs_RootRefused(t *testing.T) {
	app, logger, env := secBuild(t, secMap, nil)
	for target, want := range map[string]int{"/num": http.StatusNotFound, "/empty": http.StatusNotFound} {
		if w := createGet(app, target); w.Code != want || w.Body.String() != wantNotFoundJSON {
			t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{want, wantNotFoundJSON}, "D3: a non-string or empty result is the fixed 404: "+target))
		}
	}
	if w := createGet(app, "/q/ok.txt?tenant=acme"); w.Code != http.StatusOK || w.Body.String() != "OK-BODY" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "OK-BODY"}, "D3: a root below a declared directory is served"))
	}
	if len(logger.named("ServeRootRefused")) != 0 {
		t.Error(test.DiffMessage(len(logger.named("ServeRootRefused")), 0, "D4: an accepted root logs nothing"))
	}
	_ = env
}

func TestServeDirs_ChdirDoesNotMoveRoots(t *testing.T) {
	base := secTemp(t)
	secWrite(t, filepath.Join(base, "pub"), map[string]string{"a.txt": "A"})
	t.Chdir(base)
	app := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{"pub"}}})
	mustCreate(t, app, NewModuleBuilder().Controllers(secRelServe{}).Build())
	if w := createGet(app, "/rel/a.txt"); w.Code != http.StatusOK || w.Body.String() != "A" {
		t.Fatal(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "A"}, "D1, D3: a relative entry is read against the working directory"))
	}
	elsewhere := secTemp(t)
	secWrite(t, filepath.Join(elsewhere, "pub"), map[string]string{"a.txt": "WRONG"})
	t.Chdir(elsewhere)
	if w := createGet(app, "/rel/a.txt"); w.Code != http.StatusOK || w.Body.String() != "A" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "A"}, "D1: Chdir after New does not move the roots"))
	}
}

func TestServeDirs_SubRoot(t *testing.T) {
	app, _, env := secBuild(t, secMap, nil)
	for target, want := range map[string]int{
		"/q/acme/ok.txt?abs=" + url.QueryEscape(env.up):                        http.StatusOK,
		"/q/ok.txt?abs=" + url.QueryEscape(filepath.Join(env.up, "acme")):      http.StatusOK,
		"/q/ok.txt?abs=" + url.QueryEscape(env.serve):                          http.StatusOK,
		"/q/ok.txt?abs=" + url.QueryEscape(filepath.Join(env.serve, "sub")):    http.StatusOK,
		"/q/secret.go?abs=" + url.QueryEscape(filepath.Join(env.serve, "sub")): http.StatusNotFound,
		"/q/.git/config?tenant=acme":                                           http.StatusNotFound,
		"/q/x.txt?tenant=.git":                                                 http.StatusNotFound,
		"/q/x.txt?tenant=" + url.QueryEscape(`x\..\y`):                         http.StatusNotFound,
		"/q/private.txt?tenant=acme":                                           http.StatusNotFound,
		"/q/..%5cbeta%5cprivate.txt?tenant=acme":                               http.StatusNotFound,
	} {
		w := createGet(app, target)
		if w.Code != want || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, want, "D5, D6: "+target))
		}
	}
}

func TestServeDirs_NoWildcard(t *testing.T) {
	app, logger, env := secBuild(t, secMap, nil)
	if w := createGet(app, "/f?f=ok.txt"); w.Code != http.StatusOK || w.Body.String() != "OK-BODY" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "OK-BODY"}, "D7: a one-file handler inside the declared directory"))
	}
	for _, target := range []string{"/f?f=../secret.txt", "/f?f=" + url.QueryEscape("../etc/passwd"), "/f?f=.env", "/f?f=sub/.env"} {
		w := createGet(app, target)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), secMarker) {
			t.Error(test.DiffMessage(w.Code, "a refusal", "D7: "+target))
		}
	}
	if len(logger.named("ServeRootRefused")) == 0 {
		t.Error(test.DiffMessage(0, "at least one ServeRootRefused", "D4, D7: a one-file path outside the declared directories is logged"))
	}
	_ = env
}

func TestStatic_NoSniffAlways(t *testing.T) {
	for _, app := range secApps(t) {
		for _, way := range secWays() {
			if way.name == "e: /assets/_pages/*" {
				continue
			}
			target := way.target("ok.txt")
			first := createGet(app, target)
			r206 := httptest.NewRequest(http.MethodGet, target, nil)
			r206.Header.Set("Range", "bytes=0-1")
			w206 := httptest.NewRecorder()
			app.ServeHTTP(w206, r206)
			r304 := httptest.NewRequest(http.MethodGet, target, nil)
			if etag := first.Header().Get("Etag"); etag != "" {
				r304.Header.Set("If-None-Match", etag)
			} else if lm := first.Header().Get("Last-Modified"); lm != "" {
				r304.Header.Set("If-Modified-Since", lm)
			}
			w304 := httptest.NewRecorder()
			app.ServeHTTP(w304, r304)
			for code, w := range map[int]*httptest.ResponseRecorder{http.StatusOK: first, http.StatusPartialContent: w206} {
				if w.Code != code || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("X-Content-Type-Options")}, []any{code, "nosniff"}, "M1: "+way.name))
				}
			}
			if w304.Code == http.StatusNotModified && w304.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Error(test.DiffMessage(w304.Header().Get("X-Content-Type-Options"), "nosniff", "M1: on 304, "+way.name))
			}
		}
	}
}

func TestStatic_ContentTypes(t *testing.T) {
	want := map[string]string{
		"a.css": "text/css; charset=utf-8", "a.js": "text/javascript; charset=utf-8", "a.mjs": "text/javascript; charset=utf-8",
		"a.html": "text/html; charset=utf-8", "a.htm": "text/html; charset=utf-8", "a.svg": "image/svg+xml", "a.json": "application/json",
		"a.wasm": "application/wasm", "a.xml": "text/xml; charset=utf-8", "a.txt": "text/plain; charset=utf-8", "a.png": "image/png",
		"a.unknownext": "application/octet-stream",
	}
	for _, app := range secApps(t) {
		for _, way := range secWays() {
			if way.name == "e: /assets/_pages/*" {
				continue
			}
			for name, typ := range want {
				if got := createGet(app, way.target("types/"+name)).Header().Get("Content-Type"); got != typ {
					t.Error(test.DiffMessage(got, typ, "M2: "+way.name+", "+name))
				}
			}
		}
		if got := createGet(app, "/typed/types/a.css?type=text/x-custom").Header().Get("Content-Type"); got != "text/x-custom" {
			t.Error(test.DiffMessage(got, "text/x-custom", "M2: a type set by the handler is kept"))
		}
	}
}

func TestStatic_MultiRangeIgnored(t *testing.T) {
	app, _, _ := secBuild(t, secMap, nil)
	r := httptest.NewRequest(http.MethodGet, "/s/range.txt", nil)
	r.Header.Set("Range", "bytes=0-0,2-2")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.Len() != 100 || strings.HasPrefix(w.Header().Get("Content-Type"), "multipart/") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.Len(), w.Header().Get("Content-Type")}, []any{http.StatusOK, 100, "a single-part type"}, "M3: several ranges are ignored"))
	}
}

func TestServe_ActiveFileIsAttachment(t *testing.T) {
	app, _, _ := secBuild(t, secMap, nil)
	for _, name := range []string{"evil.html", "evil.svg", "evil.xml", "evil.js", "evil.xhtml", "EVIL.HTM", "evil.svgz", "types/a.mjs", "types/a.htm", "dir_index/index.html"} {
		w := createGet(app, "/s/"+name)
		if w.Header().Get("Content-Disposition") != "attachment" || strings.Contains(w.Header().Get("Content-Disposition"), "filename") {
			t.Error(test.DiffMessage(w.Header().Get("Content-Disposition"), "attachment", "I1, I2: "+name))
		}
	}
	for _, name := range []string{"ok.txt", "types/a.png", "types/a.css", "types/a.json", "sniff/x.png"} {
		w := createGet(app, "/s/"+name)
		if w.Header().Get("Content-Disposition") != "" || w.Header().Get("Content-Security-Policy") != "" {
			t.Error(test.DiffMessage(w.Header().Get("Content-Disposition"), "", "I1: "+name+" is not an active file"))
		}
	}
	for _, typ := range []string{"text/html", "application/xhtml+xml", "image/svg+xml", "text/xml", "application/xml", "application/rss+xml", "text/javascript", "application/javascript"} {
		w := createGet(app, "/typed/ok.txt?type="+url.QueryEscape(typ))
		if w.Header().Get("Content-Disposition") != "attachment" || w.Header().Get("Content-Type") != typ {
			t.Error(test.DiffMessage([]any{w.Header().Get("Content-Disposition"), w.Header().Get("Content-Type")}, []any{"attachment", typ}, "I1: detection by type, "+typ))
		}
	}
	for _, typ := range []string{"text/plain", "image/png", "application/json"} {
		if w := createGet(app, "/typed/ok.txt?type="+url.QueryEscape(typ)); w.Header().Get("Content-Disposition") != "" {
			t.Error(test.DiffMessage(w.Header().Get("Content-Disposition"), "", "I1: "+typ+" is not active"))
		}
	}
	for _, target := range []string{"/dir_index/index.html", "/assets/dir_index/index.html", "/assets/types/a.html", "/types/a.svg"} {
		if w := createGet(app, target); w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "" || w.Header().Get("Content-Security-Policy") != "" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Content-Disposition")}, []any{http.StatusOK, ""}, "I1: public/ and assets are the developer's own: "+target))
		}
	}
}

func TestServe_InlineOptIn(t *testing.T) {
	app, logger, _ := secBuild(t, secMap, func(o *AppOptions) { o.Headers = &HeadersOptions{IsEnabled: true} })
	w := createGet(app, "/inline/evil.html")
	if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "inline" || strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Content-Disposition"), w.Header().Get("Content-Security-Policy")}, "inline and the stage's CSP", "I3: the handler's Content-Disposition is kept"))
	}
	if len(logger.named("ServeAttachment")) != 0 {
		t.Error(test.DiffMessage(len(logger.named("ServeAttachment")), 0, "I3: an opted-in route logs no ServeAttachment"))
	}
}

func TestServe_AttachmentLoggedOnce(t *testing.T) {
	app, logger, _ := secBuild(t, secMap, nil)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			createGet(app, "/s/evil.html")
		}()
	}
	wg.Wait()
	entries := logger.named("ServeAttachment")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "I4: one line per route under 50 concurrent requests"))
	}
	e := entries[0]
	if e.level != "warn" || !strings.Contains(lcText(e.field("route")), "SERVE_s_ANY") || lcText(e.field("file")) != "evil.html" ||
		lcText(e.field("note")) != "files that run in a browser are sent as downloads; set Content-Disposition: inline in the handler when these files are yours, never for uploads" {
		t.Error(test.DiffMessage(e, "Warn with route, file and note", "I4: the line"))
	}
	createGet(app, "/f?f=evil.svg")
	if got := len(logger.named("ServeAttachment")); got != 2 {
		t.Error(test.DiffMessage(got, 2, "I4: another route logs its own line"))
	}
}

func TestServe_GuardedIsPrivate(t *testing.T) {
	env := secPrepare(t)
	build := func(global bool, controllers ...Controller) *App {
		app := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{env.serve}}})
		if global {
			app.BindGlobalGuards(pgGuard{})
		}
		mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
		return app
	}
	cases := []struct {
		note, target, want string
		app                *App
	}{
		{"global guard", "/o/ok.txt", "private, no-cache", build(true, secOtherServe{})},
		{"controller guard", "/g/ok.txt", "private, no-cache", build(false, secServeGuarded{})},
		{"route guard", "/r/ok.txt", "private, no-cache", build(false, secServeRouteGuarded{})},
		{"no guard", "/o/ok.txt", "no-cache", build(false, secOtherServe{})},
		{"handler value", "/k/ok.txt", "public, max-age=60", build(true, secServeKept{})},
	}
	for _, c := range cases {
		w := createRequest(c.app, http.MethodGet, c.target, "X-Allow", "yes")
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != c.want {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Cache-Control")}, []any{http.StatusOK, c.want}, "C2: "+c.note))
		}
	}
}

func TestServe_GuardStillRuns(t *testing.T) {
	env := secPrepare(t)
	app := New(&AppOptions{HTTP: &HTTPOptions{ServeDirs: []string{env.serve}}})
	app.BindGlobalGuards(pgGuard{})
	mustCreate(t, app, NewModuleBuilder().Controllers(secOtherServe{}).Build())
	if w := createGet(app, "/o/ok.txt"); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "OK-BODY") {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "G9: a SERVE_ route keeps its guards"))
	}
	if w := createRequest(app, http.MethodGet, "/o/ok.txt", "X-Allow", "yes"); w.Code != http.StatusOK || w.Body.String() != "OK-BODY" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "G9: and serves when the guard allows"))
	}
}

func TestStaticName_UnicodeFold(t *testing.T) {
	refused := []string{
		"server.Key", "SERVER.KEY", "dump.ſql", "app.ſwp", "a.İx", "a.ıx", "ſ", "d/x.K", "d.ſ/x.txt", "xK/y.txt",
	}
	accepted := []string{"café.txt", "日本.txt", "d/über.txt", "ß.txt", "Δ.css"}
	for _, name := range refused {
		if isServableName(name, false) {
			t.Error(test.DiffMessage(true, false, "S1.8: "+strconv.Quote(name)+" is refused by the policy"))
		}
		for _, segment := range strings.Split(name, "/") {
			if strings.ContainsAny(segment, "Kſİı") && isServableSegment(segment) {
				t.Error(test.DiffMessage(true, false, "S1.8: the segment "+strconv.Quote(segment)+" is refused"))
			}
		}
	}
	for _, name := range accepted {
		if !isServableName(name, false) {
			t.Error(test.DiffMessage(false, true, "S1.8: "+strconv.Quote(name)+" stays a legal name"))
		}
	}
	if allocs := testing.AllocsPerRun(100, func() {
		for _, name := range refused {
			_ = isServableName(name, false)
		}
	}); allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "S1.8: the scan does not allocate"))
	}
}

func TestStaticAttack_UnicodeFold(t *testing.T) {
	env := secPrepare(t)
	names := []string{"secret.Key", "SECRET.KEY", "dump.ſql", "x.ſwp", "x.İdb", "x.ıdb"}
	for _, name := range names {
		secWrite(t, env.serve, map[string]string{name: secMarker})
		secWrite(t, filepath.Join(env.up, "acme"), map[string]string{name: secMarker})
	}
	a, _ := secBuildIn(t, env, secMap, nil)
	b, _ := secBuildIn(t, env, secDiskReload, nil)
	raws := make([]string, len(names))
	for i, name := range names {
		raws[i] = (&url.URL{Path: name}).EscapedPath()
	}
	secExpect(t, []*App{a, b}, "S1.8, UnicodeFold", http.StatusNotFound, raws...)
	if w := createGet(a, "/s/server.%E2%84%AAey"); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), secMarker) {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "S1.8: /s/server.<KELVIN SIGN>ey is the fixed 404"))
	}
}

type siteServe struct{ common.HTTP }

func (c siteServe) NewController() Controller { return c }

func (c siteServe) SERVE_site_BY_lang() string { return secServeRoot + "/dir_index" }

func TestServe_SlashRedirectSameOrigin(t *testing.T) {
	env := secPrepare(t)
	app := New(&AppOptions{Logger: &lcRecordingLogger{}, HTTP: &HTTPOptions{ServeDirs: []string{env.serve}}})
	mustCreate(t, app, NewModuleBuilder().Controllers(siteServe{}).Build())
	hostile := []string{"/site/%5C%5Cevil.test", "/site/%5Cevil.test", "/site/%2F%2Fevil.test", "/site/%2Fevil.test", "/site/@evil.test", "/site/%5C%2Fevil.test", "/site/%5c%5cevil.test"}
	for _, target := range hostile {
		w := createGet(app, target)
		loc := w.Header().Get("Location")
		if w.Code >= 300 && w.Code < 400 {
			if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") || strings.Contains(loc, "\\") {
				t.Error(test.DiffMessage(loc, "an absolute path on the same site", "H3: Location of "+target))
			}
		} else if loc != "" {
			t.Error(test.DiffMessage(loc, "", "H3: no Location on a "+strconv.Itoa(w.Code)+" for "+target))
		}
	}
	w := createGet(app, "/site/en?a=1")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/site/en/?a=1" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusMovedPermanently, "/site/en/?a=1"}, "H3: the target is the absolute canonical path plus the query"))
	}
}
