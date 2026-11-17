package devtool

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type snapshotGlobalGuard struct{}

func (snapshotGlobalGuard) CanActivate(*ctx.HTTPContext) bool { return true }

type snapshotBodyDTO struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

func (dto snapshotBodyDTO) Transform(ctx.Body, common.ArgumentMetadata) any { return dto }

func snapshotHandler(snapshotBodyDTO) any { return nil }

func buildSnapshot() *Devtool {
	return DevtoolBuilder().
		AddExceptionFilters(nil, nil).
		AddMiddlewares(nil, nil).
		AddGuarders([]common.Guarder{snapshotGlobalGuard{}}, nil).
		AddInterceptors(nil, nil).
		AddVersioning(&versioning.Versioning{Type: versioning.HeaderVersion, Key: "X-Version"}).
		AddHTTPMainHandlers([]common.HTTPLayer{{
			Handler:        snapshotHandler,
			ControllerPath: "users.Controller",
			Name:           "CREATE",
			Route:          "/users/",
			Version:        "1",
			Method:         http.MethodPost,
			Pattern:        "/users/[POST]",
		}}).
		Build()
}

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
		component.Versioning.Type != int32(versioning.HeaderVersion) {
		t.Error(test.DiffMessage(*component.Versioning, HTTPVersioning{Type: int32(versioning.HeaderVersion), Value: "1", Key: "X-Version"}, "versioning"))
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

func TestDevtool_GetConfigurationReturnsSnapshot(t *testing.T) {
	snapshot := buildSnapshot()

	response, err := snapshot.GetConfiguration(context.Background(), &GetConfigurationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Controller != snapshot.Controller {
		t.Error(test.DiffMessage(response.Controller, snapshot.Controller, "controller"))
	}
}

func TestDevtool_SnapshotEncodesAsJSON(t *testing.T) {
	encoded, err := json.Marshal(buildSnapshot().GetConfigurationResponse)
	if err != nil {
		t.Fatal(err)
	}

	var decoded GetConfigurationResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Controller.Http[0].Route != "/users/" {
		t.Error(test.DiffMessage(decoded.Controller.Http[0].Route, "/users/", "route after a JSON round trip"))
	}
}

func TestLayerScope_String(t *testing.T) {
	cases := map[LayerScope]string{
		LayerScope_UNKNOWN:       "UNKNOWN",
		LayerScope_REQUEST_SCOPE: "REQUEST_SCOPE",
		LayerScope_GLOBAL_SCOPE:  "GLOBAL_SCOPE",
		LayerScope(99):           "UNKNOWN",
	}
	for scope, expected := range cases {
		if scope.String() != expected {
			t.Error(test.DiffMessage(scope.String(), expected, "LayerScope.String"))
		}
	}
}
