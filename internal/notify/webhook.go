package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultWebhookTimeout is the default timeout for webhook notifications
const DefaultWebhookTimeout = 10 * time.Second

// Webhook retry settings
const (
	webhookInitialDelay = 1 * time.Second
	webhookMaxDelay     = 30 * time.Second
	webhookMaxRetries   = 3
)

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

// WebhookNotifier sends notifications via HTTP webhook with retry support
type WebhookNotifier struct {
	url          string
	client       *http.Client
	maxRetries   int
	initialDelay time.Duration
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
		url:          url,
		maxRetries:   webhookMaxRetries,
		initialDelay: webhookInitialDelay,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Name returns the notifier name
func (n *WebhookNotifier) Name() string {
	return "webhook"
}

// Send sends a webhook notification with context support and retry on server errors.
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

	var lastErr error
	for attempt := 1; attempt <= n.maxRetries; attempt++ {
		// Check context before each attempt
		if ctx.Err() != nil {
			return fmt.Errorf("webhook send cancelled: %w", ctx.Err())
		}

		req, err := http.NewRequestWithContext(ctx, "POST", n.url, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create webhook request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := n.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send webhook: %w", err)
			if attempt < n.maxRetries {
				slog.Warn("Webhook send failed, retrying", "attempt", attempt, "error", err)
				n.wait(ctx, attempt)
				continue
			}
			return lastErr
		}

		// Read and discard response body to ensure connection can be reused
		if _, drainErr := io.Copy(io.Discard, resp.Body); drainErr != nil {
			slog.Warn("Failed to drain webhook response body", "error", drainErr)
		}
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
			if attempt < n.maxRetries {
				slog.Warn("Webhook server error, retrying", "attempt", attempt, "status", resp.StatusCode)
				n.wait(ctx, attempt)
				continue
			}
			return lastErr
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("webhook returned status %d", resp.StatusCode)
		}

		return nil
	}

	return lastErr
}

// wait pauses before retry with exponential backoff and jitter, respecting context cancellation.
// delay = initialDelay * 2^attempt + random jitter (±25%).
func (n *WebhookNotifier) wait(ctx context.Context, attempt int) {
	delay := n.initialDelay * time.Duration(1<<min(attempt, 10)) // cap shift to avoid overflow
	if delay > webhookMaxDelay {
		delay = webhookMaxDelay
	}
	// Add ±25% jitter
	jitterRange := delay / 4
	if jitterRange > 0 {
		jitterInt, _ := rand.Int(rand.Reader, big.NewInt(int64(jitterRange)*2+1))
		delay = delay - jitterRange + time.Duration(jitterInt.Int64())
	}

	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}
