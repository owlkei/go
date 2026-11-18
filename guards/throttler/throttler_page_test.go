package throttler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func pagedThrottler() Throttler {
	return Throttler{Limit: 1, TTL: time.Minute, KeyFunc: func(c *ctx.HTTPContext) string { return c.Request.URL.Path }}
}

type pagedPage struct {
	common.HTTP
	common.Guard
}

func (c pagedPage) NewController() core.Controller {
	c.Prefix("page")
	c.BindGuard(pagedThrottler())
	return c
}

func (c pagedPage) RENDER_hit() {}

type pagedAPI struct {
	common.HTTP
	common.Guard
}

func (c pagedAPI) NewController() core.Controller {
	c.Prefix("api")
	c.BindGuard(pagedThrottler())
	return c
}

func (c pagedAPI) READ_hit() string { return "ok" }

func pagedGet(app *core.App, target, accept string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func TestPageError_ThrottlerFollowsRoute(t *testing.T) {
	app := core.New()
	app.EnableRender(&core.RenderOptions{FS: fstest.MapFS{"page/hit.html": &fstest.MapFile{Data: []byte("OK")}}, Dir: "."})
	app.Create(core.NewModuleBuilder().Controllers(pagedPage{}, pagedAPI{}).Build())

	if w := pagedGet(app, "/page/hit", "application/json"); w.Code != http.StatusOK || w.Body.String() != "OK" {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R19: the first request is served"))
	}
	w := pagedGet(app, "/page/hit", "application/json")
	if w.Code != http.StatusTooManyRequests {
		t.Fatal(test.DiffMessage(w.Code, http.StatusTooManyRequests, "R19: the second request is refused"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") || strings.HasPrefix(w.Body.String(), "{") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "R19: a page route refuses in HTML whatever Accept says"))
	}
	if secs, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || secs < 0 {
		t.Error(test.DiffMessage(w.Header().Get("Retry-After"), "seconds", "R19: Retry-After is kept on the page"))
	}
	if w.Header().Get("X-Ratelimit-Limit") != "1" {
		t.Error(test.DiffMessage(w.Header().Get("X-Ratelimit-Limit"), "1", "R19: the rate headers are kept on the page"))
	}

	if w := pagedGet(app, "/api/hit", "text/html"); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "R19: the first API request is served"))
	}
	w = pagedGet(app, "/api/hit", "text/html")
	if w.Code != http.StatusTooManyRequests {
		t.Fatal(test.DiffMessage(w.Code, http.StatusTooManyRequests, "R19: the second API request is refused"))
	}
	if w.Header().Get("Content-Type") != "application/json" || !strings.HasPrefix(w.Body.String(), "{") {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "application/json", "R19: an API route refuses in JSON whatever Accept says"))
	}
	if w.Header().Get("Retry-After") == "" || w.Header().Get("X-Ratelimit-Limit") != "1" {
		t.Error(test.DiffMessage(w.Header().Get("Retry-After"), "Retry-After and rate headers", "R19: the headers are kept on the API response"))
	}
}
