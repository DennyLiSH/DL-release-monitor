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
func (m *Manager) Send(notification *Notification) {
	for _, n := range m.notifiers {
		go func(notifier Notifier) {
			if err := notifier.Send(notification); err != nil {
				log.Printf("[%s] Failed to send notification: %v", notifier.Name(), err)
			} else {
				log.Printf("[%s] Notification sent for %s %s", notifier.Name(), notification.RepoName, notification.Version)
			}
		}(n)
	}
}
