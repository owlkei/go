package bench

import (
	"net/http"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
)

type silentLogger struct{}

func (silentLogger) Debug(string, ...any) {}
func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}
func (silentLogger) Fatal(string, ...any) {}

type searchQuery struct {
	Q    string `bind:"q"`
	Page int    `bind:"page"`
}

func (query searchQuery) Transform(q ctx.Query, _ common.ArgumentMetadata) any {
	bound, _ := q.Bind(query)
	return bound
}

type userBody struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

func (body userBody) Transform(b ctx.Body, _ common.ArgumentMetadata) any {
	bound, _ := b.Bind(body)
	return bound
}

type ginjectController struct {
	common.HTTP
}

func (instance ginjectController) NewController() core.Controller {
	return instance
}

func (instance ginjectController) READ_static() string {
	return "ok"
}

func (instance ginjectController) READ_users_BY_id(param ginject.Param) ginject.Map {
	return ginject.Map{"id": param.Get("id")}
}

func (instance ginjectController) READ_search(query searchQuery) ginject.Map {
	return ginject.Map{"q": query.Q, "page": query.Page}
}

func (instance ginjectController) CREATE_users(body userBody) ginject.Map {
	return ginject.Map{"name": body.Name, "age": body.Age}
}

func (instance ginjectController) READ_error() string {
	panic(exception.BadRequestException("bad request"))
}

type ginjectMiddleware struct{}

func (ginjectMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set(mwHeader, "1")
	next()
}

type ginjectGuard struct{}

func (ginjectGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get(authHeader) == authValue
}

type ginjectInterceptor struct{}

func (ginjectInterceptor) Intercept(_ *ctx.HTTPContext, aggregation ginject.Aggregation) any {
	return aggregation.Pipe(
		aggregation.Transform(func(data any) any {
			return ginject.Map{"data": data}
		}),
	)
}

type ginjectChainController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
}

func (instance ginjectChainController) NewController() core.Controller {
	instance.BindMiddleware(ginjectMiddleware{})
	instance.BindGuard(ginjectGuard{})
	instance.BindInterceptor(ginjectInterceptor{})
	return instance
}

func (instance ginjectChainController) READ_chain() ginject.Map {
	return ginject.Map{"ok": true}
}

func NewGinject() http.Handler {
	return NewGinjectApp()
}

func NewGinjectApp() *core.App {
	app := core.New()
	app.SetLogger(silentLogger{})
	app.Create(
		core.ModuleBuilder().
			Controllers(ginjectController{}, ginjectChainController{}).
			Build(),
	)
	return app
}
