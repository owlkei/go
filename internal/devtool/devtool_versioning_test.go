package devtool

import (
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

func versionedHandlers() []common.HTTPLayer {
	return []common.HTTPLayer{{
		Handler:        snapshotHandler,
		ControllerPath: "users.Controller",
		Name:           "CREATE",
		Route:          "/users/",
		Version:        "1",
		Method:         http.MethodPost,
		Pattern:        "/users/[POST]",
	}}
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
		AddVersioning(&versioning.Versioning{Type: versioning.HeaderVersion, Key: "X-Version"}).
		AddHTTPMainHandlers(versionedHandlers()).
		Build()

	v := snapshot.Controller.Http[0].Versioning
	if v.Value != "1" || v.Key != "X-Version" || v.Type != int32(versioning.HeaderVersion) {
		t.Error(test.DiffMessage([]any{v.Value, v.Key, v.Type}, []any{"1", "X-Version", int32(versioning.HeaderVersion)}, "D1: with versioning the entry is what it is today"))
	}
}
