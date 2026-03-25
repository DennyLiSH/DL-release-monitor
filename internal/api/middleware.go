package api

import (
	"net/http"
	"strings"
)

// AuthMiddleware returns a middleware that checks for a valid API key
// If authKey is empty, no authentication is required
func AuthMiddleware(authKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication if no auth key is configured
			if authKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Get Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w, "Missing Authorization header")
				return
			}

			// Expect "Bearer <token>" format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				writeUnauthorized(w, "Invalid Authorization header format, expected 'Bearer <token>'")
				return
			}

			token := parts[1]
			if token != authKey {
				writeUnauthorized(w, "Invalid API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeUnauthorized writes an unauthorized error response
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + message + `"}`))
}
