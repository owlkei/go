package compress

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/inspect"
)

type okController struct{ common.HTTP }

func (c okController) NewController() core.Controller { return c }
func (okController) READ_ok() string                  { return "ok" }
func (okController) CREATE_ok() string                { return "ok" }
func (okController) UPDATE_ok() string                { return "ok" }
func (okController) MODIFY_ok() string                { return "ok" }
func (okController) DELETE_ok() string                { return "ok" }
func (okController) PREFLIGHT_ok() string             { return "options-route" }

type silentLogger struct{}

func (silentLogger) Debug(string, ...any) {}
func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}
func (silentLogger) Fatal(string, ...any) {}

type probeMiddleware struct{ Name string }

type probed struct {
	request *http.Request
	context any
}

var probes sync.Map

func (p probeMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	probes.Store(p.Name, probed{request: r, context: r.Context()})
	next()
}

func probeOf(name string) probed {
	v, _ := probes.Load(name)
	p, _ := v.(probed)
	return p
}

type failBody struct{ t *testing.T }

func (b failBody) Read([]byte) (int, error) {
	b.t.Error("G3: the middleware read the request body")
	return 0, fmt.Errorf("body must not be read")
}

func (failBody) Close() error { return nil }

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(code int)        { w.code = code }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newApp(t testing.TB, mws ...common.MiddlewareFn) *core.App {
	t.Helper()
	app := core.New()
	app.SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(mws...)
	app.Create(core.ModuleBuilder().Controllers(okController{}).Build())
	return app
}

func serve(app *core.App, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func request(method, target string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func panicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			text, panicked = fmt.Sprint(r), true
		}
	}()
	fn()
	return "", false
}

func field(s inspect.Snapshot, key string) any {
	for _, f := range s.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}
