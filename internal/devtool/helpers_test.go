package devtool

import (
	"net/http"

	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/versioning"
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
		AddGuarders([]any{snapshotGlobalGuard{}}, nil).
		AddInterceptors(nil, nil).
		AddVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}).
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
