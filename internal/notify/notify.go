package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Notification represents a notification payload
type Notification struct {
	RepoName   string
	Version    string
	AssetNames []string
	HTMLURL    string
}

// Notifier is the interface for notification backends
type Notifier interface {
	// Send sends a notification. Context is used for cancellation and timeout.
	Send(ctx context.Context, notification *Notification) error
	Name() string
}

// Manager manages multiple notification backends
type Manager struct {
	mu        sync.Mutex
	notifiers []Notifier
}

// NewManager creates a new notification manager
func NewManager() *Manager {
	return &Manager{
		notifiers: make([]Notifier, 0),
	}
}

// AddNotifier adds a notifier to the manager
func (m *Manager) AddNotifier(notifier Notifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifiers = append(m.notifiers, notifier)
}

// Send sends notification through all configured backends.
// Notifications are sent sequentially to avoid overwhelming external services.
// Context is used for cancellation and timeout.
// Returns a slice of errors from failed notifications (nil if all succeeded).
func (m *Manager) Send(ctx context.Context, notification *Notification) []error {
	// Copy notifiers slice under lock to allow concurrent AddNotifier calls
	m.mu.Lock()
	notifiers := make([]Notifier, len(m.notifiers))
	copy(notifiers, m.notifiers)
	m.mu.Unlock()

	var errs []error

	for _, n := range notifiers {
		// Check context before each notification
		if ctx.Err() != nil {
			errs = append(errs, fmt.Errorf("notification canceled"))
			break
		}
		if err := n.Send(ctx, notification); err != nil {
			slog.Error("Failed to send notification", "notifier", n.Name(), "error", err)
			errs = append(errs, fmt.Errorf("[%s]: %w", n.Name(), err))
		} else {
			slog.Info("Notification sent", "notifier", n.Name(), "repo", notification.RepoName, "version", notification.Version)
		}
	}

	return errs
}
