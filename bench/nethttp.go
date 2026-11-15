package bench

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func NewNetHTTP() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id")})
	})
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		writeJSON(w, http.StatusOK, map[string]any{"q": query.Get("q"), "page": page})
	})
	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var input UserInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"name": input.Name, "age": input.Age})
	})
	mux.Handle("GET /chain", netHTTPMiddleware(netHTTPGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
	}))))
	mux.HandleFunc("GET /error", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
	})
	return mux
}

func netHTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(mwHeader, "1")
		next.ServeHTTP(w, r)
	})
}

func netHTTPGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(authHeader) != authValue {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
