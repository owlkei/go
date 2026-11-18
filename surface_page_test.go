package ginject_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
)

type surfaceBooking struct {
	Nights string `bind:"nights"`
	Errors map[string]string
}

func (surfaceBooking) Transform(f ginject.Form, _ ginject.ArgumentMetadata) any {
	bound, _ := f.Bind(surfaceBooking{})
	form := bound.(surfaceBooking)
	form.Errors = map[string]string{}
	if form.Nights == "" {
		form.Errors["nights"] = "Enter the nights"
	}
	return form
}

type surfaceBookingPages struct {
	ginject.HTTP
}

func (instance surfaceBookingPages) NewController() ginject.Controller { return instance }

func (instance surfaceBookingPages) RENDER_bookings() ginject.Map {
	return ginject.Map{"Bookings": []string{"one", "two"}}
}

func (instance surfaceBookingPages) RENDER_bookings_BY_id(param ginject.Param) surfaceBooking {
	if param.Get("id") != "1" {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return surfaceBooking{Nights: "3"}
}

func (instance surfaceBookingPages) RENDER_new_OF_bookings() surfaceBooking {
	return surfaceBooking{Nights: "1"}
}

func (instance surfaceBookingPages) SUBMIT_new_OF_bookings(c ginject.HTTPContext, form surfaceBooking) surfaceBooking {
	if len(form.Errors) == 0 {
		c.Redirect("/bookings")
	}
	return form
}

func TestSurface_PagesByRoute(t *testing.T) {
	var _ = ginject.RenderOptions{IsReload: false}

	app := ginject.New()
	web := fstest.MapFS{
		"layout.html":         {Data: []byte(`<main>{{template "content" .}}</main>`)},
		"bookings/index.html": {Data: []byte(`<ul>{{range .Bookings}}<li>{{.}}</li>{{end}}</ul>`)},
		"bookings/{id}.html":  {Data: []byte(`<p>{{.Nights}} nights</p>`)},
		"bookings/new.html":   {Data: []byte(`<form method="post" action="/bookings/new"><input name="nights" value="{{.Nights}}">{{with .Errors.nights}}<b>{{.}}</b>{{end}}</form>`)},
		"404.html":            {Data: []byte(`<h1>{{.Code}} {{.Message}}</h1>`)},
		"public/robots.txt":   {Data: []byte("User-agent: *\n")},
	}
	if got := app.EnableRender(&ginject.RenderOptions{FS: web, Dir: "."}); got != app {
		t.Error(test.DiffMessage(got, app, "R3: EnableRender returns the app"))
	}
	app.Create(ginject.ModuleBuilder().Controllers(surfaceBookingPages{}).Build())

	w := serve(app, http.MethodGet, "/bookings", nil, nil)
	if want := "<main><ul><li>one</li><li>two</li></ul></main>"; w.Code != http.StatusOK || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P1, P2: RENDER_bookings renders bookings/index.html in the layout"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "R5: content type"))
	}

	w = serve(app, http.MethodGet, "/bookings/1", nil, nil)
	if want := "<main><p>3 nights</p></main>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P2: a result struct is the data of bookings/{id}.html"))
	}
	w = serve(app, http.MethodGet, "/bookings/9", nil, nil)
	if want := "<main><h1>404 No booking has this number</h1></main>"; w.Code != http.StatusNotFound || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P17: an exception is rendered by 404.html"))
	}

	form := url.Values{"nights": {""}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	w = serve(app, http.MethodPost, "/bookings/new", strings.NewReader(form.Encode()), headers)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "<b>Enter the nights</b>") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "the form with its message"}, "P14: an invalid SUBMIT_ renders its RENDER_'s template with 422"))
	}
	form = url.Values{"nights": {"2"}}
	w = serve(app, http.MethodPost, "/bookings/new", strings.NewReader(form.Encode()), headers)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P14: a valid SUBMIT_ redirects with 303"))
	}

	w = serve(app, http.MethodGet, "/robots.txt", nil, nil)
	if w.Code != http.StatusOK || w.Body.String() != "User-agent: *\n" {
		t.Error(test.DiffMessage(w.Body.String(), "User-agent: *\n", "P22: public/robots.txt is served at the root"))
	}
}

func TestSurface_PageAndViewAreGone(t *testing.T) {
	listing, err := os.ReadFile("api/ginject.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(listing), "\n") {
		for _, removed := range []string{"type Page =", "type View =", "common.Page", "common.View"} {
			if strings.Contains(line, removed) {
				t.Error(test.DiffMessage(line, "no such line", "P11: ginject.Page and ginject.View are removed from api/ginject.txt (make api)"))
			}
		}
	}
}
