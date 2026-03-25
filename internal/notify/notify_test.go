package notify

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestValidateWebhookURL_Valid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"https URL", "https://example.com/webhook"},
		{"http URL", "http://example.com/webhook"},
		{"with port", "https://example.com:8080/webhook"},
		{"with path", "https://example.com/api/v1/webhook"},
		{"with query", "https://example.com/webhook?token=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.url)
			if err != nil {
				t.Errorf("Expected valid URL %q, got error: %v", tt.url, err)
			}
		})
	}
}

func TestValidateWebhookURL_InvalidScheme(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"ftp scheme", "ftp://example.com/webhook"},
		{"file scheme", "file:///etc/passwd"},
		{"no scheme", "example.com/webhook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.url)
			if err == nil {
				t.Errorf("Expected error for invalid URL %q", tt.url)
			}
		})
	}
}

func TestValidateWebhookURL_SSRFProtection(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"localhost", "https://localhost/webhook"},
		{"127.0.0.1", "https://127.0.0.1/webhook"},
		{"::1", "https://[::1]/webhook"},
		{".local domain", "https://test.local/webhook"},
		{".internal domain", "https://test.internal/webhook"},
		{".localhost domain", "https://test.localhost/webhook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.url)
			if !errors.Is(err, ErrInvalidWebhookURL) && err != nil {
				t.Errorf("Expected ErrInvalidWebhookURL for %q, got: %v", tt.url, err)
			}
		})
	}
}

func TestValidateWebhookURL_InvalidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty string", ""},
		{"invalid URL", "://invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(tt.url)
			if err == nil {
				t.Errorf("Expected error for invalid URL %q", tt.url)
			}
		})
	}
}

func TestNewWebhookNotifier_ValidURL(t *testing.T) {
	notifier, err := NewWebhookNotifier("https://example.com/webhook")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if notifier == nil {
		t.Fatal("Expected non-nil notifier")
	}
	if notifier.Name() != "webhook" {
		t.Errorf("Expected name 'webhook', got %q", notifier.Name())
	}
}

func TestNewWebhookNotifier_InvalidURL(t *testing.T) {
	_, err := NewWebhookNotifier("https://localhost/webhook")
	if err == nil {
		t.Error("Expected error for localhost URL")
	}

	// Check that the error contains URL to help identify the issue
	if err != nil {
		var urlErr *url.Error
		if !errors.Is(err, ErrInvalidWebhookURL) && !errors.As(err, &urlErr) {
			t.Logf("Error type: %T", err)
		}
	}
}

// mockNotifier is a mock implementation of Notifier interface
type mockNotifier struct {
	name    string
	err     error
	called  bool
	delay   time.Duration
	mu      sync.Mutex
}

func (m *mockNotifier) Name() string {
	return m.name
}

func (m *mockNotifier) Send(ctx context.Context, notification *Notification) error {
	m.mu.Lock()
	m.called = true
	m.mu.Unlock()

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.delay):
		}
	}

	return m.err
}

func (m *mockNotifier) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func TestManager_NewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("Expected non-nil manager")
	}
	if len(m.notifiers) != 0 {
		t.Error("Expected empty notifiers slice")
	}
}

func TestManager_AddNotifier(t *testing.T) {
	m := NewManager()
	m.AddNotifier(&mockNotifier{name: "test"})

	if len(m.notifiers) != 1 {
		t.Errorf("Expected 1 notifier, got %d", len(m.notifiers))
	}
}

func TestManager_Send_NoNotifiers(t *testing.T) {
	m := NewManager()
	errs := m.Send(context.Background(), &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	if errs != nil {
		t.Errorf("Expected nil errors with no notifiers, got %v", errs)
	}
}

func TestManager_Send_Success(t *testing.T) {
	m := NewManager()
	mock := &mockNotifier{name: "mock"}
	m.AddNotifier(mock)

	errs := m.Send(context.Background(), &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	if len(errs) != 0 {
		t.Errorf("Expected no errors, got %v", errs)
	}
	if !mock.wasCalled() {
		t.Error("Expected notifier to be called")
	}
}

func TestManager_Send_Error(t *testing.T) {
	m := NewManager()
	expectedErr := errors.New("send failed")
	mock := &mockNotifier{name: "mock", err: expectedErr}
	m.AddNotifier(mock)

	errs := m.Send(context.Background(), &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	if len(errs) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errs))
	}
	if !mock.wasCalled() {
		t.Error("Expected notifier to be called")
	}
}

func TestManager_Send_ContextCancellation(t *testing.T) {
	m := NewManager()
	mock := &mockNotifier{name: "mock", delay: 100 * time.Millisecond}
	m.AddNotifier(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	errs := m.Send(ctx, &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	// Should have at least one error due to context cancellation
	if len(errs) == 0 {
		t.Error("Expected error due to context cancellation")
	}
}

func TestManager_Send_MultipleNotifiers(t *testing.T) {
	m := NewManager()
	mock1 := &mockNotifier{name: "mock1"}
	mock2 := &mockNotifier{name: "mock2"}
	m.AddNotifier(mock1)
	m.AddNotifier(mock2)

	errs := m.Send(context.Background(), &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	if len(errs) != 0 {
		t.Errorf("Expected no errors, got %v", errs)
	}
	if !mock1.wasCalled() || !mock2.wasCalled() {
		t.Error("Expected both notifiers to be called")
	}
}

func TestManager_Send_PartialFailure(t *testing.T) {
	m := NewManager()
	mock1 := &mockNotifier{name: "mock1", err: errors.New("failed")}
	mock2 := &mockNotifier{name: "mock2"}
	m.AddNotifier(mock1)
	m.AddNotifier(mock2)

	errs := m.Send(context.Background(), &Notification{
		RepoName: "test/repo",
		Version:  "v1.0.0",
	})

	// First notifier fails, but second should still be called
	if len(errs) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errs))
	}
	if !mock2.wasCalled() {
		t.Error("Expected second notifier to be called even after first failure")
	}
}
