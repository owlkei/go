package core

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type pathTokenController struct{ common.HTTP }

func (c pathTokenController) NewController() Controller {
	c.Path("/oauth2/token", c.CREATE_token)
	return c
}
func (c pathTokenController) CREATE_token() string { return "token" }
func (c pathTokenController) READ_payment_intents() string {
	return "pi"
}

func TestPath_ReplacesNamePath(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(pathTokenController{}).Build())

	w := createRequest(app, http.MethodPost, "/oauth2/token")
	if w.Body.String() != "token" {
		t.Error(test.DiffMessage(w.Body.String(), "token", "B22: POST /oauth2/token reaches CREATE_token"))
	}
	if w := createRequest(app, http.MethodPost, "/token"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B22: the name's own path is gone"))
	}
	psExpect(t, app, "B22", "/payment-intents", http.StatusOK)
}

type pathPrefixController struct{ common.HTTP }

func (c pathPrefixController) NewController() Controller {
	c.Prefix("v1")
	c.Path("/oauth2/token", c.CREATE_token_VERSION_NEUTRAL, c.READ_info_VERSION_2)
	return c
}
func (c pathPrefixController) CREATE_token_VERSION_NEUTRAL() string { return "token" }
func (c pathPrefixController) READ_info_VERSION_2() string          { return "v2" }

func TestPath_KeepsPrefixesMethodVersion(t *testing.T) {
	app := New()
	app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
	mustCreate(t, app, NewModuleBuilder().Controllers(pathPrefixController{}).Build().Prefix("api"))

	if w := createRequest(app, http.MethodPost, "/api/v1/oauth2/token"); w.Body.String() != "token" {
		t.Error(test.DiffMessage(w.Body.String(), "token", "B22: module and controller prefixes apply to an exact path"))
	}
	if w := createRequest(app, http.MethodGet, "/api/v1/oauth2/token", "X-Version", "2"); w.Body.String() != "v2" {
		t.Error(test.DiffMessage(w.Body.String(), "v2", "B22: the version comes from VERSION and the method from READ"))
	}
	if w := createRequest(app, http.MethodGet, "/api/v1/oauth2/token", "X-Version", "3"); w.Code != http.StatusMethodNotAllowed {
		t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "B22: another version is not routed; only the neutral POST answers that path"))
	}
	if w := createRequest(app, http.MethodPost, "/oauth2/token"); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B22: no route without the prefixes"))
	}
}

type pathStyleController struct{ common.HTTP }

func (c pathStyleController) NewController() Controller {
	c.Path("/a_b/{id}", c.READ_payment_intents)
	return c
}
func (c pathStyleController) READ_payment_intents(p ctx.Param) string { return "id=" + p.Get("id") }

func TestPath_NotRewrittenByStyle(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(pathStyleController{}).Build())

	w := createGet(app, "/a_b/7")
	if w.Code != http.StatusOK || w.Body.String() != "id=7" {
		t.Error(test.DiffMessage(w.Body.String(), "id=7", "B22: Path under kebab keeps _, and {id} is a parameter"))
	}
	psExpect(t, app, "B22", "/a-b/7", http.StatusNotFound)
}

type pathRootController struct{ common.HTTP }

func (c pathRootController) NewController() Controller {
	c.Path("/", c.READ_home_page)
	return c
}
func (c pathRootController) READ_home_page() string { return "home" }

func TestPath_RootPath(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(pathRootController{}).Build())

	if w := createGet(app, "/"); w.Body.String() != "home" {
		t.Error(test.DiffMessage(w.Body.String(), "home", "B22: Path(\"/\") is the controller root"))
	}
}

type pathBadNameController struct{ common.HTTP }

func (c pathBadNameController) NewController() Controller {
	c.Path("/x", c.READ_orgs_FB)
	return c
}
func (c pathBadNameController) READ_orgs_FB() string { return "x" }

func TestPath_NameStillChecked(t *testing.T) {
	want := "invalid route: FB in READ_orgs_FB is upper case but not a route word"
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(pathBadNameController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B23: a handler with an exact path is still parsed"))
	}
}

type pathNoHandlerController struct{ common.HTTP }

func (c pathNoHandlerController) NewController() Controller {
	c.Path("/x")
	return c
}
func (c pathNoHandlerController) READ_ok() string { return "x" }

type pathHelperController struct{ common.HTTP }

func (c pathHelperController) NewController() Controller {
	c.Path("/x", c.Helper)
	return c
}
func (c pathHelperController) READ_ok() string { return "x" }
func (c pathHelperController) Helper() string  { return "x" }

type pathWSController struct {
	common.HTTP
	common.WS
}

func (c pathWSController) NewController() Controller {
	c.Path("/x", c.SUBSCRIBE_chat)
	return c
}
func (c pathWSController) READ_ok() string        { return "x" }
func (c pathWSController) SUBSCRIBE_chat() string { return "x" }

type pathNotFuncController struct{ common.HTTP }

func (c pathNotFuncController) NewController() Controller {
	c.Path("/x", "text")
	return c
}
func (c pathNotFuncController) READ_ok() string { return "x" }

type pathTwiceController struct{ common.HTTP }

func (c pathTwiceController) NewController() Controller {
	c.Path("/one", c.READ_ok)
	c.Path("/two", c.READ_ok)
	return c
}
func (c pathTwiceController) READ_ok() string { return "x" }

type pathQueryController struct{ common.HTTP }

func (c pathQueryController) NewController() Controller {
	c.Path("/x?y=1", c.READ_ok)
	return c
}
func (c pathQueryController) READ_ok() string { return "x" }

type pathFragmentController struct{ common.HTTP }

func (c pathFragmentController) NewController() Controller {
	c.Path("/x#top", c.READ_ok)
	return c
}
func (c pathFragmentController) READ_ok() string { return "x" }

func TestPath_Panics(t *testing.T) {
	cases := []struct {
		name       string
		controller Controller
		want       string
	}{
		{"no handler", pathNoHandlerController{}, `invalid route: Path("/x") needs at least one handler`},
		{"helper", pathHelperController{}, `invalid route: Path("/x") is bound to 'Helper', which is not a handler of the controller`},
		{"subscribe", pathWSController{}, `invalid route: Path("/x") is bound to 'SUBSCRIBE_chat', which is not a handler of the controller`},
		{"not a function", pathNotFuncController{}, `invalid route: Path("/x") is bound to 'string', which is not a handler of the controller`},
		{"twice", pathTwiceController{}, `invalid route: READ_ok has two exact paths: "/one" and "/two"`},
		{"query", pathQueryController{}, `invalid route: Path("/x?y=1") holds '?': a path has no query or fragment`},
		{"fragment", pathFragmentController{}, `invalid route: Path("/x#top") holds '#': a path has no query or fragment`},
	}
	for _, c := range cases {
		text, panicked := createPanicText(func() {
			New().Create(NewModuleBuilder().Controllers(c.controller).Build())
		})
		if !panicked || !strings.Contains(text, c.want) {
			t.Error(test.DiffMessage(text, c.want, "B24: "+c.name))
		}
	}
}

type pathTwoOpsController struct{ common.HTTP }

func (c pathTwoOpsController) NewController() Controller {
	c.Path("/token", c.READ_token, c.CREATE_token)
	return c
}
func (c pathTwoOpsController) READ_token() string   { return "read" }
func (c pathTwoOpsController) CREATE_token() string { return "create" }

func TestPath_TwoOperationsOnePath(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(pathTwoOpsController{}).Build())

	if w := createGet(app, "/token"); w.Body.String() != "read" {
		t.Error(test.DiffMessage(w.Body.String(), "read", "B25: GET /token"))
	}
	if w := createRequest(app, http.MethodPost, "/token"); w.Body.String() != "create" {
		t.Error(test.DiffMessage(w.Body.String(), "create", "B25: POST /token"))
	}
}

type pathSameOpController struct{ common.HTTP }

func (c pathSameOpController) NewController() Controller {
	c.Path("/same", c.READ_one, c.READ_two)
	return c
}
func (c pathSameOpController) READ_one() string { return "one" }
func (c pathSameOpController) READ_two() string { return "two" }

func TestPath_SameOperationTwiceConflicts(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(pathSameOpController{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict") {
		t.Error(test.DiffMessage(text, "route conflict: …", "B25: one operation twice on one path"))
	}
}

type pathNameConflictController struct{ common.HTTP }

func (c pathNameConflictController) NewController() Controller {
	c.Path("/users", c.READ_other)
	return c
}
func (c pathNameConflictController) READ_other() string { return "other" }
func (c pathNameConflictController) READ_users() string { return "users" }

func TestPath_ConflictWithAnotherNameStillDetected(t *testing.T) {
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(pathNameConflictController{}).Build())
	})
	if !panicked || !strings.Contains(text, "route conflict") {
		t.Error(test.DiffMessage(text, "route conflict: …", "security: two spellings of one URL"))
	}
}

type pathGuardDeny struct{}

var pathGuardCalls int

func (g pathGuardDeny) CanActivate(c *ctx.HTTPContext) bool {
	pathGuardCalls++
	return false
}

type pathLayerController struct {
	common.HTTP
	common.Guard
}

func (c pathLayerController) NewController() Controller {
	c.Path("/oauth2/token", c.CREATE_token)
	c.BindGuard(pathGuardDeny{}, c.CREATE_token)
	c.Prefix("v1", c.CREATE_token)
	return c
}
func (c pathLayerController) CREATE_token() string { return "token" }
func (c pathLayerController) READ_open() string    { return "open" }

func TestPath_LayersAndTargetedPrefix(t *testing.T) {
	pathGuardCalls = 0
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(pathLayerController{}).Build())

	w := createRequest(app, http.MethodPost, "/v1/oauth2/token")
	if w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "B26: the guard bound to the handler runs on the exact path"))
	}
	if pathGuardCalls != 1 {
		t.Error(test.DiffMessage(pathGuardCalls, 1, "B26: guard calls"))
	}
	psExpect(t, app, "B26", "/open", http.StatusOK)
}

func TestPath_RoutesAreSeparatePerApp(t *testing.T) {
	appA := New()
	mustCreate(t, appA, NewModuleBuilder().Controllers(pathTokenController{}).Build())
	appB := New()
	mustCreate(t, appB, NewModuleBuilder().Controllers(pathTokenController{}).Build())

	for name, app := range map[string]*App{"first": appA, "second": appB} {
		if w := createRequest(app, http.MethodPost, "/oauth2/token"); w.Body.String() != "token" {
			t.Error(test.DiffMessage(w.Body.String(), "token", "B22: the "+name+" app serves the exact path"))
		}
	}
}
