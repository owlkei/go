package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutes(t *testing.T) {
	app := newApp()
	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/hello", http.StatusOK, "Hello from Owlkei Go"},
		{"/hello/ada", http.StatusOK, `{"greeting":"Hello, ada"}`},
		{"/goodbye", http.StatusNotFound, `{"code":404,"error":"Not Found","message":"Not Found"}`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
				t.Errorf("GET %s = %d %q, want %d %q", tt.path, rec.Code, rec.Body.String(), tt.wantStatus, tt.wantBody)
			}
		})
	}
}
