package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// DefaultWebhookTimeout is the default timeout for webhook notifications
const DefaultWebhookTimeout = 10 * time.Second

// ErrInvalidWebhookURL is returned when webhook URL fails security validation
var ErrInvalidWebhookURL = errors.New("webhook URL is not allowed: possible SSRF risk")

// validateWebhookURL validates the webhook URL to prevent SSRF attacks.
// It ensures the URL:
// - Uses http or https scheme
// - Does not point to private/reserved IP addresses
// - Does not use localhost or internal hostnames
func validateWebhookURL(webhookURL string) error {
	parsedURL, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("invalid scheme %q: only http and https are allowed", parsedURL.Scheme)
	}

	// Get hostname
	host := parsedURL.Hostname()
	if host == "" {
		return errors.New("URL must have a hostname")
	}

	// Block localhost variations
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || lowerHost == "127.0.0.1" || lowerHost == "::1" {
		return ErrInvalidWebhookURL
	}

	// Block internal hostnames
	if strings.HasSuffix(lowerHost, ".local") ||
		strings.HasSuffix(lowerHost, ".internal") ||
		strings.HasSuffix(lowerHost, ".localhost") {
		return ErrInvalidWebhookURL
	}

	// Resolve IP address to check for private ranges
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS lookup failed - could be a non-existent host
		// Allow it to proceed, the HTTP request will fail anyway
		return nil
	}

	for _, ip := range ips {
		// Check for loopback (127.0.0.0/8, ::1)
		if ip.IsLoopback() {
			return ErrInvalidWebhookURL
		}
		// Check for private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
		if ip.IsPrivate() {
			return ErrInvalidWebhookURL
		}
		// Check for link-local addresses (169.254.0.0/16, fe80::/10)
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return ErrInvalidWebhookURL
		}
	}

	return nil
}

// EmailNotifier sends notifications via email
type EmailNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       string
	useTLS   bool
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(host string, port int, username, password, from, to string, useTLS bool) *EmailNotifier {
	return &EmailNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       to,
		useTLS:   useTLS,
	}
}

// Name returns the notifier name
func (n *EmailNotifier) Name() string {
	return "email"
}

// Send sends an email notification with context support for cancellation and timeout.
func (n *EmailNotifier) Send(ctx context.Context, notification *Notification) error {
	// Check context before starting
	if ctx.Err() != nil {
		return ctx.Err()
	}

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

	// Use a channel to handle context cancellation for SMTP operations
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		var err error
		if n.useTLS {
			err = n.sendWithTLS(addr, msg.Bytes())
		} else {
			// Fallback to standard SendMail (for backwards compatibility)
			var auth smtp.Auth
			if n.username != "" && n.password != "" {
				auth = smtp.PlainAuth("", n.username, n.password, n.host)
			}
			err = smtp.SendMail(addr, auth, n.from, []string{n.to}, msg.Bytes())
		}
		resultCh <- result{err: err}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("email send cancelled: %w", ctx.Err())
	case res := <-resultCh:
		if res.err != nil {
			return fmt.Errorf("failed to send email: %w", res.err)
		}
		return nil
	}
}

// sendWithTLS sends email using explicit TLS connection
func (n *EmailNotifier) sendWithTLS(addr string, msg []byte) error {
	// TLS config with server name verification
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         n.host,
		MinVersion:         tls.VersionTLS12,
	}

	// Connect with TLS using tls.Dial
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close SMTP connection: %v", err)
		}
	}()

	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Failed to close SMTP client: %v", err)
		}
	}()

	// Authenticate if credentials provided
	if n.username != "" && n.password != "" {
		auth := smtp.PlainAuth("", n.username, n.password, n.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	// Set sender and recipient
	if err := client.Mail(n.from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}
	if err := client.Rcpt(n.to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	// Send data
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}
	_, err = writer.Write(msg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	return client.Quit()
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
	url    string
	client *http.Client
}

// NewWebhookNotifier creates a new webhook notifier with default timeout.
// Returns an error if the URL fails SSRF security validation.
func NewWebhookNotifier(url string) (*WebhookNotifier, error) {
	return NewWebhookNotifierWithTimeout(url, DefaultWebhookTimeout)
}

// NewWebhookNotifierWithTimeout creates a new webhook notifier with custom timeout.
// Returns an error if the URL fails SSRF security validation.
func NewWebhookNotifierWithTimeout(url string, timeout time.Duration) (*WebhookNotifier, error) {
	if err := validateWebhookURL(url); err != nil {
		return nil, fmt.Errorf("webhook URL validation failed: %w", err)
	}
	return &WebhookNotifier{
		url: url,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Name returns the notifier name
func (n *WebhookNotifier) Name() string {
	return "webhook"
}

// Send sends a webhook notification with context support.
func (n *WebhookNotifier) Send(ctx context.Context, notification *Notification) error {
	payload := map[string]any{
		"repo_name": notification.RepoName,
		"version":   notification.Version,
		"html_url":  notification.HTMLURL,
		"assets":    notification.AssetNames,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "POST", n.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard response body to ensure connection can be reused
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		log.Printf("Failed to drain webhook response body: %v", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
