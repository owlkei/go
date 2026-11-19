package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(app http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// waitFor calls path until it answers want, for at most five seconds.
func waitFor(t *testing.T, app http.Handler, path string, want int) *httptest.ResponseRecorder {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := get(app, path)
		if rec.Code == want || time.Now().After(deadline) {
			return rec
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStockWaitsForTheWarehouse(t *testing.T) {
	t.Setenv("WAREHOUSE_DELAY", "300ms")
	app := newApp()

	if rec := get(app, "/health"); rec.Code != http.StatusOK {
		t.Fatalf("GET /health while the warehouse starts = %d, want 200", rec.Code)
	}
	rec := get(app, "/stock/green-tea")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("GET /stock/green-tea while the warehouse starts = %d, Retry-After %q; want 503 with Retry-After",
			rec.Code, rec.Header().Get("Retry-After"))
	}

	rec = waitFor(t, app, "/stock/green-tea", http.StatusOK)
	if body := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || body != `{"quantity":30,"sku":"green-tea"}` {
		t.Fatalf("GET /stock/green-tea once the warehouse is up = %d %s", rec.Code, body)
	}
	if rec := get(app, "/stock/kettle"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /stock/kettle = %d, want 404", rec.Code)
	}
}

func TestBadWarehouseURLFailsForGood(t *testing.T) {
	t.Setenv("WAREHOUSE_URL", "ftp://warehouse")
	app := newApp()

	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := get(app, "/stock/green-tea")
		if rec.Code == http.StatusServiceUnavailable && rec.Header().Get("Retry-After") == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET /stock/green-tea = %d, Retry-After %q; want 503 without Retry-After once the module failed",
				rec.Code, rec.Header().Get("Retry-After"))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
