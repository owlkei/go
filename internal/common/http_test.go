package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestGetPrefixes_NoHandlers(t *testing.T) {
	r := &HTTP{}
	r.Prefix("/api")
	prefixes := r.getPrefixes()
	if len(prefixes) != 1 {
		t.Error(test.DiffMessage(len(prefixes), 1, "one prefix entry"))
		return
	}
	for k, v := range prefixes[0] {
		if k != "/api/" {
			t.Error(test.DiffMessage(k, "/api/", "prefix key"))
		}
		if v != "*" {
			t.Error(test.DiffMessage(v, "*", "wildcard value"))
		}
	}
}

func TestGetPrefixes_WithHandlers(t *testing.T) {
	r := &HTTP{}
	r.Prefix("/v1", fnTestController{}.READ_users, fnTestController{}.CREATE_orders)
	prefixes := r.getPrefixes()
	if len(prefixes) != 2 {
		t.Error(test.DiffMessage(len(prefixes), 2, "two prefix entries for two handlers"))
		return
	}
	names := map[string]bool{}
	for _, pm := range prefixes {
		for _, v := range pm {
			names[v] = true
		}
	}
	if !names["READ_users"] {
		t.Error(test.DiffMessage(names, "READ_users present", "handler name"))
	}
	if !names["CREATE_orders"] {
		t.Error(test.DiffMessage(names, "CREATE_orders present", "handler name"))
	}
}

func TestGetPrefixes_Empty(t *testing.T) {
	r := &HTTP{}
	prefixes := r.getPrefixes()
	if len(prefixes) != 0 {
		t.Error(test.DiffMessage(len(prefixes), 0, "no prefixes configured"))
	}
}

func TestAddPrefixesToRoute_WildcardApplies(t *testing.T) {
	r := &HTTP{}
	r.Prefix("/api")
	prefixes := r.getPrefixes()
	got := r.addPrefixesToRoute("/users/", "READ_users", prefixes)
	want := "/api/users/"
	if got != want {
		t.Error(test.DiffMessage(got, want, "wildcard prefix prepended without double slash"))
	}
}

func TestAddPrefixesToRoute_SpecificFnMatch(t *testing.T) {
	r := &HTTP{}
	r.Prefix("/v1", fnTestController{}.READ_users)
	prefixes := r.getPrefixes()
	got := r.addPrefixesToRoute("/users/", "READ_users", prefixes)
	want := "/v1/users/"
	if got != want {
		t.Error(test.DiffMessage(got, want, "matching fn prefix prepended"))
	}
}

func TestAddPrefixesToRoute_SpecificFnNoMatch(t *testing.T) {
	r := &HTTP{}
	r.Prefix("/v1", fnTestController{}.READ_users)
	prefixes := r.getPrefixes()
	got := r.addPrefixesToRoute("/orders/", "CREATE_orders", prefixes)
	want := "/orders/"
	if got != want {
		t.Error(test.DiffMessage(got, want, "non-matching fn prefix not prepended"))
	}
}

func TestAddPrefixesToRoute_NoPrefixes(t *testing.T) {
	r := &HTTP{}
	got := r.addPrefixesToRoute("/items/", "READ_items", nil)
	want := "/items/"
	if got != want {
		t.Error(test.DiffMessage(got, want, "no prefixes leaves route unchanged"))
	}
}

func TestAddToRouters_InitMaps(t *testing.T) {
	r := &HTTP{}
	r.addToRouters("READ_users", "/users/", "", "GET", nil)
	if HTTPRouterMap(r) == nil {
		t.Error(test.DiffMessage(HTTPRouterMap(r), "non-nil", "RouterMap initialized"))
	}
	if HTTPPatternToFuncNameMap(r) == nil {
		t.Error(test.DiffMessage(HTTPPatternToFuncNameMap(r), "non-nil", "PatternToFuncNameMap initialized"))
	}
	if HTTPFuncNameToPatternMap(r) == nil {
		t.Error(test.DiffMessage(HTTPFuncNameToPatternMap(r), "non-nil", "FuncNameToPatternMap initialized"))
	}
}

func TestAddHandlerToRouterMap_StoresPattern(t *testing.T) {
	r := &HTTP{}
	AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_items", nil)
	if len(HTTPRouterMap(r)) != 1 {
		t.Error(test.DiffMessage(len(HTTPRouterMap(r)), 1, "one route added"))
	}
	if HTTPPatternToFuncNameMap(r)["/items/||/[GET]/"] != "READ_items" {
		t.Error(test.DiffMessage(HTTPPatternToFuncNameMap(r)["/items/||/[GET]/"], "READ_items", "pattern stored"))
	}
}

func TestAddHandlerToRouterMap_SameShapeDifferentParamNamesDoesNotPanic(t *testing.T) {
	r := &HTTP{}
	AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_BY_ID", nil)
	message := routeConflictPanic(func() { AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_BY_NAME", nil) })
	if message != "" {
		t.Error(test.DiffMessage(message, "", "conflicts are checked per app by core, AddHandlerToRouterMap no longer panics"))
	}
}

func TestAddHandlerToRouterMap_DifferentShapesDoNotConflict(t *testing.T) {
	r := &HTTP{}
	AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_users_BY_id", nil)
	message := routeConflictPanic(func() {
		AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_users_BY_id_AND_name", nil)
		AddHandlerToRouterMap(r, nil, PathStyleKebab, "UPDATE_users_BY_id", nil)
		AddHandlerToRouterMap(r, nil, PathStyleKebab, "READ_users_all", nil)
	})
	if message != "" {
		t.Error(test.DiffMessage(message, "", "routes with a different shape, method or static segment must not conflict"))
	}
}

func TestHooks_RouterMapAccessors(t *testing.T) {
	h := &HTTP{}
	AddHandlerToRouterMap(h, nil, PathStyleKebab, "READ_items", nil)
	if len(HTTPRouterMap(h)) != 1 || len(HTTPPatternToFuncNameMap(h)) != 1 || len(HTTPFuncNameToPatternMap(h)) != 1 {
		t.Error(test.DiffMessage([3]int{len(HTTPRouterMap(h)), len(HTTPPatternToFuncNameMap(h)), len(HTTPFuncNameToPatternMap(h))}, [3]int{1, 1, 1}, "B18: the hook functions read what AddHandlerToRouterMap wrote"))
	}
}

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
