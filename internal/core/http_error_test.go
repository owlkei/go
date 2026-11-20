package core

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/internal/versioning"
)

func TestHTTPError_Format(t *testing.T) {
	fromMiddleware, fromHandler := lcErrorResponses(t, exception.BadRequestException("bad input"))

	for name, w := range map[string]*httptest.ResponseRecorder{"HTTP layer": fromMiddleware, "built-in filter": fromHandler} {
		lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"bad input"}`, "E1: the exact body, "+name)
		if got := w.Header().Get("Content-Type"); got != "application/json" {
			t.Error(test.DiffMessage(got, "application/json", "E1: Content-Type, "+name))
		}
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
	lcExpectBothAnswer(t, exception.Exception{}, http.StatusInternalServerError, wantInternalErrorJSON, "E2, E6, R24: the zero exception answers the fixed 500")
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
	if got := e.field("request"); got != w.Header().Get("X-Request-Id") || got == "" {
		t.Error(test.DiffMessage(got, w.Header().Get("X-Request-Id"), "E3, S7: the line carries the request ID of the X-Request-Id header through the scope's fields"))
	}
	if got := e.field("trace_id"); got == nil || got == "" {
		t.Error(test.DiffMessage(got, "a trace ID", "S7: the line carries the trace ID of the scope"))
	}
	if got := e.field("id"); got != nil {
		t.Error(test.DiffMessage(got, nil, "S7: the line no longer passes id"))
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
	if got := e.field("request"); got != w.Header().Get("X-Request-Id") || got == "" {
		t.Error(test.DiffMessage(got, w.Header().Get("X-Request-Id"), "E4, S7: the line carries the request ID through the scope's fields"))
	}
	if got := e.field("id"); got != nil {
		t.Error(test.DiffMessage(got, nil, "S7: the line no longer passes id"))
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
		lcExpectBothAnswer(t, ex, http.StatusInternalServerError, wantInternalErrorJSON, "bug 14, E6, R24: code "+strconv.Itoa(code)+" answers the fixed 500")
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
	if got := w.body.String(); got != wantInternalErrorJSON {
		t.Error(test.DiffMessage(got, wantInternalErrorJSON, "E6, R24: the fixed body is written"))
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
	if !ok || got.Code() != http.StatusInternalServerError || got.Message() != "Internal Server Error" {
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
		if ok && (got == nil || got.Code() != http.StatusConflict || got.Message() != "taken") {
			t.Error(test.DiffMessage(got, "409 taken", "E2: asException returns the exception for a "+c.name))
		}
	}
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

func TestErrorPage_ByStatus(t *testing.T) {
	const builtIn = ""
	rows := []struct {
		note  string
		files []string
		want  map[int]string
	}{
		{"none", nil, map[int]string{404: builtIn, 422: builtIn, 500: builtIn, 503: builtIn}},
		{"404.html", []string{"404"}, map[int]string{404: "404", 422: builtIn, 500: builtIn, 503: builtIn}},
		{"404, 4xx, 5xx", []string{"404", "4xx", "5xx"}, map[int]string{404: "404", 422: "4xx", 500: "5xx", 503: "5xx"}},
		{"503, 5xx", []string{"503", "5xx"}, map[int]string{404: builtIn, 422: builtIn, 500: "5xx", 503: "503"}},
	}
	for _, row := range rows {
		app, _ := pgNew(t, pgFiles(epFiles(row.files...)), epPages{}, epGuarded{})
		for code, template := range row.want {
			w := createRequest(app, http.MethodGet, "/fail?code="+strconv.Itoa(code))
			desc := "P17: " + row.note + ", a handler panic with " + strconv.Itoa(code)
			peExpectHTML(t, desc, w, code)
			if template == builtIn {
				if strings.Contains(w.Body.String(), "-file:") || !strings.Contains(w.Body.String(), strconv.Itoa(code)) {
					t.Error(test.DiffMessage(w.Body.String(), "the built-in page", desc))
				}
				continue
			}
			if want := epBody(code, template); w.Body.String() != want {
				t.Error(test.DiffMessage(w.Body.String(), want, desc))
			}
		}

		guard := createRequest(app, http.MethodGet, "/guarded")
		peExpectHTML(t, "P17: "+row.note+", a guard refusal with 422", guard, http.StatusUnprocessableEntity)
		if want := row.want[422]; want == builtIn {
			if strings.Contains(guard.Body.String(), "-file:") {
				t.Error(test.DiffMessage(guard.Body.String(), "the built-in page", "P17: "+row.note+", 422 from a guard"))
			}
		} else if guard.Body.String() != epBody(422, want) {
			t.Error(test.DiffMessage(guard.Body.String(), epBody(422, want), "P17: "+row.note+", 422 from a guard"))
		}

		missing := createRequest(app, http.MethodGet, "/nowhere", "Accept", "text/html")
		peExpectHTML(t, "P17: "+row.note+", an unknown path with Accept text/html", missing, http.StatusNotFound)
		if want := row.want[404]; want == builtIn {
			if strings.Contains(missing.Body.String(), "-file:") {
				t.Error(test.DiffMessage(missing.Body.String(), "the built-in page", "P17: "+row.note+", 404"))
			}
		} else if missing.Body.String() != epBody(404, want) {
			t.Error(test.DiffMessage(missing.Body.String(), epBody(404, want), "P17: "+row.note+", 404"))
		}

		json := createRequest(app, http.MethodGet, "/nowhere", "Accept", "application/json")
		peExpectJSON(t, "R18: "+row.note+", JSON clients never get an error page", json, http.StatusNotFound)
	}
}

func TestErrorPage_ErrorDotHTMLHasNoRole(t *testing.T) {
	msExpectPanic(t, "P9", "invalid render: error.html has no RENDER_ route",
		cpCreateWith(map[string]string{"x.html": "x", "error.html": "{{.Code}}"}, cpPageController{}))
}

func TestErrorPage_DataAndLayout(t *testing.T) {
	files := epFiles()
	files["layout.html"] = `<html>{{template "content" .}}</html>`
	files["admin/layout.html"] = `<admin>{{template "content" .}}</admin>`
	files["admin/page.html"] = `admin page`
	files["4xx.html"] = `[{{.Code}}|{{.Status}}|{{.Message}}|{{.RequestID}}|{{printf "%T" .Code}}]`
	app, _ := pgNew(t, pgFiles(files), epPages{}, epGuarded{}, epAdmin{})

	w := createRequest(app, http.MethodGet, "/fail?code=404")
	want := "<html>[404|Not Found|it failed|" + w.Header().Get("X-Request-Id") + "|int]</html>"
	if w.Body.String() != want || w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage(w.Body.String(), want, "P18: the error page inside the root layout, with its data"))
	}
	if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage([]string{w.Header().Get("Cache-Control"), w.Header().Get("Content-Type")}, []string{"no-cache", "text/html; charset=utf-8"}, "P18, R20: headers"))
	}

	admin := createRequest(app, http.MethodGet, "/admin/page?fail=1")
	if admin.Code != http.StatusNotFound || strings.Contains(admin.Body.String(), "<admin>") || !strings.HasPrefix(admin.Body.String(), "<html>[404|") {
		t.Error(test.DiffMessage(admin.Body.String(), "the root layout only", "P18: a nested layout never wraps an error page"))
	}

	post := createRequest(app, http.MethodPost, "/fail", "Accept", "text/html")
	if post.Code != http.StatusMethodNotAllowed || !strings.HasPrefix(post.Body.String(), "<html>[405|") || !strings.Contains(post.Header().Get("Allow"), "GET") {
		t.Error(test.DiffMessage(post.Body.String(), "a 405 error page with Allow", "P17, R20: a 405 uses 4xx.html and keeps Allow"))
	}
}

func TestErrorPage_FailureFallsBackToBuiltIn(t *testing.T) {
	files := epFiles()
	files["5xx.html"] = `5XXMARK{{index .Code 0}}`
	files["503.html"] = `503MARK`
	files["layout.html"] = `{{template "content" .}}`
	app, logger := pgNew(t, pgFiles(files), epPages{}, epGuarded{})

	w := createRequest(app, http.MethodGet, "/fail?code=500")
	peExpectHTML(t, "P19: 5xx.html fails", w, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "5XXMARK") || !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Error(test.DiffMessage(w.Body.String(), "the built-in page", "P19: no half page"))
	}
	entries := logger.named("RenderFailed")
	if len(entries) != 1 || entries[0].field("template") != "5xx" {
		t.Error(test.DiffMessage(entries, "one RenderFailed for 5xx", "P19: logged with the template name"))
	}

	w = createRequest(app, http.MethodGet, "/fail?code=503")
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "503MARK" {
		t.Error(test.DiffMessage(w.Body.String(), "503MARK", "P17: a working 503.html wins over the failing 5xx.html"))
	}
}

func TestPageError_RouteKindDecides(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("lp"), nil, peLayeredPage{}, peLayeredAPI{})
	triggers := []struct {
		name    string
		query   string
		headers []string
		code    int
	}{
		{"guard returns false", "", []string{"X-Guard", "false"}, http.StatusForbidden},
		{"guard panics", "", []string{"X-Guard", "panic"}, http.StatusForbidden},
		{"pipe panics", "?pipe=boom", nil, http.StatusBadRequest},
		{"interceptor panics", "", []string{"X-Intercept", "panic"}, http.StatusConflict},
		{"handler panics", "?h=boom", nil, http.StatusNotFound},
		{"filter re-panics", "?h=boom", []string{"X-Filter", "refilter"}, http.StatusGone},
	}
	for _, tr := range triggers {
		page := peDo(app, http.MethodGet, "/lp/run"+tr.query, append([]string{"Accept", peAcceptJSON}, tr.headers...)...)
		peExpectHTML(t, "R16: page route, "+tr.name+", Accept is ignored", page, tr.code)
		api := peDo(app, http.MethodGet, "/la/run"+tr.query, append([]string{"Accept", peAcceptHTML}, tr.headers...)...)
		peExpectJSON(t, "R17: API route, "+tr.name+", Accept is ignored", api, tr.code)
	}
	if w := peDo(app, http.MethodGet, "/lp/run", "Accept", peAcceptJSON); w.Code != http.StatusOK || w.Body.String() != "OK" {
		t.Error(test.DiffMessage(w.Body.String(), "OK", "R16: a request without a failure still renders"))
	}
}

func TestError_AcceptDecidesBeforeGuards(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	opts := &AppOptions{CSRF: &CSRFOptions{IsEnabled: true}, Hosts: []string{"example.com"}}
	app, _ := peAppOptions(t, peLayerFiles("page"), opts, setup, pePage{}, peAPI{})

	for _, route := range []string{"/page/ok", "/api/ok"} {
		peExpectHTML(t, "R18: a user middleware refusal on "+route+" with Accept text/html", peDo(app, http.MethodGet, route, "X-Mw", "panic", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
		peExpectJSON(t, "R18: a user middleware refusal on "+route+" with Accept application/json", peDo(app, http.MethodGet, route, "X-Mw", "panic", "Accept", peAcceptJSON), http.StatusServiceUnavailable)
		peExpectJSON(t, "R18: a user middleware refusal on "+route+" without Accept", peDo(app, http.MethodGet, route, "X-Mw", "panic"), http.StatusServiceUnavailable)
	}

	for _, route := range []string{"/page/boom", "/api/boom"} {
		peExpectHTML(t, "R18, R29: cross-origin POST to "+route+" with Accept text/html", peDo(app, http.MethodPost, route, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptHTML), http.StatusForbidden)
		peExpectJSON(t, "R18, R29: cross-origin POST to "+route+" with Accept application/json", peDo(app, http.MethodPost, route, "Sec-Fetch-Site", "cross-site", "Accept", peAcceptJSON), http.StatusForbidden)
	}

	peExpectHTML(t, "R18: hostcheck 421 with Accept text/html", peDo(app, http.MethodGet, "/page/ok", "Host", "evil.test", "Accept", peAcceptHTML), http.StatusMisdirectedRequest)
	peExpectJSON(t, "R18: hostcheck 421 with Accept application/json", peDo(app, http.MethodGet, "/page/ok", "Host", "evil.test", "Accept", peAcceptJSON), http.StatusMisdirectedRequest)

	peExpectHTML(t, "R18: 404 with Accept text/html", peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptHTML), http.StatusNotFound)
	peExpectJSON(t, "R18: 404 with Accept application/json", peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptJSON), http.StatusNotFound)

	w := peDo(app, http.MethodPost, "/page/ok", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptHTML)
	peExpectHTML(t, "R18: 405 with Accept text/html", w, http.StatusMethodNotAllowed)
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Error(test.DiffMessage(w.Header().Get("Allow"), "an Allow header holding GET", "R18, R20: the Allow header of a 405 is kept in HTML"))
	}
	w = peDo(app, http.MethodPost, "/page/ok", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptJSON)
	peExpectJSON(t, "R18: 405 with Accept application/json", w, http.StatusMethodNotAllowed)
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Error(test.DiffMessage(w.Header().Get("Allow"), "an Allow header holding GET", "R18: the Allow header of a 405 is kept in JSON"))
	}

	if w := peDo(app, http.MethodPost, "/page/boom", "Sec-Fetch-Site", "same-origin", "Accept", peAcceptHTML); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "R29: a same-origin POST passes (the SUBMIT_ redirects)"))
	}
}

func TestError_AcceptDecidesBeforeGuardsGone(t *testing.T) {
	opts := &AppOptions{Versioning: &versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"}}
	app, _ := peAppOptions(t, peLayerFiles(), opts, nil, peAPI{})
	peExpectHTML(t, "R18: 410 with Accept text/html", peDo(app, http.MethodGet, "/api/ok", "Accept", peAcceptHTML), http.StatusGone)
	peExpectJSON(t, "R18: 410 with Accept application/json", peDo(app, http.MethodGet, "/api/ok", "Accept", peAcceptJSON), http.StatusGone)
	peExpectJSON(t, "R18: 410 without Accept", peDo(app, http.MethodGet, "/api/ok"), http.StatusGone)
}

func TestError_AcceptDecidesBeforeGuardsLoadShed(t *testing.T) {
	opts := &AppOptions{LoadShed: &LoadShedOptions{IsEnabled: true, MaxInFlight: 1, MaxQueue: -1}}
	peBlockStarted = make(chan struct{})
	peBlockRelease = make(chan struct{})
	app, _ := peAppOptions(t, peLayerFiles(), opts, nil, peBlocking{})
	done := make(chan struct{})
	go func() {
		peDo(app, http.MethodGet, "/block/hold")
		close(done)
	}()
	<-peBlockStarted

	peExpectHTML(t, "R18: loadshed 503 with Accept text/html", peDo(app, http.MethodGet, "/block/hold", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
	peExpectJSON(t, "R18: loadshed 503 with Accept application/json", peDo(app, http.MethodGet, "/block/hold", "Accept", peAcceptJSON), http.StatusServiceUnavailable)

	close(peBlockRelease)
	<-done
}

func TestError_AcceptTable(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("page"), nil, pePage{})
	cases := []struct {
		name    string
		accept  []string
		isHTML  bool
		comment string
	}{
		{"text/html", []string{"text/html"}, true, "plain"},
		{"upper case", []string{"TEXT/HTML"}, true, "case-insensitive"},
		{"mixed case", []string{"Text/Html"}, true, "case-insensitive"},
		{"browser list", []string{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}, true, "a browser's value"},
		{"q-value is not read", []string{"text/html;q=0.9,*/*"}, true, "q-values are not read"},
		{"zero q-value is not read", []string{"application/json, text/html;q=0"}, true, "q-values are not read"},
		{"two lines", []string{"application/json", "text/html"}, true, "a value of any Accept line"},
		{"star", []string{"*/*"}, false, "*/* alone"},
		{"missing", nil, false, "no Accept"},
		{"empty", []string{""}, false, "empty Accept"},
		{"xhtml alone", []string{"application/xhtml+xml"}, false, "xhtml alone"},
		{"json", []string{"application/json"}, false, "json"},
		{"text plain", []string{"text/plain"}, false, "text/plain"},
	}
	for _, tc := range cases {
		var headers []string
		for _, v := range tc.accept {
			headers = append(headers, "Accept", v)
		}
		w := peDo(app, http.MethodGet, "/nowhere", headers...)
		if tc.isHTML {
			peExpectHTML(t, "R18: Accept "+tc.name+" ("+tc.comment+")", w, http.StatusNotFound)
		} else {
			peExpectJSON(t, "R18: Accept "+tc.name+" ("+tc.comment+")", w, http.StatusNotFound)
		}
	}
}

func TestError_VaryAccept(t *testing.T) {
	app, _ := peApp(t, peLayerFiles("page"), nil, pePage{})
	for name, accept := range map[string]string{"html": peAcceptHTML, "json": peAcceptJSON, "none": ""} {
		var headers []string
		if accept != "" {
			headers = []string{"Accept", accept}
		}
		w := peDo(app, http.MethodGet, "/nowhere", headers...)
		if got := strings.ToLower(strings.Join(w.Header().Values("Vary"), ",")); !strings.Contains(got, "accept") {
			t.Error(test.DiffMessage(got, "a Vary header holding Accept", "R18: a response chosen by Accept carries Vary: Accept ("+name+")"))
		}
	}
}

func TestError_JSONWithoutRenderFS(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	app, _ := peAppOptions(t, nil, &AppOptions{CSRF: &CSRFOptions{IsEnabled: true}}, setup, peAPI{})

	w := peDo(app, http.MethodGet, "/nowhere", "Accept", peAcceptHTML)
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "R18: without Render.FS a 404 is JSON whatever Accept says")
	if got := w.Header().Values("Vary"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string(nil), "R18: without Render.FS the response is byte for byte as before, no Vary"))
	}
	peExpectJSON(t, "R18: a middleware refusal is JSON without Render.FS", peDo(app, http.MethodGet, "/api/ok", "X-Mw", "panic", "Accept", peAcceptHTML), http.StatusServiceUnavailable)
	peExpectJSON(t, "R18: csrf is JSON without Render.FS", peDo(app, http.MethodPost, "/api/boom", "Sec-Fetch-Site", "cross-site", "Accept", peAcceptHTML), http.StatusForbidden)
	peExpectJSON(t, "R17: a handler exception is JSON without Render.FS", peDo(app, http.MethodGet, "/api/boom?k=notfound", "Accept", peAcceptHTML), http.StatusNotFound)
}

func TestPageError_CustomTemplate(t *testing.T) {
	files := peFilesWith(map[string]string{
		"layout.html": `<html>{{template "content" .}}</html>`,
		"4xx.html":    `[{{.Code}}|{{.Status}}|{{.Message}}|{{.RequestID}}|{{printf "%T" .Code}}]`,
	}, "lp")
	app, _ := peApp(t, files, nil, peLayeredPage{})
	w := peDo(app, http.MethodGet, "/lp/run", "X-Guard", "panic")
	want := "<html>[403|Forbidden|guard refuses|" + w.Header().Get("X-Request-Id") + "|int]</html>"
	if w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P17, P18: 4xx.html inside the root layout with Code, Status, Message and RequestID"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "a request id", "R20: the request id is on the page and in the header"))
	}
	peExpectHTML(t, "R20: custom template", w, http.StatusForbidden)
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(got, "text/html; charset=utf-8", "R20: Content-Type"))
	}
}

func TestPageError_KeepsLayerHeaders(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	app, _ := peApp(t, peLayerFiles("page"), setup, pePage{})

	w := peDo(app, http.MethodGet, "/page/ok", "X-Mw", "retry", "Accept", peAcceptHTML)
	peExpectHTML(t, "R20: error after a layer set Retry-After", w, http.StatusServiceUnavailable)
	if got := w.Header().Get("Retry-After"); got != "7" {
		t.Error(test.DiffMessage(got, "7", "R20: Retry-After set by a layer is kept"))
	}

	w = peDo(app, http.MethodPost, "/page/ok", "Accept", peAcceptHTML)
	peExpectHTML(t, "R20: 405", w, http.StatusMethodNotAllowed)
	if got := w.Header().Get("Allow"); !strings.Contains(got, "GET") {
		t.Error(test.DiffMessage(got, "GET", "R20: Allow is kept"))
	}
}

func TestPageError_DropsBodyHeaders(t *testing.T) {
	setup := func(app *App) { app.BindGlobalMiddlewares(peMiddleware{}) }
	app, _ := peApp(t, peLayerFiles("page"), setup, pePage{})
	w := peDo(app, http.MethodGet, "/page/ok", "X-Mw", "dirty", "Accept", peAcceptHTML)

	peExpectHTML(t, "R20: error after a layer set body headers", w, http.StatusBadRequest)
	for _, name := range []string{"Content-Length", "Content-Encoding", "ETag", "Last-Modified"} {
		if got := w.Header().Get(name); got != "" {
			t.Error(test.DiffMessage(got, "", "R20: "+name+" set earlier is removed"))
		}
	}
	if got := w.Header().Get("X-Custom"); got != "kept" {
		t.Error(test.DiffMessage(got, "kept", "R20: other headers are kept"))
	}
}

func TestPageError_BuiltIn(t *testing.T) {
	files := peFilesWith(map[string]string{"layout.html": `<html>LAYOUTMARK{{template "content" .}}</html>`}, "page")
	app, _ := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=hostile")

	peExpectHTML(t, "R21: built-in page", w, http.StatusBadRequest)
	body := w.Body.String()
	lower := strings.ToLower(body)
	if !strings.HasPrefix(lower, "<!doctype html>") || !strings.Contains(lower, "<html") || !strings.Contains(lower, "</html>") || !strings.Contains(lower, "<title") {
		t.Error(test.DiffMessage(body, "a complete HTML document", "R21: a complete document"))
	}
	for _, banned := range []string{"<style", "<script", "<link", "style=", "src=", "http://", "https://", "LAYOUTMARK"} {
		if strings.Contains(body, banned) {
			t.Error(test.DiffMessage(body, "no "+banned, "R21: no layout, inline style or script, or external resource"))
		}
	}
	for _, want := range []string{"400", "Bad Request"} {
		if !strings.Contains(body, want) {
			t.Error(test.DiffMessage(body, "a body holding "+want, "R21: the code and the status text"))
		}
	}
	if strings.Contains(body, "<b>") || !strings.Contains(body, "&lt;b&gt;") {
		t.Error(test.DiffMessage(body, "the message escaped", "R21: the message is escaped"))
	}
}

func TestPageError_FallsBackToBuiltIn(t *testing.T) {
	files := peFilesWith(map[string]string{"layout.html": `<html>LAYOUTMARK{{template "content" .}}</html>`, "404.html": `ERRORMARK{{index .Code 0}}`, "4xx.html": "NEXTFILE"}, "page")
	app, logger := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=notfound")
	peExpectHTML(t, "P19: 404.html fails", w, http.StatusNotFound)
	body := w.Body.String()
	if body == "" || strings.Contains(body, "ERRORMARK") || strings.Contains(body, "LAYOUTMARK") || strings.Contains(body, "NEXTFILE") || !strings.Contains(body, "no such thing") {
		t.Error(test.DiffMessage(body, "the built-in page with the same code", "P19: no half page and the next file is not tried"))
	}
	entries := logger.named("RenderFailed")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "P19: RenderFailed is logged"))
	}
	if got := entries[0].field("template"); got != "404" {
		t.Error(test.DiffMessage(got, "404", "P19: the template field names the error page"))
	}

	files = peFilesWith(map[string]string{"layout.html": `<html>{{index .Code 0}}{{template "content" .}}</html>`, "404.html": `ERRORMARK`}, "page")
	app, logger = peApp(t, files, nil, pePage{})
	w = peDo(app, http.MethodGet, "/page/boom?k=notfound")
	peExpectHTML(t, "P19: the layout fails with error data", w, http.StatusNotFound)
	if body := w.Body.String(); body == "" || strings.Contains(body, "ERRORMARK") || !strings.Contains(body, "no such thing") {
		t.Error(test.DiffMessage(body, "the built-in page with the same code", "P19: no half page when the layout fails"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "P19: RenderFailed is logged for the layout"))
	}
}

func TestPageError_UserFilterFirst(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": "BUILTIN {{.Code}}"}, "lp")
	app, _ := peApp(t, files, nil, peLayeredPage{})

	w := peDo(app, http.MethodGet, "/lp/run?h=boom", "X-Filter", "handle")
	if w.Code != http.StatusTeapot || w.Body.String() != "handled by the user filter" {
		t.Error(test.DiffMessage(w.Code, http.StatusTeapot, "R23: a user filter answers by itself, "+w.Body.String()))
	}

	w = peDo(app, http.MethodGet, "/lp/run?h=boom", "X-Filter", "refilter")
	peExpectHTML(t, "R23: an exception the filter re-panics ends as an HTML error", w, http.StatusGone)
	if w.Body.String() != "BUILTIN 410" {
		t.Error(test.DiffMessage(w.Body.String(), "BUILTIN 410", "R23: the new exception is rendered"))
	}

	w = peDo(app, http.MethodGet, "/lp/run?h=boom")
	if w.Code != http.StatusNotFound || w.Body.String() != "BUILTIN 404" {
		t.Error(test.DiffMessage(w.Body.String(), "BUILTIN 404", "R23: a filter that passes the exception on ends as an HTML error"))
	}
}

func TestClientText_Table(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": peTextBody, "5xx.html": peTextBody}, "page")
	app, logger := peApp(t, files, nil, pePage{}, peAPI{})

	for _, tc := range peCases() {
		for _, route := range []string{"/api/boom", "/page/boom"} {
			isHTML := strings.HasPrefix(route, "/page")
			before := map[string]int{}
			for _, m := range []string{"ExceptionCause", "InternalServerError", "UnhandledPanic"} {
				before[m] = peLogCount(logger, m)
			}
			w := peDo(app, http.MethodGet, route+"?k="+tc.kind)
			desc := "R24: " + tc.kind + " on " + route

			if isHTML {
				peExpectHTML(t, desc, w, tc.code)
				if want := fmt.Sprintf("%d|%s|%s", tc.code, tc.status, tc.message); w.Body.String() != want {
					t.Error(test.DiffMessage(w.Body.String(), want, desc+": .Code, .Status and .Message"))
				}
			} else {
				peExpectJSON(t, desc, w, tc.code)
				want := exception.NewException(tc.message, tc.code, tc.status).AppendJSON(nil)
				if w.Body.String() != string(want) {
					t.Error(test.DiffMessage(w.Body.String(), string(want), desc+": code, error and message"))
				}
			}
			if strings.Contains(w.Body.String(), peSecretWords) || strings.Contains(w.Body.String(), "10.0.0.5") || strings.Contains(w.Body.String(), "secret") {
				t.Error(test.DiffMessage(w.Body.String(), "no cause text", desc+": a cause never reaches the client"))
			}
			for _, m := range []string{"ExceptionCause", "InternalServerError", "UnhandledPanic"} {
				want := 0
				if m == tc.logMsg {
					want = 1
				}
				if got := peLogCount(logger, m) - before[m]; got != want {
					t.Error(test.DiffMessage(got, want, desc+": log events named "+m))
				}
			}
		}
	}
}

func TestClientText_LoggedFields(t *testing.T) {
	app, logger := peApp(t, nil, nil, peAPI{})

	peDo(app, http.MethodGet, "/api/boom?k=cause")
	entries := logger.named("ExceptionCause")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "R24: ExceptionCause is logged once"))
	}
	e := entries[0]
	if e.level != "debug" {
		t.Error(test.DiffMessage(e.level, "debug", "R24: ExceptionCause is a Debug event"))
	}
	for key, want := range map[string]any{"method": http.MethodGet, "path": "/api/boom", "code": 400} {
		if got := e.field(key); got != want {
			t.Error(test.DiffMessage(got, want, "R24: ExceptionCause field "+key))
		}
	}
	if got := lcText(e.field("cause")); !strings.Contains(got, peSecretWords) {
		t.Error(test.DiffMessage(got, "the cause's text", "R24: ExceptionCause carries the cause"))
	}

	peDo(app, http.MethodGet, "/api/boom?k=internalcause")
	entries = logger.named("InternalServerError")
	if len(entries) != 1 {
		t.Fatal(test.DiffMessage(len(entries), 1, "R24: InternalServerError is logged once"))
	}
	e = entries[0]
	if e.level != "error" {
		t.Error(test.DiffMessage(e.level, "error", "R24: InternalServerError is an Error event"))
	}
	for key, want := range map[string]any{"method": http.MethodGet, "path": "/api/boom", "message": "db down"} {
		if got := e.field(key); got != want {
			t.Error(test.DiffMessage(got, want, "R24: InternalServerError field "+key))
		}
	}
	if got := lcText(e.field("cause")); !strings.Contains(got, "10.0.0.5") {
		t.Error(test.DiffMessage(got, "the cause's text", "R24: InternalServerError carries the cause"))
	}
}

func TestClientText_RenderFailure(t *testing.T) {
	files := peFilesWith(map[string]string{"4xx.html": peTextBody, "5xx.html": peTextBody}, "page")
	app, logger := peApp(t, files, nil, pePage{})
	w := peDo(app, http.MethodGet, "/page/boom?k=render")
	peExpectHTML(t, "R24: a render failure", w, http.StatusInternalServerError)
	if want := "500|Internal Server Error|Internal Server Error"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R24: a render failure sends the fixed text"))
	}
	if n := len(logger.named("RenderFailed")); n != 1 {
		t.Error(test.DiffMessage(n, 1, "R24: RenderFailed is logged"))
	}
}

func TestClientText_JSONShapeUnchanged(t *testing.T) {
	app, _ := peApp(t, nil, nil, peAPI{})
	for kind, want := range map[string]string{
		"wrapped":  `{"code":409,"error":"Conflict","message":"taken"}`,
		"notfound": `{"code":404,"error":"Not Found","message":"no such thing"}`,
		"internal": peFixedJSON,
		"cause":    `{"code":400,"error":"Bad Request","message":"bad input"}`,
	} {
		w := peDo(app, http.MethodGet, "/api/boom?k="+kind)
		lcExpectResponse(t, w, map[string]int{"wrapped": 409, "notfound": 404, "internal": 500, "cause": 400}[kind], want, "R24: the JSON shape and key order are unchanged ("+kind+")")
	}
}

func TestClientText_FrameworkMessages(t *testing.T) {
	files := map[string]string{"4xx.html": peTextBody, "pin/json.html": "OK", "pin/form.html": "OK"}
	app, logger := peAppOptions(t, files, &AppOptions{HTTP: &HTTPOptions{MaxBodyBytes: 64}}, nil, peBodyAPI{}, pePageIn{})
	big := strings.Repeat("a", 4096)
	tooLarge := http.StatusText(http.StatusRequestEntityTooLarge)

	w := pePost(app, "/in/json", "application/json", `{"k":"`+big+`"}`)
	want := exception.NewException("Request body too large", 413, tooLarge).AppendJSON(nil)
	lcExpectResponse(t, w, http.StatusRequestEntityTooLarge, string(want), "R24: a body over the limit")

	w = pePost(app, "/in/form", "application/x-www-form-urlencoded", "k="+big)
	lcExpectResponse(t, w, http.StatusRequestEntityTooLarge, string(want), "R24: a urlencoded form over the limit")

	w = pePost(app, "/pin/json", "application/json", `{"k":"`+big+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.String() != fmt.Sprintf("413|%s|Request body too large", tooLarge) {
		t.Error(test.DiffMessage(w.Body.String(), fmt.Sprintf("413|%s|Request body too large", tooLarge), "R24, R28: the same text in HTML"))
	}

	for _, entry := range logger.named("ExceptionCause") {
		if got := lcText(entry.field("cause")); !strings.Contains(got, "request body too large") {
			t.Error(test.DiffMessage(got, "http: request body too large", "R24: the limit error is kept as the logged cause"))
		}
	}
	if n := len(logger.named("ExceptionCause")); n != 3 {
		t.Error(test.DiffMessage(n, 3, "R24: each refusal logs its cause"))
	}

	w = pePost(app, "/in/form", "application/x-www-form-urlencoded", "k=%zz")
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"Invalid form body"}`, "R24: a malformed form keeps its message and loses the cause text in error")

	w = pePost(app, "/in/json", "application/json", `{"k":`)
	lcExpectResponse(t, w, http.StatusBadRequest, `{"code":400,"error":"Bad Request","message":"Invalid JSON body"}`, "R24: a malformed JSON body loses the cause text in error")

	w = pePost(app, "/pin/form", "application/x-www-form-urlencoded", "k=%zz")
	if w.Code != http.StatusBadRequest || w.Body.String() != "400|Bad Request|Invalid form body" {
		t.Error(test.DiffMessage(w.Body.String(), "400|Bad Request|Invalid form body", "R24, R28: the same text in HTML"))
	}
}

func TestPageError_HTMLErrorIsNotCached(t *testing.T) {
	app, _ := pgNew(t, pgPageFiles(map[string]string{
		"layout.html": `<script nonce="{{nonce}}"></script>{{template "content" .}}`,
		"404.html":    `E{{.Code}}`,
	}), pgPages{})
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-cache" {
		t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Cache-Control")}, []any{404, "no-cache"}, "an HTML error page, which may carry a nonce, is never stored by a shared cache"))
	}
}

func TestGate_Pending503JSON(t *testing.T) {
	app, _, _ := lfGateApp(t, nil, nil)

	w := createGet(app, "/lfsql/hello")

	if w.Code != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(w.Code, http.StatusServiceUnavailable, "G8: a route of a pending module"))
	}
	if w.Body.String() != lfWant503JSON {
		t.Error(test.DiffMessage(w.Body.String(), lfWant503JSON, "G8: the body of ServiceUnavailableException"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "G9: JSON"))
	}
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || seconds <= 0 {
		t.Error(test.DiffMessage(w.Header().Get("Retry-After"), "a positive integer", "G10: Retry-After while pending"))
	}
}

func TestGate_Failed503NoRetryAfter(t *testing.T) {
	app, sql, _ := lfGateApp(t, nil, nil)
	app.container.settle(sql, errors.New("lf-down"))

	w := createGet(app, "/lfsql/hello")

	if w.Code != http.StatusServiceUnavailable || w.Body.String() != lfWant503JSON {
		t.Error(test.DiffMessage(w.Code, http.StatusServiceUnavailable, "G8: a route of a failed module answers 503"))
	}
	if values := w.Header().Values("Retry-After"); len(values) != 0 {
		t.Error(test.DiffMessage(values, []string(nil), "G10: no Retry-After once failed"))
	}
}

func TestGate_Page503HTML(t *testing.T) {
	page := func(name string) map[string]string {
		files := map[string]string{"gated.html": "x"}
		for _, f := range []string{"503", "5xx"} {
			if name == f || name == "both" {
				files[f+".html"] = f + ":{{.Code}}:{{.Message}}"
			}
		}
		return files
	}
	rows := []struct {
		note, files, want string
	}{
		{"503.html wins", "both", "503:503:Service Unavailable"},
		{"5xx.html", "5xx", "5xx:503:Service Unavailable"},
		{"built-in", "none", ""},
	}
	for _, row := range rows {
		app, _, _ := lfGateApp(t, page(row.files), nil)
		for _, accept := range []string{"text/html", "application/json"} {
			w := createRequest(app, http.MethodGet, "/gated", "Accept", accept)
			peExpectHTML(t, "G9: "+row.note+", Accept "+accept, w, http.StatusServiceUnavailable)
			if row.want != "" && w.Body.String() != row.want {
				t.Error(test.DiffMessage(w.Body.String(), row.want, "G9: "+row.note+", Accept "+accept))
			}
			if row.want == "" && !strings.Contains(w.Body.String(), "503") {
				t.Error(test.DiffMessage(w.Body.String(), "the built-in 503 page", "G9: "+row.note))
			}
			if w.Header().Get("Retry-After") == "" {
				t.Error(test.DiffMessage("", "Retry-After", "G10: a pending page route sends Retry-After"))
			}
		}
	}
}

func TestGate_NoCauseInBody(t *testing.T) {
	app, sql, _ := lfGateApp(t, map[string]string{"gated.html": "x"}, nil)
	app.container.settle(sql, errors.New("lf-secret-dsn host=db.internal"))

	for _, target := range []string{"/lfsql/hello", "/gated"} {
		body := createGet(app, target).Body.String()
		for _, leak := range []string{"lf-secret-dsn", "db.internal", "sql", "gated", "lfSQL"} {
			if strings.Contains(body, leak) {
				t.Error(test.DiffMessage(body, "no "+leak, "G8, R24: the cause, the module and the dependency stay out of "+target))
			}
		}
	}
}

func TestGate_GlobalMiddlewareRunsAndCanRefuse(t *testing.T) {
	var hits atomic.Int64
	app, _, _ := lfGateApp(t, nil, func(a *App) { a.BindGlobalMiddlewares(lfCountMW{Hits: &hits}) })

	w := createGet(app, "/lfsql/hello")

	if w.Code != http.StatusServiceUnavailable || hits.Load() != 1 {
		t.Error(test.DiffMessage([]int64{int64(w.Code), hits.Load()}, []int64{503, 1}, "G8: a global middleware runs before the 503"))
	}

	refusing, _, _ := lfGateApp(t, nil, func(a *App) { a.BindGlobalMiddlewares(lfRefuseMW{}) })
	if got := createGet(refusing, "/lfsql/hello").Code; got != http.StatusUnauthorized {
		t.Error(test.DiffMessage(got, http.StatusUnauthorized, "G8: a middleware that refuses wins over the 503"))
	}
}

func TestGate_NoGuardOrFilterRuns(t *testing.T) {
	lfLayerHits.Store(0)
	app, _, _ := lfGateApp(t, nil, nil)

	w := createGet(app, "/lflayered/hello")

	if w.Code != http.StatusServiceUnavailable {
		t.Error(test.DiffMessage(w.Code, http.StatusServiceUnavailable, "G8: status"))
	}
	if got := lfLayerHits.Load(); got != 0 {
		t.Error(test.DiffMessage(got, int64(0), "G8: no guard, filter or handler of the module runs"))
	}
}

func TestGate_OtherModulesServe(t *testing.T) {
	app, _, _ := lfGateApp(t, nil, nil)

	if got := createGet(app, "/lfplain/hello").Code; got != http.StatusOK {
		t.Error(test.DiffMessage(got, http.StatusOK, "G8: a route of a ready module is not gated"))
	}
}

func TestGate_405ListsGatedMethod(t *testing.T) {
	app, _, _ := lfGateApp(t, nil, nil)

	post := createRequest(app, http.MethodPost, "/lfsql/hello")
	if post.Code != http.StatusMethodNotAllowed {
		t.Error(test.DiffMessage(post.Code, http.StatusMethodNotAllowed, "G11: another method on a gated path"))
	}
	if allow := post.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Error(test.DiffMessage(allow, "GET…", "G11: Allow lists the gated route's method"))
	}
	if got := createGet(app, "/lfsql/nothing").Code; got != http.StatusNotFound {
		t.Error(test.DiffMessage(got, http.StatusNotFound, "G11: an unknown path under a gated prefix is 404"))
	}
}
