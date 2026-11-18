package ginject_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/internal/test"
)

type surfacePages struct {
	ginject.Page
}

func (instance surfacePages) NewController() ginject.Controller {
	instance.Prefix("site")
	return instance
}

func (instance surfacePages) READ_welcome() ginject.View {
	return ginject.View{Name: "welcome", Data: ginject.Map{"Who": "reader"}}
}

func TestSurface_PageController(t *testing.T) {
	var _ ginject.Page
	var _ = ginject.RenderOptions{IsReload: false}
	var _ = ginject.View{Name: "x", Data: nil}

	app := ginject.New()
	web := fstest.MapFS{
		"layout.html":  {Data: []byte(`<main>{{template "content" .}}</main>`)},
		"welcome.html": {Data: []byte(`<p>hello {{.Who}}</p>`)},
	}
	if got := app.EnableRender(&ginject.RenderOptions{FS: web, Dir: "."}); got != app {
		t.Error(test.DiffMessage(got, app, "R3: EnableRender returns the app"))
	}
	app.Create(ginject.ModuleBuilder().Controllers(surfacePages{}).Build())

	w := serve(app, http.MethodGet, "/site/welcome", nil, nil)
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "R1: GET /site/welcome status"))
	}
	if want := "<main><p>hello reader</p></main>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "R5: the page inside its layout"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "R5: content type"))
	}
}
