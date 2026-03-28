package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// envRegex matches ${VAR} or $VAR patterns for environment variable expansion
var envRegex = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Config represents the application configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	GitHub    GitHubConfig    `yaml:"github"`
	Storage   StorageConfig   `yaml:"storage"`
	Retention RetentionConfig `yaml:"retention"`
	Notify    NotifyConfig    `yaml:"notify"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port           int      `yaml:"port"`
	BaseURL        string   `yaml:"base_url"`
	AuthKey        string   `yaml:"auth_key"`        // API key for authentication (optional)
	AllowedOrigins []string `yaml:"allowed_origins"` // CORS allowed origins, empty or ["*"] allows all
}

// GitHubConfig holds GitHub API configuration
type GitHubConfig struct {
	Token        string `yaml:"token"`
	PollInterval int    `yaml:"poll_interval"` // minutes
	APIDelay     int    `yaml:"api_delay"`     // milliseconds between API requests (default: 1000)
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	Local LocalStorageConfig `yaml:"local"`
}

// LocalStorageConfig holds local storage configuration
type LocalStorageConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Path            string        `yaml:"path"`
	DownloadTimeout time.Duration `yaml:"download_timeout"` // download timeout (default: 10m)
	MaxFileSize     int64         `yaml:"max_file_size"`    // max download size in bytes (0 = no limit, default: 1GB)
}

// RetentionConfig holds retention policy configuration
type RetentionConfig struct {
	MaxVersions   int  `yaml:"max_versions"`    // keep last N versions
	KeepLastMajor bool `yaml:"keep_last_major"` // keep last of each major version
}

// NotifyConfig holds notification configuration
type NotifyConfig struct {
	Email   EmailConfig   `yaml:"email"`
	Webhook WebhookConfig `yaml:"webhook"`
}

// EmailConfig holds email notification configuration
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	SMTPUser string `yaml:"smtp_user"`
	SMTPPass string `yaml:"smtp_pass"`
	From     string `yaml:"from"`
	To       string `yaml:"to"`
	UseTLS   bool   `yaml:"use_tls"` // Use TLS for SMTP connection
}

// WebhookConfig holds webhook notification configuration
type WebhookConfig struct {
	Enabled bool          `yaml:"enabled"`
	URL     string        `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"` // webhook timeout (default: 10s)
}

// Load reads and parses the configuration file
func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	setDefaults(&cfg)

	return &cfg, nil
}

// expandEnv expands environment variables in the format ${VAR} or $VAR
func expandEnv(s string) string {
	return envRegex.ReplaceAllStringFunc(s, func(match string) string {
		var name string
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		return os.Getenv(name)
	})
}

// setDefaults sets default values for configuration
func setDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.BaseURL == "" {
		cfg.Server.BaseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}
	if len(cfg.Server.AllowedOrigins) == 0 {
		cfg.Server.AllowedOrigins = []string{"*"}
	}
	if cfg.GitHub.PollInterval == 0 {
		cfg.GitHub.PollInterval = 30
	}
	if cfg.GitHub.APIDelay == 0 {
		cfg.GitHub.APIDelay = 1000 // 1 second default
	}
	if cfg.Storage.Local.Path == "" {
		cfg.Storage.Local.Path = "./data/downloads"
	}
	if cfg.Storage.Local.DownloadTimeout == 0 {
		cfg.Storage.Local.DownloadTimeout = 10 * time.Minute
	}
	if cfg.Storage.Local.MaxFileSize == 0 {
		cfg.Storage.Local.MaxFileSize = 1 << 30 // 1 GB default
	}
	if cfg.Retention.MaxVersions == 0 {
		cfg.Retention.MaxVersions = 5
	}
	if cfg.Notify.Webhook.Timeout == 0 {
		cfg.Notify.Webhook.Timeout = 10 * time.Second
	}
}

// GetCheckInterval returns the effective check interval for a repo
func (c *Config) GetCheckInterval(repoCheckInterval int) int {
	if repoCheckInterval > 0 {
		return repoCheckInterval
	}
	return c.GitHub.PollInterval
}

// GetRetention returns the effective retention for a repo
func (c *Config) GetRetention(repoRetention int) int {
	if repoRetention > 0 {
		return repoRetention
	}
	return c.Retention.MaxVersions
}

// String returns a sanitized string representation (hides secrets)
func (c *Config) String() string {
	return fmt.Sprintf("Config{Server: {Port: %d}, GitHub: {PollInterval: %d}, Storage: {Local: {Enabled: %v, Path: %s}}, Retention: {MaxVersions: %d, KeepLastMajor: %v}}",
		c.Server.Port,
		c.GitHub.PollInterval,
		c.Storage.Local.Enabled,
		c.Storage.Local.Path,
		c.Retention.MaxVersions,
		c.Retention.KeepLastMajor,
	)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.GitHub.Token == "" {
		return fmt.Errorf("github token is required")
	}

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Storage.Local.Enabled && c.Storage.Local.Path == "" {
		return fmt.Errorf("local storage path is required when enabled")
	}

	if c.Notify.Email.Enabled {
		if c.Notify.Email.SMTPHost == "" {
			return fmt.Errorf("smtp_host is required when email notifications are enabled")
		}
		if c.Notify.Email.SMTPPort < 1 || c.Notify.Email.SMTPPort > 65535 {
			return fmt.Errorf("invalid smtp_port: %d (must be 1-65535)", c.Notify.Email.SMTPPort)
		}
	}

	if c.Notify.Webhook.Enabled && c.Notify.Webhook.URL == "" {
		return fmt.Errorf("webhook url is required when webhook notifications are enabled")
	}

	return nil
}

// ParseRepoFullName parses "owner/repo" format
func ParseRepoFullName(fullName string) (owner, repo string, err error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo format, expected 'owner/repo': %s", fullName)
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format, owner and repo must be non-empty: %s", fullName)
	}
	return parts[0], parts[1], nil
}

// AtomicConfig provides thread-safe atomic access to a Config pointer
// using copy-on-write semantics. Readers always get a consistent snapshot.
type AtomicConfig struct {
	ptr atomic.Pointer[Config]
}

// NewAtomicConfig creates a new AtomicConfig wrapping the given Config.
func NewAtomicConfig(cfg *Config) *AtomicConfig {
	ac := &AtomicConfig{}
	ac.ptr.Store(cfg)
	return ac
}

// Load atomically returns the current Config pointer.
func (ac *AtomicConfig) Load() *Config {
	return ac.ptr.Load()
}

// Store atomically replaces the Config pointer.
// Callers should create a copy, modify it, then Store the new pointer.
func (ac *AtomicConfig) Store(cfg *Config) {
	ac.ptr.Store(cfg)
}
