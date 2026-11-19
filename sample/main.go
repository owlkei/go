package main

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/log"
	"go.owlky.com/owlky/middlewares/cors"
	"go.owlky.com/owlky/middlewares/helmet"
	"go.owlky.com/owlky/sample/benchmarks"
	"go.owlky.com/owlky/sample/confs"
	"go.owlky.com/owlky/sample/shared"
	"go.owlky.com/owlky/sample/shop"
)

func main() {
	app := owlky.New()
	logger := log.NewLog(&log.LogOptions{
		Level:  log.LevelDebug,
		Format: log.FormatPretty,
	})

	mw := []owlky.MiddlewareFn{
		cors.CORS{AllowOrigins: []string{"http://localhost:3000"}}, helmet.Helmet{}, shared.LogMiddleware{},
	}

	app.
		SetLogger(logger).
		BindGlobalMiddlewares(mw...).
		BindGlobalGuards(shared.LogHTTPGuard{}, shared.LogWSGuard{}).
		BindGlobalInterceptors(shared.LogHTTPInterceptor{}, shared.LogWSInterceptor{}).
		BindGlobalExceptionFilters(shared.LogHTTPExceptionFilter{}, shared.LogWSExceptionFilter{})

	app.EnableWS(&owlky.WSOptions{
		Path: "ws",
	}, mw...)

	app.EnableAccessLog()

	app.Create(
		owlky.ModuleBuilder().
			Imports(benchmarks.Module, shop.Module, confs.ConfModule).
			Build(),
	)

	if err := app.Listen(confs.ENV.Port, nil); err != nil {
		app.Logger().Fatal("AppError", "error", err)
	}
}
