package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
)

// EmailNotifier sends notifications via email
type EmailNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       string
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(host string, port int, username, password, from, to string) *EmailNotifier {
	return &EmailNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       to,
	}
}

// Name returns the notifier name
func (n *EmailNotifier) Name() string {
	return "email"
}

// Send sends an email notification
func (n *EmailNotifier) Send(notification *Notification) error {
	subject := fmt.Sprintf("[GitHub Release] %s %s released", notification.RepoName, notification.Version)
	body := n.buildBody(notification)

	msg := bytes.NewBuffer(nil)
	msg.WriteString(fmt.Sprintf("From: %s\r\n", n.from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", n.to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := fmt.Sprintf("%s:%d", n.host, n.port)

	var auth smtp.Auth
	if n.username != "" && n.password != "" {
		auth = smtp.PlainAuth("", n.username, n.password, n.host)
	}

	if err := smtp.SendMail(addr, auth, n.from, []string{n.to}, msg.Bytes()); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (n *EmailNotifier) buildBody(notification *Notification) string {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("A new release has been detected:\n\n"))
	buf.WriteString(fmt.Sprintf("Repository: %s\n", notification.RepoName))
	buf.WriteString(fmt.Sprintf("Version: %s\n", notification.Version))
	buf.WriteString(fmt.Sprintf("URL: %s\n", notification.HTMLURL))
	buf.WriteString("\n")

	if len(notification.AssetNames) > 0 {
		buf.WriteString("Downloaded assets:\n")
		for _, name := range notification.AssetNames {
			buf.WriteString(fmt.Sprintf("  - %s\n", name))
		}
	}

	return buf.String()
}

// WebhookNotifier sends notifications via HTTP webhook
type WebhookNotifier struct {
	url string
}

// NewWebhookNotifier creates a new webhook notifier
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{url: url}
}

// Name returns the notifier name
func (n *WebhookNotifier) Name() string {
	return "webhook"
}

// Send sends a webhook notification
func (n *WebhookNotifier) Send(notification *Notification) error {
	payload := map[string]interface{}{
		"repo_name": notification.RepoName,
		"version":   notification.Version,
		"html_url":  notification.HTMLURL,
		"assets":    notification.AssetNames,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	resp, err := http.Post(n.url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
