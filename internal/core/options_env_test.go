package core

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/test"
	"go.owlky.com/owlky/internal/versioning"
)

func envSetup(t *testing.T, dotenv string, vars map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "OK_") || name == "PORT" || name == "HTTP_PROXY" {
			value := os.Getenv(name)
			_ = os.Unsetenv(name)
			t.Cleanup(func() { _ = os.Setenv(name, value) })
		}
	}
	dir := t.TempDir()
	if dotenv != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	for name, value := range vars {
		t.Setenv(name, value)
	}
}

func envNewPanic(t *testing.T, opts *AppOptions) string {
	t.Helper()
	opts.Logger = &mockLogger{}
	msg, isPanic := optRecover(func() { New(opts) })
	if !isPanic {
		t.Fatal(test.DiffMessage("no panic", "invalid app: …", "E8: New panics on a recorded error"))
	}
	return msg
}

func envAllVariables() map[string]string {
	return map[string]string{
		"OK_STARTUP_TIMEOUT": "5s", "OK_IS_ACCESS_LOG": "true", "OK_IS_DEVTOOL": "true",
		"OK_HOSTS": "a.com,b.com", "OK_ORIGINS": "https://a.com",
		"OK_SERVER__PORT": "8080", "OK_SERVER__SHUTDOWN_TIMEOUT": "7s", "OK_SERVER__IS_H2C": "true",
		"OK_SERVER__TLS__CERT_FILE": "/c.pem", "OK_SERVER__TLS__KEY_FILE": "/k.pem",
		"OK_HTTP__MAX_BODY_BYTES": "1MiB", "OK_HTTP__SERVE_DIRS": "public,assets",
		"OK_RENDER__DIR": "web", "OK_RENDER__IS_RELOAD": "true", "OK_WS__PATH": "/ws",
		"OK_VERSIONING__TYPE": "header", "OK_VERSIONING__KEY": "v", "OK_VERSIONING__DEFAULT_VERSION": "1",
		"OK_PROXY__TRUSTED_PROXIES": "10.0.0.1", "OK_PROXY__TRUSTED_HOPS": "2", "OK_PROXY__HEADER": "forwarded", "OK_PROXY__IS_HOST_FORWARDED": "true",
		"OK_LOAD_SHED__IS_ENABLED": "true", "OK_LOAD_SHED__MAX_IN_FLIGHT": "5", "OK_LOAD_SHED__MAX_QUEUE": "7",
		"OK_HTTPS_REDIRECT__IS_ENABLED": "true", "OK_HTTPS_REDIRECT__HOST": "a.com", "OK_HTTPS_REDIRECT__PORT": "8443",
		"OK_HEADERS__IS_ENABLED": "true", "OK_HEADERS__IS_DISABLED": "true", "OK_HEADERS__CONTENT_SECURITY_POLICY": "default-src 'self'",
		"OK_HEADERS__CROSS_ORIGIN_EMBEDDER_POLICY": "require-corp", "OK_HEADERS__CROSS_ORIGIN_OPENER_POLICY": "same-origin",
		"OK_HEADERS__CROSS_ORIGIN_RESOURCE_POLICY": "same-site", "OK_HEADERS__FRAME_OPTIONS": "SAMEORIGIN", "OK_HEADERS__PERMISSIONS_POLICY": "camera=()",
		"OK_HEADERS__REFERRER_POLICY": "no-referrer", "OK_HEADERS__STRICT_TRANSPORT_SECURITY": "max-age=1",
		"OK_CORS__IS_ENABLED": "true", "OK_CORS__ALLOW_METHODS": "GET,POST", "OK_CORS__ALLOW_HEADERS": "X-A", "OK_CORS__EXPOSE_HEADERS": "X-B",
		"OK_CORS__MAX_AGE": "1m", "OK_CORS__IS_ALLOW_CREDENTIALS": "true", "OK_CORS__IS_ALLOW_PRIVATE_NETWORK": "true",
		"OK_CSRF__IS_ENABLED": "true", "OK_CSRF__IS_DISABLED": "true", "OK_COMPRESS__IS_ENABLED": "true", "OK_COMPRESS__MIN_SIZE": "2KB",
	}
}

func TestOptionsFromEnv_AllGroupsNonNil(t *testing.T) {
	envSetup(t, "", nil)
	opts := OptionsFromEnv()
	groups := map[string]bool{
		"Server": opts.Server != nil, "HTTP": opts.HTTP != nil, "Render": opts.Render != nil, "WS": opts.WS != nil,
		"Versioning": opts.Versioning != nil, "Proxy": opts.Proxy != nil, "LoadShed": opts.LoadShed != nil,
		"HTTPSRedirect": opts.HTTPSRedirect != nil, "Headers": opts.Headers != nil, "CORS": opts.CORS != nil,
		"CSRF": opts.CSRF != nil, "Compress": opts.Compress != nil,
	}
	if opts.Server != nil {
		groups["Server.TLS"] = opts.Server.TLS != nil
	}
	for name, ok := range groups {
		if !ok {
			t.Error(test.DiffMessage(nil, "a non-nil group", "E1: "+name))
		}
	}
	if msg, isPanic := optRecover(func() { opts.Logger = &mockLogger{}; New(opts) }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E1: an empty environment gives a valid empty configuration"))
	}
}

func TestOptionsFromEnv_NoCallNoRead(t *testing.T) {
	envSetup(t, "OK_BOGUS=1\n", map[string]string{"OK_ALSO_BOGUS": "x", "PORT": "1234"})
	if msg, isPanic := optRecover(func() { New(&AppOptions{Logger: &mockLogger{}}) }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E1: without OptionsFromEnv nothing is read"))
	}
}

func TestOptionsFromEnv_Names(t *testing.T) {
	vars := envAllVariables()
	envSetup(t, "", vars)
	opts := OptionsFromEnv()

	var zero []string
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				zero = append(zero, path)
				return
			}
			walk(v.Elem(), path)
		case reflect.Struct:
			for i := range v.NumField() {
				f := v.Type().Field(i)
				if !f.IsExported() || slices.Contains([]string{"Logger", "Config", "FS", "Extractor"}, f.Name) {
					continue
				}
				child := f.Name
				if path != "" {
					child = path + "." + f.Name
				}
				walk(v.Field(i), child)
			}
		default:
			if v.IsZero() {
				zero = append(zero, path)
			}
		}
	}
	walk(reflect.ValueOf(opts).Elem(), "")
	if len(zero) != 0 {
		t.Error(test.DiffMessage(zero, []string(nil), "E2: every listed variable sets its own field"))
	}
	if len(vars) != 55 {
		t.Log("the golden list has", len(vars), "variables")
	}
}

func TestOptionsFromEnv_EveryNameIsKnown(t *testing.T) {
	for name, value := range envAllVariables() {
		envSetup(t, "", map[string]string{name: value})
		opts := OptionsFromEnv()
		opts.Logger = &mockLogger{}
		msg, isPanic := optRecover(func() { New(opts) })
		if isPanic && (strings.Contains(msg, "unknown variable") || strings.Contains(msg, "environment error")) {
			t.Error(test.DiffMessage(msg, "no environment error", "E2: "+name+" is a known variable"))
		}
	}
}

func TestOptionsFromEnv_CodeOnlyFields(t *testing.T) {
	for _, c := range []struct{ name, path string }{
		{"OK_LOGGER", "Logger"}, {"OK_RENDER__FS", "Render.FS"}, {"OK_SERVER__TLS__CONFIG", "Server.TLS.Config"}, {"OK_VERSIONING__EXTRACTOR", "Versioning.Extractor"},
	} {
		envSetup(t, "", map[string]string{c.name: "x"})
		msg := envNewPanic(t, OptionsFromEnv())
		if !strings.Contains(msg, c.name) || !strings.Contains(msg, c.path+" is set in code only") {
			t.Error(test.DiffMessage(msg, c.name+": "+c.path+" is set in code only", "E3: "+c.name))
		}
	}
}

func TestOptionsFromEnv_PORTFallback(t *testing.T) {
	envSetup(t, "", map[string]string{"PORT": "4000"})
	if got := OptionsFromEnv().Server.Port; got != 4000 {
		t.Error(test.DiffMessage(got, 4000, "E4: PORT fills Server.Port"))
	}
	envSetup(t, "PORT=4100\n", nil)
	if got := OptionsFromEnv().Server.Port; got != 4100 {
		t.Error(test.DiffMessage(got, 4100, "E4/E7: PORT of .env"))
	}
}

func TestOptionsFromEnv_OKServerPortBeatsPORT(t *testing.T) {
	envSetup(t, "", map[string]string{"PORT": "4000", "OK_SERVER__PORT": "4200"})
	opts := OptionsFromEnv()
	if opts.Server.Port != 4200 {
		t.Error(test.DiffMessage(opts.Server.Port, 4200, "E4: OK_SERVER__PORT wins"))
	}
	if msg, isPanic := optRecover(func() { opts.Logger = &mockLogger{}; New(opts) }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E4: both set is not an error"))
	}
	envSetup(t, "PORT=4100\n", map[string]string{"OK_SERVER__PORT": "4200"})
	if got := OptionsFromEnv().Server.Port; got != 4200 {
		t.Error(test.DiffMessage(got, 4200, "E4: OK_SERVER__PORT in one source beats PORT in the other"))
	}
}

func TestOptionsFromEnv_IgnoresHTTPProxy(t *testing.T) {
	envSetup(t, "HTTP_PROXY=http://evil\nDATABASE_URL=x\n", map[string]string{"HTTP_PROXY": "http://evil", "HOME_X": "1"})
	opts := OptionsFromEnv()
	if msg, isPanic := optRecover(func() { opts.Logger = &mockLogger{}; New(opts) }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E4/E7: names without OK_ are never read"))
	}
	if opts.Proxy.TrustedHops != 0 || len(opts.Proxy.TrustedProxies) != 0 {
		t.Error(test.DiffMessage(opts.Proxy, "zero", "E4: HTTP_PROXY is not read"))
	}
}

func TestOptionsFromEnv_ParsesString(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_WS__PATH": "/live"})
	if got := OptionsFromEnv().WS.Path; got != "/live" {
		t.Error(test.DiffMessage(got, "/live", "E5: string as is"))
	}
}

func TestOptionsFromEnv_ParsesInt(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_LOAD_SHED__MAX_QUEUE": "-3", "OK_SERVER__PORT": "8080"})
	opts := OptionsFromEnv()
	if opts.LoadShed.MaxQueue != -3 || opts.Server.Port != 8080 {
		t.Error(test.DiffMessage([]int{opts.LoadShed.MaxQueue, opts.Server.Port}, []int{-3, 8080}, "E5: int by Atoi"))
	}
	envSetup(t, "", map[string]string{"OK_SERVER__PORT": "80x"})
	if msg := envNewPanic(t, OptionsFromEnv()); !strings.Contains(msg, "OK_SERVER__PORT (environment)") {
		t.Error(test.DiffMessage(msg, "OK_SERVER__PORT (environment): …", "E5: a bad int"))
	}
}

func TestOptionsFromEnv_ParsesBool(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_IS_DEVTOOL": "true", "OK_IS_ACCESS_LOG": "false"})
	opts := OptionsFromEnv()
	if !opts.IsDevtool || opts.IsAccessLog {
		t.Error(test.DiffMessage([]bool{opts.IsDevtool, opts.IsAccessLog}, []bool{true, false}, "E5: bool true and false"))
	}
	for _, bad := range []string{"yes", "TRUE", "1", "True", "on"} {
		envSetup(t, "", map[string]string{"OK_IS_DEVTOOL": bad})
		if msg := envNewPanic(t, OptionsFromEnv()); !strings.Contains(msg, "OK_IS_DEVTOOL (environment)") {
			t.Error(test.DiffMessage(msg, "OK_IS_DEVTOOL (environment): …", "E5: bool refuses "+bad))
		}
	}
}

func TestOptionsFromEnv_ParsesDuration(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_STARTUP_TIMEOUT": "1m30s", "OK_CORS__MAX_AGE": "0s"})
	opts := OptionsFromEnv()
	if opts.StartupTimeout != 90*time.Second || opts.CORS.MaxAge != 0 {
		t.Error(test.DiffMessage([]time.Duration{opts.StartupTimeout, opts.CORS.MaxAge}, []time.Duration{90 * time.Second, 0}, "E5: durations, 0s accepted"))
	}
	for _, bad := range []string{"15", "0", "abc"} {
		envSetup(t, "", map[string]string{"OK_STARTUP_TIMEOUT": bad})
		if msg := envNewPanic(t, OptionsFromEnv()); !strings.Contains(msg, "OK_STARTUP_TIMEOUT (environment)") {
			t.Error(test.DiffMessage(msg, "OK_STARTUP_TIMEOUT (environment): …", "E5: a duration needs a unit: "+bad))
		}
	}
}

func TestOptionsFromEnv_ParsesByteSize(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HTTP__MAX_BODY_BYTES": "10MiB", "OK_COMPRESS__MIN_SIZE": "2KB"})
	opts := OptionsFromEnv()
	if opts.HTTP.MaxBodyBytes != 10*MiB || opts.Compress.MinSize != 2*KB {
		t.Error(test.DiffMessage([]ByteSize{opts.HTTP.MaxBodyBytes, opts.Compress.MinSize}, []ByteSize{10 * MiB, 2 * KB}, "E5: ByteSize by B1"))
	}
}

func TestOptionsFromEnv_ParsesEnums(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_PROXY__HEADER": "forwarded", "OK_VERSIONING__TYPE": "media-type"})
	opts := OptionsFromEnv()
	if opts.Proxy.Header != ProxyHeaderForwarded || opts.Versioning.Type != versioning.TypeMediaType {
		t.Error(test.DiffMessage([]any{opts.Proxy.Header, opts.Versioning.Type}, []any{ProxyHeaderForwarded, versioning.TypeMediaType}, "E5: enums by lower-case name"))
	}
	for name, value := range map[string]string{"OK_PROXY__HEADER": "Forwarded", "OK_VERSIONING__TYPE": "mediatype"} {
		envSetup(t, "", map[string]string{name: value})
		if msg := envNewPanic(t, OptionsFromEnv()); !strings.Contains(msg, name+" (environment)") {
			t.Error(test.DiffMessage(msg, name+" (environment): …", "E5: unknown enum name "+value))
		}
	}
	for text, want := range map[string]versioning.Type{"query": versioning.TypeQuery, "header": versioning.TypeHeader, "custom": versioning.TypeCustom} {
		envSetup(t, "", map[string]string{"OK_VERSIONING__TYPE": text})
		if got := OptionsFromEnv().Versioning.Type; got != want {
			t.Error(test.DiffMessage(got, want, "E5: versioning type "+text))
		}
	}
}

func TestOptionsFromEnv_ParsesListWithSpaces(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HOSTS": " a.com ,\tb.com,c.com "})
	if got := OptionsFromEnv().Hosts; !slices.Equal(got, []string{"a.com", "b.com", "c.com"}) {
		t.Error(test.DiffMessage(got, []string{"a.com", "b.com", "c.com"}, "E5: list split on commas, elements trimmed"))
	}
}

func TestOptionsFromEnv_BadValue(t *testing.T) {
	envSetup(t, "A=1\nB=2\nOK_HTTP__MAX_BODY_BYTES=10Mb\n", nil)
	msg := envNewPanic(t, OptionsFromEnv())
	want := `OK_HTTP__MAX_BODY_BYTES (.env:3): "10Mb" is not a size: `
	if !strings.Contains(msg, want) {
		t.Error(test.DiffMessage(msg, want+"…", "E6: a value that does not convert names the variable, source and line"))
	}
}

func TestOptionsFromEnv_EmptyElement(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_ORIGINS": "https://a.com,,https://b.com"})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, "OK_ORIGINS (environment): element 2 of 3 is empty") {
		t.Error(test.DiffMessage(msg, "OK_ORIGINS (environment): element 2 of 3 is empty", "E6: one empty element"))
	}
}

func TestOptionsFromEnv_BadElementRefusesList(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HOSTS": "a.com, ,b.com"})
	opts := OptionsFromEnv()
	if len(opts.Hosts) != 0 {
		t.Error(test.DiffMessage(opts.Hosts, []string(nil), "E6: a bad element refuses the whole variable"))
	}
	envNewPanic(t, opts)
}

func TestOptionsFromEnv_EmptyValue(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_ORIGINS": ""})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, "OK_ORIGINS (environment): empty value; remove the variable to unset it") {
		t.Error(test.DiffMessage(msg, "OK_ORIGINS (environment): empty value; remove the variable to unset it", "E6: an empty value"))
	}
}

func TestOptionsFromEnv_UnknownWithHint(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HOST": "a.com"})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, "OK_HOST (environment): unknown variable; did you mean OK_HOSTS?") {
		t.Error(test.DiffMessage(msg, "OK_HOST (environment): unknown variable; did you mean OK_HOSTS?", "E6: closest name"))
	}
}

func TestOptionsFromEnv_UnknownWithoutHint(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_QQQQQQQQQQQQQQQQQQQQ": "1"})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, "OK_QQQQQQQQQQQQQQQQQQQQ (environment): unknown variable") || strings.Contains(msg, "did you mean") {
		t.Error(test.DiffMessage(msg, "unknown variable, no hint", "E6: no hint beyond distance 3"))
	}
}

func TestOptionsFromEnv_AllErrorsInOnePanic(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HOST": "a", "OK_HTTP__MAX_BODY_BYTES": "1Mb", "OK_ORIGINS": ""})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.HasPrefix(msg, color.FmtRed("invalid app: 3 environment errors:\n  ")[:len("\x1b[31minvalid app: 3 environment errors:\n  ")]) {
		t.Error(test.DiffMessage(msg, "invalid app: 3 environment errors:\\n  …", "E8: one panic, the count, one error per line"))
	}
	if strings.Count(msg, "\n  OK_") != 3 {
		t.Error(test.DiffMessage(strings.Count(msg, "\n  OK_"), 3, "E8: every error on its own indented line"))
	}
}

func TestOptionsFromEnv_NeverPanics(t *testing.T) {
	envSetup(t, "OK_HOSTS=\"unterminated\n", map[string]string{"OK_HOST": "a", "OK_LOGGER": "x"})
	if msg, isPanic := optRecover(func() { OptionsFromEnv() }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E8: OptionsFromEnv records errors and never panics"))
	}
}

func TestOptionsFromEnv_DotEnvSyntaxErrorNoValue(t *testing.T) {
	envSetup(t, "A=1\nB=2\nC=3\nOK_HOSTS=\"topsecretvalue\n", nil)
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, ".env:4:") || strings.Contains(msg, "topsecretvalue") {
		t.Error(test.DiffMessage(msg, ".env:4: … without the value", "E8: a .env syntax error, never the value"))
	}
}

func TestOptionsFromEnv_MeaningErrorNamesVariable(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_PROXY__TRUSTED_PROXIES": "not-a-cidr"})
	msg := envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, "invalid app: Proxy.TrustedProxies") || !strings.Contains(msg, " (from OK_PROXY__TRUSTED_PROXIES, environment)") {
		t.Error(test.DiffMessage(msg, "invalid app: Proxy.TrustedProxies… (from OK_PROXY__TRUSTED_PROXIES, environment)", "E6: a meaning error names the variable"))
	}
	envSetup(t, "X=1\nOK_PROXY__TRUSTED_PROXIES=not-a-cidr\n", nil)
	msg = envNewPanic(t, OptionsFromEnv())
	if !strings.Contains(msg, " (from OK_PROXY__TRUSTED_PROXIES, .env:2)") {
		t.Error(test.DiffMessage(msg, "… (from OK_PROXY__TRUSTED_PROXIES, .env:2)", "E6: and the .env line"))
	}
}

func TestOptionsFromEnv_EnvBeatsDotEnv(t *testing.T) {
	envSetup(t, "OK_WS__PATH=/dotenv\nOK_HOSTS=dot.com\n", map[string]string{"OK_WS__PATH": "/env"})
	opts := OptionsFromEnv()
	if opts.WS.Path != "/env" || !slices.Equal(opts.Hosts, []string{"dot.com"}) {
		t.Error(test.DiffMessage([]any{opts.WS.Path, opts.Hosts}, []any{"/env", []string{"dot.com"}}, "E7: the environment wins, .env fills the rest"))
	}
}

func TestOptionsFromEnv_DotEnvMissingSkipped(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_WS__PATH": "/env"})
	if got := OptionsFromEnv().WS.Path; got != "/env" {
		t.Error(test.DiffMessage(got, "/env", "E7: a missing .env is skipped"))
	}
}

func TestOptionsFromEnv_DotEnvOtherKeysIgnored(t *testing.T) {
	envSetup(t, "DATABASE_URL=postgres://x\nSECRET=\"a b\"\nOK_WS__PATH=/ws\n", nil)
	opts := OptionsFromEnv()
	if opts.WS.Path != "/ws" {
		t.Error(test.DiffMessage(opts.WS.Path, "/ws", "E7: OK_ keys are read from .env"))
	}
	if msg, isPanic := optRecover(func() { opts.Logger = &mockLogger{}; New(opts) }); isPanic {
		t.Error(test.DiffMessage(msg, "no panic", "E7: other keys are left to the app"))
	}
}

func TestOptionsFromEnv_DotEnvNoExpansion(t *testing.T) {
	envSetup(t, "OK_WS__PATH=/${HOME}\n", nil)
	if got := OptionsFromEnv().WS.Path; got != "/${HOME}" {
		t.Error(test.DiffMessage(got, "/${HOME}", "E7: no variable expansion in .env"))
	}
}

func TestOptionsFromEnv_CodeWinsAfter(t *testing.T) {
	envSetup(t, "", map[string]string{
		"OK_IS_ACCESS_LOG": "true", "OK_IS_DEVTOOL": "true", "OK_HTTP__MAX_BODY_BYTES": "1KiB", "OK_SERVER__PORT": "8080", "OK_WS__PATH": "/ws",
	})
	opts := OptionsFromEnv()
	opts.IsAccessLog = false
	opts.HTTP.MaxBodyBytes = 0
	opts.Server.Port = 0
	opts.WS.Path = ""
	opts.Logger = &mockLogger{}
	app := New(opts)
	app.Create(NewModuleBuilder().Controllers(optBodyController{}).Build())

	if app.tracer.Enabled() {
		t.Error(test.DiffMessage(true, false, "E7: an explicit false written after wins"))
	}
	if app.devtool == nil {
		t.Error(test.DiffMessage(nil, "a devtool", "E7: what code leaves alone keeps the variable's value"))
	}
	body := `{"k":"` + strings.Repeat("a", 2048) + `"}`
	if w := optServe(app, http.MethodPost, "/echo", body, map[string]string{"Content-Type": "application/json"}); w.Code != http.StatusCreated {
		t.Error(test.DiffMessage(w.Code, 201, "E7: an explicit 0 written after wins (default 10MiB)"))
	}
}

func TestOptionsFromEnv_StartupLogSources(t *testing.T) {
	envSetup(t, "A=1\nB=2\nOK_WS__PATH=/ws\n", map[string]string{"OK_HTTP__MAX_BODY_BYTES": "1MiB", "PORT": "4000", "OK_HOSTS": "a.com,b.com", "OK_STARTUP_TIMEOUT": "5s"})
	l := &srvLogger{}
	opts := OptionsFromEnv()
	opts.IsDevtool = true
	opts.Logger = l
	New(opts)

	got := map[string][2]any{}
	for _, e := range l.named("AppOption") {
		if e.level != "info" {
			t.Error(test.DiffMessage(e.level, "info", "E9: AppOption is an Info line"))
		}
		got[e.field("option").(string)] = [2]any{e.field("value"), e.field("source")}
	}
	want := map[string][2]any{
		"HTTP.MaxBodyBytes": {"1MiB", "OK_HTTP__MAX_BODY_BYTES"},
		"Server.Port":       {4000, "PORT"},
		"WS.Path":           {"/ws", ".env:3"},
		"Hosts":             {"a.com,b.com", "OK_HOSTS"},
		"StartupTimeout":    {"5s", "OK_STARTUP_TIMEOUT"},
		"IsDevtool":         {true, "code"},
		"Logger":            {"set", "code"},
	}
	for option, w := range want {
		if fmtPair(got[option]) != fmtPair(w) {
			t.Error(test.DiffMessage(got[option], w, "E9: "+option))
		}
	}
	if _, ok := got["IsAccessLog"]; ok {
		t.Error(test.DiffMessage(got["IsAccessLog"], "no line", "E9: a zero field logs nothing"))
	}
}

func fmtPair(p [2]any) string { return fmt.Sprintf("%v|%T|%v|%T", p[0], p[0], p[1], p[1]) }

func TestOptionsFromEnv_CodeOverridesEnvValueIsCode(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_WS__PATH": "/env"})
	l := &srvLogger{}
	opts := OptionsFromEnv()
	opts.WS.Path = "/code"
	opts.Logger = l
	New(opts)
	for _, e := range l.named("AppOption") {
		if e.field("option") == "WS.Path" && (e.field("value") != "/code" || e.field("source") != "code") {
			t.Error(test.DiffMessage([]any{e.field("value"), e.field("source")}, []any{"/code", "code"}, "E9: a field that differs from what OptionsFromEnv set is code"))
		}
	}
}

func TestOptionsFromEnv_NoLogWithoutEnv(t *testing.T) {
	envSetup(t, "", nil)
	l := &srvLogger{}
	New(&AppOptions{Logger: l, IsDevtool: true, Hosts: []string{"a.com"}})
	if n := len(l.named("AppOption")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "E9: no AppOption line without OptionsFromEnv"))
	}
}

func TestNew_ServeDirsBoundFromEnv(t *testing.T) {
	envSetup(t, "", map[string]string{"OK_HTTP__SERVE_DIRS": "public, assets"})
	if got := OptionsFromEnv().HTTP.ServeDirs; !slices.Equal(got, []string{"public", "assets"}) {
		t.Error(test.DiffMessage(got, []string{"public", "assets"}, "D1: ServeDirs binds from OK_HTTP__SERVE_DIRS"))
	}
}
