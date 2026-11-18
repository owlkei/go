package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type routeCase struct {
	fn, kebab, written string
}

var routeCases = []routeCase{
	{"READ", "/", "/"},
	{"READ_users", "/users/", "/users/"},
	{"READ_user_profile", "/user-profile/", "/user_profile/"},
	{"READ_payment_intents", "/payment-intents/", "/payment_intents/"},
	{"READ_userProfile", "/userProfile/", "/userProfile/"},
	{"READ_user__profile", "/user-profile/", "/user_profile/"},
	{"READ__users", "/users/", "/users/"},
	{"READ_users_", "/users/", "/users/"},
	{"READ_profile_OF_users", "/users/profile/", "/users/profile/"},
	{"READ_user_profile_OF_big_orgs", "/big-orgs/user-profile/", "/big_orgs/user_profile/"},
	{"READ_orders_2024", "/orders-2024/", "/orders_2024/"},
	{"READ_404", "/404/", "/404/"},
	{"READ_v2_users", "/v2-users/", "/v2_users/"},
	{"READ_2fa_codes", "/2fa-codes/", "/2fa_codes/"},
	{"READ_Users", "/Users/", "/Users/"},
	{"READ_orgsFB", "/orgsFB/", "/orgsFB/"},
	{"READ_apiV2", "/apiV2/", "/apiV2/"},
	{"READ_é_cafés", "/é-cafés/", "/é_cafés/"},
	{"READ_BY_ID", "/{ID}/", "/{ID}/"},
	{"READ_users_BY_user_id", "/users/{user_id}/", "/users/{user_id}/"},
	{"READ_posts_BY_post_id_OF_users_BY_user_id", "/users/{user_id}/posts/{post_id}/", "/users/{user_id}/posts/{post_id}/"},
	{"READ_members_BY_user_name_AND_member_id_OF_club_users_BY_id", "/club-users/{id}/members/{user_name}/{member_id}/", "/club_users/{id}/members/{user_name}/{member_id}/"},
	{"MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2", "/dm-conversations/with/{participant_id}/dm-events/", "/dm_conversations/with/{participant_id}/dm_events/"},
	{"CREATE_owned_lists_OF_users_BY_id_VERSION_", "/users/{id}/owned-lists/", "/users/{id}/owned_lists/"},
	{"READ_ANY", "/*/", "/*/"},
	{"READ_assets_ANY", "/assets*/", "/assets*/"},
	{"READ_a_b_ANY", "/a-b*/", "/a_b*/"},
	{"READ_me_ANY_bers", "/me*bers/", "/me*bers/"},
	{"READ_big_me_ANY_bers_x", "/big-me*bers-x/", "/big_me*bers_x/"},
	{"SERVE_ANY", "/*/", "/*/"},
	{"SERVE_static_ANY", "/static/*/", "/static/*/"},
	{"SERVE_ANY_OF_static", "/static/*/", "/static/*/"},
	{"SERVE_a_b_ANY", "/a-b/*/", "/a_b/*/"},
	{"SERVE_static_files_ANY", "/static-files/*/", "/static_files/*/"},
	{"SERVE_ANY_OF_static_files", "/static-files/*/", "/static_files/*/"},
	{"SERVE_assets_ANY_OF_public", "/public/assets/*/", "/public/assets/*/"},
	{"SERVE_me_ANY_bers", "/me*bers/", "/me*bers/"},
	{"READ_robots_txt_FILE", "/robots.txt/", "/robots.txt/"},
	{"READ_logo_png_FILE", "/logo.png/", "/logo.png/"},
	{"READ_site_map_xml_FILE", "/site-map.xml/", "/site_map.xml/"},
	{"READ_my_logo_png_FILE_OF_static_files", "/static-files/my-logo.png/", "/static_files/my_logo.png/"},
	{"READ_a_b_FILE_c", "/a.b-c/", "/a.b_c/"},
	{"READ_ANY_html_FILE", "/*.html/", "/*.html/"},
	{"READ_ANY_html_FILE_OF_members_OF_ANY_jpeg_FILE_VERSION_112____3", "/*.jpeg/members/*.html/", "/*.jpeg/members/*.html/"},
	{"READ_img_ANY_png_FILE", "/img*.png/", "/img*.png/"},
	{"READ_BY_id_OF_users", "/users/{id}/", "/users/{id}/"},
}

func expectRoutes(t *testing.T, rule string, style PathStyle, pick func(routeCase) string) {
	t.Helper()
	for _, c := range routeCases {
		_, route, _ := ParseFuncNameToURL(c.fn, style)
		if want := pick(c); route != want {
			t.Error(test.DiffMessage(route, want, rule+": route of "+c.fn))
		}
	}
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

func routePanicText(fn func()) (string, bool) {
	value, panicked := recoverValue(fn)
	if !panicked {
		return "", false
	}
	return fmt.Sprint(value), true
}

func expectRoutePanic(t *testing.T, rule, name, want string) {
	t.Helper()
	for _, style := range []PathStyle{PathStyleKebab, PathStyleAsWritten} {
		text, panicked := routePanicText(func() { ParseFuncNameToURL(name, style) })
		if !panicked || !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, want, fmt.Sprintf("%s: %s style %d", rule, name, style)))
		}
	}
}

func upperCaseMessage(word, name string) string {
	return "invalid route: " + word + " in " + name + " is upper case but not a route word; write it in lower case or camelCase"
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
