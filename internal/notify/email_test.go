package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid https URL",
			url:     "https://example.com/webhook",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://example.com/webhook",
			wantErr: false,
		},
		{
			name:    "localhost blocked",
			url:     "http://localhost/webhook",
			wantErr: true,
		},
		{
			name:    "127.0.0.1 blocked",
			url:     "http://127.0.0.1/webhook",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			url:     "ftp://example.com/webhook",
			wantErr: true,
		},
		{
			name:    "internal hostname blocked",
			url:     "http://internal.local/webhook",
			wantErr: true,
		},
		{
			name:    "invalid URL format",
			url:     "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateWebhookURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestWebhookNotifier_Send(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header not set to application/json")
		}

		// Return success
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier, err := NewWebhookNotifierWithTimeout(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create webhook notifier: %v", err)
	}

	notification := &Notification{
		RepoName:   "owner/repo",
		Version:    "v1.0.0",
		AssetNames: []string{"app.zip", "app.tar.gz"},
		HTMLURL:    "https://github.com/owner/repo/releases/tag/v1.0.0",
	}

	err = notifier.Send(context.Background(), notification)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
}

func TestWebhookNotifier_SendWithRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first two attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on third attempt
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier, err := NewWebhookNotifierWithTimeout(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create webhook notifier: %v", err)
	}

	notification := &Notification{
		RepoName:   "owner/repo",
		Version:    "v1.0.0",
		AssetNames: []string{"app.zip"},
		HTMLURL:    "https://github.com/owner/repo/releases/tag/v1.0.0",
	}

	err = notifier.Send(context.Background(), notification)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestWebhookNotifier_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier, err := NewWebhookNotifierWithTimeout(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create webhook notifier: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	notification := &Notification{
		RepoName:   "owner/repo",
		Version:    "v1.0.0",
		AssetNames: []string{"app.zip"},
		HTMLURL:    "https://github.com/owner/repo/releases/tag/v1.0.0",
	}

	err = notifier.Send(ctx, notification)
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

func TestWebhookNotifier_InvalidURL(t *testing.T) {
	_, err := NewWebhookNotifierWithTimeout("http://localhost/webhook", 5*time.Second)
	if err == nil {
		t.Error("Expected error for localhost URL")
	}
}

func TestWebhookNotifier_Name(t *testing.T) {
	notifier, err := NewWebhookNotifierWithTimeout("https://example.com/webhook", 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to create webhook notifier: %v", err)
	}
	if notifier.Name() != "webhook" {
		t.Errorf("Name() = %q, want %q", notifier.Name(), "webhook")
	}
}

func TestEmailNotifier_Name(t *testing.T) {
	notifier := NewEmailNotifier("smtp.example.com", 587, "user", "pass", "from@example.com", "to@example.com", true)
	if notifier.Name() != "email" {
		t.Errorf("Name() = %q, want %q", notifier.Name(), "email")
	}
}

func TestEmailNotifier_Send_ContextCancellation(t *testing.T) {
	notifier := NewEmailNotifier("smtp.example.com", 587, "user", "pass", "from@example.com", "to@example.com", true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	notification := &Notification{
		RepoName:   "owner/repo",
		Version:    "v1.0.0",
		AssetNames: []string{"app.zip"},
		HTMLURL:    "https://github.com/owner/repo/releases/tag/v1.0.0",
	}

	err := notifier.Send(ctx, notification)
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}
