package bench

import (
	"net/http"
	"strings"
)

const (
	authHeader = "Authorization"
	authValue  = "Bearer bench"
	mwHeader   = "X-Bench-Middleware"
)

type Scenario struct {
	Name        string
	Method      string
	Path        string
	Body        string
	ContentType string
	Auth        bool
	Status      int
	Contains    string
}

var Scenarios = []Scenario{
	{Name: "Static", Method: http.MethodGet, Path: "/static", Status: http.StatusOK, Contains: "ok"},
	{Name: "Param", Method: http.MethodGet, Path: "/users/42", Status: http.StatusOK, Contains: `"id":"42"`},
	{Name: "Query", Method: http.MethodGet, Path: "/search?q=golang&page=3", Status: http.StatusOK, Contains: `"page":3`},
	{Name: "JSONBody", Method: http.MethodPost, Path: "/users", Body: `{"name":"Ada","age":36}`, ContentType: "application/json", Status: http.StatusCreated, Contains: `"name":"Ada"`},
	{Name: "Chain", Method: http.MethodGet, Path: "/chain", Auth: true, Status: http.StatusOK, Contains: `"data"`},
	{Name: "Exception", Method: http.MethodGet, Path: "/error", Status: http.StatusBadRequest},
	{Name: "NotFound", Method: http.MethodGet, Path: "/missing", Status: http.StatusNotFound},
}

func (s Scenario) Request() *http.Request {
	var body *strings.Reader
	if s.Body != "" {
		body = strings.NewReader(s.Body)
	} else {
		body = strings.NewReader("")
	}
	r, _ := http.NewRequest(s.Method, "http://bench.local"+s.Path, body)
	if s.ContentType != "" {
		r.Header.Set("Content-Type", s.ContentType)
	}
	if s.Auth {
		r.Header.Set(authHeader, authValue)
	}
	return r
}

type UserInput struct {
	Name string `json:"name" bind:"name"`
	Age  int    `json:"age" bind:"age"`
}
