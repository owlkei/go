package common

import (
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestBindInterceptor_Chaining(t *testing.T) {
	i := &Interceptor{}
	ret := i.BindInterceptor(mockInterceptable{})
	if ret != i {
		t.Error(test.DiffMessage(ret, i, "BindInterceptor should return self"))
	}
	if len(i.interceptorHandlers) != 1 {
		t.Error(test.DiffMessage(len(i.interceptorHandlers), 1, "one handler after one bind"))
	}
	i.BindInterceptor(mockInterceptable{})
	if len(i.interceptorHandlers) != 2 {
		t.Error(test.DiffMessage(len(i.interceptorHandlers), 2, "two handlers after two binds"))
	}
}

func TestInterceptorShapeError_MessageContainsType(t *testing.T) {
	err := InterceptorShapeError(noInterceptMethod{})
	if err == nil {
		t.Fatal(test.DiffMessage(nil, "non-nil error", "InterceptorShapeError must not return nil"))
	}
	if !strings.Contains(err.Error(), "noInterceptMethod") {
		t.Error(test.DiffMessage(err.Error(), "contains noInterceptMethod", "error message should name the offending type"))
	}
}
