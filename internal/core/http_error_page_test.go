package core

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

type epPages struct{ common.HTTP }

func (c epPages) NewController() Controller { return c }

func (c epPages) RENDER_fail(h *ctx.HTTPContext) {
	code, _ := strconv.Atoi(h.Query().Get("code"))
	if code == http.StatusNotFound {
		panic(exception.NotFoundException("it failed"))
	}
	panic(exception.NewException("it failed", code))
}

type epGuard struct{}

func (epGuard) CanActivate(*ctx.HTTPContext) bool {
	panic(exception.NewException("guard says no", http.StatusUnprocessableEntity))
}

type epGuarded struct {
	common.HTTP
	common.Guard
}

func (c epGuarded) NewController() Controller {
	c.BindGuard(epGuard{})
	return c
}

func (c epGuarded) RENDER_guarded() {}

func epFiles(names ...string) map[string]string {
	files := map[string]string{"fail.html": "x", "guarded.html": "x"}
	for _, name := range names {
		files[name+".html"] = name + "-file:{{.Code}}"
	}
	return files
}

func epBody(code int, template string) string {
	return template + "-file:" + strconv.Itoa(code)
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

type epAdmin struct{ common.HTTP }

func (c epAdmin) NewController() Controller {
	c.Prefix("admin")
	return c
}

func (c epAdmin) RENDER_page(h *ctx.HTTPContext) {
	if h.Query().Get("fail") != "" {
		panic(exception.NotFoundException("missing"))
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
