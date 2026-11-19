package common

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/internal/aggregation"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/routing"
	"go.owlky.com/owlky/internal/test"
)

type bindController struct{}

func (bindController) READ_users() {}

func (bindController) READ_orders() {}

func (bindController) READ_items() {}

func (bindController) SUBSCRIBE_message() {}

func (bindController) SUBSCRIBE_status() {}

func (bindController) SUBSCRIBE_typing() {}

func (bindController) Helper() {}

func bindRoutes() *HTTP {
	return buildHTTP(map[string]string{
		"READ_users":  "/users/",
		"READ_orders": "/orders/",
		"READ_items":  "/items/",
	})
}

func bindEvents() *WS {
	return buildWS(map[string]string{
		"message": "SUBSCRIBE_message",
		"status":  "SUBSCRIBE_status",
		"typing":  "SUBSCRIBE_typing",
	})
}

func recoverText(fn func()) (string, bool) {
	var text string
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				text = fmt.Sprint(r)
				panicked = true
			}
		}()
		fn()
	}()
	return text, panicked
}

func expectBindPanic(t *testing.T, rule string, fn func(), wants ...string) {
	t.Helper()
	text, panicked := recoverText(fn)
	if !panicked {
		t.Error(test.DiffMessage("no panic", "a panic at Create()", rule+": a layer bound to a non-handler must panic"))
		return
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Error(test.DiffMessage(text, "contains "+want, rule+": panic message"))
		}
	}
}

func expectTargets(t *testing.T, rule string, actual, expected []string) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Error(test.DiffMessage(actual, expected, rule))
	}
}

func sortedNames(names []string) []string {
	sort.Strings(names)
	return names
}

func httpGuardsTargets(handlers ...any) []string {
	l := &Guard{}
	l.BindGuard(mockGuarder{}, handlers...)
	items := InjectProvidersIntoHTTPGuards(l, bindRoutes(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsGuardsTargets(handlers ...any) []string {
	l := &Guard{}
	l.BindGuard(mockWSGuarder{}, handlers...)
	items := InjectProvidersIntoWSGuards(l, bindEvents(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
}

func httpInterceptorsTargets(handlers ...any) []string {
	l := &Interceptor{}
	l.BindInterceptor(mockInterceptable{}, handlers...)
	items := InjectProvidersIntoHTTPInterceptors(l, bindRoutes(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsInterceptorsTargets(handlers ...any) []string {
	l := &Interceptor{}
	l.BindInterceptor(mockWSInterceptable{}, handlers...)
	items := InjectProvidersIntoWSInterceptors(l, bindEvents(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
}

func httpExceptionFiltersTargets(handlers ...any) []string {
	l := &ExceptionFilter{}
	l.BindExceptionFilter(mockExFilter{}, handlers...)
	items := InjectProvidersIntoHTTPExceptionFilters(l, bindRoutes(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsExceptionFiltersTargets(handlers ...any) []string {
	l := &ExceptionFilter{}
	l.BindExceptionFilter(mockWSExFilter{}, handlers...)
	items := InjectProvidersIntoWSExceptionFilters(l, bindEvents(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
}

func httpMiddlewaresTargets(handlers ...any) []string {
	l := &Middleware{}
	l.BindMiddleware(mockMiddlewareFn{}, handlers...)
	items := InjectProvidersIntoHTTPMiddlewares(l, bindRoutes(), noopCB, testConstruct)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func buildBenchHTTP(n int) *HTTP {
	r := &HTTP{
		patternToFuncNameMap: make(map[string]string, n),
		funcNameToPatternMap: make(map[string]string, n),
	}
	for i := range n {
		fn := fmt.Sprintf("READ_resource%d", i)
		route := fmt.Sprintf("/resource%d/", i)
		p := routing.MethodRouteVersionToPattern("GET", route, "")
		HTTPPatternToFuncNameMap(r)[p] = fn
		HTTPFuncNameToPatternMap(r)[fn] = p
	}
	return r
}

func buildBenchWS(n int) *WS {
	ws := &WS{
		funcNameByEvent: make(map[string]string, n),
	}
	for i := range n {
		event := fmt.Sprintf("event%d", i)
		fn := fmt.Sprintf("ON_event%d", i)
		ws.funcNameByEvent[event] = fn
	}
	return ws
}

var benchCB = func(_ int, _ reflect.Type, _ reflect.Value, _ reflect.Value) {}

type mockExFilter struct{}

func (mockExFilter) Catch(_ *ctx.HTTPContext, _ *exception.Exception) {}

type mockWSExFilter struct{}

func (mockWSExFilter) Catch(_ *ctx.WSContext, _ *exception.Exception) {}

type noCatchExFilter struct{}

type wrongParamExFilter struct{}

func (wrongParamExFilter) Catch(_ int, _ *exception.Exception) {}

var noopCB = func(_ int, _ reflect.Type, _ reflect.Value, _ reflect.Value) {}

var testConstruct = func(obj any, constructor string) any {
	if method := reflect.ValueOf(obj).MethodByName(constructor); method.IsValid() {
		return method.Call(nil)[0].Interface()
	}
	return obj
}

func buildHTTP(fnToRoute map[string]string) *HTTP {
	r := &HTTP{
		patternToFuncNameMap: make(map[string]string, len(fnToRoute)),
		funcNameToPatternMap: make(map[string]string, len(fnToRoute)),
	}
	for fn, route := range fnToRoute {
		p := routing.MethodRouteVersionToPattern("GET", route, "")
		HTTPPatternToFuncNameMap(r)[p] = fn
		HTTPFuncNameToPatternMap(r)[fn] = p
	}
	return r
}

func buildWS(patternToFn map[string]string) *WS {
	ws := &WS{
		funcNameByEvent: make(map[string]string, len(patternToFn)),
	}
	for p, fn := range patternToFn {
		ws.funcNameByEvent[p] = fn
	}
	return ws
}

type customError struct{ text string }

func (e *customError) Error() string { return e.text }

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

type fnTestController struct{}

func (fnTestController) READ_users() {}

func (fnTestController) CREATE_orders() {}

type mockGuarder struct{}

func (mockGuarder) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mockWSGuarder struct{}

func (mockWSGuarder) CanActivate(_ *ctx.WSContext) bool { return true }

type denyGuarder struct{}

func (denyGuarder) CanActivate(_ *ctx.HTTPContext) bool { return false }

type denyWSGuarder struct{}

func (denyWSGuarder) CanActivate(_ *ctx.WSContext) bool { return false }

type noCanActivateGuarder struct{}

type wrongReturnGuarder struct{}

func (wrongReturnGuarder) CanActivate(_ *ctx.HTTPContext) string { return "" }

type wrongParamGuarder struct{}

func (wrongParamGuarder) CanActivate(_ int) bool { return true }

type pathController struct{ HTTP }

func (pathController) CREATE_token() string { return "token" }

func (pathController) READ_token() string { return "read" }

func (pathController) READ_payment_intents() string { return "pi" }

func (pathController) READ_users_VERSION_2() string { return "v2" }

func (pathController) READ_profile_BY_id() string { return "profile" }

func patternOf(r *HTTP, fn string) string { return HTTPFuncNameToPatternMap(r)[fn] }

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

type mockInterceptable struct{}

func (mockInterceptable) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any { return nil }

type mockWSInterceptable struct{}

func (mockWSInterceptable) Intercept(_ *ctx.WSContext, _ *aggregation.Aggregation) any { return nil }

type noInterceptMethod struct{}

type wrongParamInterceptable struct{}

func (wrongParamInterceptable) Intercept(_ int, _ *aggregation.Aggregation) any { return nil }

type mockMiddlewareFn struct{}

func (mockMiddlewareFn) Use(_ *http.Request, _ http.ResponseWriter, _ ctx.Next) {}

func recoverValue(fn func()) (any, bool) {
	var value any
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				value = r
				panicked = true
			}
		}()
		fn()
	}()
	return value, panicked
}

type mockContextPipeable struct{}

func (mockContextPipeable) Transform(_ *ctx.HTTPContext, m ArgumentMetadata) any { return m }

type mockBodyPipeable struct{}

func (mockBodyPipeable) Transform(_ ctx.Body, m ArgumentMetadata) any { return m }

type mockFormPipeable struct{}

func (mockFormPipeable) Transform(_ ctx.Form, m ArgumentMetadata) any { return m }

type mockQueryPipeable struct{}

func (mockQueryPipeable) Transform(_ ctx.Query, m ArgumentMetadata) any { return m }

type mockHeaderPipeable struct{}

func (mockHeaderPipeable) Transform(_ ctx.Header, m ArgumentMetadata) any { return m }

type mockParamPipeable struct{}

func (mockParamPipeable) Transform(_ ctx.Param, m ArgumentMetadata) any { return m }

type mockFilePipeable struct{}

func (mockFilePipeable) Transform(_ ctx.File, m ArgumentMetadata) any { return m }

type mockWSPayloadPipeable struct{}

func (mockWSPayloadPipeable) Transform(_ ctx.WSPayload, m ArgumentMetadata) any { return m }

var (
	_ ContextPipeable   = mockContextPipeable{}
	_ BodyPipeable      = mockBodyPipeable{}
	_ FormPipeable      = mockFormPipeable{}
	_ QueryPipeable     = mockQueryPipeable{}
	_ HeaderPipeable    = mockHeaderPipeable{}
	_ ParamPipeable     = mockParamPipeable{}
	_ FilePipeable      = mockFilePipeable{}
	_ WSPayloadPipeable = mockWSPayloadPipeable{}
)

type ptrGuard struct{ allow bool }

func (g *ptrGuard) CanActivate(*ctx.HTTPContext) bool { return g != nil && g.allow }

type ptrWSGuard struct{ allow bool }

func (g *ptrWSGuard) CanActivate(*ctx.WSContext) bool { return g != nil && g.allow }

type embedGuard struct{ denyGuarder }

type embedWSGuard struct{ denyWSGuarder }

type unexportedGuard struct{}

func (unexportedGuard) canActivate(*ctx.HTTPContext) bool { return true }

type ptrInterceptor struct{ result any }

func (i *ptrInterceptor) Intercept(*ctx.HTTPContext, *aggregation.Aggregation) any {
	if i == nil {
		return "nil"
	}
	return i.result
}

type ptrWSInterceptor struct{ result any }

func (i *ptrWSInterceptor) Intercept(*ctx.WSContext, *aggregation.Aggregation) any {
	if i == nil {
		return "nil"
	}
	return i.result
}

type embedInterceptor struct{ mockInterceptable }

type embedWSInterceptor struct{ mockWSInterceptable }

type unexportedInterceptor struct{}

func (unexportedInterceptor) intercept(*ctx.HTTPContext, *aggregation.Aggregation) any { return nil }

type recordingFilter struct{ got []*exception.Exception }

func (f *recordingFilter) Catch(_ *ctx.HTTPContext, ex *exception.Exception) {
	if f == nil {
		return
	}
	f.got = append(f.got, ex)
}

type recordingWSFilter struct{ got []*exception.Exception }

func (f *recordingWSFilter) Catch(_ *ctx.WSContext, ex *exception.Exception) {
	if f == nil {
		return
	}
	f.got = append(f.got, ex)
}

type embedFilter struct{ mockExFilter }

type embedWSFilter struct{ mockWSExFilter }

type unexportedFilter struct{}

func (unexportedFilter) catch(*ctx.HTTPContext, *exception.Exception) {}

func reflectHTTPGuard(x any) (HTTPCanActivate, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("CanActivate")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPCanActivate)
	return fn, ok
}

func reflectWSGuard(x any) (WSCanActivate, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("CanActivate")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSCanActivate)
	return fn, ok
}

func reflectHTTPInterceptor(x any) (HTTPIntercept, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Intercept")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPIntercept)
	return fn, ok
}

func reflectWSInterceptor(x any) (WSIntercept, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Intercept")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSIntercept)
	return fn, ok
}

func reflectHTTPFilter(x any) (HTTPCatch, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Catch")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(HTTPCatch)
	return fn, ok
}

func reflectWSFilter(x any) (WSCatch, bool) {
	if x == nil {
		return nil, false
	}
	m := reflect.ValueOf(x).MethodByName("Catch")
	if !m.IsValid() {
		return nil, false
	}
	fn, ok := m.Interface().(WSCatch)
	return fn, ok
}

func guardSubjects() []any {
	return []any{
		mockGuarder{}, denyGuarder{}, &mockGuarder{}, &denyGuarder{},
		ptrGuard{allow: true}, &ptrGuard{allow: true}, &ptrGuard{}, (*ptrGuard)(nil),
		embedGuard{}, &embedGuard{},
		ptrWSGuard{allow: true}, &ptrWSGuard{allow: true}, (*ptrWSGuard)(nil),
		embedWSGuard{}, &embedWSGuard{},
		mockWSGuarder{}, denyWSGuarder{},
		wrongReturnGuarder{}, wrongParamGuarder{}, noCanActivateGuarder{}, unexportedGuard{},
		42, "CanActivate", struct{}{}, []int{1}, func() {},
	}
}

func interceptorSubjects() []any {
	return []any{
		mockInterceptable{}, &mockInterceptable{},
		ptrInterceptor{result: 1}, &ptrInterceptor{result: 1}, (*ptrInterceptor)(nil),
		embedInterceptor{}, &embedInterceptor{},
		mockWSInterceptable{}, &mockWSInterceptable{},
		ptrWSInterceptor{result: 1}, &ptrWSInterceptor{result: 1}, (*ptrWSInterceptor)(nil),
		embedWSInterceptor{}, &embedWSInterceptor{},
		wrongParamInterceptable{}, noInterceptMethod{}, unexportedInterceptor{},
		42, "Intercept", struct{}{}, func() {},
	}
}

func filterSubjects() []any {
	return []any{
		mockExFilter{}, &mockExFilter{},
		recordingFilter{}, &recordingFilter{}, (*recordingFilter)(nil),
		embedFilter{}, &embedFilter{},
		mockWSExFilter{}, &mockWSExFilter{},
		recordingWSFilter{}, &recordingWSFilter{}, (*recordingWSFilter)(nil),
		embedWSFilter{}, &embedWSFilter{},
		wrongParamExFilter{}, noCatchExFilter{}, unexportedFilter{},
		42, "Catch", struct{}{}, func() {},
	}
}

func expectNoPanic(t *testing.T, rule string, fn func()) {
	t.Helper()
	if text, panicked := recoverText(fn); panicked {
		t.Error(test.DiffMessage(text, "no panic", rule))
	}
}

func recoverGuardRejectionWS(mw func(*ctx.WSContext), c *ctx.WSContext) (rec any) {
	defer func() { rec = recover() }()
	mw(c)
	return nil
}

func newInterceptorTestWSContext() *ctx.WSContext {
	return ctx.NewWSContext()
}

func noopInterceptWS(*ctx.WSContext, *aggregation.Aggregation) any { return nil }
