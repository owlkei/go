package bench

import (
	"net/http"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/log"
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

func (query searchQuery) Transform(q owlky.Query, _ owlky.ArgumentMetadata) any {
	bound, _ := q.Bind(query)
	return bound
}

type userBody struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

func (body userBody) Transform(b owlky.Body, _ owlky.ArgumentMetadata) any {
	bound, _ := b.Bind(body)
	return bound
}

type owlkyController struct {
	owlky.HTTP
}

func (instance owlkyController) NewController() owlky.Controller {
	return instance
}

func (instance owlkyController) READ_static() string {
	return "ok"
}

func (instance owlkyController) READ_users_BY_id(param owlky.Param) owlky.Map {
	return owlky.Map{"id": param.Get("id")}
}

func (instance owlkyController) READ_search(query searchQuery) owlky.Map {
	return owlky.Map{"q": query.Q, "page": query.Page}
}

func (instance owlkyController) CREATE_users(body userBody) owlky.Map {
	return owlky.Map{"name": body.Name, "age": body.Age}
}

func (instance owlkyController) READ_error() string {
	panic(exception.BadRequestException("bad request"))
}

type owlkyMiddleware struct{}

func (owlkyMiddleware) Use(_ *http.Request, w http.ResponseWriter, next owlky.Next) {
	w.Header().Set(mwHeader, "1")
	next()
}

type owlkyGuard struct{}

func (owlkyGuard) CanActivate(c owlky.HTTPContext) bool {
	return c.Request.Header.Get(authHeader) == authValue
}

type owlkyInterceptor struct{}

func (owlkyInterceptor) Intercept(_ owlky.HTTPContext, aggregation owlky.Aggregation) any {
	return aggregation.Pipe(
		aggregation.Transform(func(data any) any {
			return owlky.Map{"data": data}
		}),
	)
}

type owlkyChainController struct {
	owlky.HTTP
	owlky.Middleware
	owlky.Guard
	owlky.Interceptor
}

func (instance owlkyChainController) NewController() owlky.Controller {
	instance.BindMiddleware(owlkyMiddleware{})
	instance.BindGuard(owlkyGuard{})
	instance.BindInterceptor(owlkyInterceptor{})
	return instance
}

func (instance owlkyChainController) READ_chain() owlky.Map {
	return owlky.Map{"ok": true}
}

func NewOwlky() http.Handler {
	return NewOwlkyApp()
}

func NewOwlkyApp() owlky.App {
	return newOwlkyApp(owlky.New(&owlky.AppOptions{Logger: silentLogger{}}))
}

func NewOwlkyAppWithLog() owlky.App {
	return newOwlkyApp(owlky.New(&owlky.AppOptions{
		Logger:      log.NewLog(&log.LogOptions{Format: log.FormatJSON}),
		IsAccessLog: true,
	}))
}

func newOwlkyApp(app owlky.App) owlky.App {
	app.Create(
		owlky.ModuleBuilder().
			Controllers(owlkyController{}, owlkyChainController{}).
			Build(),
	)
	return app
}
