package shop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/core"
)

func call(t *testing.T, app *core.App, method, path, token, body string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	return rec.Code, out
}

func TestShopRoutes(t *testing.T) {
	app := core.New()
	app.Create(core.ModuleBuilder().Imports(Module).Build())

	user := `{"email":"a@example.com","name":"Ann","password":"secret12"}`
	if code, out := call(t, app, http.MethodPost, "/users?email=a@example.com&name=Ann&password=secret12", "", user); code >= 300 || out["store"] == nil {
		t.Fatalf("register: %d %v", code, out)
	}
	if code, _ := call(t, app, http.MethodPost, "/users?email=a@example.com&name=Ann&password=secret12", "", user); code != http.StatusConflict {
		t.Fatalf("duplicate register: %d, want 409", code)
	}
	if code, _ := call(t, app, http.MethodPost, "/sessions", "", `{"email":"a@example.com","password":"wrong"}`); code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d, want 401", code)
	}

	code, out := call(t, app, http.MethodPost, "/sessions", "", `{"email":"a@example.com","password":"secret12"}`)
	token, _ := out["token"].(string)
	if code >= 300 || token == "" {
		t.Fatalf("login: %d %v", code, out)
	}

	if code, _ := call(t, app, http.MethodGet, "/store", "", ""); code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("store without token: %d", code)
	}
	if code, out := call(t, app, http.MethodGet, "/store", token, ""); code != http.StatusOK || out["Name"] != "Ann's Store" {
		t.Fatalf("store: %d %v", code, out)
	}

	code, category := call(t, app, http.MethodPost, "/store/categories", token, `{"name":"Books"}`)
	categoryID, _ := category["ID"].(string)
	if code >= 300 || categoryID == "" {
		t.Fatalf("create category: %d %v", code, category)
	}
	if code, page := call(t, app, http.MethodGet, "/store/categories?page=1&limit=10", token, ""); code != http.StatusOK || page["total"] != float64(1) {
		t.Fatalf("list categories: %d %v", code, page)
	}

	productsPath := "/store/categories/" + categoryID + "/products"
	code, product := call(t, app, http.MethodPost, productsPath, token, `{"name":"Go","price":12.5}`)
	productID, _ := product["ID"].(string)
	if code >= 300 || productID == "" || product["Price"] != 12.5 {
		t.Fatalf("create product: %d %v", code, product)
	}
	if code, out := call(t, app, http.MethodPut, productsPath+"/"+productID, token, `{"name":"Go 2","price":20}`); code != http.StatusOK || out["Name"] != "Go 2" {
		t.Fatalf("update product: %d %v", code, out)
	}
	if code, out := call(t, app, http.MethodGet, productsPath+"/"+productID, token, ""); code != http.StatusOK || out["Name"] != "Go 2" {
		t.Fatalf("read product: %d %v", code, out)
	}

	if code, _ := call(t, app, http.MethodDelete, "/store/categories/"+categoryID, token, ""); code != http.StatusOK {
		t.Fatalf("delete category: %d", code)
	}
	if code, _ := call(t, app, http.MethodGet, productsPath+"/"+productID, token, ""); code != http.StatusNotFound {
		t.Fatalf("product after category delete: %d, want 404", code)
	}

	if code, _ := call(t, app, http.MethodDelete, "/sessions", token, ""); code != http.StatusOK {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := call(t, app, http.MethodGet, "/store", token, ""); code == http.StatusOK {
		t.Fatal("store after logout still served")
	}
}
