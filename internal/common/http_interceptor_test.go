package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestInjectProvidersIntoHTTPInterceptors_Empty(t *testing.T) {
	ic := &Interceptor{}
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	items := InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)
	if len(items) != 0 {
		t.Error(test.DiffMessage(len(items), 0, "no bound interceptors → empty result"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_ApplyAll(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{
		"READ_users":    "/users/",
		"CREATE_orders": "/orders/",
	})

	items := InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)
	if len(items) != 2 {
		t.Error(test.DiffMessage(len(items), 2, "interceptor with no handlers applies to all patterns"))
	}
	for _, item := range items {
		if item.HTTP.Method != "GET" {
			t.Error(test.DiffMessage(item.HTTP.Method, "GET", "method"))
		}
		if item.HTTP.Pattern == "" {
			t.Error(test.DiffMessage(item.HTTP.Pattern, "non-empty", "pattern"))
		}
		if item.HTTP.Common.Name == "" {
			t.Error(test.DiffMessage(item.HTTP.Common.Name, "non-empty", "name"))
		}
	}
}

func TestInjectProvidersIntoHTTPInterceptors_MainHandlerName(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{"READ_items": "/items/"})
	items := InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)

	if len(items) != 1 {
		t.Error(test.DiffMessage(len(items), 1, "one pattern → one item"))
		return
	}
	if items[0].HTTP.Common.MainHandlerName != "READ_items" {
		t.Error(test.DiffMessage(items[0].HTTP.Common.MainHandlerName, "READ_items", "main handler name"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_HandlerIsCallableIntercept(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(mockInterceptable{})

	r := buildHTTP(map[string]string{"READ_users": "/users/"})
	items := InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)
	if len(items) != 1 {
		t.Fatal(test.DiffMessage(len(items), 1, "one pattern → one item"))
	}

	if _, ok := items[0].HTTP.Common.Handler.(HTTPIntercept); !ok {
		t.Fatal(test.DiffMessage(items[0].HTTP.Common.Handler, "HTTPIntercept", "Handler must be callable as HTTPIntercept"))
	}
}

func TestInjectProvidersIntoHTTPInterceptors_NoIntercept_Panics(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(noInterceptMethod{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "interceptor with no Intercept method must panic"))
		}
	}()
	InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)
}

func TestInjectProvidersIntoHTTPInterceptors_WrongParamType_Panics(t *testing.T) {
	ic := &Interceptor{}
	ic.BindInterceptor(wrongParamInterceptable{})
	r := buildHTTP(map[string]string{"READ_users": "/users/"})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error(test.DiffMessage(nil, "panic", "Intercept with a non-context first param must panic"))
		}
	}()
	InjectProvidersIntoHTTPInterceptors(ic, r, noopCB, testConstruct)
}

func TestAsHTTPInterceptor_Valid(t *testing.T) {
	fn, ok := AsHTTPInterceptor(mockInterceptable{})
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "mockInterceptable must match HTTPIntercept"))
	}
	fn(nil, aggregation.NewAggregation())
}

func TestAsHTTPInterceptor_NoMethod(t *testing.T) {
	_, ok := AsHTTPInterceptor(noInterceptMethod{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an interceptable with no Intercept must not match"))
	}
}

func TestAsHTTPInterceptor_WrongShape(t *testing.T) {
	_, ok := AsHTTPInterceptor(wrongParamInterceptable{})
	if ok {
		t.Error(test.DiffMessage(true, false, "an Intercept with a non-context first param must not match"))
	}
}
