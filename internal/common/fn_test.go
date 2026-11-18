package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type fnTestController struct{}

func (fnTestController) READ_users()    {}
func (fnTestController) CREATE_orders() {}

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
