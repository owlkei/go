package core

import (
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/internal/versioning"
)

type z1Controller struct {
	common.HTTP
}

func (c z1Controller) NewController() Controller { return c }

func (c z1Controller) READ_users() string                  { return "list" }
func (c z1Controller) CREATE_users() string                { return "created" }
func (c z1Controller) READ_users_BY_id(p ctx.Param) string { return "user:" + p.Get("id") }
func (c z1Controller) DELETE_users_BY_id() string          { return "deleted" }
func (c z1Controller) READ_report_VERSION_1() string       { return "report1" }
func (c z1Controller) READ_report_VERSION_2() string       { return "report2" }
func (c z1Controller) READ_files_ANY() string              { return "glob" }

func TestCreate_Characterization_Routes(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New()
		app.Create(NewModuleBuilder().Controllers(z1Controller{}).Build())

		cases := []struct {
			method, target string
			code           int
			body           string
		}{
			{http.MethodGet, "/users", http.StatusOK, "list"},
			{http.MethodPost, "/users", http.StatusCreated, "created"},
			{http.MethodGet, "/users/7", http.StatusOK, "user:7"},
			{http.MethodDelete, "/users/7", http.StatusOK, "deleted"},
			{http.MethodGet, "/filesabc", http.StatusOK, "glob"},
			{http.MethodGet, "/nothing", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createRequest(app, c.method, c.target)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of "+c.method+" "+c.target))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of "+c.method+" "+c.target))
			}
		}
	})
}

type z1VersionedController struct {
	common.HTTP
}

func (c z1VersionedController) NewController() Controller           { return c }
func (c z1VersionedController) READ_report_VERSION_1() string       { return "report1" }
func (c z1VersionedController) READ_report_VERSION_2() string       { return "report2" }
func (c z1VersionedController) READ_report_BY_id_VERSION_2() string { return "byid2" }

func TestCreate_Characterization_Versions(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New()
		app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version"})
		app.Create(NewModuleBuilder().Controllers(z1VersionedController{}).Build())

		cases := []struct {
			target, version string
			code            int
			body            string
		}{
			{"/report", "1", http.StatusOK, "report1"},
			{"/report", "2", http.StatusOK, "report2"},
			{"/report/5", "2", http.StatusOK, "byid2"},
			{"/report", "3", http.StatusNotFound, wantNotFoundJSON},
			{"/report/5", "1", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createRequest(app, http.MethodGet, c.target, "X-Version", c.version)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of GET "+c.target+" v"+c.version))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of GET "+c.target+" v"+c.version))
			}
		}
	})
}

type z1PrefixedController struct {
	common.HTTP
}

func (c z1PrefixedController) NewController() Controller { return c }
func (c z1PrefixedController) READ_ping() string         { return "pong" }

type z1OwnController struct {
	common.HTTP
}

func (c z1OwnController) NewController() Controller { return c }
func (c z1OwnController) READ_own() string          { return "own" }

func TestCreate_Characterization_NestedPrefixes(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		child := NewModuleBuilder().Controllers(z1PrefixedController{}).Build()
		child.Prefix("v1")
		root := NewModuleBuilder().Imports(child).Controllers(z1OwnController{}).Build()
		root.Prefix("api")

		app := New()
		app.Create(root)

		cases := []struct {
			target string
			code   int
			body   string
		}{
			{"/api/v1/ping", http.StatusOK, "pong"},
			{"/api/own", http.StatusOK, "own"},
			{"/ping", http.StatusNotFound, wantNotFoundJSON},
			{"/v1/ping", http.StatusNotFound, wantNotFoundJSON},
			{"/own", http.StatusNotFound, wantNotFoundJSON},
		}
		for _, c := range cases {
			w := createGet(app, c.target)
			if w.Code != c.code {
				t.Error(test.DiffMessage(w.Code, c.code, "Z1: status of GET "+c.target))
			}
			if w.Body.String() != c.body {
				t.Error(test.DiffMessage(w.Body.String(), c.body, "Z1: body of GET "+c.target))
			}
		}
	})
}

var z1Trace createRecorder

type z1Middleware struct{ Name string }

func (m z1Middleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	z1Trace.add("mw:" + m.Name)
	next()
}

type z1Guard struct{ Name string }

func (g z1Guard) CanActivate(_ *ctx.HTTPContext) bool {
	z1Trace.add("guard:" + g.Name)
	return true
}

type z1Interceptor struct{ Name string }

func (i z1Interceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	z1Trace.add("intercept:" + i.Name)
	return agg.Pipe()
}

type z1LayeredController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
}

func (c z1LayeredController) NewController() Controller {
	c.BindMiddleware(z1Middleware{Name: "c1"})
	c.BindMiddleware(z1Middleware{Name: "c2"})
	c.BindGuard(z1Guard{Name: "c1"})
	c.BindGuard(z1Guard{Name: "c2"})
	c.BindInterceptor(z1Interceptor{Name: "c1"})
	c.BindInterceptor(z1Interceptor{Name: "c2"})
	return c
}

func (c z1LayeredController) READ_layered() string {
	z1Trace.add("handler")
	return "ok"
}

func TestCreate_Characterization_LayerOrderPerRoute(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		app := New()
		app.BindGlobalMiddlewares(z1Middleware{Name: "g1"}, z1Middleware{Name: "g2"})
		app.BindGlobalGuards(z1Guard{Name: "g1"})
		app.BindGlobalInterceptors(z1Interceptor{Name: "g1"})
		app.Create(NewModuleBuilder().Controllers(z1LayeredController{}).Build())

		w := createGet(app, "/layered")
		if w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Fatal(test.DiffMessage(w.Body.String(), "ok", "Z1: the layered route answers"))
		}

		want := "[mw:g1 mw:g2 mw:c1 mw:c2 guard:g1 guard:c1 guard:c2 intercept:g1 intercept:c1 intercept:c2 handler]"
		if got := z1Trace.list(); len(got) == 0 || sprint(got) != want {
			t.Error(test.DiffMessage(sprint(got), want, "Z1: the layers of one route run in this order"))
		}
	})
}

var z1Built createRecorder

type z1ProvLeafA struct{}

func (p z1ProvLeafA) NewProvider() Provider { z1Built.add("leafA"); return p }

type z1ProvShared struct{}

func (p z1ProvShared) NewProvider() Provider { z1Built.add("shared"); return p }

type z1ProvLeafB struct{ Shared z1ProvShared }

func (p z1ProvLeafB) NewProvider() Provider { z1Built.add("leafB"); return p }

type z1ProvRoot struct {
	A z1ProvLeafA
	B z1ProvLeafB
}

func (p z1ProvRoot) NewProvider() Provider { z1Built.add("root"); return p }

type z1ProvRootDep struct{}

func (p z1ProvRootDep) NewProvider() Provider { z1Built.add("rootDep"); return p }

func TestCreate_Characterization_ProviderConstructionOrder(t *testing.T) {
	freshProcess(t, func(t *testing.T) {
		shared := NewModuleBuilder().Providers(z1ProvShared{}).Build()
		modA := NewModuleBuilder().Imports(shared).Providers(z1ProvLeafA{}).Build()
		modB := NewModuleBuilder().Imports(shared).Providers(z1ProvLeafB{}).Build()
		root := NewModuleBuilder().Imports(modA, modB).Providers(z1ProvRoot{}, z1ProvRootDep{}).Build()

		New().Create(root)

		want := "[shared leafA leafB root rootDep]"
		if got := sprint(z1Built.list()); got != want {
			t.Error(test.DiffMessage(got, want, "Z1: providers are constructed imports first, in import order, then the module's own"))
		}
	})
}
