package core

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type dvController struct{ common.HTTP }

func (c dvController) NewController() Controller { return c }
func (c dvController) READ_dv_VERSION_2() string { return "dv" }

func TestDevtool_WithoutVersioning(t *testing.T) {
	app := New()
	app.EnableDevtool()

	text, panicked := createPanicText(func() {
		app.Create(NewModuleBuilder().Controllers(dvController{}).Build())
	})
	if panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "D1: EnableDevtool without EnableVersioning does not panic"))
	}
	if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(app.devtool, "a snapshot with one HTTP entry", "D1: the snapshot is built"))
	}

	v := app.devtool.Controller.Http[0].Versioning
	if v == nil {
		t.Fatal(test.DiffMessage(nil, "a versioning entry", "D1: the entry has versioning"))
	}
	if v.Value != "2" || v.Key != "" || v.Type != 0 {
		t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"2", "", int32(0)}, "D1: value is the route's version, key and type are empty"))
	}
}

func TestDevtool_WithVersioningUnchanged(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New()
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
		app.EnableDevtool()
		app.Create(NewModuleBuilder().Controllers(dvController{}).Build())

		if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
			t.Fatal(test.DiffMessage(app.devtool, "a snapshot with one HTTP entry", "D1: the snapshot is built"))
		}
		v := app.devtool.Controller.Http[0].Versioning
		if v.Value != "2" || v.Key != "X-Version" || v.Type != int32(versioning.TypeHeader) {
			t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"2", "X-Version", int32(versioning.TypeHeader)}, "D1: with versioning the snapshot is what it is today"))
		}
	})
}

type dvGuard struct{}

func (dvGuard) NewGuard() dvGuard                   { return dvGuard{} }
func (dvGuard) CanActivate(c *ctx.HTTPContext) bool { return true }

type dvGuardedController struct {
	common.HTTP
	common.Guard
}

func (c dvGuardedController) NewController() Controller {
	c.BindGuard(dvGuard{}, c.READ_dv)
	return c
}

func (c dvGuardedController) READ_dv() string { return "dv" }

func TestDevtool_SnapshotNamesUnchangedByMove(t *testing.T) {
	app := New()
	app.EnableDevtool()
	app.Create(NewModuleBuilder().Controllers(dvGuardedController{}).Build())

	if app.devtool == nil || len(app.devtool.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(app.devtool, "one HTTP entry", "B22: the snapshot is built"))
	}
	e := app.devtool.Controller.Http[0]
	got := []any{e.HttpMethod, e.Route, len(e.Guards)}
	want := []any{"GET", "/dv/", 1}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatal(test.DiffMessage(got, want, "B22: method, route and guard count"))
	}
	if e.Guards[0].Name != "core.dvGuard" {
		t.Error(test.DiffMessage(e.Guards[0].Name, "core.dvGuard", "B22, D4: layer names keep the package name, not the path"))
	}
}
