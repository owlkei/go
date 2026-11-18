package ginject_test

import (
	"net/http"
	"testing"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/internal/test"
)

type tokenController struct {
	ginject.HTTP
}

func (instance tokenController) NewController() ginject.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}

func (instance tokenController) CREATE_token() ginject.Map { return ginject.Map{"token": "t"} }

func (instance tokenController) READ_payment_intents() []string { return []string{"pi"} }

var billingModule = ginject.ModuleBuilder().
	PathStyle(ginject.PathStyleKebab).
	Controllers(billingController{}).
	Build()

type billingController struct {
	ginject.HTTP
}

func (instance billingController) NewController() ginject.Controller { return instance }

func (instance billingController) READ_invoice_lines() string { return "lines" }

func TestSurface_PathStyle(t *testing.T) {
	style := ginject.PathStyle(ginject.PathStyleAsWritten)
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().
		PathStyle(style).
		Imports(billingModule).
		Controllers(tokenController{}).
		Build().
		Prefix("api"))

	if w := serve(app, http.MethodPost, "/api/oauth2/token", nil, nil); w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B22: POST /api/oauth2/token"))
	}
	if w := serve(app, http.MethodGet, "/api/payment_intents", nil, nil); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17c: GET /api/payment_intents under PathStyleAsWritten"))
	}
	if w := serve(app, http.MethodGet, "/api/payment-intents", nil, nil); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B12: the kebab spelling is 404 under PathStyleAsWritten"))
	}
	if w := serve(app, http.MethodGet, "/api/invoice-lines", nil, nil); w.Code != http.StatusOK || w.Body.String() != "lines" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17d: a module with PathStyleKebab keeps its URLs"))
	}
}

func TestSurface_PathStyleKebabIsTheDefault(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(tokenController{}).Build())

	if w := serve(app, http.MethodGet, "/payment-intents", nil, nil); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B1: GET /payment-intents without a style"))
	}
}

func TestSurface_PathStyleConstantsAreDistinct(t *testing.T) {
	if ginject.PathStyleKebab == ginject.PathStyleAsWritten {
		t.Error(test.DiffMessage(ginject.PathStyleKebab, "a different value", "the two styles differ"))
	}
}
