package common

import (
	"fmt"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

func TestGetFnName(t *testing.T) {
	cases := []struct {
		handler any
		want    string
	}{
		{fnTestController{}.READ_users, "READ_users"},
		{fnTestController{}.CREATE_orders, "CREATE_orders"},
	}
	for _, c := range cases {
		got := GetFuncName(c.handler)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "GetFuncName"))
		}
	}
}

func TestToWSEventName(t *testing.T) {
	cases := []struct {
		s, want string
	}{
		{"/message/", "message"},
		{"/room/events/", "room/events"},
		{"status", "status"},
		{"/nested/deep/", "nested/deep"},
		{"/event/", "event"},
	}
	for _, c := range cases {
		got := ToWSEventName(c.s)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "ToWSEventName"))
		}
	}
}

func TestParseFnNameToURL_AllHTTPMethods(t *testing.T) {
	cases := []struct {
		fn, wantMethod, wantRoute string
	}{
		{"READ_health", "GET", "/health/"},
		{"CREATE_users", "POST", "/users/"},
		{"UPDATE_users", "PUT", "/users/"},
		{"MODIFY_users", "PATCH", "/users/"},
		{"DELETE_users", "DELETE", "/users/"},
		{"PREFLIGHT_health", "OPTIONS", "/health/"},
	}
	for _, c := range cases {
		method, route, version := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if method != c.wantMethod {
			t.Error(test.DiffMessage(method, c.wantMethod, c.fn+" method"))
		}
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, c.fn+" route"))
		}
		if version != "" {
			t.Error(test.DiffMessage(version, "", c.fn+" version"))
		}
	}
}

func TestParseFnNameToURL_InvalidInput(t *testing.T) {
	cases := []struct {
		fn, wantMethod string
	}{
		{"INVALID_users", ""},
		{"lowercase_users", ""},
		{"", ""},
	}
	for _, c := range cases {
		method, _, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if method != c.wantMethod {
			t.Error(test.DiffMessage(method, c.wantMethod, c.fn+" method should be empty"))
		}
	}
}

func TestParseFnNameToURL_VersionExtraction(t *testing.T) {
	cases := []struct {
		fn, wantRoute, wantVersion string
	}{
		{"READ_users_VERSION_v1", "/users/", "v1"},
		{"READ_users_VERSION_V_12", "/users/", "V_12"},

		{"READ_users_VERSION_", "/users/", ""},

		{"READ_users_VERSION", "/users/", ""},
	}
	for _, c := range cases {
		_, route, version := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, c.fn+" route"))
		}
		if version != c.wantVersion {
			t.Error(test.DiffMessage(version, c.wantVersion, c.fn+" version"))
		}
	}
}

func TestParseFnNameToURL_BareOperation(t *testing.T) {
	cases := []struct {
		fn, wantRoute string
	}{
		{"READ", "/"},
		{"CREATE", "/"},
		{"READ_VERSION_v1", "/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, c.fn+" route"))
		}
	}
}

func TestParseFnNameToURL_ParamWithoutPath(t *testing.T) {
	_, route, _ := ParseFuncNameToURL("READ_BY_id", PathStyleKebab)
	want := "/{id}/"
	if route != want {
		t.Error(test.DiffMessage(route, want, "READ_BY_id route"))
	}
}

func TestParseFnNameToURL_ThreeParamsKeepOrder(t *testing.T) {
	method, route, version := ParseFuncNameToURL("READ_x_BY_a_AND_b_AND_c", PathStyleKebab)

	if method != "GET" {
		t.Error(test.DiffMessage(method, "GET", "B25: method"))
	}
	if route != "/x/{a}/{b}/{c}/" {
		t.Error(test.DiffMessage(route, "/x/{a}/{b}/{c}/", "B25: three parameters keep declaration order"))
	}
	if version != "" {
		t.Error(test.DiffMessage(version, "", "B25: version"))
	}
}

func TestParseFnNameToURL_FourParamsKeepOrder(t *testing.T) {
	method, route, version := ParseFuncNameToURL("READ_BY_a_AND_b_AND_c_AND_d", PathStyleKebab)

	if method != "GET" {
		t.Error(test.DiffMessage(method, "GET", "B25: method"))
	}
	if route != "/{a}/{b}/{c}/{d}/" {
		t.Error(test.DiffMessage(route, "/{a}/{b}/{c}/{d}/", "B25: four parameters without a resource keep order"))
	}
	if version != "" {
		t.Error(test.DiffMessage(version, "", "B25: version"))
	}
}

func TestParseFnNameToURL_ThreeParamsPerResource(t *testing.T) {
	method, route, version := ParseFuncNameToURL("READ_m_BY_a_AND_b_AND_c_OF_u_BY_d_AND_e_AND_f", PathStyleKebab)

	if method != "GET" {
		t.Error(test.DiffMessage(method, "GET", "B25: method"))
	}
	if route != "/u/{d}/{e}/{f}/m/{a}/{b}/{c}/" {
		t.Error(test.DiffMessage(route, "/u/{d}/{e}/{f}/m/{a}/{b}/{c}/", "B25: each resource keeps its own parameter order"))
	}
	if version != "" {
		t.Error(test.DiffMessage(version, "", "B25: version"))
	}
}

func TestParseFnNameToURL_FrozenCases(t *testing.T) {
	cases := []struct {
		fn, method, route, version string
	}{
		{"READ", "GET", "/", ""},
		{"READ_BY_ID_AND_NAME", "GET", "/{ID}/{NAME}/", ""},
		{"READ_users_BY_id_OF_posts_BY_post_id", "GET", "/posts/{post_id}/users/{id}/", ""},
		{"READ_ANY", "GET", "/*/", ""},
		{"SERVE_assets_ANY", "SERVE", "/assets/*/", ""},
		{"READ_logo_png_FILE", "GET", "/logo.png/", ""},
		{"READ_VERSION_1_2", "GET", "/", "1_2"},
		{"read_users", "", "", ""},
	}
	for _, c := range cases {
		method, route, version := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if method != c.method {
			t.Error(test.DiffMessage(method, c.method, "B26: "+c.fn+" method"))
		}
		if route != c.route {
			t.Error(test.DiffMessage(route, c.route, "B26: "+c.fn+" route"))
		}
		if version != c.version {
			t.Error(test.DiffMessage(version, c.version, "B26: "+c.fn+" version"))
		}
	}
}

func TestInjectProvidersIntoHTTPGuards_NoHandlersBindsAllRoutes(t *testing.T) {
	expectTargets(t, "B7", httpGuardsTargets(), []string{"READ_items", "READ_orders", "READ_users"})
}

func TestInjectProvidersIntoHTTPGuards_HandlersBindOnlyThoseRoutes(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given routes", httpGuardsTargets(c.READ_users, c.READ_items), []string{"READ_items", "READ_users"})
}

func TestInjectProvidersIntoHTTPGuards_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", httpGuardsTargets(c.READ_users, c.READ_users), []string{"READ_users"})
}

func TestInjectProvidersIntoHTTPGuards_OtherTransportHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: a WS handler binds nothing on the HTTP side", httpGuardsTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_status), []string{})
}

func TestInjectProvidersIntoHTTPGuards_RouteAndOtherTransportHandlerBindsRoute(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the route is bound", httpGuardsTargets(c.READ_orders, c.SUBSCRIBE_message), []string{"READ_orders"})
}

func TestInjectProvidersIntoHTTPGuards_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { httpGuardsTargets(c.Helper) },
		"invalid guard:", "mockGuarder", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoWSGuards_NoHandlersBindsAllEvents(t *testing.T) {
	expectTargets(t, "B7", wsGuardsTargets(), []string{"message", "status", "typing"})
}

func TestInjectProvidersIntoWSGuards_HandlersBindOnlyThoseEvents(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given events", wsGuardsTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_typing), []string{"message", "typing"})
}

func TestInjectProvidersIntoWSGuards_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", wsGuardsTargets(c.SUBSCRIBE_status, c.SUBSCRIBE_status), []string{"status"})
}

func TestInjectProvidersIntoWSGuards_HTTPHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: an HTTP handler binds nothing on the WS side", wsGuardsTargets(c.READ_users, c.READ_orders), []string{})
}

func TestInjectProvidersIntoWSGuards_EventAndHTTPHandlerBindsEvent(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the event is bound", wsGuardsTargets(c.SUBSCRIBE_status, c.READ_users), []string{"status"})
}

func TestInjectProvidersIntoWSGuards_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { wsGuardsTargets(c.Helper) },
		"invalid guard:", "mockWSGuarder", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoHTTPGuards_NilHandlerPanics(t *testing.T) {
	expectBindPanic(t, "B10", func() { httpGuardsTargets(nil) }, "invalid guard:", "mockGuarder")
}

func TestInjectProvidersIntoHTTPGuards_NonFuncHandlerPanics(t *testing.T) {
	expectBindPanic(t, "B10", func() { httpGuardsTargets(42) }, "invalid guard:", "is bound to 'int'")
}

func TestInjectProvidersIntoWSGuards_NilHandlerPanics(t *testing.T) {
	expectBindPanic(t, "B10", func() { wsGuardsTargets(nil) }, "invalid guard:", "mockWSGuarder")
}

func TestInjectProvidersIntoWSGuards_NonFuncHandlerPanics(t *testing.T) {
	expectBindPanic(t, "B10", func() { wsGuardsTargets("READ_users") }, "invalid guard:", "is bound to 'string'")
}

func TestInjectProvidersIntoHTTPInterceptors_NoHandlersBindsAllRoutes(t *testing.T) {
	expectTargets(t, "B7", httpInterceptorsTargets(), []string{"READ_items", "READ_orders", "READ_users"})
}

func TestInjectProvidersIntoHTTPInterceptors_HandlersBindOnlyThoseRoutes(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given routes", httpInterceptorsTargets(c.READ_users, c.READ_items), []string{"READ_items", "READ_users"})
}

func TestInjectProvidersIntoHTTPInterceptors_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", httpInterceptorsTargets(c.READ_users, c.READ_users), []string{"READ_users"})
}

func TestInjectProvidersIntoHTTPInterceptors_OtherTransportHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: a WS handler binds nothing on the HTTP side", httpInterceptorsTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_status), []string{})
}

func TestInjectProvidersIntoHTTPInterceptors_RouteAndOtherTransportHandlerBindsRoute(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the route is bound", httpInterceptorsTargets(c.READ_orders, c.SUBSCRIBE_message), []string{"READ_orders"})
}

func TestInjectProvidersIntoHTTPInterceptors_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { httpInterceptorsTargets(c.Helper) },
		"invalid interceptor:", "mockInterceptable", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoWSInterceptors_NoHandlersBindsAllEvents(t *testing.T) {
	expectTargets(t, "B7", wsInterceptorsTargets(), []string{"message", "status", "typing"})
}

func TestInjectProvidersIntoWSInterceptors_HandlersBindOnlyThoseEvents(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given events", wsInterceptorsTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_typing), []string{"message", "typing"})
}

func TestInjectProvidersIntoWSInterceptors_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", wsInterceptorsTargets(c.SUBSCRIBE_status, c.SUBSCRIBE_status), []string{"status"})
}

func TestInjectProvidersIntoWSInterceptors_HTTPHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: an HTTP handler binds nothing on the WS side", wsInterceptorsTargets(c.READ_users, c.READ_orders), []string{})
}

func TestInjectProvidersIntoWSInterceptors_EventAndHTTPHandlerBindsEvent(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the event is bound", wsInterceptorsTargets(c.SUBSCRIBE_status, c.READ_users), []string{"status"})
}

func TestInjectProvidersIntoWSInterceptors_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { wsInterceptorsTargets(c.Helper) },
		"invalid interceptor:", "mockWSInterceptable", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoHTTPExceptionFilters_NoHandlersBindsAllRoutes(t *testing.T) {
	expectTargets(t, "B7", httpExceptionFiltersTargets(), []string{"READ_items", "READ_orders", "READ_users"})
}

func TestInjectProvidersIntoHTTPExceptionFilters_HandlersBindOnlyThoseRoutes(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given routes", httpExceptionFiltersTargets(c.READ_users, c.READ_items), []string{"READ_items", "READ_users"})
}

func TestInjectProvidersIntoHTTPExceptionFilters_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", httpExceptionFiltersTargets(c.READ_users, c.READ_users), []string{"READ_users"})
}

func TestInjectProvidersIntoHTTPExceptionFilters_OtherTransportHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: a WS handler binds nothing on the HTTP side", httpExceptionFiltersTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_status), []string{})
}

func TestInjectProvidersIntoHTTPExceptionFilters_RouteAndOtherTransportHandlerBindsRoute(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the route is bound", httpExceptionFiltersTargets(c.READ_orders, c.SUBSCRIBE_message), []string{"READ_orders"})
}

func TestInjectProvidersIntoHTTPExceptionFilters_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { httpExceptionFiltersTargets(c.Helper) },
		"invalid exception filter:", "mockExFilter", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoWSExceptionFilters_NoHandlersBindsAllEvents(t *testing.T) {
	expectTargets(t, "B7", wsExceptionFiltersTargets(), []string{"message", "status", "typing"})
}

func TestInjectProvidersIntoWSExceptionFilters_HandlersBindOnlyThoseEvents(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given events", wsExceptionFiltersTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_typing), []string{"message", "typing"})
}

func TestInjectProvidersIntoWSExceptionFilters_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", wsExceptionFiltersTargets(c.SUBSCRIBE_status, c.SUBSCRIBE_status), []string{"status"})
}

func TestInjectProvidersIntoWSExceptionFilters_HTTPHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: an HTTP handler binds nothing on the WS side", wsExceptionFiltersTargets(c.READ_users, c.READ_orders), []string{})
}

func TestInjectProvidersIntoWSExceptionFilters_EventAndHTTPHandlerBindsEvent(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the event is bound", wsExceptionFiltersTargets(c.SUBSCRIBE_status, c.READ_users), []string{"status"})
}

func TestInjectProvidersIntoWSExceptionFilters_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { wsExceptionFiltersTargets(c.Helper) },
		"invalid exception filter:", "mockWSExFilter", "is bound to 'Helper', which is not a handler of the controller")
}

func TestInjectProvidersIntoHTTPMiddlewares_NoHandlersBindsAllRoutes(t *testing.T) {
	expectTargets(t, "B7", httpMiddlewaresTargets(), []string{"READ_items", "READ_orders", "READ_users"})
}

func TestInjectProvidersIntoHTTPMiddlewares_HandlersBindOnlyThoseRoutes(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given routes", httpMiddlewaresTargets(c.READ_users, c.READ_items), []string{"READ_items", "READ_users"})
}

func TestInjectProvidersIntoHTTPMiddlewares_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", httpMiddlewaresTargets(c.READ_users, c.READ_users), []string{"READ_users"})
}

func TestInjectProvidersIntoHTTPMiddlewares_OtherTransportHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: a WS handler binds nothing on the HTTP side", httpMiddlewaresTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_status), []string{})
}

func TestInjectProvidersIntoHTTPMiddlewares_RouteAndOtherTransportHandlerBindsRoute(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the route is bound", httpMiddlewaresTargets(c.READ_orders, c.SUBSCRIBE_message), []string{"READ_orders"})
}

func TestInjectProvidersIntoHTTPMiddlewares_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { httpMiddlewaresTargets(c.Helper) },
		"invalid middleware:", "mockMiddlewareFn", "is bound to 'Helper', which is not a handler of the controller")
}

func TestParseFuncNameToURL_Kebab(t *testing.T) {
	expectRoutes(t, "B1, B2", PathStyleKebab, func(c routeCase) string { return c.kebab })
}

func TestParseFuncNameToURL_AsWritten(t *testing.T) {
	expectRoutes(t, "B1, B2", PathStyleAsWritten, func(c routeCase) string { return c.written })
}

func TestParseFuncNameToURL_KeepsMethodForEveryStyle(t *testing.T) {
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		method, _, _ := ParseFuncNameToURL("MODIFY_dm_events", style)
		if method != "PATCH" {
			t.Error(test.DiffMessage(method, "PATCH", "B1: method"))
		}
	}
}

func TestParseFuncNameToURL_ParamNameVerbatim(t *testing.T) {
	cases := []struct{ fn, route string }{
		{"READ_users_BY_user_id", "/users/{user_id}/"},
		{"READ_users_BY_user__id", "/users/{user__id}/"},
		{"READ_users_BY__id", "/users/{_id}/"},
		{"READ_BY_ID", "/{ID}/"},
	}
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		for _, c := range cases {
			_, route, _ := ParseFuncNameToURL(c.fn, style)
			if route != c.route {
				t.Error(test.DiffMessage(route, c.route, fmt.Sprintf("B3, B21: %s style %d", c.fn, style)))
			}
		}
	}
	_, route, _ := ParseFuncNameToURL("READ_users_BY_id__OF_big_orgs", PathStyleKebab)
	if route != "/big-orgs/users/{id_}/" {
		t.Error(test.DiffMessage(route, "/big-orgs/users/{id_}/", "B3: trailing _ stays in the parameter, kebab resource"))
	}
	_, route, _ = ParseFuncNameToURL("READ_users_BY_id__OF_big_orgs", PathStyleAsWritten)
	if route != "/big_orgs/users/{id_}/" {
		t.Error(test.DiffMessage(route, "/big_orgs/users/{id_}/", "B3: trailing _ stays in the parameter, as written resource"))
	}
}

func TestParseFuncNameToURL_VersionUntouched(t *testing.T) {
	cases := []struct{ fn, version string }{
		{"READ_user_profile_VERSION_v1_v1", "v1_v1"},
		{"READ_users_VERSION_V2", "V2"},
		{"READ_users_VERSION_V_12", "V_12"},
		{"READ_x_VERSION_112____3", "112_3"},
		{"READ_users_VERSION", ""},
		{"READ_users_VERSION_", ""},
		{"READ_users_VERSION_2", "2"},
	}
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		for _, c := range cases {
			_, _, version := ParseFuncNameToURL(c.fn, style)
			if version != c.version {
				t.Error(test.DiffMessage(version, c.version, "B4: version of "+c.fn))
			}
		}
	}
	_, route, _ := ParseFuncNameToURL("READ_user_profile_VERSION_v1_v1", PathStyleKebab)
	if route != "/user-profile/" {
		t.Error(test.DiffMessage(route, "/user-profile/", "B4: the route before VERSION is kebab-cased"))
	}
}

func TestParseFuncNameToURL_AnyJoints(t *testing.T) {
	cases := []struct{ fn, kebab, written string }{
		{"READ_me_ANY_bers", "/me*bers/", "/me*bers/"},
		{"READ_big_me_ANY_bers_x", "/big-me*bers-x/", "/big_me*bers_x/"},
		{"READ_a_b_ANY", "/a-b*/", "/a_b*/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.kebab {
			t.Error(test.DiffMessage(route, c.kebab, "B5: kebab "+c.fn))
		}
		_, route, _ = ParseFuncNameToURL(c.fn, PathStyleAsWritten)
		if route != c.written {
			t.Error(test.DiffMessage(route, c.written, "B5: as written "+c.fn))
		}
	}
}

func TestParseFuncNameToURL_ServeAnyKebab(t *testing.T) {
	_, byName, _ := ParseFuncNameToURL("SERVE_static_files_ANY", PathStyleKebab)
	_, byOf, _ := ParseFuncNameToURL("SERVE_ANY_OF_static_files", PathStyleKebab)
	if byName != "/static-files/*/" || byOf != byName {
		t.Error(test.DiffMessage([]string{byName, byOf}, []string{"/static-files/*/", "/static-files/*/"}, "B6: the name form and the OF form give one route"))
	}
}

func TestParseFuncNameToURL_FileKebab(t *testing.T) {
	cases := []struct{ fn, route string }{
		{"READ_site_map_xml_FILE", "/site-map.xml/"},
		{"READ_a_b_FILE_c", "/a.b-c/"},
		{"READ_ANY_html_FILE", "/*.html/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.route {
			t.Error(test.DiffMessage(route, c.route, "B7: "+c.fn))
		}
	}
}

func TestParseFuncNameToURL_FileAfterAnyKeepsPrefix(t *testing.T) {
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		_, route, _ := ParseFuncNameToURL("READ_img_ANY_png_FILE", style)
		if route != "/img*.png/" {
			t.Error(test.DiffMessage(route, "/img*.png/", "B21: FILE after ANY keeps the text before the wildcard"))
		}
	}
}

func TestParseFuncNameToURL_EmptyWordsDropped(t *testing.T) {
	cases := []struct{ fn, kebab, written string }{
		{"READ_user__profile", "/user-profile/", "/user_profile/"},
		{"READ__users", "/users/", "/users/"},
		{"READ_users_", "/users/", "/users/"},
		{"READ_a___b", "/a-b/", "/a_b/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.kebab {
			t.Error(test.DiffMessage(route, c.kebab, "B8: kebab "+c.fn))
		}
		_, route, _ = ParseFuncNameToURL(c.fn, PathStyleAsWritten)
		if route != c.written {
			t.Error(test.DiffMessage(route, c.written, "B8: as written "+c.fn))
		}
	}
}

func TestParseFuncNameToURL_UpperCaseWordPanics(t *testing.T) {
	cases := []struct{ fn, word string }{
		{"READ_orgs_FB", "FB"},
		{"READ_FB", "FB"},
		{"READ_orgs_V2", "V2"},
		{"READ_plan_B", "B"},
		{"READ_URL_shortener", "URL"},
		{"READ_ÉCOLE", "ÉCOLE"},
		{"READ_users_CREATE_x", "CREATE"},
		{"READ_ALL", "ALL"},
		{"READ_logo_PNG_FILE", "PNG"},
		{"READ_ANY_HTML_FILE", "HTML"},
	}
	for _, c := range cases {
		expectRoutePanic(t, "B9", c.fn, upperCaseMessage(c.word, c.fn))
	}
}

func TestParseFuncNameToURL_UpperCaseAllowed(t *testing.T) {
	names := []string{
		"READ_orgsFB", "READ_apiV2", "READ_Users", "READ_404", "READ_BY_ID",
		"READ_users_VERSION_V2", "READ_users_BY_ID", "READ_users_BY_user_ID_AND_NAME",
		"READ_x_VERSION_NEUTRAL",
	}
	for _, name := range names {
		for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
			text, panicked := routePanicText(func() { ParseFuncNameToURL(name, style) })
			if panicked {
				t.Error(test.DiffMessage(text, "no panic", "B9: "+name+" is allowed"))
			}
		}
	}
}

func TestParseFuncNameToURL_NotARouteNeverChecked(t *testing.T) {
	for _, name := range []string{"Helper_FB", "findUser", "Helper", "CREATEUSER_FB", "read_FB"} {
		var method, route, version string
		text, panicked := routePanicText(func() { method, route, version = ParseFuncNameToURL(name, PathStyleKebab) })
		if panicked {
			t.Error(test.DiffMessage(text, "no panic", "B10: "+name+" is not a route"))
		}
		if method != "" || route != "" || version != "" {
			t.Error(test.DiffMessage([]string{method, route, version}, []string{"", "", ""}, "B10: "+name))
		}
	}
}

func TestParseFuncNameToURL_MalformedPanics(t *testing.T) {
	cases := []struct{ rule, fn, want string }{
		{"B13", "READ_users_BY", "invalid route: BY in READ_users_BY has no parameter name"},
		{"B13", "READ_users_BY_OF_x", "invalid route: BY in READ_users_BY_OF_x has no parameter name"},
		{"B13", "READ_users_BY_id_AND", "invalid route: AND in READ_users_BY_id_AND has no parameter name"},
		{"B13", "DELETE_BY_ID_AND_VERSION", "invalid route: AND in DELETE_BY_ID_AND_VERSION has no parameter name"},
		{"B14", "READ_files_BY_name_ANY_x", "invalid route: x in READ_files_BY_name_ANY_x comes after a parameter; start the next resource with OF"},
		{"B14", "READ_users_BY_id_FILE", "invalid route: FILE in READ_users_BY_id_FILE comes after a parameter; start the next resource with OF"},
		{"B15", "READ_OF_users", "invalid route: OF in READ_OF_users needs a resource or a parameter on both sides"},
		{"B15", "READ_users_OF", "invalid route: OF in READ_users_OF needs a resource or a parameter on both sides"},
		{"B15", "READ_a_OF_OF_b", "invalid route: OF in READ_a_OF_OF_b needs a resource or a parameter on both sides"},
		{"B15", "SERVE_ANY_OF_", "invalid route: OF in SERVE_ANY_OF_ needs a resource or a parameter on both sides"},
		{"B16", "READ_txt_FILE", "invalid route: FILE in READ_txt_FILE needs a name and an extension before it"},
		{"B16", "READ_FILE", "invalid route: FILE in READ_FILE needs a name and an extension before it"},
		{"B16", "READ_FILE_ANY", "invalid route: FILE in READ_FILE_ANY needs a name and an extension before it"},
		{"B16", "READ_logo_png_FILE_FILE", "invalid route: FILE twice in one resource of READ_logo_png_FILE_FILE"},
		{"B16", "READ_ANY_ANY_FILE", "invalid route: ANY twice in a row in READ_ANY_ANY_FILE; one ANY already matches any text"},
		{"B16", "READ_logo_ANY_FILE", "invalid route: FILE in READ_logo_ANY_FILE needs a word as the extension, not ANY"},
		{"B16", "SERVE_x_ANY_ANY", "invalid route: ANY twice in a row in SERVE_x_ANY_ANY; one ANY already matches any text"},
	}
	for _, c := range cases {
		expectRoutePanic(t, c.rule, c.fn, c.want)
	}
}

func TestParseFuncNameToURL_ParamBeforeOfIsAllowed(t *testing.T) {
	for _, name := range []string{"READ_BY_id_OF_users", "READ_users_BY_id_OF_orgs", "READ_users_AND_id"} {
		text, panicked := routePanicText(func() { ParseFuncNameToURL(name, PathStyleKebab) })
		if panicked {
			t.Error(test.DiffMessage(text, "no panic", "B15: "+name))
		}
	}
	_, route, _ := ParseFuncNameToURL("READ_users_AND_id", PathStyleKebab)
	if route != "/users/{id}/" {
		t.Error(test.DiffMessage(route, "/users/{id}/", "B13: AND is interchangeable with BY"))
	}
}

func TestParseFuncNameToURL_AnyAfterParam(t *testing.T) {
	cases := []struct{ fn, route string }{
		{"READ_files_BY_name_ANY", "/files/{name}/*/"},
		{"READ_files_BY_dir_AND_name_ANY", "/files/{dir}/{name}/*/"},
		{"READ_files_BY_name_ANY_OF_x", "/x/files/{name}/*/"},
		{"SERVE_files_BY_name_ANY", "/files/{name}/*/"},
		{"READ_BY_name_ANY", "/{name}/*/"},
	}
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		for _, c := range cases {
			_, route, _ := ParseFuncNameToURL(c.fn, style)
			if route != c.route {
				t.Error(test.DiffMessage(route, c.route, "B20: "+c.fn))
			}
		}
	}
	method, _, _ := ParseFuncNameToURL("SERVE_files_BY_name_ANY", PathStyleKebab)
	if method != "SERVE" {
		t.Error(test.DiffMessage(method, "SERVE", "B20: the method stays SERVE"))
	}
}

func TestParseFuncNameToURL_ResourceWithoutWordsLeavesNoEmptySegment(t *testing.T) {
	_, route, _ := ParseFuncNameToURL("READ_BY_id_OF_users", PathStyleKebab)
	if route != "/users/{id}/" {
		t.Error(test.DiffMessage(route, "/users/{id}/", "B21: no empty segment"))
	}
}

func TestParseFuncNameToURL_StillEnclosedInSlashes(t *testing.T) {
	for _, c := range routeCases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if !strings.HasPrefix(route, "/") || !strings.HasSuffix(route, "/") || strings.Contains(route, "//") {
			t.Error(test.DiffMessage(route, "/…/ with no empty segment", "B21: "+c.fn))
		}
	}
}

func TestParseFuncNameToURL_ServeTrailingAnyIsSegment(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"SERVE_static_ANY", "/static/*/"},
		{"SERVE_a_b_ANY", "/a-b/*/"},
		{"SERVE_assets_ANY_OF_public", "/public/assets/*/"},
	}
	for _, c := range cases {
		method, route, version := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if method != "SERVE" {
			t.Error(test.DiffMessage(method, "SERVE", "V1: "+c.fn+" method"))
		}
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V1: "+c.fn+" route"))
		}
		if version != "" {
			t.Error(test.DiffMessage(version, "", "V1: "+c.fn+" version"))
		}
	}
}

func TestParseFuncNameToURL_ServeAnyBetweenNamesIsGlob(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"SERVE_ANY", "/*/"},
		{"SERVE_ANY_OF_static", "/static/*/"},
		{"SERVE_me_ANY_bers", "/me*bers/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V2: "+c.fn+" route"))
		}
	}
}

func TestParseFuncNameToURL_ReadTrailingAnyStaysGlob(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"READ_assets_ANY", "/assets*/"},
		{"CREATE_assets_ANY", "/assets*/"},
		{"READ_a_b_ANY", "/a-b*/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V2: "+c.fn+" route"))
		}
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

func TestParseWSFnNameToEvent_Exact(t *testing.T) {
	cases := []struct {
		fn, want string
	}{
		{"SUBSCRIBE_message", "message"},
		{"SUBSCRIBE_chat_message", "chat.message"},
		{"SUBSCRIBE_room_events", "room.events"},
		{"SUBSCRIBE_a_b_c", "a.b.c"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok {
			t.Error(test.DiffMessage(ok, true, c.fn+" should be valid WS fn"))
		}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, c.fn+" pattern"))
		}
	}
}

func TestParseWSFnNameToEvent_Wildcards(t *testing.T) {
	cases := []struct {
		fn, want string
	}{
		{"SUBSCRIBE_chat_ANY", "chat.*"},
		{"SUBSCRIBE_chat_ANY_message", "chat.*.message"},
		{"SUBSCRIBE_ANY", "*"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok {
			t.Error(test.DiffMessage(ok, true, c.fn+" should be valid WS fn"))
		}
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, c.fn+" pattern"))
		}
	}
}

func TestParseWSFnNameToEvent_Invalid(t *testing.T) {
	cases := []string{
		"READ_users",
		"CREATE_orders",
		"",
		"SUBSCRIBE",
		"SUBSCRIBE_",
		"lowercase_message",
	}
	for _, fn := range cases {
		_, ok := ParseWSFuncNameToEvent(fn)
		if ok {
			t.Error(test.DiffMessage(ok, false, fn+" should not be valid WS fn"))
		}
	}
}

func TestParseWSFnNameToEvent_CaseNormalization(t *testing.T) {
	got, ok := ParseWSFuncNameToEvent("SUBSCRIBE_Chat_Message")
	if !ok {
		t.Error(test.DiffMessage(ok, true, "SUBSCRIBE_Chat_Message should parse"))
	}
	if got != "chat.message" {
		t.Error(test.DiffMessage(got, "chat.message", "tokens lowercased"))
	}
}

func TestParseWSFnNameToEvent_All(t *testing.T) {
	cases := []struct{ fn, want string }{
		{"SUBSCRIBE_chat_ALL", "chat.**"},
		{"SUBSCRIBE_ALL", "**"},
		{"SUBSCRIBE_chat_ANY_ALL", "chat.*.**"},
		{"SUBSCRIBE_chat_ANY", "chat.*"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok || got != c.want {
			t.Error(test.DiffMessage(got, c.want, "B18: "+c.fn))
		}
	}
}

func TestParseWSFnNameToEvent_AllNotLastPanics(t *testing.T) {
	for _, fn := range []string{"SUBSCRIBE_chat_ALL_x", "SUBSCRIBE_ALL_chat", "SUBSCRIBE_ALL_ANY"} {
		want := "invalid route: ALL in " + fn + " must be the last word"
		text, panicked := routePanicText(func() { ParseWSFuncNameToEvent(fn) })
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "B19: "+fn))
		}
	}
}

func TestParseWSFnNameToEvent_UpperCaseWordPanics(t *testing.T) {
	cases := []struct{ fn, word string }{
		{"SUBSCRIBE_chat_PERSON_ANY", "PERSON"},
		{"SUBSCRIBE_chat_V2", "V2"},
		{"SUBSCRIBE_FB", "FB"},
	}
	for _, c := range cases {
		text, panicked := routePanicText(func() { ParseWSFuncNameToEvent(c.fn) })
		if want := upperCaseMessage(c.word, c.fn); !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, "B9: "+c.fn))
		}
	}
}

func TestParseWSFnNameToEvent_UpperCaseAllowedAndOthersIgnored(t *testing.T) {
	cases := []struct{ fn, want string }{
		{"SUBSCRIBE_Chat_Message", "chat.message"},
		{"SUBSCRIBE_chatRoom", "chatroom"},
		{"SUBSCRIBE_chat_ANY_message", "chat.*.message"},
	}
	for _, c := range cases {
		got, ok := ParseWSFuncNameToEvent(c.fn)
		if !ok || got != c.want {
			t.Error(test.DiffMessage(got, c.want, "B9: "+c.fn))
		}
	}
	for _, fn := range []string{"READ_orgs_FB", "Helper_FB", "SUBSCRIBE", "subscribe_FB"} {
		text, panicked := routePanicText(func() { ParseWSFuncNameToEvent(fn) })
		if panicked {
			t.Error(test.DiffMessage(text, "no panic", "B10: "+fn+" is not a WebSocket route"))
		}
	}
}
