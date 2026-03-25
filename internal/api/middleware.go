package api

import (
	"crypto/subtle"
	"encoding/json"
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
			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(token), []byte(authKey)) != 1 {
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
	// Use json.Marshal to safely encode the message and avoid JSON injection
	data, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		// This should never happen with a map[string]string
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}
