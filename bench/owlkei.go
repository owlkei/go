package bench

import (
	"net/http"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/log"
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

func (query searchQuery) Transform(q owlkei.Query, _ owlkei.ArgumentMetadata) any {
	bound, _ := q.Bind(query)
	return bound
}

type userBody struct {
	Name string `bind:"name"`
	Age  int    `bind:"age"`
}

func (body userBody) Transform(b owlkei.Body, _ owlkei.ArgumentMetadata) any {
	bound, _ := b.Bind(body)
	return bound
}

type owlkeiController struct {
	owlkei.HTTP
}

func (instance owlkeiController) NewController() owlkei.Controller {
	return instance
}

func (instance owlkeiController) READ_static() string {
	return "ok"
}

func (instance owlkeiController) READ_users_BY_id(param owlkei.Param) owlkei.Map {
	return owlkei.Map{"id": param.Get("id")}
}

func (instance owlkeiController) READ_search(query searchQuery) owlkei.Map {
	return owlkei.Map{"q": query.Q, "page": query.Page}
}

func (instance owlkeiController) CREATE_users(body userBody) owlkei.Map {
	return owlkei.Map{"name": body.Name, "age": body.Age}
}

func (instance owlkeiController) READ_error() string {
	panic(exception.BadRequestException("bad request"))
}

type owlkeiMiddleware struct{}

func (owlkeiMiddleware) Use(_ *http.Request, w http.ResponseWriter, next owlkei.Next) {
	w.Header().Set(mwHeader, "1")
	next()
}

type owlkeiGuard struct{}

func (owlkeiGuard) CanActivate(c owlkei.HTTPContext) bool {
	return c.Request.Header.Get(authHeader) == authValue
}

type owlkeiInterceptor struct{}

func (owlkeiInterceptor) Intercept(_ owlkei.HTTPContext, aggregation owlkei.Aggregation) any {
	return aggregation.Pipe(
		aggregation.Transform(func(data any) any {
			return owlkei.Map{"data": data}
		}),
	)
}

type owlkeiChainController struct {
	owlkei.HTTP
	owlkei.Middleware
	owlkei.Guard
	owlkei.Interceptor
}

func (instance owlkeiChainController) NewController() owlkei.Controller {
	instance.BindMiddleware(owlkeiMiddleware{})
	instance.BindGuard(owlkeiGuard{})
	instance.BindInterceptor(owlkeiInterceptor{})
	return instance
}

func (instance owlkeiChainController) READ_chain() owlkei.Map {
	return owlkei.Map{"ok": true}
}

func NewOwlkei() http.Handler {
	return NewOwlkeiApp()
}

func NewOwlkeiApp() owlkei.App {
	return newOwlkeiApp(owlkei.New(&owlkei.AppOptions{Logger: silentLogger{}}))
}

func NewOwlkeiAppWithLog() owlkei.App {
	return newOwlkeiApp(owlkei.New(&owlkei.AppOptions{
		Logger:      log.NewLog(&log.LogOptions{Format: log.FormatJSON}),
		IsAccessLog: true,
	}))
}

func newOwlkeiApp(app owlkei.App) owlkei.App {
	app.Create(
		owlkei.ModuleBuilder().
			Controllers(owlkeiController{}, owlkeiChainController{}).
			Build(),
	)
	return app
}
