package core

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/internal/test"
	"golang.org/x/net/websocket"
)

func TestLoadServerOptions_NilEqualsZero(t *testing.T) {
	fromNil := loadServerOptions(nil)
	fromZero := loadServerOptions(&ServerOptions{})

	if fromNil != fromZero {
		t.Error(test.DiffMessage(fromNil, fromZero, "O1: nil options equal an empty ServerOptions"))
	}
	if fromNil.TLSConfig != nil || fromNil.IsH2C {
		t.Error(test.DiffMessage(fromNil, "no TLSConfig, no H2C", "O1: nil options enable nothing"))
	}
}

func TestLoadServerOptions_ShutdownTimeoutDefault(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{0, 30 * time.Second},
		{-time.Second, 30 * time.Second},
		{5 * time.Second, 5 * time.Second},
	}
	for _, c := range cases {
		got := loadServerOptions(&ServerOptions{ShutdownTimeout: c.in}).ShutdownTimeout
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, fmt.Sprintf("O2: ShutdownTimeout %v", c.in)))
		}
	}
}

func TestLoadServerOptions_BodyCapDefaultNegativePositive(t *testing.T) {
	if got := loadServerOptions(&ServerOptions{}).MaxRequestBodyBytes; got != 10<<20 {
		t.Error(test.DiffMessage(got, int64(10<<20), "O3: zero means 10 MiB"))
	}
	if got := loadServerOptions(&ServerOptions{MaxRequestBodyBytes: -1}).MaxRequestBodyBytes; got >= 0 {
		t.Error(test.DiffMessage(got, "a negative value", "O3: negative means no cap"))
	}
	if got := loadServerOptions(&ServerOptions{MaxRequestBodyBytes: 123}).MaxRequestBodyBytes; got != 123 {
		t.Error(test.DiffMessage(got, int64(123), "O3: a positive value is kept"))
	}
}

func TestServerDefaults_Values(t *testing.T) {
	app := New()
	want := serverDefaults{
		readHeaderTimeout: 5 * time.Second,
		readTimeout:       30 * time.Second,
		writeTimeout:      30 * time.Second,
		idleTimeout:       75 * time.Second,
		maxHeaderBytes:    64 << 10,
	}
	if app.serverDefaults != want {
		t.Error(test.DiffMessage(app.serverDefaults, want, "T1: a new app carries the table's values"))
	}
}

func TestNewServer_AppliesDefaults(t *testing.T) {
	app := New()
	app.serverDefaults = serverDefaults{
		readHeaderTimeout: 1 * time.Second,
		readTimeout:       2 * time.Second,
		writeTimeout:      3 * time.Second,
		idleTimeout:       4 * time.Second,
		maxHeaderBytes:    5000,
	}
	srv := app.newServer(loadServerOptions(nil), false)

	got := []any{srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout, srv.MaxHeaderBytes}
	want := []any{1 * time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 5000}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Error(test.DiffMessage(got, want, "T1: the server takes the app's serverDefaults"))
	}
	if srv.Handler != http.Handler(app) {
		t.Error(test.DiffMessage(srv.Handler, app, "S1: the handler is the app"))
	}
	if srv.ErrorLog == nil {
		t.Error(test.DiffMessage(nil, "a logger", "S6: the server's error output goes to the app's logger"))
	}
}

func TestListen_TLSConfigIsAnError(t *testing.T) {
	h := newSrv(t)
	port := srvHeldPort(t)

	err := h.app.Listen(port, &ServerOptions{TLSConfig: &tls.Config{}})

	want := "ginject: TLSConfig is set: use ListenTLS, or Serve with your listener"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "O4: Listen refuses a TLSConfig"))
	}
}

func TestListenTLS_H2CIsAnError(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	port := srvHeldPort(t)

	err := h.app.ListenTLS(port, "", "", &ServerOptions{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}},
		IsH2C:     true,
	})

	want := "ginject: IsH2C applies to connections without TLS"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "O5: ListenTLS refuses H2C"))
	}
}

func TestServe_TLSConfigWithH2CIsAnError(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)

	err := h.app.Serve(h.ln, &ServerOptions{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}},
		IsH2C:     true,
	})

	want := "ginject: IsH2C applies to connections without TLS"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "O5: Serve refuses TLSConfig with H2C"))
	}
	if !srvListenerOpen(h.ln) {
		t.Error(test.DiffMessage("closed", "open", "S2: the listener is left open"))
	}
}

func TestServe_OptionsReadOnce(t *testing.T) {
	h := newSrv(t)
	opts := &ServerOptions{MaxRequestBodyBytes: 64}
	h.start(opts)
	opts.MaxRequestBodyBytes = 1 << 20

	big := `{"k":"` + strings.Repeat("a", 4096) + `"}`
	r := srvFetch(srvClient(), http.MethodPost, "http://"+h.addr()+"/upload", big, false)

	if r.err != nil || r.status != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusRequestEntityTooLarge, "O6: changing the options after the call starts has no effect"))
	}
}

func TestServe_ShutdownTimeoutReadOnce(t *testing.T) {
	h := newSrv(t)
	opts := &ServerOptions{ShutdownTimeout: 50 * time.Millisecond}
	h.start(opts)
	opts.ShutdownTimeout = time.Hour

	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	h.sigs <- syscall.SIGTERM

	err := h.wait()
	if err == nil || err.Error() != "ginject: shutdown timed out after 50ms" {
		t.Error(test.DiffMessage(err, "ginject: shutdown timed out after 50ms", "O6: the timeout is the one read at the start"))
	}
	<-res
}

func TestServe_TLSConfigNotModified(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	cfg := &tls.Config{Certificates: []tls.Certificate{certs.cert}}
	h.start(&ServerOptions{TLSConfig: cfg})

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://"+h.addr()+"/ok", "", false)
	h.app.Stop()
	_ = h.wait()

	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(cfg.NextProtos) != 0 {
		t.Error(test.DiffMessage(cfg.NextProtos, []string(nil), "O6: the user's TLSConfig is cloned, never modified"))
	}
}

func TestServe_NegativeBodyCapDisablesCap(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{MaxRequestBodyBytes: -1})

	big := `{"k":"` + strings.Repeat("a", 1<<20) + `"}`
	r := srvFetch(srvClient(), http.MethodPost, "http://"+h.addr()+"/upload", big, false)

	if r.err != nil || r.status != http.StatusCreated {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusCreated, "O3: a negative cap serves large bodies"))
	}
}

func TestServe_AnswersOverTheListener(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	r := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/ok", "", false)

	if r.err != nil || r.status != http.StatusOK || !strings.Contains(r.body, "ok") {
		t.Error(test.DiffMessage([]any{r.status, r.body, r.err}, "200 ok", "S1: Serve answers on the given listener"))
	}
}

func TestListen_BeforeCreateReturnsError(t *testing.T) {
	app := New()
	port := srvFreePort(t)

	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Error(test.DiffMessage(rec, "an error", "S2/bug 5: Listen before Create must not panic"))
			}
		}()
		err = app.Listen(port, nil)
	}()

	want := "ginject: Create must be called before serving"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "S2: Listen before Create"))
	}
}

func TestServe_BeforeCreateReturnsError(t *testing.T) {
	log := &srvLogger{}
	app := New()
	app.SetLogger(log)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	got := app.Serve(ln, nil)

	want := "ginject: Create must be called before serving"
	if got == nil || got.Error() != want {
		t.Error(test.DiffMessage(got, want, "S2: Serve before Create"))
	}
	if !srvListenerOpen(ln) {
		t.Error(test.DiffMessage("closed", "open", "S2: the listener is left open"))
	}
	if n := len(log.all()); n != 0 {
		t.Error(test.DiffMessage(n, 0, "S2: nothing is logged"))
	}
}

func TestServe_SecondCallReturnsError(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	before := len(h.log.all())

	got := h.app.Serve(second, nil)

	want := "ginject: app is already serving"
	if got == nil || got.Error() != want {
		t.Error(test.DiffMessage(got, want, "S2: a second serving call"))
	}
	if !srvListenerOpen(second) {
		t.Error(test.DiffMessage("closed", "open", "S2: the second listener is left open"))
	}
	if n := len(h.log.all()); n != before {
		t.Error(test.DiffMessage(n, before, "S2: nothing is logged"))
	}
}

func TestServe_AfterStopReturnsError(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	h.app.Stop()
	if err := h.wait(); err != nil {
		t.Fatal(err)
	}
	again, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	before := len(h.log.all())

	got := h.app.Serve(again, nil)

	want := "ginject: app is stopped"
	if got == nil || got.Error() != want {
		t.Error(test.DiffMessage(got, want, "S2/G9: a serving call after the app stopped"))
	}
	if !srvListenerOpen(again) {
		t.Error(test.DiffMessage("closed", "open", "S2: the listener is left open"))
	}
	if n := len(h.log.all()); n != before {
		t.Error(test.DiffMessage(n, before, "S2: nothing is logged"))
	}
}

func TestServe_ErrorLeavesListenerOpen(t *testing.T) {
	h := newSrv(t)
	err := h.app.Serve(h.ln, &ServerOptions{TLSConfig: &tls.Config{}})

	want := "ginject: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "S5: a TLSConfig without certificates"))
	}
	if !srvListenerOpen(h.ln) {
		t.Error(test.DiffMessage("closed", "open", "S2: the listener is left open"))
	}
}

func TestListen_PortInUseReturnsBeforeLogging(t *testing.T) {
	h := newSrv(t)
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port
	before := len(h.log.all())

	var got error
	out := srvCaptureStdout(t, func() { got = h.app.Listen(port, nil) })

	prefix := fmt.Sprintf("ginject: listen on :%d: ", port)
	if got == nil || !strings.HasPrefix(got.Error(), prefix) {
		t.Fatal(test.DiffMessage(got, prefix+"<err>", "S3: the bind error is wrapped"))
	}
	var opErr *net.OpError
	if !errors.As(got, &opErr) {
		t.Error(test.DiffMessage(got, "a wrapped *net.OpError", "S3: net.Listen's error is wrapped with %w"))
	}
	if n := len(h.log.all()); n != before {
		t.Error(test.DiffMessage(n, before, "bug 4, S3: no startup line is logged"))
	}
	if out != "" {
		t.Error(test.DiffMessage(out, "", "bug 4, S3: no banner is printed"))
	}
	if names := srvCur.hookNames(); len(names) != 0 {
		t.Error(test.DiffMessage(names, []string(nil), "S3: no hook runs"))
	}

	h.start(nil)
	r := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/ok", "", false)
	if r.err != nil || r.status != http.StatusOK {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusOK, "S3: the app can be served again"))
	}
}

func TestListen_PortOutOfRangeReturnsError(t *testing.T) {
	h := newSrv(t)
	before := len(h.log.all())

	got := h.app.Listen(70000, nil)

	if got == nil || !strings.HasPrefix(got.Error(), "ginject: listen on :70000: ") {
		t.Error(test.DiffMessage(got, "ginject: listen on :70000: <err>", "S3: an invalid port"))
	}
	if n := len(h.log.all()); n != before {
		t.Error(test.DiffMessage(n, before, "S3: nothing is logged"))
	}
}

func TestListen_LogsBoundAddress(t *testing.T) {
	h := newSrv(t)
	h.startWith(func() error { return h.app.Listen(0, nil) })

	entries := h.log.named("ServerListening")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "S4: one ServerListening line"))
	}
	address, _ := entries[0].field("address").(string)
	_, port, err := net.SplitHostPort(address)
	if err != nil || port == "" || port == "0" {
		t.Fatal(test.DiffMessage(address, "the bound host:port, not :0", "S4: Listen(0, nil) logs the port the system chose"))
	}
	r := srvFetch(srvClient(), http.MethodGet, "http://127.0.0.1:"+port+"/ok", "", false)
	if r.err != nil || r.status != http.StatusOK {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusOK, "S1: Listen serves on every interface"))
	}
}

func TestListen_NoBannerWithoutTerminal(t *testing.T) {
	h := newSrv(t)

	out := srvCaptureStdout(t, func() {
		h.sigs <- syscall.SIGTERM
		if err := h.app.Serve(h.ln, nil); err != nil {
			t.Error(err)
		}
	})

	if strings.Contains(out, "GG!") || strings.Contains(out, "\x1b") {
		t.Error(test.DiffMessage(out, "no banner, no ANSI escape", "bug 6, S4/C17: stdout is not a terminal"))
	}
}

func TestListen_LogsStartupInOrder(t *testing.T) {
	h := newSrv(t)
	h.sigs <- syscall.SIGTERM
	if err := h.app.Serve(h.ln, nil); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, e := range h.log.all() {
		switch e.msg {
		case "InstanceLoader", "RouteExplorer", "ServerListening":
			names = append(names, e.msg)
		}
	}
	if len(names) < 3 || names[len(names)-1] != "ServerListening" || names[0] != "InstanceLoader" {
		t.Fatal(test.DiffMessage(names, "InstanceLoader…, RouteExplorer…, ServerListening", "S4: startup lines then ServerListening"))
	}
	seenRoute := false
	for _, n := range names {
		if n == "RouteExplorer" {
			seenRoute = true
		}
		if n == "InstanceLoader" && seenRoute {
			t.Error(test.DiffMessage(names, "InstanceLoader before RouteExplorer", "S4: order of the startup lines"))
		}
	}
	if got := len(h.log.named("RouteExplorer")); got != len(h.app.http.routes) {
		t.Error(test.DiffMessage(got, len(h.app.http.routes), "S4: one RouteExplorer line per route"))
	}
}

func TestServe_LogsProtocolsPlain(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	e := h.log.named("ServerListening")[0]
	got := []any{e.field("tls"), e.field("protocols")}
	want := []any{false, "HTTP/1.1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Error(test.DiffMessage(got, want, "S4: plain server"))
	}
	if addr, _ := e.field("address").(string); addr != h.addr() {
		t.Error(test.DiffMessage(addr, h.addr(), "S4: the address is the listener's"))
	}
}

func TestServe_LogsProtocolsTLS(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})

	e := h.log.named("ServerListening")[0]
	got := []any{e.field("tls"), e.field("protocols")}
	want := []any{true, "HTTP/1.1, HTTP/2"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Error(test.DiffMessage(got, want, "S4: TLS server"))
	}
}

func TestServe_LogsProtocolsH2C(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{IsH2C: true})

	e := h.log.named("ServerListening")[0]
	got := []any{e.field("tls"), e.field("protocols")}
	want := []any{false, "HTTP/1.1, h2c"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Error(test.DiffMessage(got, want, "S4: h2c server"))
	}
}

func TestListenTLS_BadCertificateFile(t *testing.T) {
	h := newSrv(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := srvHeldPort(t)
	before := len(h.log.all())

	err := h.app.ListenTLS(port, certFile, keyFile, nil)

	if err == nil || !strings.HasPrefix(err.Error(), "ginject: load TLS certificate: ") {
		t.Error(test.DiffMessage(err, "ginject: load TLS certificate: <err>", "S5: garbage in the pair"))
	}
	missing := h.app.ListenTLS(port, filepath.Join(dir, "none.pem"), keyFile, nil)
	if missing == nil || !strings.HasPrefix(missing.Error(), "ginject: load TLS certificate: ") {
		t.Error(test.DiffMessage(missing, "ginject: load TLS certificate: <err>", "S5: a missing file"))
	}
	if n := len(h.log.all()); n != before {
		t.Error(test.DiffMessage(n, before, "S5: nothing is logged"))
	}
}

func TestListenTLS_NoCertificateSource(t *testing.T) {
	h := newSrv(t)
	port := srvHeldPort(t)
	want := "ginject: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates"

	for name, opts := range map[string]*ServerOptions{
		"nil options":       nil,
		"empty TLSConfig":   {TLSConfig: &tls.Config{}},
		"empty options set": {},
	} {
		err := h.app.ListenTLS(port, "", "", opts)
		if err == nil || err.Error() != want {
			t.Error(test.DiffMessage(err, want, "C15, S5: "+name))
		}
	}
}

func TestListenTLS_OneEmptyFileName(t *testing.T) {
	h := newSrv(t)
	port := srvHeldPort(t)
	want := "ginject: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates"

	if err := h.app.ListenTLS(port, "cert.pem", "", nil); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "S5: key name empty"))
	}
	if err := h.app.ListenTLS(port, "", "key.pem", nil); err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "S5: cert name empty"))
	}
}

func TestServe_TLSConfigWithoutCertificatesIsAnError(t *testing.T) {
	h := newSrv(t)

	err := h.app.Serve(h.ln, &ServerOptions{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}})

	want := "ginject: ListenTLS needs certFile and keyFile, or a TLSConfig with certificates"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "C15, S5: Serve never falls back to plaintext"))
	}
	if !srvListenerOpen(h.ln) {
		t.Error(test.DiffMessage("closed", "open", "S2: the listener is left open"))
	}
}

func TestListenTLS_TLSConfigCertificates(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	port := srvFreePort(t)
	h.startWith(func() error {
		return h.app.ListenTLS(port, "", "", &ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})
	})

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(port)+"/ok", "", false)

	if r.err != nil || r.status != http.StatusOK {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusOK, "S5: certificates from the TLSConfig"))
	}
}

func TestListenTLS_GetCertificateIsACertificateSource(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	port := srvFreePort(t)
	h.startWith(func() error {
		return h.app.ListenTLS(port, "", "", &ServerOptions{TLSConfig: &tls.Config{
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return &certs.cert, nil },
		}})
	})

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(port)+"/ok", "", false)

	if r.err != nil || r.status != http.StatusOK {
		t.Error(test.DiffMessage([]any{r.status, r.err}, http.StatusOK, "S5: GetCertificate counts as a certificate source"))
	}
}

func TestListenTLS_CertificateFiles(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certs.certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, certs.keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	port := srvFreePort(t)
	h.startWith(func() error { return h.app.ListenTLS(port, certFile, keyFile, nil) })

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(port)+"/proto", "", false)

	if r.err != nil || r.status != http.StatusOK || !strings.Contains(r.body, "HTTP/2.0") {
		t.Error(test.DiffMessage([]any{r.status, r.body, r.err}, "200 HTTP/2.0", "S1, S5: ListenTLS with a file pair"))
	}
}

func TestServe_ServerErrorsGoToLogger(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})

	c := srvDial(t, h.addr())
	_, _ = c.Write([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07})
	srvWaitCh(t, h.log.seen("HTTPServer"), "the HTTPServer warning")

	e := h.log.named("HTTPServer")[0]
	msg, _ := e.field("message").(string)
	if e.level != "warn" || msg == "" || strings.HasSuffix(msg, "\n") || !strings.Contains(msg, "TLS handshake error") {
		t.Error(test.DiffMessage([]any{e.level, msg}, "warn, a TLS handshake error line without a newline", "bug 7, S6: server errors reach the app's logger"))
	}
}

func TestProtocols_PlainIsHTTP1(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	r := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/proto", "", false)

	if r.err != nil || !strings.Contains(r.body, "HTTP/1.1") {
		t.Error(test.DiffMessage([]any{r.body, r.err}, "HTTP/1.1", "H1: plain serves HTTP/1.1"))
	}
}

func TestProtocols_PlainRefusesPriorKnowledgeHTTP2(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	r := srvFetch(srvH2CClient(), http.MethodGet, "http://"+h.addr()+"/proto", "", false)

	if r.err == nil && strings.Contains(r.body, "HTTP/2.0") {
		t.Error(test.DiffMessage(r.body, "not HTTP/2.0", "H1: a prior-knowledge client is not served as HTTP/2"))
	}
}

func TestProtocols_TLSNegotiatesHTTP2(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://"+h.addr()+"/proto", "", false)

	if r.err != nil || r.proto != "HTTP/2.0" || !strings.Contains(r.body, "HTTP/2.0") {
		t.Error(test.DiffMessage([]any{r.proto, r.body, r.err}, "HTTP/2.0", "H2: ALPN picks HTTP/2"))
	}
}

func TestProtocols_TLSServesHTTP1Client(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})

	r := srvFetch(srvTLSClient(certs.pool, false), http.MethodGet, "https://"+h.addr()+"/proto", "", false)

	if r.err != nil || r.proto != "HTTP/1.1" || !strings.Contains(r.body, "HTTP/1.1") {
		t.Error(test.DiffMessage([]any{r.proto, r.body, r.err}, "HTTP/1.1", "H2: an HTTP/1.1 client is served"))
	}
}

func TestProtocols_TLSWithoutH2InNextProtosTurnsHTTP2Off(t *testing.T) {
	h := newSrv(t)
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{
		Certificates: []tls.Certificate{certs.cert},
		NextProtos:   []string{"http/1.1"},
	}})

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://"+h.addr()+"/proto", "", false)

	if r.err != nil || r.proto != "HTTP/1.1" {
		t.Error(test.DiffMessage([]any{r.proto, r.err}, "HTTP/1.1", "H2: a NextProtos without h2 turns HTTP/2 off"))
	}
}

func TestProtocols_H2CServesBothOnOnePort(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{IsH2C: true})

	h2 := srvFetch(srvH2CClient(), http.MethodGet, "http://"+h.addr()+"/proto", "", false)
	h1 := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/proto", "", false)

	if h2.err != nil || !strings.Contains(h2.body, "HTTP/2.0") {
		t.Error(test.DiffMessage([]any{h2.body, h2.err}, "HTTP/2.0", "H3: an h2c client is served as HTTP/2"))
	}
	if h1.err != nil || !strings.Contains(h1.body, "HTTP/1.1") {
		t.Error(test.DiffMessage([]any{h1.body, h1.err}, "HTTP/1.1", "H3: other clients get HTTP/1.1"))
	}
}

func TestProtocols_H2CUpgradeRequestAnsweredAsHTTP1(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{IsH2C: true})
	c := srvDial(t, h.addr())

	_, err := c.Write([]byte("GET /proto HTTP/1.1\r\nHost: x\r\nConnection: Upgrade, HTTP2-Settings\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABkAAQCAAAAAAIAAAAA\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK || !strings.Contains(string(b), "HTTP/1.1") {
		t.Error(test.DiffMessage([]any{res.StatusCode, string(b)}, "200 HTTP/1.1", "H3: the Upgrade: h2c form is answered as HTTP/1.1"))
	}
}

func TestProtocols_WebSocketOverTLS(t *testing.T) {
	h := newSrv(t, func(app *App) { app.EnableWS(&WSOptions{Path: "/ws"}) })
	certs := srvMakeCert(t)
	h.start(&ServerOptions{TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs.cert}}})

	cfg, err := websocket.NewConfig("wss://"+h.addr()+"/ws", "https://"+h.addr())
	if err != nil {
		t.Fatal(err)
	}
	cfg.TlsConfig = &tls.Config{RootCAs: certs.pool}
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(test.DiffMessage(err, nil, "H4: wss:// works on a TLS server"))
	}
	_ = conn.Close()

	r := srvFetch(srvTLSClient(certs.pool, true), http.MethodGet, "https://"+h.addr()+"/ws", "", false)
	if r.err == nil && r.status == http.StatusSwitchingProtocols {
		t.Error(test.DiffMessage(r.status, "not 101", "H4: an HTTP/2 connection is never upgraded"))
	}
}

func TestTimeouts_SlowHeaders(t *testing.T) {
	h := newSrv(t, func(app *App) { app.serverDefaults.readHeaderTimeout = srvShort })
	h.start(nil)
	c := srvDial(t, h.addr())

	_, _ = c.Write([]byte("GET /ok HTTP/1.1\r\nHost: x\r\n"))

	if !srvClosedByServer(c) {
		t.Error(test.DiffMessage("kept open", "closed", "C1, T1: a client that never finishes its headers is disconnected"))
	}
}

func TestTimeouts_SlowBody(t *testing.T) {
	h := newSrv(t, func(app *App) { app.serverDefaults.readTimeout = srvShort })
	h.start(nil)
	c := srvDial(t, h.addr())

	_, _ = c.Write([]byte("POST /upload HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"k\":"))

	if !srvClosedByServer(c) {
		t.Error(test.DiffMessage("kept open", "closed", "C2, T1: a client that stalls in the body is disconnected"))
	}
}

func TestTimeouts_SlowReader(t *testing.T) {
	h := newSrv(t, func(app *App) { app.serverDefaults.writeTimeout = srvShort })
	h.start(nil)
	c := srvDial(t, h.addr())

	_, _ = c.Write([]byte("GET /flood HTTP/1.1\r\nHost: x\r\n\r\n"))

	select {
	case err := <-srvCur.writeErr:
		if err == nil {
			t.Error(test.DiffMessage(nil, "a write error", "C3, T1: a client that does not read is cut after WriteTimeout"))
		}
	case <-time.After(srvWait):
		t.Fatal("the server never gave up writing to a client that does not read")
	}
}

func TestTimeouts_IdleConnection(t *testing.T) {
	h := newSrv(t, func(app *App) { app.serverDefaults.idleTimeout = srvShort })
	h.start(nil)
	c := srvDial(t, h.addr())
	br := bufio.NewReader(c)

	_, _ = c.Write([]byte("GET /ok HTTP/1.1\r\nHost: x\r\n\r\n"))
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	if !srvClosedByServer(c) {
		t.Error(test.DiffMessage("kept open", "closed", "C4, T1: an idle keep-alive connection is closed after IdleTimeout"))
	}
}

func TestTimeouts_HeadersOverLimit431(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	c := srvDial(t, h.addr())

	_, _ = c.Write([]byte("GET /ok HTTP/1.1\r\nHost: x\r\nX-Big: " + strings.Repeat("a", 80<<10) + "\r\n\r\n"))
	res, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(test.DiffMessage(err, "a 431 response", "C5, T1: headers over 64 KiB"))
	}
	_ = res.Body.Close()

	if res.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Error(test.DiffMessage(res.StatusCode, http.StatusRequestHeaderFieldsTooLarge, "C5: headers over the limit answer 431"))
	}
}

func TestTimeouts_BodyOverCap413(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{MaxRequestBodyBytes: 64})
	big := `{"k":"` + strings.Repeat("a", 4096) + `"}`

	declared := srvFetch(srvClient(), http.MethodPost, "http://"+h.addr()+"/upload", big, false)
	chunked := srvFetch(srvClient(), http.MethodPost, "http://"+h.addr()+"/upload", big, true)

	if declared.err != nil || declared.status != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage([]any{declared.status, declared.err}, http.StatusRequestEntityTooLarge, "C6: a declared body over the cap"))
	}
	if chunked.err != nil || chunked.status != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage([]any{chunked.status, chunked.err}, http.StatusRequestEntityTooLarge, "C6: a chunked body over the cap"))
	}
}

func TestTimeouts_HandlerExtendsWriteDeadline(t *testing.T) {
	h := newSrv(t, func(app *App) { app.serverDefaults.writeTimeout = 50 * time.Millisecond })
	h.start(nil)

	extended := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/stream", "", false)
	plain := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/nostream", "", false)

	if extended.err != nil || !strings.Contains(extended.body, "late") {
		t.Error(test.DiffMessage([]any{extended.body, extended.err}, "late", "C7, T2: a handler streams past WriteTimeout by extending its deadline"))
	}
	if plain.err == nil && strings.Contains(plain.body, "late") {
		t.Error(test.DiffMessage(plain.body, "a cut connection", "T1: without the extension the same handler is cut"))
	}
}

func TestShutdown_SignalDrainsRunningRequest(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")

	h.sigs <- syscall.SIGTERM
	srvWaitRefused(t, h.addr())
	srvCur.open()
	r := srvAwait(t, res)
	err := h.wait()

	if r.err != nil || r.status != http.StatusOK || !strings.Contains(r.body, "finished") {
		t.Error(test.DiffMessage([]any{r.status, r.body, r.err}, "200 finished", "bug 1, C8, G3: a running request finishes normally"))
	}
	if err != nil {
		t.Error(test.DiffMessage(err, nil, "G8: nil after a drained shutdown"))
	}
	entries := h.log.named("ShutdownSignal")
	if len(entries) != 1 || entries[0].field("signal") != syscall.SIGTERM.String() {
		t.Error(test.DiffMessage(entries, "ShutdownSignal signal="+syscall.SIGTERM.String(), "G1: the signal is logged"))
	}
}

func TestShutdown_StopDrainsRunningRequest(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")

	stopped := make(chan struct{})
	go func() {
		h.app.Stop()
		close(stopped)
	}()
	srvWaitRefused(t, h.addr())
	srvCur.open()
	r := srvAwait(t, res)
	srvWaitCh(t, stopped, "Stop")
	err := h.wait()

	if r.err != nil || r.status != http.StatusOK || !strings.Contains(r.body, "finished") {
		t.Error(test.DiffMessage([]any{r.status, r.body, r.err}, "200 finished", "bug 2, G6: Stop drains a running request"))
	}
	if err != nil {
		t.Error(test.DiffMessage(err, nil, "G8: nil after Stop"))
	}
	if n := len(h.log.named("Shutdown")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "G1: Stop logs Shutdown"))
	}
}

func TestShutdown_RefusesNewConnections(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")

	h.sigs <- syscall.SIGTERM
	srvWaitRefused(t, h.addr())

	srvCur.open()
	srvAwait(t, res)
	_ = h.wait()
}

func TestShutdown_ClosesIdleConnections(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	c := srvDial(t, h.addr())
	_, _ = c.Write([]byte("GET /ok HTTP/1.1\r\nHost: x\r\n\r\n"))
	res, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	h.sigs <- syscall.SIGTERM

	if !srvClosedByServer(c) {
		t.Error(test.DiffMessage("kept open", "closed", "G2: idle keep-alive connections are closed at once"))
	}
	_ = h.wait()
}

func TestShutdown_HooksAfterLastRequest(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")

	h.sigs <- syscall.SIGTERM
	srvWaitRefused(t, h.addr())

	if names := srvCur.hookNames(); len(names) != 0 {
		t.Error(test.DiffMessage(names, []string(nil), "bug 1, G3: no hook runs while a request is running"))
	}
	srvCur.open()
	srvAwait(t, res)
	if err := h.wait(); err != nil {
		t.Fatal(err)
	}

	names := srvCur.hookNames()
	if fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "C10, G7: hooks run once each, in the reverse of initialization"))
	}
	if !srvCur.rootSawDone.Load() {
		t.Error(test.DiffMessage(false, true, "C10, G3: the hooks run after the last request returned"))
	}
}

func TestShutdown_TimeoutCutsRequestAndReports(t *testing.T) {
	h := newSrv(t)
	h.start(&ServerOptions{ShutdownTimeout: 50 * time.Millisecond})
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")

	h.sigs <- syscall.SIGTERM
	err := h.wait()

	want := "ginject: shutdown timed out after 50ms"
	if err == nil || err.Error() != want {
		t.Error(test.DiffMessage(err, want, "C11, G4: the error names the timeout"))
	}
	srvWaitCh(t, srvCur.cancelled, "the request's context to be cancelled")
	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G4: the hooks still ran"))
	}
	if r := srvAwait(t, res); r.err == nil && r.status == http.StatusOK {
		t.Error(test.DiffMessage(r.status, "a cut connection", "G4: the connection of the request is closed"))
	}
}

func TestShutdown_StopsInterceptingSignals(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	res := srvFetchAsync(srvClient(), "http://"+h.addr()+"/block")
	srvWaitCh(t, srvCur.entered, "the handler")
	if got := h.stops.Load(); got != 0 {
		t.Fatal(test.DiffMessage(got, int32(0), "G5: signals are intercepted until the first one arrives"))
	}

	h.sigs <- syscall.SIGINT
	srvWaitRefused(t, h.addr())

	if got := h.stops.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int32(1), "bug 3, G5: interception stops before the drain, so a second signal kills the process"))
	}
	srvCur.open()
	srvAwait(t, res)
	_ = h.wait()
	if got := h.regs.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int32(1), "G1: signals are registered once through the seam"))
	}
}

func TestShutdown_SignalRegistrationReleasedOnStop(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	h.app.Stop()
	_ = h.wait()

	if regs, stops := h.regs.Load(), h.stops.Load(); regs != 1 || stops != regs {
		t.Error(test.DiffMessage([]int32{regs, stops}, []int32{1, 1}, "Concurrency: the signal registration is released after Stop"))
	}
}

func TestShutdown_ListenerFailureRunsHooks(t *testing.T) {
	h := newSrv(t)
	h.started = true
	failing := srvFailingListener{Listener: h.ln, err: errors.New("boom")}
	go func() { h.done <- h.app.Serve(failing, nil) }()

	err := h.wait()

	if err == nil || err.Error() != "ginject: serve: boom" {
		t.Error(test.DiffMessage(err, "ginject: serve: boom", "G8: the listener failure is returned"))
	}
	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G8: the hooks run when the server stops by itself"))
	}
	if regs, stops := h.regs.Load(), h.stops.Load(); stops != regs {
		t.Error(test.DiffMessage([]int32{regs, stops}, "equal", "Concurrency: the signal registration is released on every path"))
	}
}

func TestShutdown_ReturnsNilWhenDrained(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	h.sigs <- syscall.SIGTERM

	if err := h.wait(); err != nil {
		t.Error(test.DiffMessage(err, nil, "G8: nil after a shutdown that drained in time"))
	}
}

func TestShutdown_HookPanicDoesNotStopOthers(t *testing.T) {
	h := newSrv(t)
	srvCur.rootExtra = func() { panic("boom") }

	h.app.Stop()

	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G7: the remaining hooks still run"))
	}
	entries := h.log.named("OnShutdownPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "G7: the panic is logged once"))
	}
	if entries[0].level != "error" || entries[0].field("panic") != "boom" || entries[0].field("module") != h.root.Name {
		t.Error(test.DiffMessage(entries[0].args, []any{"module", h.root.Name, "panic", "boom"}, "G7: the log line names the module and the value"))
	}
}

func TestShutdown_AppCannotServeAgain(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	h.sigs <- syscall.SIGTERM
	if err := h.wait(); err != nil {
		t.Fatal(err)
	}
	again, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()

	got := h.app.Serve(again, nil)

	if got == nil || got.Error() != "ginject: app is stopped" {
		t.Error(test.DiffMessage(got, "ginject: app is stopped", "G9: the app cannot serve again"))
	}
	w := httptest.NewRecorder()
	h.app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "G9: ServeHTTP still answers"))
	}
}

func TestStop_WithoutServerRunsHooksOnce(t *testing.T) {
	h := newSrv(t)

	h.app.Stop()
	h.app.Stop()

	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G6: an app that is not serving runs the hooks once"))
	}
}

func TestStop_ConcurrentCallsRunHooksOnce(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.app.Stop()
		}()
	}
	wg.Wait()
	err := h.wait()

	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G6: 16 concurrent Stop calls run the hooks once"))
	}
	if err != nil {
		t.Error(test.DiffMessage(err, nil, "G6: the serving call returns nil"))
	}
}

func TestStop_ConcurrentCallsWithoutServerRunHooksOnce(t *testing.T) {
	h := newSrv(t)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.app.Stop()
		}()
	}
	wg.Wait()

	if names := srvCur.hookNames(); fmt.Sprint(names) != "[root child]" {
		t.Error(test.DiffMessage(names, []string{"root", "child"}, "G6: 16 concurrent Stop calls run the hooks once"))
	}
}

func TestStop_ReturnsAfterHooks(t *testing.T) {
	h := newSrv(t)
	h.start(nil)
	var ran atomic.Bool
	srvCur.mu.Lock()
	srvCur.childExtra = func() { ran.Store(true) }
	srvCur.mu.Unlock()

	h.app.Stop()

	if !ran.Load() {
		t.Error(test.DiffMessage(false, true, "G6: Stop returns when the hooks have finished"))
	}
	_ = h.wait()
}

func TestChecklist_PanicOverRealConnection(t *testing.T) {
	h := newSrv(t)
	h.start(nil)

	r := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/panic", "", false)
	after := srvFetch(srvClient(), http.MethodGet, "http://"+h.addr()+"/ok", "", false)

	if r.err != nil || r.status != http.StatusInternalServerError || strings.TrimSpace(r.body) != strings.TrimSpace(string(internalErrorBody)) {
		t.Error(test.DiffMessage([]any{r.status, r.body, r.err}, string(internalErrorBody), "C16, 0007 E3: the fixed 500 over a real connection"))
	}
	if after.err != nil || after.status != http.StatusOK {
		t.Error(test.DiffMessage([]any{after.status, after.err}, http.StatusOK, "C16: the server keeps serving"))
	}
}

func TestChecklist_NoGoroutineLeak(t *testing.T) {
	h := newSrv(t)
	before := runtime.NumGoroutine()
	h.start(nil)

	c := srvDial(t, h.addr())
	_, _ = c.Write([]byte("GET /ok HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"))
	_, _ = io.Copy(io.Discard, c)
	_ = c.Close()
	h.app.Stop()
	if err := h.wait(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(srvWait)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Error(test.DiffMessage(after, before, "C18: no goroutine of the server outlives the serving call"))
	}
}

func TestServeHTTP_WithoutServerUsesDefaultBodyCap(t *testing.T) {
	app := New()
	app.Create(NewModuleBuilder().Controllers(bodyLimitController{}).Build())

	payload := `{"k":"` + strings.Repeat("a", 10<<20) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(w.Code, http.StatusRequestEntityTooLarge, "A1: ServeHTTP without a serving call caps bodies at 10 MiB"))
	}
}

func TestListen_LogsModuleNameWhenResolved(t *testing.T) {
	logger := &allCallsCapturingLogger{}
	app := New()
	app.SetLogger(logger)
	app.Create(testListenModule())

	serveOnce(t, app)

	call := logger.findCall("InstanceLoader")
	if call == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil", "expected an InstanceLoader log entry"))
	}
	want := []any{"module", "core.testListenModule initialized"}
	if len(call.args) != 2 || call.args[0] != want[0] || call.args[1] != want[1] {
		t.Error(test.DiffMessage(call.args, want, "module log args"))
	}
}

func TestListen_SkipsModuleLogWhenNameUnresolved(t *testing.T) {
	logger := &allCallsCapturingLogger{}
	app := New()
	app.SetLogger(logger)

	m := NewModuleBuilder().Build()
	m.Name = ""
	app.Create(m)

	serveOnce(t, app)

	if call := logger.findCall("InstanceLoader"); call != nil {
		t.Error(test.DiffMessage(call.msg, "<no InstanceLoader log>", "module log should be skipped when Name is unresolved"))
	}
}

func TestListen_LogsNestedModuleNamesRecursively(t *testing.T) {
	logger := &allCallsCapturingLogger{}
	app := New()
	app.SetLogger(logger)
	app.Create(testListenParentModule())

	serveOnce(t, app)

	calls := logger.findAllCalls("InstanceLoader")
	gotNames := make(map[string]bool, len(calls))
	for _, c := range calls {
		gotNames[c.args[1].(string)] = true
	}

	wantNames := []string{
		"core.testListenParentModule initialized",
		"core.testListenChildModule initialized",
	}
	for _, name := range wantNames {
		if !gotNames[name] {
			t.Error(test.DiffMessage(gotNames, name, "expected nested module name to be logged"))
		}
	}
}

func TestGetLocalIP(t *testing.T) {
	ip := getLocalIP()
	if ip != "" {
		if net := reflect.TypeOf(ip).Kind(); net != reflect.String {
			t.Error(test.DiffMessage(net, reflect.String, "getLocalIP should return string"))
		}
	}
}

func TestLogBoostrapNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Error(test.DiffMessage(r, nil, "logBoostrap should not panic"))
		}
	}()
	logBoostrap(8080)
}

func TestHooks_OnShutdownReverseOrder(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	mustCreate(t, app, hkDiamond(rec))

	app.Stop()

	got := sprint(hkFilter(rec.list(), "shutdown:"))
	if want := "[root C A B]"; got != want {
		t.Error(test.DiffMessage(got, want, "K4: OnShutdown runs in the exact reverse of the initialization order"))
	}
	app.Stop()
	if got := len(hkFilter(rec.list(), "shutdown:")); got != 4 {
		t.Error(test.DiffMessage(got, 4, "K4: OnShutdown runs once per app"))
	}
}

func TestHooks_NoShutdownWithoutCreate(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	hkDiamond(rec)

	app.Stop()

	if got := hkFilter(rec.list(), "shutdown:"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "K4: an app that never ran Create runs no OnShutdown"))
	}
}

func TestHooks_NoShutdownWhenCreateDidNotFinish(t *testing.T) {
	rec := &createRecorder{}
	app := New()
	root := hkDiamond(rec)
	root.OnReady = func() { panic("ready boom") }

	_, panicked := createPanicValue(func() { app.Create(root) })
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", "a panic", "K3: the panic stops Create"))
	}
	app.Stop()

	if got := hkFilter(rec.list(), "shutdown:"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "K4: an app whose Create did not finish runs no OnShutdown"))
	}
}

func TestStop_SecondCallerWaitsWhileHooksRun_NotServing(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	m := NewModuleBuilder().Build()
	m.OnShutdown = func() {
		close(entered)
		<-release
		finished.Store(true)
	}
	app := New()
	mustCreate(t, app, m)

	rvStopWaitsForHooks(t, app, entered, release, &finished, "G6/H2")
}

func TestStop_SecondCallerWaitsWhileHooksRun_Serving(t *testing.T) {
	h := newSrv(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	srvCur.mu.Lock()
	srvCur.childExtra = func() {
		close(entered)
		<-release
		finished.Store(true)
	}
	srvCur.mu.Unlock()
	h.start(nil)

	rvStopWaitsForHooks(t, h.app, entered, release, &finished, "G6/H2")
	_ = h.wait()
}

func TestStop_FromInsideShutdownHookReturnsAtOnce(t *testing.T) {
	returned := make(chan struct{})
	app := New()
	m := NewModuleBuilder().Build()
	m.OnShutdown = func() {
		app.Stop()
		close(returned)
	}
	mustCreate(t, app, m)

	go app.Stop()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "Stop returns", "G6: Stop called from inside an OnShutdown hook must not deadlock"))
	}
}

func TestStop_FromInsideShutdownHookReturnsAtOnce_Serving(t *testing.T) {
	h := newSrv(t)
	returned := make(chan struct{})
	srvCur.mu.Lock()
	srvCur.childExtra = func() {
		h.app.Stop()
		close(returned)
	}
	srvCur.mu.Unlock()
	h.start(nil)

	go h.app.Stop()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "Stop returns", "G6: Stop called from inside an OnShutdown hook must not deadlock while serving"))
	}
	_ = h.wait()
}

func TestShutdown_FlushesAfterHooks(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	root := NewModuleBuilder().Build()
	root.OnShutdown = func() { app.Logger().Info("from-shutdown-hook") }
	mustCreate(t, app, root)
	w.hold()
	openWhenEntered(w)
	app.Stop()
	if !strings.Contains(w.String(), "from-shutdown-hook") {
		t.Error(test.DiffMessage(w.String(), "the hook's line written when Stop returns", "M2: the sink is flushed after the OnShutdown hooks"))
	}
}

func TestStop_WithoutServeFlushes(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	w.hold()
	app.Logger().Info("embedded")
	openWhenEntered(w)
	app.Stop()
	if !strings.Contains(w.String(), "embedded") {
		t.Error(test.DiffMessage(w.String(), "the line written when Stop returns", "X4: Stop on an app that never served flushes"))
	}
}

func TestShutdown_LinesAfterStopSynchronous(t *testing.T) {
	w := redirectStdout(t)
	app := New()
	mustCreate(t, app, NewModuleBuilder().Build())
	app.Stop()
	for i := range 20 {
		line := "after-stop-" + strconv.Itoa(i)
		app.Logger().Info(line)
		if !strings.Contains(w.String(), line) {
			t.Fatal(test.DiffMessage(w.String(), line, "M2: lines logged after Stop returned are synchronous"))
		}
	}
}

func TestShutdown_AsyncCountedPerApp(t *testing.T) {
	w := redirectStdout(t)
	first, second := New(), New()
	mustCreate(t, first, NewModuleBuilder().Build())
	mustCreate(t, second, NewModuleBuilder().Build())
	first.Stop()
	w.hold()
	withinCore(t, "M3: still asynchronous while one of two apps runs", func() {
		first.Logger().Info("one-app-left")
	})
	w.openAll()
	second.Stop()
	first.Logger().Info("none-left")
	if !strings.Contains(w.String(), "none-left") || !strings.Contains(w.String(), "one-app-left") {
		t.Error(test.DiffMessage(w.String(), "both lines written", "M3: after both apps shut down the sink is synchronous and nothing is lost"))
	}
}

func TestShutdown_AccessLogLinesFlushedAfterHooks(t *testing.T) {
	rec := &lcRecordingLogger{}
	app := New().SetLogger(rec).EnableAccessLog()
	root := NewModuleBuilder().Controllers(tracePipelineController{}).Build()
	root.OnShutdown = func() {
		app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline?hook=1", nil))
	}
	app.Create(root)

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
	app.Stop()

	var infos []lcLogEntry
	for _, e := range rec.named("/tracepipeline") {
		if e.level == "info" {
			infos = append(infos, e)
		}
	}
	if len(infos) != 2 {
		t.Fatal(test.DiffMessage(len(infos), 2, "A10: the request before Stop and the one made by an OnShutdown hook are both logged once Stop returned"))
	}
	if infos[0].field("code") != 200 || infos[0].field("operation") != "GET" || infos[0].field("transport") != "HTTP" {
		t.Error(test.DiffMessage(infos[0].args, "code 200, GET, HTTP", "A1: the line carries the request"))
	}
}

func TestShutdown_AccessLogStoppedOnce(t *testing.T) {
	rec := &lcRecordingLogger{}
	app := New().SetLogger(rec).EnableAccessLog()
	app.Create(NewModuleBuilder().Controllers(tracePipelineController{}).Build())
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tracepipeline", nil))
	app.Stop()
	app.Stop()
	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "A10: after shutdown the access log no longer listens"))
	}
}

func TestServer_InstanceLoaderOnlyReady(t *testing.T) {
	okm := lfNamed("okm", NewModuleBuilder())
	pendm := lfNamed("pendm", NewModuleBuilder().Providers(lfSQL{}))
	release := lfHold(pendm)
	root := lfNamed("rootm", NewModuleBuilder().Imports(okm).AsyncImports(pendm))
	app := New()
	logger := lfLogger(app)
	lfCreate(t, app, root)
	lfRelease(t, app, release)

	app.logStartup(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, false, "HTTP/1.1")

	var loaded []string
	for _, e := range logger.named("InstanceLoader") {
		loaded = append(loaded, lcText(e.field("module")))
	}
	for _, want := range []string{"okm initialized", "rootm initialized"} {
		if !slices.Contains(loaded, want) {
			t.Error(test.DiffMessage(loaded, want, "V: InstanceLoader lists ready modules"))
		}
	}
	if slices.Contains(loaded, "pendm initialized") {
		t.Error(test.DiffMessage(loaded, "no pendm", "V: InstanceLoader skips a module that is not ready"))
	}
}
