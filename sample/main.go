package main

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/middlewares/cors"
	"github.com/dangduoc08/ginject/middlewares/helmet"
	"github.com/dangduoc08/ginject/sample/benchmarks"
	"github.com/dangduoc08/ginject/sample/confs"
	"github.com/dangduoc08/ginject/sample/shared"
	"github.com/dangduoc08/ginject/sample/shop"
)

func main() {
	app := ginject.New()
	logger := log.NewLog(&log.LogOptions{
		Level:  log.LevelDebug,
		Format: log.FormatPretty,
	})

	mw := []ginject.MiddlewareFn{
		cors.CORS{AllowOrigins: []string{"http://localhost:3000"}}, helmet.Helmet{}, shared.LogMiddleware{},
	}

	app.
		SetLogger(logger).
		BindGlobalMiddlewares(mw...).
		BindGlobalGuards(shared.LogHTTPGuard{}, shared.LogWSGuard{}).
		BindGlobalInterceptors(shared.LogHTTPInterceptor{}, shared.LogWSInterceptor{}).
		BindGlobalExceptionFilters(shared.LogHTTPExceptionFilter{}, shared.LogWSExceptionFilter{})

	app.EnableWS(&ginject.WSOptions{
		Path: "ws",
	}, mw...)

	app.EnableAccessLog()

	app.Create(
		ginject.ModuleBuilder().
			Imports(benchmarks.Module, shop.Module, confs.ConfModule).
			Build(),
	)

	if err := app.Listen(confs.ENV.Port, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
