package notify

import (
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
	Send(notification *Notification) error
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

// Send sends notification through all configured backends
// Notifications are sent sequentially to avoid overwhelming external services
func (m *Manager) Send(notification *Notification) {
	for _, n := range m.notifiers {
		if err := n.Send(notification); err != nil {
			log.Printf("[%s] Failed to send notification: %v", n.Name(), err)
		} else {
			log.Printf("[%s] Notification sent for %s %s", n.Name(), notification.RepoName, notification.Version)
		}
	}
}
