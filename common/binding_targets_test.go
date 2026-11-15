package common

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

type bindController struct{}

func (bindController) READ_users()        {}
func (bindController) READ_orders()       {}
func (bindController) READ_items()        {}
func (bindController) SUBSCRIBE_message() {}
func (bindController) SUBSCRIBE_status()  {}
func (bindController) SUBSCRIBE_typing()  {}
func (bindController) Helper()            {}

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
	items := l.InjectProvidersIntoHTTPGuards(bindRoutes(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsGuardsTargets(handlers ...any) []string {
	l := &Guard{}
	l.BindGuard(mockWSGuarder{}, handlers...)
	items := l.InjectProvidersIntoWSGuards(bindEvents(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
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

func httpInterceptorsTargets(handlers ...any) []string {
	l := &Interceptor{}
	l.BindInterceptor(mockInterceptable{}, handlers...)
	items := l.InjectProvidersIntoHTTPInterceptors(bindRoutes(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsInterceptorsTargets(handlers ...any) []string {
	l := &Interceptor{}
	l.BindInterceptor(mockWSInterceptable{}, handlers...)
	items := l.InjectProvidersIntoWSInterceptors(bindEvents(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
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

func httpExceptionFiltersTargets(handlers ...any) []string {
	l := &ExceptionFilter{}
	l.BindExceptionFilter(mockExFilter{}, handlers...)
	items := l.InjectProvidersIntoHTTPExceptionFilters(bindRoutes(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsExceptionFiltersTargets(handlers ...any) []string {
	l := &ExceptionFilter{}
	l.BindExceptionFilter(mockWSExFilter{}, handlers...)
	items := l.InjectProvidersIntoWSExceptionFilters(bindEvents(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
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

func httpMiddlewaresTargets(handlers ...any) []string {
	l := &Middleware{}
	l.BindMiddleware(mockMiddlewareFn{}, handlers...)
	items := l.InjectProvidersIntoHTTPMiddlewares(bindRoutes(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.HTTP.Common.MainHandlerName)
	}
	return sortedNames(names)
}

func wsMiddlewaresTargets(handlers ...any) []string {
	l := &Middleware{}
	l.BindMiddleware(mockMiddlewareFn{}, handlers...)
	items := l.InjectProvidersIntoWSMiddlewares(bindEvents(), noopCB)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.WS.EventName)
	}
	return sortedNames(names)
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

func TestInjectProvidersIntoWSMiddlewares_NoHandlersBindsAllEvents(t *testing.T) {
	expectTargets(t, "B7", wsMiddlewaresTargets(), []string{"message", "status", "typing"})
}

func TestInjectProvidersIntoWSMiddlewares_HandlersBindOnlyThoseEvents(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: only the given events", wsMiddlewaresTargets(c.SUBSCRIBE_message, c.SUBSCRIBE_typing), []string{"message", "typing"})
}

func TestInjectProvidersIntoWSMiddlewares_DuplicateHandlerBindsOnce(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8: a handler given twice yields one item", wsMiddlewaresTargets(c.SUBSCRIBE_status, c.SUBSCRIBE_status), []string{"status"})
}

func TestInjectProvidersIntoWSMiddlewares_HTTPHandlerBindsNothing(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B9: an HTTP handler binds nothing on the WS side", wsMiddlewaresTargets(c.READ_users, c.READ_orders), []string{})
}

func TestInjectProvidersIntoWSMiddlewares_EventAndHTTPHandlerBindsEvent(t *testing.T) {
	c := bindController{}
	expectTargets(t, "B8, B9: only the event is bound", wsMiddlewaresTargets(c.SUBSCRIBE_status, c.READ_users), []string{"status"})
}

func TestInjectProvidersIntoWSMiddlewares_NonHandlerPanics(t *testing.T) {
	c := bindController{}
	expectBindPanic(t, "B10", func() { wsMiddlewaresTargets(c.Helper) },
		"invalid middleware:", "mockMiddlewareFn", "is bound to 'Helper', which is not a handler of the controller")
}
