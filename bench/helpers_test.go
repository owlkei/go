package bench

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/middlewares/helmet"
)

type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header { return w.header }

func (w *discardWriter) WriteHeader(code int) { w.code = code }

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

type bodyReader struct {
	*bytes.Reader
}

func (bodyReader) Close() error { return nil }

func benchmarkHandler(b *testing.B, handler http.Handler) {
	for _, s := range Scenarios {
		b.Run(s.Name, func(b *testing.B) {
			r := s.Request()
			payload := []byte(s.Body)
			reader := bodyReader{bytes.NewReader(payload)}
			w := &discardWriter{header: http.Header{}}

			b.ReportAllocs()
			for b.Loop() {
				reader.Reset(payload)
				r.Body = reader
				r.ContentLength = int64(len(payload))
				clear(w.header)
				w.code = 0
				handler.ServeHTTP(w, r)
			}
		})
	}
}

type bookingRow struct {
	ID    int
	Court string
	Name  string
	Time  string
}

func bookingRows() []bookingRow {
	rows := make([]bookingRow, 10)
	for i := range rows {
		rows[i] = bookingRow{ID: i + 1, Court: "Court A", Name: "Player <" + string(rune('a'+i)) + ">", Time: "18:00"}
	}
	return rows
}

var pageFiles = fstest.MapFS{
	"layout.html": {Data: []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>{{block "title" .}}Bookings{{end}}</title>` +
		`<link rel="stylesheet" href="{{asset "app.css"}}"><script nonce="{{nonce}}">document.documentElement.dataset.okTheme="light"</script></head>` +
		`<body><main>{{template "content" .}}</main></body></html>`)},
	"bookings.html": {Data: []byte(`{{define "title"}}Bookings{{end}}<table data-ok-table><thead><tr><th>#</th><th>Court</th><th>Name</th><th>Time</th></tr></thead><tbody>` +
		`{{range .Rows}}<tr><td>{{.ID}}</td><td>{{.Court}}</td><td><a href="/bookings/{{.ID}}">{{.Name}}</a></td><td>{{.Time}}</td></tr>{{end}}</tbody></table>`)},
	"assets/app.css": {Data: []byte("body{margin:0}")},
}

type pageController struct {
	ginject.HTTP
}

func (c pageController) NewController() ginject.Controller { return c }

func (c pageController) RENDER_bookings() map[string]any {
	return map[string]any{"Rows": bookingRows()}
}

type jsonController struct {
	ginject.HTTP
}

func (c jsonController) NewController() ginject.Controller { return c }

func (c jsonController) READ_json() ginject.Map {
	return ginject.Map{"rows": bookingRows()}
}

func newPageApp(render bool) ginject.App {
	app := ginject.New().SetLogger(silentLogger{})
	app.BindGlobalMiddlewares(helmet.Helmet{})
	builder := ginject.ModuleBuilder().Controllers(jsonController{})
	if render {
		app.EnableRender(&ginject.RenderOptions{FS: pageFiles, Dir: "."})
		builder = ginject.ModuleBuilder().Controllers(jsonController{}, pageController{})
	}
	app.Create(builder.Build())
	return app
}

func heapAfterGC() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func checkScenarios(t *testing.T, name string, handler http.Handler) {
	for _, s := range Scenarios {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, s.Request())
		if w.Code != s.Status {
			t.Error(test.DiffMessage(w.Code, s.Status, name+" "+s.Name+" status, body "+w.Body.String()))
		}
		if s.Contains != "" && !strings.Contains(w.Body.String(), s.Contains) {
			t.Error(test.DiffMessage(w.Body.String(), s.Contains, name+" "+s.Name+" body"))
		}
	}
}
