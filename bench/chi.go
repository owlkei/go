package bench

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewChi() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/static", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": chi.URLParam(req, "id")})
	})
	r.Get("/search", func(w http.ResponseWriter, req *http.Request) {
		query := req.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		writeJSON(w, http.StatusOK, map[string]any{"q": query.Get("q"), "page": page})
	})
	r.Post("/users", func(w http.ResponseWriter, req *http.Request) {
		var input UserInput
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"name": input.Name, "age": input.Age})
	})
	r.With(netHTTPMiddleware, netHTTPGuard).Get("/chain", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
	})
	r.Get("/error", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
	})
	return r
}
