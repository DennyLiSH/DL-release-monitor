package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
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
			slog.Error("Failed to close SMTP connection", "error", err)
		}
	}()

	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			slog.Error("Failed to close SMTP client", "error", err)
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
