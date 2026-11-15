package common

import (
	"fmt"
	"sync"
	"sync/atomic"
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
		method, route, version := ParseFuncNameToURL(c.fn)
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
		method, _, _ := ParseFuncNameToURL(c.fn)
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
		_, route, version := ParseFuncNameToURL(c.fn)
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
		_, route, _ := ParseFuncNameToURL(c.fn)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, c.fn+" route"))
		}
	}
}

func TestParseFnNameToURL_ParamWithoutPath(t *testing.T) {
	_, route, _ := ParseFuncNameToURL("READ_BY_id")
	want := "/{id}/"
	if route != want {
		t.Error(test.DiffMessage(route, want, "READ_BY_id route"))
	}
}

type constructTestType struct{ id int }

var constructTestCallCount int

func (constructTestType) NewTestProvider() any {
	constructTestCallCount++
	return constructTestType{id: constructTestCallCount}
}

func TestConstruct_CachesSingletonPerType(t *testing.T) {
	constructTestCallCount = 0

	first := Construct(constructTestType{}, "NewTestProvider")
	second := Construct(constructTestType{}, "NewTestProvider")

	if constructTestCallCount != 1 {
		t.Error(test.DiffMessage(constructTestCallCount, 1, "constructor should run exactly once"))
	}
	if first != second {
		t.Error(test.DiffMessage(second, first, "Construct should return the cached singleton"))
	}
}

type constructNoCtorType struct{ v int }

func TestConstruct_MissingConstructorReturnsInputUnchanged(t *testing.T) {
	in := constructNoCtorType{v: 5}
	got := Construct(in, "DoesNotExist")
	if got != in {
		t.Error(test.DiffMessage(got, in, "Construct with unknown constructor should return input unchanged"))
	}
}

var constructSeq atomic.Int64

func constructUnique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, constructSeq.Add(1))
}

type constructRole struct{ Role string }

var constructRoleCalls atomic.Int64

func (g *constructRole) NewGuard() any {
	constructRoleCalls.Add(1)
	return &constructRole{Role: g.Role}
}

type constructPlain struct{ Role string }

func TestConstruct_DifferentValuesGetOwnInstance(t *testing.T) {
	admin, user := constructUnique("admin"), constructUnique("user")

	first := Construct(&constructRole{Role: admin}, "NewGuard").(*constructRole)
	second := Construct(&constructRole{Role: user}, "NewGuard").(*constructRole)

	if first.Role != admin {
		t.Error(test.DiffMessage(first.Role, admin, "B1: first configuration keeps its role"))
	}
	if second.Role != user {
		t.Error(test.DiffMessage(second.Role, user, "B1: second configuration must not get the first instance"))
	}
	if first == second {
		t.Error(test.DiffMessage(second, "a different instance", "B1: two configurations share no instance"))
	}
}

func TestConstruct_DifferentValuesWithoutConstructorGetOwnValue(t *testing.T) {
	admin, user := constructUnique("admin"), constructUnique("user")
	a := &constructPlain{Role: admin}
	b := &constructPlain{Role: user}

	first := Construct(a, "NewGuard")
	second := Construct(b, "NewGuard")

	if first != any(a) {
		t.Error(test.DiffMessage(first, a, "B1: no constructor returns the given value"))
	}
	if second != any(b) {
		t.Error(test.DiffMessage(second, b, "B1: a second configuration returns its own value"))
	}
}

func TestConstruct_EqualValuesShareInstance(t *testing.T) {
	role := constructUnique("shared")
	before := constructRoleCalls.Load()

	first := Construct(&constructRole{Role: role}, "NewGuard")
	second := Construct(&constructRole{Role: role}, "NewGuard")

	if calls := constructRoleCalls.Load() - before; calls != 1 {
		t.Error(test.DiffMessage(calls, int64(1), "B2: equal configurations run the constructor once"))
	}
	if first != second {
		t.Error(test.DiffMessage(second, first, "B2: equal configurations return the identical result"))
	}
}

func TestConstruct_SameNameDifferentTypesDoNotShare(t *testing.T) {
	role := constructUnique("same-name")

	newA := func() any {
		type guard struct{ Role string }
		return &guard{Role: role}
	}
	newB := func() any {
		type guard struct{ Role string }
		return &guard{Role: role}
	}
	a, b := newA(), newB()
	if fmt.Sprintf("%T", a) != fmt.Sprintf("%T", b) {
		t.Fatal(test.DiffMessage(fmt.Sprintf("%T", b), fmt.Sprintf("%T", a), "fixture: both types print the same name"))
	}

	first := Construct(a, "NewGuard")
	second := Construct(b, "NewGuard")

	if first != a {
		t.Error(test.DiffMessage(first, a, "B3: first type gets its own value"))
	}
	if second != b {
		t.Error(test.DiffMessage(second, b, "B3: a type with the same printed name must not get the other type's instance"))
	}
}

type constructBoth struct{ Role string }

var constructBothGuardCalls, constructBothMiddlewareCalls atomic.Int64

func (c *constructBoth) NewGuard() any {
	constructBothGuardCalls.Add(1)
	return "guard:" + c.Role
}

func (c *constructBoth) NewMiddleware() any {
	constructBothMiddlewareCalls.Add(1)
	return "middleware:" + c.Role
}

func TestConstruct_ConstructorNameIsPartOfKey(t *testing.T) {
	role := constructUnique("both")
	guardBefore := constructBothGuardCalls.Load()
	middlewareBefore := constructBothMiddlewareCalls.Load()

	guard := Construct(&constructBoth{Role: role}, "NewGuard")
	middleware := Construct(&constructBoth{Role: role}, "NewMiddleware")
	guardAgain := Construct(&constructBoth{Role: role}, "NewGuard")

	if guard != "guard:"+role {
		t.Error(test.DiffMessage(guard, "guard:"+role, "B4: NewGuard result"))
	}
	if middleware != "middleware:"+role {
		t.Error(test.DiffMessage(middleware, "middleware:"+role, "B4: NewMiddleware result must not be the NewGuard result"))
	}
	if guardAgain != guard {
		t.Error(test.DiffMessage(guardAgain, guard, "B4: the same constructor name is cached"))
	}
	if calls := constructBothGuardCalls.Load() - guardBefore; calls != 1 {
		t.Error(test.DiffMessage(calls, int64(1), "B4: NewGuard runs once"))
	}
	if calls := constructBothMiddlewareCalls.Load() - middlewareBefore; calls != 1 {
		t.Error(test.DiffMessage(calls, int64(1), "B4: NewMiddleware runs once"))
	}
}

func TestConstruct_MissingConstructorKeepsFirstValueOfConfiguration(t *testing.T) {
	role := constructUnique("missing")
	first := &constructPlain{Role: role}
	second := &constructPlain{Role: role}

	gotFirst := Construct(first, "DoesNotExist")
	gotSecond := Construct(second, "DoesNotExist")

	if gotFirst != any(first) {
		t.Error(test.DiffMessage(gotFirst, first, "B5: the first call returns its value"))
	}
	if gotSecond != any(first) {
		t.Error(test.DiffMessage(gotSecond, first, "B5: an equal configuration returns the first value"))
	}
}

type constructConcurrent struct{ Role string }

var constructConcurrentCalls atomic.Int64

func (c *constructConcurrent) NewGuard() any {
	constructConcurrentCalls.Add(1)
	return &constructConcurrent{Role: c.Role}
}

func TestConstruct_ConcurrentEqualValuesConstructOnce(t *testing.T) {
	const workers = 64
	role := constructUnique("concurrent")
	before := constructConcurrentCalls.Load()

	results := make([]any, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = Construct(&constructConcurrent{Role: role}, "NewGuard")
		}()
	}
	close(start)
	wg.Wait()

	if calls := constructConcurrentCalls.Load() - before; calls != 1 {
		t.Error(test.DiffMessage(calls, int64(1), "B6: the constructor runs exactly once"))
	}
	for i, result := range results {
		if result != results[0] {
			t.Error(test.DiffMessage(result, results[0], fmt.Sprintf("B6: worker %d gets the shared instance", i)))
		}
	}
}

func TestConstruct_ConcurrentDifferentValuesKeepTheirOwn(t *testing.T) {
	const workers = 64
	roles := make([]string, workers)
	for i := range roles {
		roles[i] = constructUnique("own")
	}

	results := make([]*constructRole, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = Construct(&constructRole{Role: roles[i]}, "NewGuard").(*constructRole)
		}()
	}
	close(start)
	wg.Wait()

	for i, result := range results {
		if result.Role != roles[i] {
			t.Error(test.DiffMessage(result.Role, roles[i], fmt.Sprintf("B1: worker %d gets its own configuration", i)))
		}
	}
}

type constructWithFunc struct {
	Role string
	Hook func()
}

var constructWithFuncCalls atomic.Int64

func (c *constructWithFunc) NewGuard() any {
	constructWithFuncCalls.Add(1)
	return &constructWithFunc{Role: c.Role, Hook: c.Hook}
}

func TestConstruct_ValuesWithFuncFieldsAreNeverShared(t *testing.T) {
	role := constructUnique("func")
	before := constructWithFuncCalls.Load()

	first := Construct(&constructWithFunc{Role: role, Hook: func() {}}, "NewGuard")
	second := Construct(&constructWithFunc{Role: role, Hook: func() {}}, "NewGuard")

	if calls := constructWithFuncCalls.Load() - before; calls != 2 {
		t.Error(test.DiffMessage(calls, int64(2), "B1: a value holding a func is never equal to another"))
	}
	if first == second {
		t.Error(test.DiffMessage(second, "a different instance", "B1: values with func fields get their own instance"))
	}
}

type constructWithChan struct {
	Role string
	Ch   chan int
}

var constructWithChanCalls atomic.Int64

func (c *constructWithChan) NewGuard() any {
	constructWithChanCalls.Add(1)
	return &constructWithChan{Role: c.Role, Ch: c.Ch}
}

func TestConstruct_ValuesWithDifferentChannelsAreNeverShared(t *testing.T) {
	role := constructUnique("chan")
	before := constructWithChanCalls.Load()

	first := Construct(&constructWithChan{Role: role, Ch: make(chan int)}, "NewGuard")
	second := Construct(&constructWithChan{Role: role, Ch: make(chan int)}, "NewGuard")

	if calls := constructWithChanCalls.Load() - before; calls != 2 {
		t.Error(test.DiffMessage(calls, int64(2), "B1: values with different channels are not equal"))
	}
	if first == second {
		t.Error(test.DiffMessage(second, "a different instance", "B1: values with different channels get their own instance"))
	}
}

func TestParseFnNameToURL_ThreeParamsKeepOrder(t *testing.T) {
	method, route, version := ParseFuncNameToURL("READ_x_BY_a_AND_b_AND_c")

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
	method, route, version := ParseFuncNameToURL("READ_BY_a_AND_b_AND_c_AND_d")

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
	method, route, version := ParseFuncNameToURL("READ_m_BY_a_AND_b_AND_c_OF_u_BY_d_AND_e_AND_f")

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
		{"SERVE_assets_ANY", "SERVE", "/assets*/", ""},
		{"READ_logo_png_FILE", "GET", "/logo.png/", ""},
		{"READ_VERSION_1_2", "GET", "/", "1_2"},
		{"READ_users_CREATE_x", "GET", "/users_CREATE_x/", ""},
		{"read_users", "", "", ""},
	}
	for _, c := range cases {
		method, route, version := ParseFuncNameToURL(c.fn)
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
