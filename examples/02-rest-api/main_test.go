package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func call(t *testing.T, app http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestBooks(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"list", http.MethodGet, "/books", "", http.StatusOK,
			`[{"id":"1","title":"The Go Programming Language","author":"Alan Donovan","year":2015},` +
				`{"id":"2","title":"Designing Data-Intensive Applications","author":"Martin Kleppmann","year":2017}]`},
		{"find", http.MethodGet, "/books/2", "", http.StatusOK,
			`{"id":"2","title":"Designing Data-Intensive Applications","author":"Martin Kleppmann","year":2017}`},
		{"find a missing book", http.MethodGet, "/books/9", "", http.StatusNotFound,
			`{"code":404,"error":"Not Found","message":"No book with id 9"}`},
		{"add", http.MethodPost, "/books", `{"title":"Go in Action","author":"William Kennedy","year":2015}`, http.StatusCreated,
			`{"id":"3","title":"Go in Action","author":"William Kennedy","year":2015}`},
		{"add an invalid book", http.MethodPost, "/books", `{"title":" ","year":3000}`, http.StatusUnprocessableEntity,
			`{"code":422,"error":"Unprocessable Entity","message":"title is required; author is required; year must be between 1450 and this year"}`},
		{"replace", http.MethodPut, "/books/1", `{"title":"The Go Programming Language","author":"Alan Donovan, Brian Kernighan","year":2015}`, http.StatusOK,
			`{"id":"1","title":"The Go Programming Language","author":"Alan Donovan, Brian Kernighan","year":2015}`},
		{"remove", http.MethodDelete, "/books/2", "", http.StatusOK,
			`{"id":"2","title":"Designing Data-Intensive Applications","author":"Martin Kleppmann","year":2017}`},
		{"remove a missing book", http.MethodDelete, "/books/9", "", http.StatusNotFound,
			`{"code":404,"error":"Not Found","message":"No book with id 9"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := call(t, newApp(), tt.method, tt.path, tt.body)
			if status != tt.wantStatus || body != tt.wantBody {
				t.Errorf("%s %s = %d %s\nwant %d %s", tt.method, tt.path, status, body, tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestAddedBookIsListed(t *testing.T) {
	app := newApp()
	call(t, app, http.MethodPost, "/books", `{"title":"Go in Action","author":"William Kennedy","year":2015}`)
	if _, body := call(t, app, http.MethodGet, "/books/3", ""); !strings.Contains(body, "Go in Action") {
		t.Errorf("GET /books/3 = %s, want the book added before", body)
	}
}
