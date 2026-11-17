package ginject_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/guards/throttler"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/middlewares/proxy"
)

func TestSurface_ConstantValues(t *testing.T) {
	cases := []struct {
		name      string
		got, want int64
	}{
		{"VersioningQuery", int64(ginject.VersioningQuery), 1},
		{"VersioningHeader", int64(ginject.VersioningHeader), 2},
		{"VersioningCustom", int64(ginject.VersioningCustom), 3},
		{"VersioningMediaType", int64(ginject.VersioningMediaType), 4},
		{"StrategyFixedWindow", int64(throttler.StrategyFixedWindow), 0},
		{"StrategySlidingWindow", int64(throttler.StrategySlidingWindow), 1},
		{"StrategyTokenBucket", int64(throttler.StrategyTokenBucket), 2},
		{"HeaderXForwarded", int64(proxy.HeaderXForwarded), 0},
		{"HeaderForwarded", int64(proxy.HeaderForwarded), 1},
		{"LevelDebug", int64(log.LevelDebug), int64(slog.LevelDebug)},
		{"LevelInfo", int64(log.LevelInfo), int64(slog.LevelInfo)},
		{"LevelWarn", int64(log.LevelWarn), int64(slog.LevelWarn)},
		{"LevelError", int64(log.LevelError), int64(slog.LevelError)},
		{"LevelFatal", int64(log.LevelFatal), 12},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Error(test.DiffMessage(c.got, c.want, "B15: the value of "+c.name))
		}
	}
	var zero log.Format
	if zero == log.FormatText || zero == log.FormatJSON || zero == log.FormatPretty {
		t.Error(test.DiffMessage(zero, "not one of the named formats", "B15: the zero Format still means the default format"))
	}
}

type wrongGuard struct{}

func (wrongGuard) NewGuard() wrongGuard              { return wrongGuard{} }
func (wrongGuard) CanActivate(string) bool           { return true }
func (wrongGuard) NewController() ginject.Controller { return nil }

type wrongShapeController struct {
	ginject.HTTP
	ginject.Guard
}

func (c wrongShapeController) NewController() ginject.Controller {
	c.BindGuard(wrongGuard{}, c.READ_x)
	return c
}

func (wrongShapeController) READ_x() string { return "x" }

func TestSurface_WrongLayerShapeNamesTheRootTypes(t *testing.T) {
	msg := func() (msg string) {
		defer func() { msg = fmt.Sprint(recover()) }()
		app := ginject.New()
		app.Create(ginject.ModuleBuilder().Controllers(wrongShapeController{}).Build())
		return ""
	}()
	if !strings.Contains(msg, "CanActivate must be func(ginject.HTTPContext) bool to be bound as a HTTP guard") {
		t.Error(test.DiffMessage(msg, "invalid guard: <type>.CanActivate must be func(ginject.HTTPContext) bool to be bound as a HTTP guard", "B12: the message spells the type as users write it"))
	}
}

type wrongInterceptor struct{}

func (wrongInterceptor) NewInterceptor() wrongInterceptor { return wrongInterceptor{} }
func (wrongInterceptor) Intercept(string) any             { return nil }

type wrongFilter struct{}

func (wrongFilter) NewExceptionFilter() wrongFilter { return wrongFilter{} }
func (wrongFilter) Catch(string)                    {}

type wrongHTTPInterceptorController struct {
	ginject.HTTP
	ginject.Interceptor
}

func (c wrongHTTPInterceptorController) NewController() ginject.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.READ_x)
	return c
}

func (wrongHTTPInterceptorController) READ_x() string { return "x" }

type wrongHTTPFilterController struct {
	ginject.HTTP
	ginject.ExceptionFilter
}

func (c wrongHTTPFilterController) NewController() ginject.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.READ_x)
	return c
}

func (wrongHTTPFilterController) READ_x() string { return "x" }

type wrongWSGuardController struct {
	ginject.WS
	ginject.Guard
}

func (c wrongWSGuardController) NewController() ginject.Controller {
	c.BindGuard(wrongGuard{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSGuardController) SUBSCRIBE_x() string { return "x" }

type wrongWSInterceptorController struct {
	ginject.WS
	ginject.Interceptor
}

func (c wrongWSInterceptorController) NewController() ginject.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSInterceptorController) SUBSCRIBE_x() string { return "x" }

type wrongWSFilterController struct {
	ginject.WS
	ginject.ExceptionFilter
}

func (c wrongWSFilterController) NewController() ginject.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSFilterController) SUBSCRIBE_x() string { return "x" }

func TestSurface_EveryWrongShapeMessage(t *testing.T) {
	cases := []struct {
		controller ginject.Controller
		ws         bool
		want       string
	}{
		{wrongShapeController{}, false, ".CanActivate must be func(ginject.HTTPContext) bool to be bound as a HTTP guard"},
		{wrongHTTPInterceptorController{}, false, ".Intercept must be func(ginject.HTTPContext, ginject.Aggregation) any to be bound as a HTTP interceptor"},
		{wrongHTTPFilterController{}, false, ".Catch must be func(ginject.HTTPContext, *exception.Exception) to be bound as a HTTP exception filter"},
		{wrongWSGuardController{}, true, ".CanActivate must be func(ginject.WSContext) bool to be bound as a WS guard"},
		{wrongWSInterceptorController{}, true, ".Intercept must be func(ginject.WSContext, ginject.Aggregation) any to be bound as a WS interceptor"},
		{wrongWSFilterController{}, true, ".Catch must be func(ginject.WSContext, *exception.Exception) to be bound as a WS exception filter"},
	}
	for _, c := range cases {
		msg := func() (msg string) {
			defer func() { msg = fmt.Sprint(recover()) }()
			app := ginject.New()
			if c.ws {
				app.EnableWS(&ginject.WSOptions{Path: "/ws"})
			}
			app.Create(ginject.ModuleBuilder().Controllers(c.controller).Build())
			return ""
		}()
		if !strings.Contains(msg, c.want) {
			t.Error(test.DiffMessage(msg, "…"+c.want, "B12: the exact shape message"))
		}
	}
}
