package devtool

import (
	"context"
	"encoding/json"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

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
