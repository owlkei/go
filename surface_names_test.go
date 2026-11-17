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
