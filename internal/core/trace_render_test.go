package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/trace"
)

func traceRenderApp(t *testing.T, files map[string]string, listen bool, controllers ...Controller) (*App, *traceRecorder) {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{pgPages{}}
		files = pgPageFileMap(files)
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	var rec *traceRecorder
	if listen {
		rec = newTraceRecorder(app)
	}
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, rec
}

func traceIndex(events []trace.Event, stage string) int {
	for i, e := range events {
		if e.Stage == stage {
			return i
		}
	}
	return -1
}

func traceStages(events []trace.Event, stage string) []trace.Event {
	var out []trace.Event
	for _, e := range events {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

func TestTrace_RenderEvent(t *testing.T) {
	app, rec := traceRenderApp(t, map[string]string{"bookings/list.html": "LIST"}, true, pgBookings{})

	w := createGet(app, "/bookings/list")
	if w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R41: the page renders"))
	}
	events := rec.all()
	renders := traceStages(events, trace.StageRender)
	if len(renders) != 1 {
		t.Fatal(test.DiffMessage(len(renders), 1, "R41: one render event per render"))
	}
	e := renders[0]
	if e.Name != "bookings/list" || e.Transport != trace.TransportHTTP || e.Code == http.StatusInternalServerError || e.Duration < 0 {
		t.Error(test.DiffMessage([]any{e.Name, e.Transport}, []any{"bookings/list", trace.TransportHTTP}, "R41: Name and Transport"))
	}

	handler, render, complete := traceIndex(events, trace.StageHandler), traceIndex(events, trace.StageRender), traceIndex(events, trace.StageComplete)
	if handler < 0 || handler >= render || render >= complete {
		t.Error(test.DiffMessage([]int{handler, render, complete}, "handler < render < complete", "R41: the render event comes after handler and before complete"))
	}

	h := events[handler]
	if e.ID == "" || e.ID != h.ID || e.TraceID != h.TraceID || e.ParentID != e.ID {
		t.Error(test.DiffMessage([]any{e.ID, e.TraceID, e.ParentID}, []any{h.ID, h.TraceID, h.ID}, "R41: the trace ids of the request"))
	}
	if e.SpanID == "" || e.SpanID == h.SpanID {
		t.Error(test.DiffMessage(e.SpanID, "its own span id", "R41: a span id of its own"))
	}
}

func TestTrace_RenderEventOnFailure(t *testing.T) {
	app, rec := traceRenderApp(t, map[string]string{"rows.html": `{{template "partials/x" (dict "odd")}}`, "partials/x.html": "x"}, true)

	w := createGet(app, "/rows")
	if w.Code != http.StatusInternalServerError {
		t.Fatal(test.DiffMessage(w.Code, http.StatusInternalServerError, "R41: the render fails"))
	}
	events := rec.all()
	renders := traceStages(events, trace.StageRender)
	if len(renders) == 0 {
		t.Fatal(test.DiffMessage(0, "at least one", "R41: a failed render emits its event"))
	}
	if renders[0].Name != "rows" || renders[0].Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage([]any{renders[0].Name, renders[0].Code}, []any{"rows", http.StatusInternalServerError}, "R41: Name and Code 500 of the failed render"))
	}
	if render, complete := traceIndex(events, trace.StageRender), traceIndex(events, trace.StageComplete); render > complete || traceIndex(events, trace.StageHandler) > render {
		t.Error(test.DiffMessage([]int{render, complete}, "render before complete", "R41: order on failure"))
	}
}

func TestTrace_RenderEventNamesTheErrorPage(t *testing.T) {
	custom, rec := traceRenderApp(t, map[string]string{"404.html": `<p>{{.Code}}</p>`}, true)
	custom.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	custom.ServeHTTP(httptest.NewRecorder(), r)
	renders := traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "404" || renders[0].Transport != trace.TransportHTTP {
		t.Error(test.DiffMessage(renders, "one render event named 404 (the JSON 404 renders nothing)", "R41: 404.html"))
	}

	class, rec := traceRenderApp(t, map[string]string{"4xx.html": `<p>{{.Code}}</p>`}, true)
	r = httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	class.ServeHTTP(httptest.NewRecorder(), r)
	renders = traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "4xx" {
		t.Error(test.DiffMessage(renders, "one render event named 4xx", "R41: 4xx.html"))
	}

	builtIn, rec := traceRenderApp(t, nil, true)
	r = httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.Header.Set("Accept", "text/html")
	builtIn.ServeHTTP(httptest.NewRecorder(), r)
	renders = traceStages(rec.all(), trace.StageRender)
	if len(renders) != 1 || renders[0].Name != "error (built-in)" {
		t.Error(test.DiffMessage(renders, "one render event named error (built-in)", "R41: the built-in page"))
	}
}

func TestTrace_NoRenderEventWhenOff(t *testing.T) {
	app, _ := traceRenderApp(t, map[string]string{"bookings/list.html": "LIST", "rows.html": `{{template "partials/x" (dict "odd")}}`, "partials/x.html": "x"}, false, pgBookings{}, pgRowsOnly{})
	var seen int
	stop := app.tracer.Listen(func(trace.Event) { seen++ })
	stop()

	if w := createGet(app, "/bookings/list"); w.Body.String() != "LIST" {
		t.Error(test.DiffMessage(w.Body.String(), "LIST", "R41: the page still renders"))
	}
	if w := createGet(app, "/rows"); w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, "R41: a failure still answers"))
	}
	if seen != 0 {
		t.Error(test.DiffMessage(seen, 0, "R41: with tracing off no render event is emitted"))
	}
}

type pgRowsOnly struct{ common.HTTP }

func (c pgRowsOnly) NewController() Controller { return c }
func (c pgRowsOnly) RENDER_rows()              {}

func TestTrace_RenderEventNamesTheTemplatePath(t *testing.T) {
	files := map[string]string{"bookings/index.html": "LIST", "bookings/{id}.html": "ONE {{.Who}}"}
	app, rec := traceRenderApp(t, files, true, rnBookings{})

	createGet(app, "/bookings")
	createGet(app, "/bookings/7")
	var names []string
	for _, e := range traceStages(rec.all(), trace.StageRender) {
		names = append(names, e.Name)
	}
	if len(names) != 2 || names[0] != "bookings/index" || names[1] != "bookings/{id}" {
		t.Error(test.DiffMessage(names, []string{"bookings/index", "bookings/{id}"}, "R41: Name is the template path without .html"))
	}
}
