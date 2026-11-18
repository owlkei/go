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

type psRootController struct{ common.HTTP }

func (c psRootController) NewController() Controller { return c }
func (c psRootController) READ_root_page() string    { return "root" }

type psLeafController struct{ common.HTTP }

func (c psLeafController) NewController() Controller { return c }
func (c psLeafController) READ_leaf_page() string    { return "leaf" }

type psMidController struct{ common.HTTP }

func (c psMidController) NewController() Controller { return c }
func (c psMidController) READ_mid_page() string     { return "mid" }

type psDynamicController struct{ common.HTTP }

func (c psDynamicController) NewController() Controller { return c }
func (c psDynamicController) READ_dynamic_page() string { return "dynamic" }

func psExpect(t *testing.T, app *App, rule, target string, code int) {
	t.Helper()
	if w := createGet(app, target); w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status of GET "+target))
	}
}

func TestPathStyle_DefaultKebab(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(psRootController{}).Build())

	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
	psExpect(t, app, "B12", "/root_page", http.StatusNotFound)
}

func TestPathStyle_OldSpellingIs404(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(psRootController{}).Build())

	psExpect(t, app, "B12: the route exists under its kebab spelling", "/root-page", http.StatusOK)
	lcExpectResponse(t, createGet(app, "/root_page"), http.StatusNotFound, wantNotFoundJSON, "B12: the underscore spelling is not a route under kebab")
}

func TestPathStyle_RootAsWrittenReachesImports(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	dynamic := func() *Module { return NewModuleBuilder().Controllers(psDynamicController{}).Build() }
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(leaf, dynamic).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17c", "/root_page", http.StatusOK)
	psExpect(t, app, "B17c", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17c", "/dynamic_page", http.StatusOK)
	psExpect(t, app, "B17c", "/root-page", http.StatusNotFound)
	psExpect(t, app, "B17c", "/leaf-page", http.StatusNotFound)
	psExpect(t, app, "B17c", "/dynamic-page", http.StatusNotFound)
}

func TestPathStyle_ExplicitKebabWins(t *testing.T) {
	leaf := NewModuleBuilder().PathStyle(common.PathStyleKebab).Controllers(psLeafController{}).Build()
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(leaf).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17d", "/leaf-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf_page", http.StatusNotFound)
	psExpect(t, app, "B17d", "/root_page", http.StatusOK)
}

func TestPathStyle_ExplicitAsWrittenInsideKebabRoot(t *testing.T) {
	leaf := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psLeafController{}).Build()
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleKebab).
		Imports(leaf).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17b", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
}

func TestPathStyle_NearestSettingWins(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	mid := NewModuleBuilder().PathStyle(common.PathStyleKebab).Imports(leaf).Controllers(psMidController{}).Build()
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		Imports(mid).
		Controllers(psRootController{}).
		Build())

	psExpect(t, app, "B17d", "/root_page", http.StatusOK)
	psExpect(t, app, "B17d", "/mid-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf-page", http.StatusOK)
	psExpect(t, app, "B17d", "/leaf_page", http.StatusNotFound)
}

func TestPathStyle_ModuleStyleAppliesToItsOwnControllersOnly(t *testing.T) {
	styled := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psLeafController{}).Build()
	app := New()
	mustCreate(t, app, NewModuleBuilder().Imports(styled).Controllers(psRootController{}).Build())

	psExpect(t, app, "B17b", "/leaf_page", http.StatusOK)
	psExpect(t, app, "B17b", "/root-page", http.StatusOK)
	psExpect(t, app, "B17b", "/root_page", http.StatusNotFound)
}

func psTwoParents(first, second string) *App {
	m := NewModuleBuilder().Controllers(psLeafController{}).Build()
	a := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Imports(m).Build()
	b := NewModuleBuilder().PathStyle(common.PathStyleKebab).Imports(m).Build()
	byName := map[string]*Module{"A": a, "B": b}
	app := New()
	app.Create(NewModuleBuilder().Imports(byName[first], byName[second]).Build())
	return app
}

func TestPathStyle_TwoParentsFirstDiscoveredWins(t *testing.T) {
	appAB := psTwoParents("A", "B")
	psExpect(t, appAB, "B17e", "/leaf_page", http.StatusOK)
	psExpect(t, appAB, "B17e", "/leaf-page", http.StatusNotFound)

	appBA := psTwoParents("B", "A")
	psExpect(t, appBA, "B17e", "/leaf-page", http.StatusOK)
	psExpect(t, appBA, "B17e", "/leaf_page", http.StatusNotFound)
}

func TestPathStyle_TwoParentsRegisterTheRouteOnce(t *testing.T) {
	text, panicked := createPanicText(func() { psTwoParents("A", "B") })
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17e: a module imported twice binds its controller once"))
	}
}

func TestPathStyle_InvalidValuePanics(t *testing.T) {
	want := "invalid module: PathStyle needs ginject.PathStyleKebab or ginject.PathStyleAsWritten"
	for _, style := range []common.PathStyle{0, 9, -1} {
		text, panicked := createPanicText(func() { NewModuleBuilder().PathStyle(style) })
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "B17a: PathStyle value"))
		}
	}
}

func TestPathStyle_ReturnsTheBuilder(t *testing.T) {
	b := NewModuleBuilder()
	if b.PathStyle(common.PathStyleKebab) != b {
		t.Error(test.DiffMessage("another builder", "the same builder", "B17a: PathStyle returns the builder"))
	}
}

func TestPathStyle_LastCallWins(t *testing.T) {
	app := New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleAsWritten).
		PathStyle(common.PathStyleKebab).
		Controllers(psRootController{}).
		Build())
	psExpect(t, app, "B17a", "/root-page", http.StatusOK)

	app = New()
	mustCreate(t, app, NewModuleBuilder().
		PathStyle(common.PathStyleKebab).
		PathStyle(common.PathStyleAsWritten).
		Controllers(psRootController{}).
		Build())
	psExpect(t, app, "B17a", "/root_page", http.StatusOK)
}

type psPrefixController struct{ common.HTTP }

func (c psPrefixController) NewController() Controller {
	c.Prefix("my_ctl")
	return c
}
func (c psPrefixController) READ_root_page() string { return "p" }

func TestPathStyle_PrefixNotRewritten(t *testing.T) {
	kebab := NewModuleBuilder().Controllers(psPrefixController{}).Build().Prefix("pay_ments")
	app := New()
	mustCreate(t, app, kebab)
	psExpect(t, app, "B11", "/pay_ments/my_ctl/root-page", http.StatusOK)

	written := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psPrefixController{}).Build().Prefix("my-api")
	app = New()
	mustCreate(t, app, written)
	psExpect(t, app, "B11", "/my-api/my_ctl/root_page", http.StatusOK)
}

type psParamController struct{ common.HTTP }

func (c psParamController) NewController() Controller { return c }
func (c psParamController) READ_user_profile_BY_user_id_VERSION_NEUTRAL(p ctx.Param) string {
	return p.Get("user_id")
}
func (c psParamController) READ_ver_sion_VERSION_v1_v1() string { return "versioned" }

type psWSController struct{ common.WS }

func (c psWSController) NewController() Controller   { return c }
func (c psWSController) SUBSCRIBE_chat_room() string { return "room" }

func TestPathStyle_OnlyResourceWords(t *testing.T) {
	for _, style := range []common.PathStyle{common.PathStyleKebab, common.PathStyleAsWritten} {
		app := New()
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
		builder := NewModuleBuilder().PathStyle(style).Controllers(psParamController{})
		mustCreate(t, app, builder.Build())

		profile, version := "/user-profile/42", "/ver-sion"
		if style == common.PathStyleAsWritten {
			profile, version = "/user_profile/42", "/ver_sion"
		}
		w := createGet(app, profile)
		if w.Code != http.StatusOK || w.Body.String() != "42" {
			t.Error(test.DiffMessage(w.Body.String(), "42", "B17f: the parameter keeps its name, GET "+profile))
		}
		w = createRequest(app, http.MethodGet, version, "X-Version", "v1_v1")
		if w.Code != http.StatusOK || w.Body.String() != "versioned" {
			t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17f: the version keeps its text, GET "+version))
		}
	}
}

func TestPathStyle_DoesNotTouchEvents(t *testing.T) {
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Controllers(psWSController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B17f: an event is the same under either style"))
	}
	if got := app.ws.eventMatcher; got == nil {
		t.Fatal(test.DiffMessage("nil", "an event matcher", "B17f: events registered"))
	}
	if _, _, ok := app.ws.eventMatcher.Match("chat.room"); !ok {
		t.Error(test.DiffMessage(false, true, "B17f: SUBSCRIBE_chat_room is the event chat.room under as written"))
	}
}

func TestTwoApps_PathStyleNotShared(t *testing.T) {
	leaf := NewModuleBuilder().Controllers(psLeafController{}).Build()
	asWritten := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Imports(leaf).Build()
	plain := NewModuleBuilder().Imports(leaf).Build()

	appA := New()
	mustCreate(t, appA, asWritten)
	appB := New()
	mustCreate(t, appB, plain)
	appC := New()
	mustCreate(t, appC, asWritten)

	psExpect(t, appA, "B17g", "/leaf_page", http.StatusOK)
	psExpect(t, appB, "B17g", "/leaf-page", http.StatusOK)
	psExpect(t, appB, "B17g", "/leaf_page", http.StatusNotFound)
	psExpect(t, appC, "B17g", "/leaf_page", http.StatusOK)
	psExpect(t, appC, "B17g", "/leaf-page", http.StatusNotFound)
}

type psUpperController struct{ common.HTTP }

func (c psUpperController) NewController() Controller { return c }
func (c psUpperController) READ_orgs_FB() string      { return "x" }

type psHelperController struct{ common.HTTP }

func (c psHelperController) NewController() Controller { return c }
func (c psHelperController) READ_ok() string           { return "ok" }
func (c psHelperController) Helper_FB() string         { return "helper" }

func TestCreate_UpperCaseRoutePanics(t *testing.T) {
	want := "invalid route: FB in READ_orgs_FB is upper case but not a route word; write it in lower case or camelCase"
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(psUpperController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B9: Create stops on READ_orgs_FB"))
	}
}

func TestCreate_NonRouteMethodIsNeverChecked(t *testing.T) {
	app := New()
	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(psHelperController{}).Build())
	})
	if panicked {
		t.Error(test.DiffMessage(text, "no panic", "B10: Helper_FB is not a route"))
	}
	psExpect(t, app, "B10", "/ok", http.StatusOK)
}

type psMalformedController struct{ common.HTTP }

func (c psMalformedController) NewController() Controller { return c }
func (c psMalformedController) READ_users_BY() string     { return "x" }

func TestCreate_MalformedRoutePanics(t *testing.T) {
	want := "invalid route: BY in READ_users_BY has no parameter name"
	text, panicked := createPanicText(func() {
		New().Create(NewModuleBuilder().Controllers(psMalformedController{}).Build())
	})
	if !panicked || !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, "B13: Create stops on a malformed name"))
	}
}
