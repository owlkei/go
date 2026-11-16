package core

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestNoRoute_404FixedBody(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/nowhere")

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N1: a path without a route answers the fixed 404")
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "N1: Content-Type"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "N1: X-Request-Id is set"))
	}
}

func TestNoRoute_404DoesNotEchoThePath(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/<script>alert(1)</script>")

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "bug 7, N1: the body does not depend on the request")
}

func TestNoRoute_LongPathSameBody(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/"+strings.Repeat("a", 100000))

	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "bug 7, N1: a 100,000-character path gets the same body")
	if w.Body.Len() != 54 {
		t.Error(test.DiffMessage(w.Body.Len(), 54, "bug 7, N1: the body is 54 bytes"))
	}
}

func TestNoRoute_404ForEveryMethod(t *testing.T) {
	app := lcApp(t)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete, http.MethodOptions} {
		w := lcServe(app, method, "/nowhere")
		if w.Code != http.StatusNotFound {
			t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "N1: "+method+" to a path with no route under any method"))
		}
	}
}

func TestNoRoute_405WithAllow(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/ok")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "bug 8, N2: a path with a route for another method answers 405")
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Error(test.DiffMessage(got, "GET, HEAD, POST", "N2: Allow lists the methods that have a route, HEAD with GET"))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "N2: Content-Type"))
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "N2: X-Request-Id is set"))
	}
}

func TestNoRoute_405ForOptionsAndUnknownMethod(t *testing.T) {
	app := lcApp(t)
	for _, method := range []string{http.MethodOptions, "BREW"} {
		w := lcServe(app, method, "/ok")

		lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: "+method+" gets the 405")
		if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
			t.Error(test.DiffMessage(got, "GET, HEAD, POST", "N2: Allow for "+method))
		}
	}
}

func TestNoRoute_AllowOrder(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodTrace, "/bare")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: TRACE on a path with a route for every DSL method")
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Error(test.DiffMessage(got, "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", "N2: Allow follows the fixed order, not the order of registration"))
	}
}

func TestNoRoute_AllowWithoutGetHasNoHead(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/postonly")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: GET on a POST-only path")
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Error(test.DiffMessage(got, "POST", "N2: HEAD is listed exactly when GET is"))
	}
}

func TestNoRoute_HeadIn405Allow(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodDelete, "/readonly")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2: DELETE on a GET-only path")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, N3: HEAD is listed with GET"))
	}
}

func TestNoRoute_HeadOnPostOnlyPathIs405(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodHead, "/postonly")

	if w.Code != http.StatusMethodNotAllowed {
		t.Error(test.DiffMessage(w.Code, http.StatusMethodNotAllowed, "N3: HEAD is served only by a GET route"))
	}
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Error(test.DiffMessage(got, "POST", "N2: Allow for a HEAD request"))
	}
}

func TestNoRoute_HeadOnUnknownPathIs404(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodHead, "/nowhere")

	if w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "N1: HEAD to a path with no route"))
	}
}

func TestNoRoute_HeadUsesGetRoute(t *testing.T) {
	app := lcApp(t)
	var method string
	lcSetHandler(func(c *ctx.HTTPContext) any {
		method = c.Method
		return "H"
	})

	get := lcServe(app, http.MethodGet, "/ok")
	getLog := lcAll()
	lcClearLog()
	head := lcServe(app, http.MethodHead, "/ok")
	headLog := lcAll()

	if head.Code != get.Code || head.Code != http.StatusOK {
		t.Error(test.DiffMessage(head.Code, get.Code, "bug 9, N3: HEAD answers the status of GET"))
	}
	if head.Header().Get("Content-Type") != get.Header().Get("Content-Type") || head.Header().Get("Content-Type") == "" {
		t.Error(test.DiffMessage(head.Header().Get("Content-Type"), get.Header().Get("Content-Type"), "N3: HEAD answers the headers of GET"))
	}
	if head.Body.String() != "H>m2>m1>g2>g1" {
		t.Error(test.DiffMessage(head.Body.String(), "H>m2>m1>g2>g1", "N3: through ServeHTTP on a recorder the body is what the handler wrote"))
	}
	if method != http.MethodHead {
		t.Error(test.DiffMessage(method, http.MethodHead, "N3: c.Method stays HEAD"))
	}
	lcExpectEntries(t, headLog, getLog, "N3: HEAD goes through the whole lifecycle of the GET route")
}

func TestNoRoute_HeadGoesThroughGuards(t *testing.T) {
	app := lcApp(t)
	lcSetGuard("g2", func(*ctx.HTTPContext) bool { return false })
	w := lcServe(app, http.MethodHead, "/ok")

	lcExpectResponse(t, w, http.StatusForbidden, "F[mf2]Access denied", "N3: a guard that refuses GET refuses HEAD")
	lcExpectEntries(t, lcEntries("handler"), nil, "N3: the handler does not run")
}

func TestNoRoute_PathOverLimit(t *testing.T) {
	app := lcApp(t)
	w := lcServe(app, http.MethodGet, "/"+strings.Repeat("a", 4097))
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N4: a path longer than 4,096 bytes matches nothing")

	w = lcServe(app, http.MethodGet, strings.Repeat("/a", 65))
	lcExpectResponse(t, w, http.StatusNotFound, wantNotFoundJSON, "N4: a path with more than 64 segments matches nothing")
}
