package core

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

var lyRec createRecorder
var lyBuilt atomic.Int64

type lyGuard struct{ Role string }

func (g lyGuard) NewGuard() any {
	lyBuilt.Add(1)
	return lyGuard{Role: g.Role}
}

func (g lyGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add(g.Role)
	return true
}

type lyPlainGuard struct{ Role string }

func (g lyPlainGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add("plain:" + g.Role)
	return true
}

type lyBoth struct{ Role string }

func (b lyBoth) NewGuard() any {
	lyBuilt.Add(1)
	return lyBothGuard(b)
}

func (b lyBoth) NewMiddleware() any {
	lyBuilt.Add(100)
	return lyBothMiddleware(b)
}

func (b lyBoth) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

type lyBothGuard struct{ Role string }

func (g lyBothGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add("guard:" + g.Role)
	return true
}

type lyBothMiddleware struct{ Role string }

func (m lyBothMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	lyRec.add("middleware:" + m.Role)
	next()
}

type lyFuncGuard struct {
	Role string
	Hook func()
}

func (g lyFuncGuard) NewGuard() any {
	lyBuilt.Add(1)
	return g
}

func (g lyFuncGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type lyChanGuard struct {
	Role string
	Ch   chan int
}

func (g lyChanGuard) NewGuard() any {
	lyBuilt.Add(1)
	return g
}

func (g lyChanGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type lyController struct{ common.HTTP }

func (c lyController) NewController() Controller { return c }
func (c lyController) READ_ly() string           { return "ly" }

func lyRun(t *testing.T, setup func(app *App)) {
	t.Helper()
	lyRec = createRecorder{}
	lyBuilt.Store(0)

	app := New()
	setup(app)
	mustCreate(t, app, ModuleBuilder().Controllers(lyController{}).Build())
	if w := createGet(app, "/ly"); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "the route answers"))
	}
}

func TestLayers_DifferentConfigurationsGetOwnInstance(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyGuard{Role: "admin"}, lyGuard{Role: "user"})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: two configurations of one type are constructed separately"))
	}
	if got := sprint(lyRec.list()); got != "[admin user]" {
		t.Error(test.DiffMessage(got, "[admin user]", "I7: each configuration keeps its own role"))
	}
}

func TestLayers_EqualConfigurationsShareInstance(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyGuard{Role: "same"}, lyGuard{Role: "same"})
	})

	if got := lyBuilt.Load(); got != 1 {
		t.Error(test.DiffMessage(got, int64(1), "I7: equal configurations run the constructor once"))
	}
}

func TestLayers_WithoutConstructorKeepsEachValue(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyPlainGuard{Role: "a"}, lyPlainGuard{Role: "b"})
	})

	if got := sprint(lyRec.list()); got != "[plain:a plain:b]" {
		t.Error(test.DiffMessage(got, "[plain:a plain:b]", "a layer with no constructor is used as given, one per configuration"))
	}
}

func TestLayers_ConstructorNameIsPartOfKey(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyBoth{Role: "both"})
		app.BindGlobalMiddlewares(lyBoth{Role: "both"})
	})

	if got := lyBuilt.Load(); got != 101 {
		t.Error(test.DiffMessage(got, int64(101), "I7: NewGuard and NewMiddleware each run once for one value"))
	}
	if got := sprint(lyRec.list()); got != "[middleware:both guard:both]" {
		t.Error(test.DiffMessage(got, "[middleware:both guard:both]", "I7: the guard and the middleware are different instances"))
	}
}

func TestLayers_ValuesWithFuncFieldsAreNeverShared(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyFuncGuard{Role: "func", Hook: func() {}}, lyFuncGuard{Role: "func", Hook: func() {}})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: a value holding a func is never equal to another"))
	}
}

func TestLayers_ValuesWithDifferentChannelsAreNeverShared(t *testing.T) {
	lyRun(t, func(app *App) {
		app.BindGlobalGuards(lyChanGuard{Role: "chan", Ch: make(chan int)}, lyChanGuard{Role: "chan", Ch: make(chan int)})
	})

	if got := lyBuilt.Load(); got != 2 {
		t.Error(test.DiffMessage(got, int64(2), "I7: values with different channels are not equal"))
	}
}
