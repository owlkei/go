package common

import (
	"testing"

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

func TestParseFnNameToURL(t *testing.T) {
	testCases := make(map[string][]string)

	testCases["READ_members_BY_user_name_AND_member_id_OF_club_users_BY_id"] = []string{
		"GET",
		"/club-users/{id}/members/{user_name}/{member_id}/",
		"",
	}

	testCases["UPDATE_products_BY_productId_AND_productRanks_OF_categories_BY_categoryId_AND_categoryRank_OF_shops_BY_shopId_AND_shopRanks_VERSION_V_12"] = []string{
		"PUT",
		"/shops/{shopId}/{shopRanks}/categories/{categoryId}/{categoryRank}/products/{productId}/{productRanks}/",
		"V_12",
	}

	testCases["CREATE_owned_lists_OF_users_BY_id_VERSION_"] = []string{
		"POST",
		"/users/{id}/owned-lists/",
		"",
	}

	testCases["UPDATE_ANY_OF_members_OF_ANY_OF_users_BY_id_VERSION_NEUTRAL"] = []string{
		"PUT",
		"/users/{id}/*/members/*/",
		"NEUTRAL",
	}

	testCases["READ_ANY_html_FILE_OF_members_OF_ANY_jpeg_FILE_VERSION_112____3"] = []string{
		"GET",
		"/*.jpeg/members/*.html/",
		"112_3",
	}

	testCases["DELETE_image_png_FILE_OF_members_OF_users_BY_id_VERSION_NEUTRAL__"] = []string{
		"DELETE",
		"/users/{id}/members/image.png/",
		"NEUTRAL",
	}

	testCases["MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2"] = []string{
		"PATCH",
		"/dm-conversations/with/{participant_id}/dm-events/",
		"V2",
	}

	testCases["READ_me_ANY_bers_OF_us_ANY_ers_BY_id_VERSION_v1_v1"] = []string{
		"GET",
		"/us*ers/{id}/me*bers/",
		"v1_v1",
	}

	for fn, results := range testCases {
		method, route, version := ParseFuncNameToURL(fn, PathStyleKebab)
		if method != results[0] {
			t.Error(test.DiffMessage(results[0], method, "method should be equal"))
		}

		if route != results[1] {
			t.Error(test.DiffMessage(results[1], route, "route should be equal"))
		}

		if version != results[2] {
			t.Error(test.DiffMessage(results[2], version, "version should be equal"))
		}
	}
}

func routeConflictPanic(fn func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				message = err.Error()
			}
		}
	}()
	fn()
	return ""
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

func TestHooks_EventMapAccessors(t *testing.T) {
	ws := &WS{}
	AddHandlerToEventMap(ws, "SUBSCRIBE_chat_ANY", nil)
	if _, ok := WSEventMap(ws)["chat.*"]; !ok {
		t.Error(test.DiffMessage(WSEventMap(ws), "<map with chat.*>", "B18: WSEventMap reads what AddHandlerToEventMap wrote"))
	}
}
