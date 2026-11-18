package devtool

import (
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

func TestDevtoolBuilder_BuildsHTTPComponent(t *testing.T) {
	snapshot := buildSnapshot()

	if len(snapshot.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(len(snapshot.Controller.Http), 1, "HTTP components"))
	}
	component := snapshot.Controller.Http[0]

	if component.Handler != "CREATE" {
		t.Error(test.DiffMessage(component.Handler, "CREATE", "handler"))
	}
	if component.HttpMethod != http.MethodPost {
		t.Error(test.DiffMessage(component.HttpMethod, http.MethodPost, "HTTP method"))
	}
	if component.Route != "/users/" {
		t.Error(test.DiffMessage(component.Route, "/users/", "route"))
	}
	if component.Id == "" {
		t.Error(test.DiffMessage(component.Id, "a non-empty id", "id"))
	}
	if component.Versioning.Key != "X-Version" || component.Versioning.Value != "1" ||
		component.Versioning.Type != int32(versioning.TypeHeader) {
		t.Error(test.DiffMessage(*component.Versioning, HTTPVersioning{Type: int32(versioning.TypeHeader), Value: "1", Key: "X-Version"}, "versioning"))
	}
}

func TestDevtoolBuilder_GlobalGuardHasGlobalScope(t *testing.T) {
	component := buildSnapshot().Controller.Http[0]

	if len(component.Guards) != 1 {
		t.Fatal(test.DiffMessage(len(component.Guards), 1, "guards"))
	}
	if component.Guards[0].Scope != LayerScope_GLOBAL_SCOPE {
		t.Error(test.DiffMessage(component.Guards[0].Scope, LayerScope_GLOBAL_SCOPE, "guard scope"))
	}
	if component.Guards[0].Name != "devtool.snapshotGlobalGuard" {
		t.Error(test.DiffMessage(component.Guards[0].Name, "devtool.snapshotGlobalGuard", "guard name"))
	}
}

func TestDevtoolBuilder_DescribesBodySchema(t *testing.T) {
	body := buildSnapshot().Controller.Http[0].Request.Body

	names := map[string]bool{}
	for _, field := range body {
		names[field.Name] = true
	}
	if !names["name"] || !names["age"] {
		t.Error(test.DiffMessage(names, map[string]bool{"name": true, "age": true}, "body schema fields"))
	}
}

func TestDevtool_WithoutVersioning(t *testing.T) {
	var snapshot *Devtool
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatal(test.DiffMessage(r, "no panic", "D1: Build without AddVersioning does not panic"))
			}
		}()
		snapshot = DevtoolBuilder().
			AddExceptionFilters(nil, nil).
			AddMiddlewares(nil, nil).
			AddGuarders(nil, nil).
			AddInterceptors(nil, nil).
			AddHTTPMainHandlers(versionedHandlers()).
			Build()
	}()

	if len(snapshot.Controller.Http) != 1 {
		t.Fatal(test.DiffMessage(len(snapshot.Controller.Http), 1, "D1: HTTP components"))
	}
	v := snapshot.Controller.Http[0].Versioning
	if v == nil {
		t.Fatal(test.DiffMessage(nil, "a versioning entry", "D1: the entry has versioning"))
	}
	if v.Value != "1" || v.Key != "" || v.Type != 0 {
		t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"1", "", int32(0)}, "D1: value is the route's version, key and type are empty"))
	}
}

func TestDevtool_WithVersioningUnchanged(t *testing.T) {
	snapshot := DevtoolBuilder().
		AddExceptionFilters(nil, nil).
		AddMiddlewares(nil, nil).
		AddGuarders(nil, nil).
		AddInterceptors(nil, nil).
		AddVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}).
		AddHTTPMainHandlers(versionedHandlers()).
		Build()

	v := snapshot.Controller.Http[0].Versioning
	if v.Value != "1" || v.Key != "X-Version" || v.Type != int32(versioning.TypeHeader) {
		t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"1", "X-Version", int32(versioning.TypeHeader)}, "D1: with versioning the entry is what it is today"))
	}
}
