package notify

import (
	"context"
	"fmt"
	"log"
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
	m.notifiers = append(m.notifiers, notifier)
}

// Send sends notification through all configured backends.
// Notifications are sent sequentially to avoid overwhelming external services.
// Context is used for cancellation and timeout.
// Returns a slice of errors from failed notifications (nil if all succeeded).
func (m *Manager) Send(ctx context.Context, notification *Notification) []error {
	var errs []error

	for _, n := range m.notifiers {
		// Check context before each notification
		if ctx.Err() != nil {
			errs = append(errs, fmt.Errorf("notification canceled"))
			break
		}
		if err := n.Send(ctx, notification); err != nil {
			log.Printf("[%s] Failed to send notification: %v", n.Name(), err)
			errs = append(errs, fmt.Errorf("[%s]: %w", n.Name(), err))
		} else {
			log.Printf("[%s] Notification sent for %s %s", n.Name(), notification.RepoName, notification.Version)
		}
	}

	return errs
}
