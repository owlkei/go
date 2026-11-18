package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/test"
)

type pathController struct{ HTTP }

func (pathController) CREATE_token() string         { return "token" }
func (pathController) READ_token() string           { return "read" }
func (pathController) READ_payment_intents() string { return "pi" }
func (pathController) READ_users_VERSION_2() string { return "v2" }
func (pathController) READ_profile_BY_id() string   { return "profile" }

func patternOf(r *HTTP, fn string) string { return HTTPFuncNameToPatternMap(r)[fn] }

func TestAddHandlerToRouterMap_StyleChangesRegisteredPattern(t *testing.T) {
	kebab := &HTTP{}
	AddHandlerToRouterMap(kebab, nil, PathStyleKebab, "READ_payment_intents", nil)
	written := &HTTP{}
	AddHandlerToRouterMap(written, nil, PathStyleAsWritten, "READ_payment_intents", nil)

	wantKebab := routing.MethodRouteVersionToPattern("GET", "/payment-intents/", "")
	wantWritten := routing.MethodRouteVersionToPattern("GET", "/payment_intents/", "")
	if got := patternOf(kebab, "READ_payment_intents"); got != wantKebab {
		t.Error(test.DiffMessage(got, wantKebab, "B1: kebab pattern"))
	}
	if got := patternOf(written, "READ_payment_intents"); got != wantWritten {
		t.Error(test.DiffMessage(got, wantWritten, "B1: as written pattern"))
	}
}

func TestAddHandlerToRouterMap_PrefixesNotRewritten(t *testing.T) {
	r := &HTTP{}
	r.Prefix("my_api")
	AddHandlerToRouterMap(r, []string{"/pay_ments/"}, PathStyleKebab, "READ_payment_intents", nil)
	want := routing.MethodRouteVersionToPattern("GET", "/pay_ments/my_api/payment-intents/", "")
	if got := patternOf(r, "READ_payment_intents"); got != want {
		t.Error(test.DiffMessage(got, want, "B11: prefixes keep their text, the name part is kebab"))
	}
}

func TestPath_ReplacesNamePath(t *testing.T) {
	c := pathController{}
	c.Path("/oauth2/token", c.CREATE_token)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "CREATE_token", c.CREATE_token)

	want := routing.MethodRouteVersionToPattern("POST", "/oauth2/token/", "")
	if got := patternOf(&c.HTTP, "CREATE_token"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: the exact path replaces the name's path"))
	}
}

func TestPath_AppliesPrefixesBeforeModulePrefixes(t *testing.T) {
	c := pathController{}
	c.Prefix("v1")
	c.Path("/oauth2/token", c.CREATE_token)
	AddHandlerToRouterMap(&c.HTTP, []string{"/api/"}, PathStyleKebab, "CREATE_token", c.CREATE_token)

	want := routing.MethodRouteVersionToPattern("POST", "/api/v1/oauth2/token/", "")
	if got := patternOf(&c.HTTP, "CREATE_token"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: controller and module prefixes still apply"))
	}
}

func TestPath_KeepsVersionFromName(t *testing.T) {
	c := pathController{}
	c.Path("/people", c.READ_users_VERSION_2)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "READ_users_VERSION_2", c.READ_users_VERSION_2)

	want := routing.MethodRouteVersionToPattern("GET", "/people/", "2")
	if got := patternOf(&c.HTTP, "READ_users_VERSION_2"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: the version comes from VERSION"))
	}
}

func TestPath_NotRewrittenByStyle(t *testing.T) {
	c := pathController{}
	c.Path("/a_b/{id}", c.READ_payment_intents)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "READ_payment_intents", c.READ_payment_intents)

	want := routing.MethodRouteVersionToPattern("GET", "/a_b/{id}/", "")
	if got := patternOf(&c.HTTP, "READ_payment_intents"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: an exact path is never kebab-cased"))
	}
}

func TestPath_RootPath(t *testing.T) {
	c := pathController{}
	c.Path("/", c.READ_token)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "READ_token", c.READ_token)

	want := routing.MethodRouteVersionToPattern("GET", "/", "")
	if got := patternOf(&c.HTTP, "READ_token"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: Path(\"/\") is the controller root"))
	}
}

func TestPath_TwoOperationsOnePath(t *testing.T) {
	c := pathController{}
	c.Path("/token", c.READ_token, c.CREATE_token)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "READ_token", c.READ_token)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "CREATE_token", c.CREATE_token)

	if got, want := patternOf(&c.HTTP, "READ_token"), routing.MethodRouteVersionToPattern("GET", "/token/", ""); got != want {
		t.Error(test.DiffMessage(got, want, "B25: GET"))
	}
	if got, want := patternOf(&c.HTTP, "CREATE_token"), routing.MethodRouteVersionToPattern("POST", "/token/", ""); got != want {
		t.Error(test.DiffMessage(got, want, "B25: POST"))
	}
	if len(HTTPRouterMap(&c.HTTP)) != 2 {
		t.Error(test.DiffMessage(len(HTTPRouterMap(&c.HTTP)), 2, "B25: two routes"))
	}
}

func TestPath_OtherHandlersKeepTheirNamePath(t *testing.T) {
	c := pathController{}
	c.Path("/oauth2/token", c.CREATE_token)
	AddHandlerToRouterMap(&c.HTTP, nil, PathStyleKebab, "READ_payment_intents", c.READ_payment_intents)

	want := routing.MethodRouteVersionToPattern("GET", "/payment-intents/", "")
	if got := patternOf(&c.HTTP, "READ_payment_intents"); got != want {
		t.Error(test.DiffMessage(got, want, "B22: a handler without an exact path is unchanged"))
	}
}
