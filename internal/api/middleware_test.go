package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_NoAuthKey(t *testing.T) {
	// When authKey is empty, all requests should pass
	middleware := AuthMiddleware("")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Body.String() != "success" {
		t.Errorf("Expected body 'success', got '%s'", rec.Body.String())
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	middleware := AuthMiddleware("secret-key")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "secret-key"},
		{"wrong prefix", "Basic secret-key"},
		{"empty token", "Bearer "},
		{"missing token", "Bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := AuthMiddleware("secret-key")

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Handler should not be called")
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			middleware(handler).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	middleware := AuthMiddleware("secret-key")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	middleware := AuthMiddleware("secret-key")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if rec.Body.String() != "success" {
		t.Errorf("Expected body 'success', got '%s'", rec.Body.String())
	}
}

func TestAuthMiddleware_CaseInsensitiveBearer(t *testing.T) {
	middleware := AuthMiddleware("secret-key")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []string{
		"Bearer secret-key",
		"bearer secret-key",
		"BEARER secret-key",
		"BeArEr secret-key",
	}

	for i, authHeader := range tests {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", authHeader)
			rec := httptest.NewRecorder()

			middleware(handler).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d for header '%s', got %d", http.StatusOK, authHeader, rec.Code)
			}
		})
	}
}

func TestWriteUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()

	writeUnauthorized(rec, "Test error message")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	expectedBody := `{"error":"Test error message"}`
	if rec.Body.String() != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}
