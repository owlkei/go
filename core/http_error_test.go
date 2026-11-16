package core

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/trace"
)

type lcLogEntry struct {
	level string
	msg   string
	args  []any
}

func (e lcLogEntry) field(key string) any {
	for i := 0; i+1 < len(e.args); i += 2 {
		if k, ok := e.args[i].(string); ok && k == key {
			return e.args[i+1]
		}
	}
	return nil
}

type lcRecordingLogger struct {
	mu      sync.Mutex
	entries []lcLogEntry
}

func (l *lcRecordingLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, lcLogEntry{level: level, msg: msg, args: append([]any(nil), args...)})
}

func (l *lcRecordingLogger) named(msg string) []lcLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []lcLogEntry
	for _, e := range l.entries {
		if e.msg == msg {
			out = append(out, e)
		}
	}
	return out
}

func (l *lcRecordingLogger) Debug(msg string, args ...any) { l.record("debug", msg, args) }
func (l *lcRecordingLogger) Info(msg string, args ...any)  { l.record("info", msg, args) }
func (l *lcRecordingLogger) Warn(msg string, args ...any)  { l.record("warn", msg, args) }
func (l *lcRecordingLogger) Error(msg string, args ...any) { l.record("error", msg, args) }
func (l *lcRecordingLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args) }

func lcText(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return fmt.Sprint(v)
}

func lcRuntimeErrorPanic(*ctx.HTTPContext) any {
	var items []int
	i := len(os.Args) + 10
	return items[i]
}

func lcRuntimeErrorMiddleware(http.ResponseWriter, *http.Request, ctx.Next) {
	_ = lcRuntimeErrorPanic(nil)
}

func lcNilPanic(*ctx.HTTPContext) any {
	var v any
	panic(v)
}

func lcNilPanicMiddleware(http.ResponseWriter, *http.Request, ctx.Next) {
	var v any
	panic(v)
}

func lcErrorResponses(t *testing.T, value any) (fromMiddleware, fromHandler *httptest.ResponseRecorder) {
	t.Helper()
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", lcPanicMiddleware(value))
	fromMiddleware = lcServe(app, http.MethodGet, "/bare")

	app = lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcPanicWith(value))
	fromHandler = lcServe(app, http.MethodGet, "/bare")
	return fromMiddleware, fromHandler
}

func lcExpectBothAnswer(t *testing.T, value any, code int, body, desc string) {
	t.Helper()
	fromMiddleware, fromHandler := lcErrorResponses(t, value)
	lcExpectResponse(t, fromMiddleware, code, body, desc+" (HTTP layer)")
	lcExpectResponse(t, fromHandler, code, body, desc+" (built-in filter)")
}

func TestHTTPError_Format(t *testing.T) {
	fromMiddleware, fromHandler := lcErrorResponses(t, exception.BadRequestException("bad input"))

	for name, w := range map[string]*httptest.ResponseRecorder{"HTTP layer": fromMiddleware, "built-in filter": fromHandler} {
		lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"bad input"}`, "E1: the exact body, "+name)
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Error(test.DiffMessage(got, "application/json", "E1: Content-Type, "+name))
		}
	}
}

func lcDirtyHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Length", "5")
	h.Set("Content-Encoding", "gzip")
	h.Set("ETag", `"abc"`)
	h.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	h.Set("X-Custom", "kept")
	h.Set("Access-Control-Allow-Origin", "*")
}

func lcExpectEntityHeadersRemoved(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	for _, name := range []string{"Content-Length", "Content-Encoding", "ETag", "Last-Modified"} {
		if got := w.Header().Get(name); got != "" {
			t.Error(test.DiffMessage(got, "", "E1: "+name+" set earlier in the request is removed, "+desc))
		}
	}
	for name, want := range map[string]string{"X-Custom": "kept", "Access-Control-Allow-Origin": "*"} {
		if got := w.Header().Get(name); got != want {
			t.Error(test.DiffMessage(got, want, "E1: "+name+" is kept, "+desc))
		}
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "E1: X-Request-Id is kept, "+desc))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "E1: Content-Type is JSON, "+desc))
	}
}

func TestHTTPError_RemovesEntityHeaders(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		lcDirtyHeaders(w)
		panic(exception.BadRequestException("bad input"))
	})
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectEntityHeadersRemoved(t, w, "HTTP layer")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"bad input"}`, "E1: the body")
}

func TestHTTPError_BuiltInFilterRemovesEntityHeaders(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, next ctx.Next) {
		lcDirtyHeaders(w)
		next()
	})
	lcSetHandler(lcPanicWith(exception.BadRequestException("bad input")))
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectEntityHeadersRemoved(t, w, "built-in filter")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"bad input"}`, "E9: the body")
}

func TestHTTPError_ExceptionValueAnswersWithItsCode(t *testing.T) {
	lcExpectBothAnswer(t, exception.NotFoundException("no user"), http.StatusNotFound, `{"code":404,"error":"Not Found","message":"no user"}`, "E2: an exception value answers with its code, error and message")
}

func TestHTTPError_ExceptionPointerWrapped(t *testing.T) {
	ex := exception.ConflictException("taken", exception.ExceptionOptions{Description: "duplicate"})
	lcExpectBothAnswer(t, &ex, http.StatusConflict, `{"code":409,"error":"duplicate","message":"taken"}`, "E2: a non-nil *exception.Exception answers with its code, error and message")
}

func TestHTTPError_ExceptionWrappedTwoLevelsDeep(t *testing.T) {
	ex := exception.ConflictException("taken")
	var inner error = &ex
	wrapped := fmt.Errorf("service: %w", fmt.Errorf("repository: %w", inner))
	lcExpectBothAnswer(t, wrapped, http.StatusConflict, `{"code":409,"error":"Conflict","message":"taken"}`, "E2: an error that wraps a *exception.Exception two levels deep answers with it")
}

func TestHTTPError_ExceptionValueWrappedTwoLevelsDeep(t *testing.T) {
	ex := exception.ConflictException("taken")
	wrapped := fmt.Errorf("service: %w", fmt.Errorf("repository: %w", ex))
	lcExpectBothAnswer(t, wrapped, http.StatusConflict, `{"code":409,"error":"Conflict","message":"taken"}`, "E2: an error that wraps an exception.Exception value two levels deep answers with it")
}

func TestHTTPError_NilExceptionPointerIsNotAnException(t *testing.T) {
	var ex *exception.Exception
	lcExpectBothAnswer(t, ex, http.StatusInternalServerError, wantInternalErrorJSON, "E2: a nil *exception.Exception is not an exception, E3 applies")
}

func TestHTTPError_EmptyFieldsFallBack(t *testing.T) {
	lcExpectBothAnswer(t, exception.NotFoundException(""), http.StatusNotFound, `{"code":404,"error":"Not Found","message":"Unhandled exception has occurred"}`, "E2: an empty message becomes the default message")
	lcExpectBothAnswer(t, exception.Exception{}, http.StatusInternalServerError, `{"code":500,"error":"Internal Server Error","message":"Unhandled exception has occurred"}`, "E2, E6: the zero exception answers 500 with the default texts")
}

func TestHTTPError_StringPanicIsFixed500(t *testing.T) {
	lcExpectBothAnswer(t, "handler secret: /etc/passwd", http.StatusInternalServerError, wantInternalErrorJSON, "bug 2, E3: a string panic never reaches the client")
}

func TestHTTPError_ErrorPanicIsFixed500(t *testing.T) {
	lcExpectBothAnswer(t, errors.New("wrap: sql: no rows; dsn=postgres://u:p@h"), http.StatusInternalServerError, wantInternalErrorJSON, "bug 2, E3: an error panic never reaches the client")
}

func TestHTTPError_RuntimeErrorPanicIsFixed500(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", lcRuntimeErrorMiddleware)
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/bare"), http.StatusInternalServerError, wantInternalErrorJSON, "bug 2, E3: a runtime error in a middleware never reaches the client")

	app = lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcRuntimeErrorPanic)
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/bare"), http.StatusInternalServerError, wantInternalErrorJSON, "bug 2, E3: a runtime error in a handler never reaches the client")
}

func TestHTTPError_NilPanicIsFixed500(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", lcNilPanicMiddleware)
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/bare"), http.StatusInternalServerError, wantInternalErrorJSON, "E3: panic(nil) in a middleware answers the fixed 500")

	app = lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcNilPanic)
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/bare"), http.StatusInternalServerError, wantInternalErrorJSON, "E3: panic(nil) in a handler answers the fixed 500")
}

func TestHTTPError_ArbitraryValuePanicIsFixed500(t *testing.T) {
	type secret struct{ Token string }
	lcExpectBothAnswer(t, secret{Token: "s3cr3t"}, http.StatusInternalServerError, wantInternalErrorJSON, "E3: any other panic value answers the fixed 500")
	lcExpectBothAnswer(t, 42, http.StatusInternalServerError, wantInternalErrorJSON, "E3: an int panic answers the fixed 500")
}

func TestHTTPError_LogsValueStackAndID(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(lcPanicWith("handler secret: /etc/passwd"))
	w := lcServe(app, http.MethodGet, "/bare")

	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "E3: the framework logs UnhandledPanic once"))
	}
	e := entries[0]
	if e.level != "error" {
		t.Error(test.DiffMessage(e.level, "error", "E3: the line is logged with Error"))
	}
	if got := e.field("id"); got != w.Header().Get("X-Request-Id") || got == "" {
		t.Error(test.DiffMessage(got, w.Header().Get("X-Request-Id"), "E3: the line carries the request ID of the X-Request-Id header"))
	}
	if got := e.field("method"); got != http.MethodGet {
		t.Error(test.DiffMessage(got, http.MethodGet, "E3: the line carries the method"))
	}
	if got := e.field("path"); got != "/bare" {
		t.Error(test.DiffMessage(got, "/bare", "E3: the line carries the path"))
	}
	if got := lcText(e.field("panic")); !strings.Contains(got, "handler secret: /etc/passwd") {
		t.Error(test.DiffMessage(got, "handler secret: /etc/passwd", "E3: the line carries the panic value"))
	}
	stack := lcText(e.field("stack"))
	if stack == "" || stack == "<nil>" {
		t.Error(test.DiffMessage(stack, "<a goroutine stack>", "E3: the line carries the stack"))
	}
	if len(stack) > 4096 {
		t.Error(test.DiffMessage(len(stack), "<= 4096", "E3: the stack is at most 4 KiB"))
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Error(test.DiffMessage(w.Body.String(), wantInternalErrorJSON, "E3: nothing of the value is in the response"))
	}
}

func TestHTTPError_MiddlewareStringPanicIsLoggedOnce(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetMiddleware("m1", lcPanicMiddleware("pq: connection refused 10.0.0.5"))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, wantInternalErrorJSON, "bug 3, E3: the middleware's text never reaches the client")
	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "E3: a middleware's string panic is logged once"))
	}
	if got := lcText(entries[0].field("panic")); !strings.Contains(got, "10.0.0.5") {
		t.Error(test.DiffMessage(got, "pq: connection refused 10.0.0.5", "E3: the log line carries the value"))
	}
}

func TestHTTPError_ExceptionPanicIsNotLoggedAsUnhandled(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(lcPanicWith(exception.BadRequestException("bad input")))
	lcServe(app, http.MethodGet, "/bare")

	if n := len(logger.named("UnhandledPanic")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "E3: an exception is the developer's answer, not an unhandled panic"))
	}
}

func TestHTTPError_AfterHandlerWrote(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		c.Text("first")
		panic(exception.BadRequestException("late"))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusOK, "first", "bug 5, E4: the status and body stay what was sent")
	lcExpectEntries(t, lcEntries("filter:"), nil, "E4: no filter runs")
	entries := logger.named("ErrorAfterResponse")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "E4: the framework logs ErrorAfterResponse once"))
	}
	e := entries[0]
	if got := e.field("code"); got != http.StatusOK {
		t.Error(test.DiffMessage(got, http.StatusOK, "E4: the line carries the status sent"))
	}
	if got := e.field("id"); got != w.Header().Get("X-Request-Id") {
		t.Error(test.DiffMessage(got, w.Header().Get("X-Request-Id"), "E4: the line carries the request ID"))
	}
	if got := e.field("method"); got != http.MethodGet {
		t.Error(test.DiffMessage(got, http.MethodGet, "E4: the line carries the method"))
	}
	if got := e.field("path"); got != "/ok" {
		t.Error(test.DiffMessage(got, "/ok", "E4: the line carries the path"))
	}
	if got := lcText(e.field("panic")); !strings.Contains(got, "late") {
		t.Error(test.DiffMessage(got, "late", "E4: the line carries the exception's message"))
	}
	if stack := lcText(e.field("stack")); stack == "" || len(stack) > 4096 {
		t.Error(test.DiffMessage(len(stack), "1..4096", "E4: the line carries a stack of at most 4 KiB"))
	}
}

func TestHTTPError_AfterHandlerWroteStringPanic(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		c.Status(http.StatusAccepted).JSON(ctx.Map{"ok": true})
		panic("late secret")
	})
	w := lcServe(app, http.MethodGet, "/bare")

	lcExpectResponse(t, w, http.StatusAccepted, `{"ok":true}`, "E4: a string panic after the response writes nothing more")
	if n := len(logger.named("ErrorAfterResponse")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "E4: one ErrorAfterResponse line"))
	}
	if n := len(logger.named("UnhandledPanic")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "E4: the line of E3 is not written as well"))
	}
}

func TestHTTPError_AfterMiddlewareWrote(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetMiddleware("g2", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("mw"))
		panic("late")
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusAccepted, "mw", "E4: the middleware's response stays")
	lcExpectEntries(t, lcEntries("filter:"), nil, "E4: no filter runs")
	entries := logger.named("ErrorAfterResponse")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "E4: one ErrorAfterResponse line"))
	}
	if got := entries[0].field("code"); got != http.StatusAccepted {
		t.Error(test.DiffMessage(got, http.StatusAccepted, "E4: the line carries the status sent"))
	}
}

func TestHTTPError_AfterServeFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		http.ServeFile(c.ResponseWriter, c.Request, file)
		panic(exception.BadRequestException("late"))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusOK, "file body", "E4: a file served by http.ServeFile stays")
	lcExpectEntries(t, lcEntries("filter:"), nil, "E4: no filter runs")
	if n := len(logger.named("ErrorAfterResponse")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "E4: one ErrorAfterResponse line"))
	}
}

func TestHTTPError_AfterRedirect(t *testing.T) {
	app := lcNewApp(t, lcConfig{})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		c.Redirect("/elsewhere")
		panic(exception.BadRequestException("late"))
	})
	w := lcServe(app, http.MethodGet, "/ok")

	if w.Code != http.StatusMovedPermanently {
		t.Error(test.DiffMessage(w.Code, http.StatusMovedPermanently, "E4: the redirect's status stays"))
	}
	if got := w.Header().Get("Location"); got != "/elsewhere" {
		t.Error(test.DiffMessage(got, "/elsewhere", "E4: the redirect's Location stays"))
	}
	lcExpectEntries(t, lcEntries("filter:"), nil, "E4: no filter runs")
}

type lcStrictWriter struct {
	header http.Header
	status int
	infos  []int
	body   bytes.Buffer
}

func (w *lcStrictWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *lcStrictWriter) WriteHeader(code int) {
	if code < 100 || code > 999 {
		panic("invalid WriteHeader code " + strconv.Itoa(code))
	}
	if code >= 100 && code <= 199 {
		w.infos = append(w.infos, code)
		return
	}
	if w.status == 0 {
		w.status = code
	}
}

func (w *lcStrictWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func TestHTTPError_InformationalStatusDoesNotCount(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, next ctx.Next) {
		w.WriteHeader(http.StatusEarlyHints)
		next()
	})
	lcSetHandler(lcPanicWith(exception.BadRequestException("bad input")))
	w := &lcStrictWriter{}
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bare", nil))

	if w.status != http.StatusBadRequest {
		t.Error(test.DiffMessage(w.status, http.StatusBadRequest, "E4: an informational status is not a response, the error is still answered"))
	}
	if got := w.body.String(); got != `{"code":400,"error":"Bad Request","message":"bad input"}` {
		t.Error(test.DiffMessage(got, `{"code":400,"error":"Bad Request","message":"bad input"}`, "E4: the error body is written"))
	}
	if n := len(logger.named("ErrorAfterResponse")); n != 0 {
		t.Error(test.DiffMessage(n, 0, "E4: no ErrorAfterResponse line for an informational status"))
	}
}

func TestHTTPError_AbortHandlerRepanics(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetHandler(lcPanicWith(http.ErrAbortHandler))
	w := httptest.NewRecorder()

	var rec any
	func() {
		defer func() { rec = recover() }()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	}()

	if rec != http.ErrAbortHandler {
		t.Fatal(test.DiffMessage(rec, http.ErrAbortHandler, "E5: http.ErrAbortHandler is re-panicked for net/http"))
	}
	lcExpectEntries(t, lcEntries("filter:"), nil, "E5: no filter runs")
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "E5: nothing is written"))
	}
	if len(logger.named("UnhandledPanic"))+len(logger.named("ErrorAfterResponse")) != 0 {
		t.Error(test.DiffMessage(logger.entries, "<no line>", "E5: nothing is logged"))
	}
}

func TestHTTPError_AbortHandlerInMiddlewareRepanics(t *testing.T) {
	app := lcNewApp(t, lcConfig{})
	lcSetMiddleware("g1", lcPanicMiddleware(http.ErrAbortHandler))
	w := httptest.NewRecorder()

	var rec any
	func() {
		defer func() { rec = recover() }()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	}()

	if rec != http.ErrAbortHandler {
		t.Error(test.DiffMessage(rec, http.ErrAbortHandler, "E5: http.ErrAbortHandler from a middleware is re-panicked"))
	}
	if w.Body.Len() != 0 {
		t.Error(test.DiffMessage(w.Body.String(), "", "E5: nothing is written"))
	}
}

func TestHTTPError_ServesAfterAbortHandler(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(http.ErrAbortHandler))
	func() {
		defer func() { _ = recover() }()
		lcServe(app, http.MethodGet, "/ok")
	}()

	lcSetHandler(nil)
	w := lcServe(app, http.MethodGet, "/ok")
	lcExpectResponse(t, w, http.StatusOK, "H>m2>m1>g2>g1", "E5: the app keeps serving after an aborted request")
}

func TestHTTPError_InvalidCodeAnswers500(t *testing.T) {
	for _, code := range []int{0, 7, 200, 399, 600, 999, 1008, 4001, -1} {
		ex := exception.NewException("x", code, "custom error")
		lcExpectBothAnswer(t, ex, http.StatusInternalServerError, `{"code":500,"error":"custom error","message":"x"}`, "bug 14, E6: code "+strconv.Itoa(code)+" answers 500 and keeps error and message")
	}
}

func TestHTTPError_InvalidCodeNeverCallsWriteHeaderWithIt(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcPanicWith(exception.NewException("x", 7, "custom error")))
	w := &lcStrictWriter{}
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bare", nil))

	if w.status != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.status, http.StatusInternalServerError, "bug 14, E6: WriteHeader is called with 500, never with 7"))
	}
	if got := w.body.String(); got != `{"code":500,"error":"custom error","message":"x"}` {
		t.Error(test.DiffMessage(got, `{"code":500,"error":"custom error","message":"x"}`, "E6: the body is written"))
	}
}

func TestHTTPError_BoundaryCodesAreKept(t *testing.T) {
	lcExpectBothAnswer(t, exception.NewException("x", 400, "custom error"), 400, `{"code":400,"error":"custom error","message":"x"}`, "E6: 400 is kept")
	lcExpectBothAnswer(t, exception.NewException("x", 599, "custom error"), 599, `{"code":599,"error":"custom error","message":"x"}`, "E6: 599 is kept")
}

func TestHTTPError_FilterNeverSeesPanicText(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith("handler secret: /etc/passwd"))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, "F[mf2]Internal Server Error", "bug 2, E7: a filter that prints GetMessage() prints the fixed text")
	got, ok := lcCaughtBy("mf2")
	if !ok || got.GetCode() != http.StatusInternalServerError || got.GetMessage() != "Internal Server Error" {
		t.Error(test.DiffMessage(got, "500 Internal Server Error", "E7: the filter receives InternalServerErrorException(\"Internal Server Error\")"))
	}
}

func TestHTTPError_FilterReceivesAnExceptionForAnError(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(errors.New("dsn=postgres://u:p@h")))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, "F[mf2]Internal Server Error", "E7: an error panic reaches the filter as the fixed exception")
}

func TestHTTPError_FilterPanicTextNotPassedOn(t *testing.T) {
	app := lcApp(t)
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcFilterPanicWith("filter blew up: internal detail"))
	w := lcServe(app, http.MethodGet, "/ok")

	lcExpectResponse(t, w, http.StatusInternalServerError, "F[mf1]Internal Server Error", "bug 4, E7: the next filter receives the fixed exception, not the filter's panic text")
}

func TestHTTPError_FilterPanicIsLogged(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{logger: logger})
	lcSetHandler(lcPanicWith(exception.BadRequestException("boom")))
	lcSetFilter("mf2", lcFilterPanicWith("filter blew up: internal detail"))
	lcServe(app, http.MethodGet, "/ok")

	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 || !strings.Contains(lcText(entries[0].field("panic")), "internal detail") {
		t.Error(test.DiffMessage(len(entries), 1, "E7: a filter's non-exception panic is logged (E3) before it reaches the next filter"))
	}
}

func lcTraceApp(t *testing.T, events *[]trace.Event, mu *sync.Mutex) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{setup: func(app *App) {
		app.tracer.Listen(func(e trace.Event) {
			mu.Lock()
			defer mu.Unlock()
			*events = append(*events, e)
		})
	}})
}

func lcTraceStage(events []trace.Event, stage string) []trace.Event {
	var out []trace.Event
	for _, e := range events {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

func TestTrace_MiddlewarePanicEmitsEventWithCode(t *testing.T) {
	var mu sync.Mutex
	var events []trace.Event
	app := lcTraceApp(t, &events, &mu)
	lcSetMiddleware("g2", lcPanicMiddleware(exception.ForbiddenException("nope")))
	w := lcServe(app, http.MethodGet, "/ok")

	mu.Lock()
	defer mu.Unlock()
	middlewares := lcTraceStage(events, trace.StageMiddleware)
	if len(middlewares) != 2 {
		t.Fatal(test.DiffMessage(len(middlewares), 2, "bug 6, E8: a middleware that panics still emits its event"))
	}
	if middlewares[0].Code != 0 {
		t.Error(test.DiffMessage(middlewares[0].Code, 0, "E8: a middleware that does not panic emits Code 0"))
	}
	panicked := middlewares[1]
	if panicked.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(panicked.Code, http.StatusForbidden, "E8: the event of a panicking middleware carries the status of the error response"))
	}
	if panicked.Name != "core.lcMiddleware" || panicked.Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage([]string{panicked.Name, panicked.Transport}, []string{"core.lcMiddleware", trace.TransportHTTP}, "E8: name and transport are those of a normal event"))
	}
	if panicked.ID == "" || panicked.ID != w.Header().Get("X-Request-Id") {
		t.Error(test.DiffMessage(panicked.ID, w.Header().Get("X-Request-Id"), "E8: the event carries the request ID"))
	}
}

func TestTrace_MiddlewareStringPanicEmitsEventWith500(t *testing.T) {
	var mu sync.Mutex
	var events []trace.Event
	app := lcTraceApp(t, &events, &mu)
	lcSetMiddleware("g1", lcPanicMiddleware("secret"))
	lcServe(app, http.MethodGet, "/ok")

	mu.Lock()
	defer mu.Unlock()
	middlewares := lcTraceStage(events, trace.StageMiddleware)
	if len(middlewares) != 1 || middlewares[0].Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(middlewares, "one event with Code 500", "E8: a string panic emits an event with Code 500"))
	}
}

func TestTrace_CompleteCodeIsWrittenStatus(t *testing.T) {
	var mu sync.Mutex
	var events []trace.Event
	app := lcTraceApp(t, &events, &mu)
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ ctx.Next) {
		w.WriteHeader(http.StatusNoContent)
	})
	lcServe(app, http.MethodGet, "/ok")

	mu.Lock()
	defer mu.Unlock()
	complete := lcTraceStage(events, trace.StageComplete)
	if len(complete) != 1 || complete[0].Code != http.StatusNoContent {
		t.Error(test.DiffMessage(complete, "one complete event with Code 204", "E8: the complete event's Code is the status actually written"))
	}
}

func TestTrace_CompleteCodeOfAnErrorIsTheAnswer(t *testing.T) {
	var mu sync.Mutex
	var events []trace.Event
	app := lcTraceApp(t, &events, &mu)
	lcSetMiddleware("g1", lcPanicMiddleware(exception.ForbiddenException("nope")))
	lcServe(app, http.MethodGet, "/ok")

	mu.Lock()
	defer mu.Unlock()
	complete := lcTraceStage(events, trace.StageComplete)
	if len(complete) != 1 || complete[0].Code != http.StatusForbidden {
		t.Error(test.DiffMessage(complete, "one complete event with Code 403", "E8: the complete event carries the error response's status"))
	}
}

func TestGlobalHTTPExceptionFilter_EqualsHTTPError(t *testing.T) {
	values := []any{
		exception.BadRequestException("bad input"),
		exception.NotFoundException("", exception.ExceptionOptions{Description: "gone"}),
		exception.Exception{},
		exception.NewException("x", 7, "custom error"),
		exception.NewException("</script>\"& \\\n", 599),
		errors.New("secret"),
		"secret",
	}
	for i, value := range values {
		fromMiddleware, fromHandler := lcErrorResponses(t, value)

		if fromMiddleware.Code != fromHandler.Code {
			t.Error(test.DiffMessage(fromHandler.Code, fromMiddleware.Code, "E9: the built-in filter answers the HTTP layer's status for value "+strconv.Itoa(i)))
		}
		if fromMiddleware.Body.String() != fromHandler.Body.String() {
			t.Error(test.DiffMessage(fromHandler.Body.String(), fromMiddleware.Body.String(), "E9: the built-in filter writes the HTTP layer's body for value "+strconv.Itoa(i)))
		}
		if fromMiddleware.Header().Get("Content-Type") != fromHandler.Header().Get("Content-Type") {
			t.Error(test.DiffMessage(fromHandler.Header().Get("Content-Type"), fromMiddleware.Header().Get("Content-Type"), "E9: the same Content-Type for value "+strconv.Itoa(i)))
		}
	}
}

func TestHTTPError_FixedBodiesAreTheDocumentedBytes(t *testing.T) {
	bodies := map[string][2]string{
		"notFoundBody":         {string(notFoundBody), wantNotFoundJSON},
		"methodNotAllowedBody": {string(methodNotAllowedBody), wantMethodNotAllowedJSON},
		"goneBody":             {string(goneBody), wantGoneJSON},
		"internalErrorBody":    {string(internalErrorBody), wantInternalErrorJSON},
	}
	for name, pair := range bodies {
		if pair[0] != pair[1] {
			t.Error(test.DiffMessage(pair[0], pair[1], "P7: "+name+" holds the documented bytes"))
		}
	}
}

func TestAsException_RecognizesExceptionsOnly(t *testing.T) {
	ex := exception.ConflictException("taken")
	var nilPointer *exception.Exception
	var pointerError error = &ex
	cases := []struct {
		name  string
		value any
		ok    bool
	}{
		{"value", ex, true},
		{"pointer", &ex, true},
		{"wrapped pointer", fmt.Errorf("a: %w", fmt.Errorf("b: %w", pointerError)), true},
		{"wrapped value", fmt.Errorf("a: %w", ex), true},
		{"nil pointer", nilPointer, false},
		{"string", "secret", false},
		{"error", errors.New("secret"), false},
		{"nil", nil, false},
		{"int", 42, false},
	}
	for _, c := range cases {
		got, ok := asException(c.value)
		if ok != c.ok {
			t.Error(test.DiffMessage(ok, c.ok, "E2: asException for a "+c.name))
			continue
		}
		if ok && (got == nil || got.GetCode() != http.StatusConflict || got.GetMessage() != "taken") {
			t.Error(test.DiffMessage(got, "409 taken", "E2: asException returns the exception for a "+c.name))
		}
	}
}

func lcDeepPanic(depth int) any {
	if depth == 0 {
		panic("deep secret")
	}
	return lcDeepPanic(depth-1) != nil
}

func TestHTTPError_LoggedStackIsCappedAt4KiB(t *testing.T) {
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(func(*ctx.HTTPContext) any { return lcDeepPanic(300) })
	lcServe(app, http.MethodGet, "/bare")

	entries := logger.named("UnhandledPanic")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "E3: one UnhandledPanic line"))
	}
	if stack := lcText(entries[0].field("stack")); len(stack) == 0 || len(stack) > 4096 {
		t.Error(test.DiffMessage(len(stack), "1..4096", "E3: a deep stack is cut to at most 4 KiB"))
	}
}
