package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/test"
	ctxlookalike "github.com/dangduoc08/ginject/internal/test/lookalike/ctx"
	"github.com/dangduoc08/ginject/trace"
)

type appLookalikeController struct {
	common.HTTP
}

func (c appLookalikeController) NewController() Controller { return c }
func (c appLookalikeController) READ_lookalike(x *ctxlookalike.HTTPContext) string {
	return ""
}

func TestApp_LookalikeParameterPanicsAtCreate(t *testing.T) {
	app := New()

	rec := planPanicOf(func() {
		app.Create(ModuleBuilder().Controllers(appLookalikeController{}).Build())
	})

	want := planUnresolved("func(*ctx.HTTPContext) string", 0)
	if rec == nil || !strings.Contains(fmt.Sprint(rec), want) {
		t.Error(test.DiffMessage(rec, want, "bug 1, B1, B2: a lookalike *ctx.HTTPContext stops Create()"))
	}
}

type appResultController struct {
	common.HTTP
}

func (c appResultController) NewController() Controller { return c }
func (c appResultController) READ_pointer() *returnUser {
	return &returnUser{Name: "ann"}
}
func (c appResultController) READ_created() (int, *returnUser) {
	return http.StatusAccepted, &returnUser{Name: "bob"}
}
func (c appResultController) READ_array() [3]int  { return [3]int{1, 2, 3} }
func (c appResultController) READ_anystring() any { return "ok" }
func (c appResultController) READ_anyint() any    { return 7 }
func (c appResultController) READ_wrongpipe(p nilPipe) string {
	return "unreachable"
}

func serveResult(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	app := New()
	app.Create(ModuleBuilder().Controllers(appResultController{}).Build())
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestApp_HandlerReturningPointerWritesJSON(t *testing.T) {
	w := serveResult(t, "/pointer")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "bug 2, B19: status"))
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"name":"ann"}` {
		t.Error(test.DiffMessage(got, `{"name":"ann"}`, "bug 2, B19: a pointer result is JSON, never an address"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "bug 2, B19: content type"))
	}
}

func TestApp_HandlerReturningStatusAndPointerWritesJSON(t *testing.T) {
	w := serveResult(t, "/created")
	if w.Code != http.StatusAccepted {
		t.Error(test.DiffMessage(w.Code, http.StatusAccepted, "bug 2, B20: the first result sets the status"))
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"name":"bob"}` {
		t.Error(test.DiffMessage(got, `{"name":"bob"}`, "bug 2, B19: the second result is JSON"))
	}
}

func TestApp_HandlerReturningArrayWritesJSON(t *testing.T) {
	w := serveResult(t, "/array")
	if got := strings.TrimSpace(w.Body.String()); got != "[1,2,3]" {
		t.Error(test.DiffMessage(got, "[1,2,3]", "bug 4, B19: an array result is JSON"))
	}
}

func TestApp_HandlerReturningAnyStringWritesText(t *testing.T) {
	w := serveResult(t, "/anystring")
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "bug 3, B18: status"))
	}
	if got := w.Body.String(); got != "ok" {
		t.Error(test.DiffMessage(got, "ok", "bug 3, B18: an any holding a string is text"))
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/plain; charset=utf-8", "bug 3, B18: content type"))
	}
}

func TestApp_HandlerReturningAnyIntKeepsJSONContentType(t *testing.T) {
	w := serveResult(t, "/anyint")
	if got := w.Body.String(); got != "7" {
		t.Error(test.DiffMessage(got, "7", "B18 (Q2): an any holding an int keeps its body"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "B18 (Q2): an any holding an int keeps its content type"))
	}
}

func TestApp_PipeWrongResultIs500(t *testing.T) {
	w := serveResult(t, "/wrongpipe")
	if w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, "bug 5, B11: a wrong Transform result is a 500"))
	}
	if strings.Contains(w.Body.String(), "reflect:") {
		t.Error(test.DiffMessage(w.Body.String(), "no reflection text", "bug 5, B11: the client never sees reflection's message"))
	}
	if strings.Contains(w.Body.String(), "unreachable") {
		t.Error(test.DiffMessage(w.Body.String(), "no handler output", "B11: the handler is not called"))
	}
}

var appServeDir string

type appServeController struct {
	common.HTTP
}

func (c appServeController) NewController() Controller { return c }
func (c appServeController) SERVE_logo_png_FILE() string {
	return filepath.Join(appServeDir, "logo.png")
}
func (c appServeController) READ_named() string { return "named" }

func handlerEventNames(t *testing.T, path string) []string {
	t.Helper()
	appServeDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(appServeDir, "logo.png"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := New()
	var mu sync.Mutex
	var names []string
	app.event.On(trace.EventName, func(args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if te := args[0].(trace.Event); te.Stage == trace.StageHandler {
			names = append(names, te.Name)
		}
	})
	app.Create(ModuleBuilder().Controllers(appServeController{}).Build())

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), names...)
}

func TestApp_ServeRouteHandlerEventHasName(t *testing.T) {
	names := handlerEventNames(t, "/logo.png")
	if len(names) != 1 || names[0] != "SERVE_logo_png_FILE" {
		t.Error(test.DiffMessage(names, []string{"SERVE_logo_png_FILE"}, "bug 7, B16: the handler event of a SERVE route carries the method name"))
	}
}

func TestApp_ReadRouteHandlerEventHasName(t *testing.T) {
	names := handlerEventNames(t, "/named")
	if len(names) != 1 || names[0] != "READ_named" {
		t.Error(test.DiffMessage(names, []string{"READ_named"}, "B16: the handler event of a READ route carries the method name"))
	}
}
