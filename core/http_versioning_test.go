package core

import (
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type verController struct {
	common.HTTP
}

func (ctl verController) NewController() Controller            { return ctl }
func (ctl verController) READ_ver_VERSION_1() string           { return "v1" }
func (ctl verController) READ_ver_VERSION_2() string           { return "v2" }
func (ctl verController) READ_neutral_VERSION_NEUTRAL() string { return "neutral" }
func (ctl verController) READ_plain() string                   { return "plain" }

func verApp(t *testing.T, defaultVersion string) *App {
	t.Helper()
	lcReset(t)
	app := New()
	app.EnableVersioning(versioning.Versioning{Type: versioning.HeaderVersion, Key: "X-Version", DefaultVersion: defaultVersion})
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.Create(ModuleBuilder().Controllers(verController{}).Build())
	return app
}

func TestVersioning_ExactThenNeutral(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "2"), http.StatusOK, "v2", "V1: the route registered for the request's version")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "1"), http.StatusOK, "v1", "V1: another version of the same path")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/neutral", "X-Version", "7"), http.StatusOK, "neutral", "V1: with no route for the version, the neutral one")
}

func TestVersioning_UnknownVersionIs404(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver", "X-Version", "7"), http.StatusNotFound, wantNotFoundJSON, "V1: no route for the version and no neutral one")
}

func TestVersioning_NeutralAnswersWithoutVersion(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodGet, "/neutral")

	lcExpectResponse(t, w, http.StatusOK, "neutral", "bug 11, V2: a neutral route answers a request without a version")
}

func TestVersioning_NeutralAnswersWithDefaultVersion(t *testing.T) {
	app := verApp(t, "1")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/neutral"), http.StatusOK, "neutral", "V2: a neutral route answers with a default version")
	lcExpectResponse(t, lcServe(app, http.MethodGet, "/ver"), http.StatusOK, "v1", "V1: the default version selects the exact route")
}

func TestVersioning_UnversionedWithoutVersionIs410(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodGet, "/plain")

	lcExpectResponse(t, w, http.StatusGone, wantGoneJSON, "V3: a request without a version to a route registered without one answers 410")
	lcExpectEntries(t, lcAll(), []string{"mw:g1"}, "V3: the global middlewares run, no module layer and no filter")
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "V3: Content-Type"))
	}
}

func TestVersioning_UnversionedMiddlewareStopsThe410(t *testing.T) {
	app := verApp(t, "")
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, _ func()) {
		_, _ = w.Write([]byte("mine"))
	})
	w := lcServe(app, http.MethodGet, "/plain")

	lcExpectResponse(t, w, http.StatusOK, "mine", "V3, L10: a middleware that does not call next() replaces the 410")
}

func TestVersioning_UnversionedWithVersionIs404(t *testing.T) {
	app := verApp(t, "")

	lcExpectResponse(t, lcServe(app, http.MethodGet, "/plain", "X-Version", "1"), http.StatusNotFound, wantNotFoundJSON, "V3: with a version, a route registered without one is not found")
}

func TestVersioning_405UsesTheRequestVersion(t *testing.T) {
	app := verApp(t, "")

	w := lcServe(app, http.MethodDelete, "/ver", "X-Version", "1")
	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2, V1: DELETE on a versioned path answers 405 for that version")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, V1: Allow lists the methods of the request's version"))
	}

	lcExpectResponse(t, lcServe(app, http.MethodDelete, "/ver", "X-Version", "9"), http.StatusNotFound, wantNotFoundJSON, "N2, V1: a version with no route at all is a 404, not a 405")
}

func TestVersioning_405ForANeutralRoute(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodDelete, "/neutral", "X-Version", "9")

	lcExpectResponse(t, w, http.StatusMethodNotAllowed, wantMethodNotAllowedJSON, "N2, V1: the neutral route counts for the Allow of any version")
	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Error(test.DiffMessage(got, "GET, HEAD", "N2, V1: Allow lists the neutral route's methods"))
	}
}

func TestVersioning_HeadUsesTheVersionedGetRoute(t *testing.T) {
	app := verApp(t, "")
	w := lcServe(app, http.MethodHead, "/ver", "X-Version", "2")

	lcExpectResponse(t, w, http.StatusOK, "v2", "N3, V1: HEAD resolves the version like GET")
}
