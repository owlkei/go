package bench

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/middlewares/helmet"
)

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
	ginject.Page
}

func (c pageController) NewController() ginject.Controller { return c }

func (c pageController) READ_bookings() ginject.View {
	return ginject.View{Name: "bookings", Data: map[string]any{"Rows": bookingRows()}}
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

func BenchmarkGinjectPage(b *testing.B) {
	app := newPageApp(true)
	for _, s := range []struct{ name, path string }{{"JSONList", "/json"}, {"PageList", "/bookings"}} {
		b.Run(s.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, s.path, nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				app.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("status %d", w.Code)
				}
			}
		})
	}
}

func heapAfterGC() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func BenchmarkGinjectPageIdleMemory(b *testing.B) {
	for b.Loop() {
		before := heapAfterGC()
		withoutRender := newPageApp(false)
		mid := heapAfterGC()
		withRender := newPageApp(true)
		after := heapAfterGC()
		b.ReportMetric(float64(mid-before), "B-app")
		b.ReportMetric(float64(after-mid), "B-app+render")
		runtime.KeepAlive(withoutRender)
		runtime.KeepAlive(withRender)
	}
}
