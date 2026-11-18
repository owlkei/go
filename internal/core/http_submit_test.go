package core

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type sbPages struct{ common.HTTP }

func (c sbPages) NewController() Controller { return c }

func (c sbPages) RENDER_new_OF_bookings() pfBooking { return pfBooking{Errors: map[string]string{}} }

func (c sbPages) SUBMIT_new_OF_bookings(h *ctx.HTTPContext, form pfBooking) pfBooking {
	if len(form.Errors) == 0 {
		h.Redirect("/bookings")
	}
	return form
}

func (c sbPages) RENDER_bookings_BY_id(p ctx.Param) ctx.Map { return ctx.Map{"Id": p.Get("id")} }

func (c sbPages) SUBMIT_bookings_BY_id(p ctx.Param, form pfBooking) ctx.Map {
	return ctx.Map{"Id": p.Get("id"), "Name": form.Name}
}

func (c sbPages) RENDER() {}

func (c sbPages) SUBMIT(h *ctx.HTTPContext) { h.Redirect("/bookings/new") }

func (c sbPages) CREATE_api(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"created": true} }

func sbFiles() map[string]string {
	return map[string]string{
		"index.html":         "HOME",
		"bookings/new.html":  `FORM name={{.Name}} errors={{len .Errors}}`,
		"bookings/{id}.html": `BOOKING {{.Id}} {{.Name}}`,
	}
}

func sbValid() string {
	return pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})
}

func TestSubmit_PostsToRenderPath(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	w := pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": "", "email": "x"}))
	if w.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(w.Body.String(), "FORM name= errors=2") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "FORM name= errors=2"}, "P12: SUBMIT_new_OF_bookings posts to /bookings/new and renders its RENDER_'s template"))
	}
	w = pfPost(app, "/bookings/new", pfURLEncoded, sbValid())
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: a valid post redirects"))
	}

	w = pfPost(app, "/bookings/42", pfURLEncoded, pfEncode(map[string]string{"name": "Bo"}))
	if w.Code != http.StatusUnprocessableEntity || w.Body.String() != "BOOKING 42 Bo" {
		t.Error(test.DiffMessage(w.Body.String(), "BOOKING 42 Bo", "P12: a parameter reaches the SUBMIT_ and the {id} template"))
	}

	w = pfPost(app, "/", pfURLEncoded, "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings/new" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: SUBMIT posts to the root of RENDER"))
	}
	if w := pfPost(app, "/new", pfURLEncoded, sbValid()); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P12: the SUBMIT_'s own words are not a path"))
	}
}

func TestSubmit_PostsUnderModulePrefix(t *testing.T) {
	inner := NewModuleBuilder().Controllers(sbPages{}).Build()
	inner.Prefix("shop")
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(map[string]string{
		"shop.html":               "SHOP",
		"shop/bookings/new.html":  `FORM {{.Name}}`,
		"shop/bookings/{id}.html": `BOOKING {{.Id}}`,
	}), Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Imports(inner).Build())

	w := pfPost(app, "/shop/bookings/new", pfURLEncoded, sbValid())
	if w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P12: the post follows the module prefix of its page"))
	}
	if w := pfPost(app, "/bookings/new", pfURLEncoded, sbValid()); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "P12: and only there"))
	}
}

func TestSubmit_Rerenders422(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	w := pfPost(app, "/bookings/new", pfURLEncoded, pfEncode(map[string]string{"name": pfHostile, "email": "nobody"}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P14: a SUBMIT_ that renders answers 422"))
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "P14: Content-Type"))
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Error(test.DiffMessage(cc, "no-cache", "P14: Cache-Control"))
	}
	if strings.Contains(w.Body.String(), "<script>") {
		t.Error(test.DiffMessage(w.Body.String(), "no raw script", "R26: submitted values are escaped"))
	}
}

func TestSubmit_Redirect303(t *testing.T) {
	app, _ := pfApp(t, nil)

	for name, target := range map[string]string{"context method": "/bookings/new", "parameter": "/bookings/viaparam"} {
		w := pfPost(app, target, pfURLEncoded, sbValid())
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
			t.Error(test.DiffMessage([]any{w.Code, w.Header().Get("Location")}, []any{http.StatusSeeOther, "/bookings"}, "P14: a redirect without a 3xx is 303 ("+name+")"))
		}
		if w.Body.Len() != 0 && strings.Contains(w.Body.String(), "<form") {
			t.Error(test.DiffMessage(w.Body.String(), "no page", "P14, R27: the result is ignored after a redirect ("+name+")"))
		}
	}
	if w := pfPost(app, "/bookings/explicit", pfURLEncoded, sbValid()); w.Code != http.StatusTemporaryRedirect {
		t.Error(test.DiffMessage(w.Code, http.StatusTemporaryRedirect, "P14: an explicit 3xx is kept"))
	}
}

type sbStatus struct{ common.HTTP }

func (c sbStatus) NewController() Controller { return c }

func (c sbStatus) RENDER_ctx()                   {}
func (c sbStatus) SUBMIT_ctx(h *ctx.HTTPContext) { h.Status(http.StatusOK) }
func (c sbStatus) RENDER_result()                {}
func (c sbStatus) SUBMIT_result() (int, ctx.Map) { return http.StatusCreated, ctx.Map{"Word": "made"} }
func (c sbStatus) RENDER_plain()                 {}
func (c sbStatus) SUBMIT_plain()                 {}
func (c sbStatus) RENDER_nodata()                {}
func (c sbStatus) SUBMIT_nodata() ctx.Map        { return nil }

func TestSubmit_StatusOverride(t *testing.T) {
	files := map[string]string{
		"ctx.html":    "CTX",
		"result.html": "RESULT {{.Word}}",
		"plain.html":  "PLAIN",
		"nodata.html": "NODATA",
	}
	app, _ := pgNew(t, pgFiles(files), sbStatus{})

	if w := pfPost(app, "/ctx", pfURLEncoded, ""); w.Code != http.StatusOK || w.Body.String() != "CTX" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusOK, "CTX"}, "P14: c.Status(200) replaces the default"))
	}
	if w := pfPost(app, "/result", pfURLEncoded, ""); w.Code != http.StatusCreated || w.Body.String() != "RESULT made" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusCreated, "RESULT made"}, "P14: the (int, data) form replaces the default"))
	}
	if w := pfPost(app, "/plain", pfURLEncoded, ""); w.Code != http.StatusUnprocessableEntity || w.Body.String() != "PLAIN" {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "PLAIN"}, "P14: no result and no redirect renders with nil, 422"))
	}
	if w := pfPost(app, "/nodata", pfURLEncoded, ""); w.Code != http.StatusUnprocessableEntity {
		t.Error(test.DiffMessage(w.Code, http.StatusUnprocessableEntity, "P14: a nil result renders with 422"))
	}
	if w := createGet(app, "/ctx"); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "P7: the RENDER_ stays 200"))
	}
}

type sbGuarded struct {
	common.HTTP
	common.Guard
}

func (c sbGuarded) NewController() Controller {
	c.BindGuard(pgGuard{}, c.RENDER_only)
	c.BindGuard(pgGuard{})
	return c
}

func (c sbGuarded) RENDER_only()                   {}
func (c sbGuarded) SUBMIT_only(h *ctx.HTTPContext) { h.Redirect("/done") }

type sbGuardedRenderOnly struct {
	common.HTTP
	common.Guard
}

func (c sbGuardedRenderOnly) NewController() Controller {
	c.BindGuard(pgGuard{}, c.RENDER_only)
	return c
}

func (c sbGuardedRenderOnly) RENDER_only()                   {}
func (c sbGuardedRenderOnly) SUBMIT_only(h *ctx.HTTPContext) { h.Redirect("/done") }

func TestSubmit_GuardBoundToRenderOnly(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"only.html": "PAGE"}), sbGuardedRenderOnly{})

	if w := createGet(app, "/only"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P16: the guard bound to RENDER_only covers the GET"))
	}
	if w := createRequest(app, http.MethodPost, "/only"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P16: the guard does not run on the POST"))
	}
}

func TestSubmit_GuardBoundToAllCoversBoth(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"only.html": "PAGE"}), sbGuarded{})

	if w := createRequest(app, http.MethodPost, "/only"); w.Code != http.StatusForbidden {
		t.Error(test.DiffMessage(w.Code, http.StatusForbidden, "P16: BindGuard(g) covers the SUBMIT_ too"))
	}
	if w := createRequest(app, http.MethodPost, "/only", "X-Allow", "yes"); w.Code != http.StatusSeeOther {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P16: and lets it through when it allows"))
	}
}

func TestSubmit_OtherMethodIs405(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		w := createRequest(app, method, "/bookings/new", "Accept", "text/html")
		if w.Code != http.StatusMethodNotAllowed {
			t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "P16: "+method+" on a page"))
		}
		allow := w.Header().Get("Allow")
		if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
			t.Error(test.DiffMessage(allow, "GET and POST", "P16: Allow of "+method))
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "text/html", "R16: the 405 of a page is HTML"))
		}
	}
}

type sbFailing struct{ common.HTTP }

func (c sbFailing) NewController() Controller { return c }
func (c sbFailing) RENDER_fail()              {}
func (c sbFailing) SUBMIT_fail()              { panic(exception.NotFoundException("no such booking")) }

func TestSubmit_ErrorsAreHTML(t *testing.T) {
	app, _ := pgNew(t, pgFiles(map[string]string{"fail.html": "x"}), sbFailing{})
	w := createRequest(app, http.MethodPost, "/fail", "Accept", "application/json")
	peExpectHTML(t, "R16: a SUBMIT_ is a page route, its error is HTML", w, http.StatusNotFound)
}

func TestSubmit_CreateStaysAnAPIRoute(t *testing.T) {
	app, _ := pgNew(t, pgFiles(sbFiles()), sbPages{})
	w := createRequest(app, http.MethodPost, "/api")
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"created":true`) {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusCreated, `{"created":true}`}, "P16: CREATE_ stays an API route, 201 and JSON"))
	}
}
