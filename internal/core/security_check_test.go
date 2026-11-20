package core

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/test"
)

type scFormController struct{ common.HTTP }

func (c scFormController) NewController() Controller { return c }

func (c scFormController) RENDER_form() {}

func (c scFormController) SUBMIT_form() {}

type scSubmitController struct{ common.HTTP }

func (c scSubmitController) NewController() Controller { return c }

func (c scSubmitController) RENDER_login() {}

func (c scSubmitController) SUBMIT_login() {}

type scPageOnlyController struct{ common.HTTP }

func (c scPageOnlyController) NewController() Controller { return c }

func (c scPageOnlyController) RENDER_about() {}

type scAPIController struct{ common.HTTP }

func (c scAPIController) NewController() Controller { return c }

func (c scAPIController) READ_ping() string { return "pong" }

func (c scAPIController) CREATE_items() string { return "created" }

func secApp(t *testing.T, opts *AppOptions, controllers ...Controller) *srvLogger {
	t.Helper()
	l := &srvLogger{}
	if opts == nil {
		opts = &AppOptions{}
	}
	opts.Logger = l
	files := map[string]string{}
	for _, c := range controllers {
		switch c.(type) {
		case scFormController:
			files["form.html"] = "f"
		case scSubmitController:
			files["login.html"] = "l"
		case scPageOnlyController:
			files["about.html"] = "a"
		}
	}
	opts.Render = &RenderOptions{FS: pgFiles(files), Dir: "."}
	app := New(opts)
	app.Create(NewModuleBuilder().Controllers(controllers...).Build())
	return l
}

func scMissing(t *testing.T, l *srvLogger) (missing []string, lines int) {
	t.Helper()
	entries := l.named("SecurityCheck")
	if len(entries) == 0 {
		return nil, 0
	}
	e := entries[0]
	if e.level != "warn" {
		t.Error(test.DiffMessage(e.level, "warn", "C1: the line is a Warn"))
	}
	text, _ := e.field("missing").(string)
	for _, name := range strings.Split(text, ",") {
		missing = append(missing, strings.TrimSpace(name))
	}
	if url, _ := e.field("docs").(string); !strings.HasPrefix(url, "http") {
		t.Error(test.DiffMessage(url, "an URL", "C1: the line carries the docs link"))
	}
	return missing, len(entries)
}

func TestSecurityCheck_SubmitWithoutCSRF(t *testing.T) {
	l := secApp(t, &AppOptions{Headers: &HeadersOptions{IsEnabled: true}}, scSubmitController{})
	missing, n := scMissing(t, l)
	if n != 1 || !slices.Equal(missing, []string{"csrf"}) {
		t.Error(test.DiffMessage(missing, []string{"csrf"}, "C1: a SUBMIT_ route without CSRF"))
	}
}

func TestSecurityCheck_RenderWithoutHeaders(t *testing.T) {
	l := secApp(t, nil, scPageOnlyController{})
	missing, n := scMissing(t, l)
	if n != 1 || !slices.Equal(missing, []string{"headers"}) {
		t.Error(test.DiffMessage(missing, []string{"headers"}, "C1: a RENDER_ route without headers"))
	}
}

func TestSecurityCheck_BothInOneLine(t *testing.T) {
	l := secApp(t, nil, scFormController{})
	missing, n := scMissing(t, l)
	if n != 1 || !slices.Equal(missing, []string{"csrf", "headers"}) {
		t.Error(test.DiffMessage(missing, []string{"csrf", "headers"}, "C1: one line, both names"))
	}
}

func TestSecurityCheck_SilencedByIsDisabled(t *testing.T) {
	l := secApp(t, &AppOptions{CSRF: &CSRFOptions{IsDisabled: true}}, scFormController{})
	missing, _ := scMissing(t, l)
	if !slices.Equal(missing, []string{"headers"}) {
		t.Error(test.DiffMessage(missing, []string{"headers"}, "C1: CSRF.IsDisabled silences csrf"))
	}
	l = secApp(t, &AppOptions{Headers: &HeadersOptions{IsDisabled: true}}, scFormController{})
	missing, _ = scMissing(t, l)
	if !slices.Equal(missing, []string{"csrf"}) {
		t.Error(test.DiffMessage(missing, []string{"csrf"}, "C1: Headers.IsDisabled silences headers"))
	}
	l = secApp(t, &AppOptions{CSRF: &CSRFOptions{IsDisabled: true}, Headers: &HeadersOptions{IsDisabled: true}}, scFormController{})
	if _, n := scMissing(t, l); n != 0 {
		t.Error(test.DiffMessage(n, 0, "C2: both silenced, no line"))
	}
}

func TestSecurityCheck_QuietWhenEnabled(t *testing.T) {
	l := secApp(t, &AppOptions{
		Origins: []string{"https://a.com"},
		CSRF:    &CSRFOptions{IsEnabled: true},
		Headers: &HeadersOptions{IsEnabled: true},
	}, scFormController{})
	if _, n := scMissing(t, l); n != 0 {
		t.Error(test.DiffMessage(n, 0, "C2: every protection on, no line"))
	}
}

func TestSecurityCheck_QuietForAPIOnly(t *testing.T) {
	l := &srvLogger{}
	app := New(&AppOptions{Logger: l})
	app.Create(NewModuleBuilder().Controllers(scAPIController{}).Build())
	if _, n := scMissing(t, l); n != 0 {
		t.Error(test.DiffMessage(n, 0, "C2: an API-only app has no warning"))
	}
}

func TestSecurityCheck_AsyncModuleRoutesCount(t *testing.T) {
	factory := func() *Module { return NewModuleBuilder().Build() }
	user := NewModuleBuilder().AsyncImports(factory).Controllers(scFormController{}).Build()
	l := &srvLogger{}
	app := New(&AppOptions{Logger: l, Render: &RenderOptions{FS: pgFiles(map[string]string{"form.html": "f"}), Dir: "."}})
	app.Create(NewModuleBuilder().Imports(user).Build())
	missing, n := scMissing(t, l)
	if n != 1 || !slices.Equal(missing, []string{"csrf", "headers"}) {
		t.Error(test.DiffMessage(missing, []string{"csrf", "headers"}, "C1: routes of AsyncImports modules count"))
	}
}

func TestSecurityCheck_ChangesNoResponse(t *testing.T) {
	app := New(&AppOptions{Logger: &srvLogger{}, Render: &RenderOptions{FS: pgFiles(map[string]string{"form.html": "f"}), Dir: "."}})
	app.Create(NewModuleBuilder().Controllers(scFormController{}).Build())
	w := preDo(app, http.MethodPost, "/form", map[string]string{"Origin": "https://evil.com"})
	if w.Code == http.StatusForbidden || w.Header().Get("X-Frame-Options") != "" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header()}, "no stage, no refusal", "C3: the check changes nothing"))
	}
}

func TestSecurityCheck_LoggedOnce(t *testing.T) {
	l := secApp(t, nil, scFormController{})
	if n := len(l.named("SecurityCheck")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "C1: at most one line"))
	}
}
