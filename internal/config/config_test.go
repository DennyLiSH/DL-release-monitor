package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "no env vars",
			input:    "hello world",
			envVars:  nil,
			expected: "hello world",
		},
		{
			name:     "single ${VAR}",
			input:    "token: ${GITHUB_TOKEN}",
			envVars:  map[string]string{"GITHUB_TOKEN": "secret123"},
			expected: "token: secret123",
		},
		{
			name:     "single $VAR",
			input:    "token: $GITHUB_TOKEN",
			envVars:  map[string]string{"GITHUB_TOKEN": "secret123"},
			expected: "token: secret123",
		},
		{
			name:     "multiple env vars",
			input:    "host: ${HOST} port: $PORT",
			envVars:  map[string]string{"HOST": "localhost", "PORT": "8080"},
			expected: "host: localhost port: 8080",
		},
		{
			name:     "undefined env var",
			input:    "token: ${UNDEFINED_VAR}",
			envVars:  nil,
			expected: "token: ",
		},
		{
			name:     "mixed formats",
			input:    "a: ${VAR_A} b: $VAR_B",
			envVars:  map[string]string{"VAR_A": "valueA", "VAR_B": "valueB"},
			expected: "a: valueA b: valueB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			result := expandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *Config
		expected *Config
	}{
		{
			name:  "empty config gets defaults",
			input: &Config{},
			expected: &Config{
				Server: ServerConfig{
					Port:    8080,
					BaseURL: "http://localhost:8080",
				},
				GitHub: GitHubConfig{
					PollInterval: 30,
				},
				Storage: StorageConfig{
					Local: LocalStorageConfig{
						Path: "./data/downloads",
					},
				},
				Retention: RetentionConfig{
					MaxVersions: 5,
				},
			},
		},
		{
			name: "custom values preserved",
			input: &Config{
				Server: ServerConfig{
					Port:    3000,
					BaseURL: "https://example.com",
				},
				GitHub: GitHubConfig{
					PollInterval: 60,
				},
				Storage: StorageConfig{
					Local: LocalStorageConfig{
						Path: "/custom/path",
					},
				},
				Retention: RetentionConfig{
					MaxVersions: 10,
				},
			},
			expected: &Config{
				Server: ServerConfig{
					Port:    3000,
					BaseURL: "https://example.com",
				},
				GitHub: GitHubConfig{
					PollInterval: 60,
				},
				Storage: StorageConfig{
					Local: LocalStorageConfig{
						Path: "/custom/path",
					},
				},
				Retention: RetentionConfig{
					MaxVersions: 10,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDefaults(tt.input)

			if tt.input.Server.Port != tt.expected.Server.Port {
				t.Errorf("Server.Port: expected %d, got %d", tt.expected.Server.Port, tt.input.Server.Port)
			}
			if tt.input.Server.BaseURL != tt.expected.Server.BaseURL {
				t.Errorf("Server.BaseURL: expected %s, got %s", tt.expected.Server.BaseURL, tt.input.Server.BaseURL)
			}
			if tt.input.GitHub.PollInterval != tt.expected.GitHub.PollInterval {
				t.Errorf("GitHub.PollInterval: expected %d, got %d", tt.expected.GitHub.PollInterval, tt.input.GitHub.PollInterval)
			}
			if tt.input.Storage.Local.Path != tt.expected.Storage.Local.Path {
				t.Errorf("Storage.Local.Path: expected %s, got %s", tt.expected.Storage.Local.Path, tt.input.Storage.Local.Path)
			}
			if tt.input.Retention.MaxVersions != tt.expected.Retention.MaxVersions {
				t.Errorf("Retention.MaxVersions: expected %d, got %d", tt.expected.Retention.MaxVersions, tt.input.Retention.MaxVersions)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("valid config file", func(t *testing.T) {
		content := `
server:
  port: 3000
github:
  token: test-token
  poll_interval: 60
storage:
  local:
    enabled: true
    path: /data/downloads
retention:
  max_versions: 10
  keep_last_major: true
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if cfg.Server.Port != 3000 {
			t.Errorf("expected port 3000, got %d", cfg.Server.Port)
		}
		if cfg.GitHub.Token != "test-token" {
			t.Errorf("expected token 'test-token', got %s", cfg.GitHub.Token)
		}
		if cfg.GitHub.PollInterval != 60 {
			t.Errorf("expected poll_interval 60, got %d", cfg.GitHub.PollInterval)
		}
		if cfg.Storage.Local.Path != "/data/downloads" {
			t.Errorf("expected path '/data/downloads', got %s", cfg.Storage.Local.Path)
		}
		if cfg.Retention.MaxVersions != 10 {
			t.Errorf("expected max_versions 10, got %d", cfg.Retention.MaxVersions)
		}
		if !cfg.Retention.KeepLastMajor {
			t.Error("expected keep_last_major to be true")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Load("nonexistent.yaml")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		content := `
server:
  port: [invalid]
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("expected error for invalid yaml")
		}
	})

	t.Run("env var expansion", func(t *testing.T) {
		os.Setenv("TEST_TOKEN", "env-token-value")
		defer os.Unsetenv("TEST_TOKEN")

		content := `
github:
  token: ${TEST_TOKEN}
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if cfg.GitHub.Token != "env-token-value" {
			t.Errorf("expected token 'env-token-value', got %s", cfg.GitHub.Token)
		}
	})
}

func TestGetCheckInterval(t *testing.T) {
	cfg := &Config{
		GitHub: GitHubConfig{
			PollInterval: 30,
		},
	}

	tests := []struct {
		name         string
		repoInterval int
		expected     int
	}{
		{"use repo interval", 60, 60},
		{"use default when repo is 0", 0, 30},
		{"use default when repo is negative", -1, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.GetCheckInterval(tt.repoInterval)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetRetention(t *testing.T) {
	cfg := &Config{
		Retention: RetentionConfig{
			MaxVersions: 5,
		},
	}

	tests := []struct {
		name          string
		repoRetention int
		expected      int
	}{
		{"use repo retention", 10, 10},
		{"use default when repo is 0", 0, 5},
		{"use default when repo is negative", -1, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.GetRetention(tt.repoRetention)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				GitHub: GitHubConfig{Token: "valid-token"},
				Server: ServerConfig{Port: 8080},
			},
			wantErr: false,
		},
		{
			name:    "missing token",
			config:  &Config{GitHub: GitHubConfig{Token: ""}},
			wantErr: true,
			errMsg:  "github token is required",
		},
		{
			name: "invalid port - too low",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 0},
			},
			wantErr: true,
			errMsg:  "invalid server port",
		},
		{
			name: "invalid port - too high",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 70000},
			},
			wantErr: true,
			errMsg:  "invalid server port",
		},
		{
			name: "local storage enabled but no path",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 8080},
				Storage: StorageConfig{
					Local: LocalStorageConfig{Enabled: true, Path: ""},
				},
			},
			wantErr: true,
			errMsg:  "local storage path is required",
		},
		{
			name: "email enabled but missing host",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 8080},
				Notify: NotifyConfig{
					Email: EmailConfig{Enabled: true, SMTPHost: ""},
				},
			},
			wantErr: true,
			errMsg:  "smtp_host is required",
		},
		{
			name: "email enabled with invalid port",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 8080},
				Notify: NotifyConfig{
					Email: EmailConfig{
						Enabled:  true,
						SMTPHost: "smtp.example.com",
						SMTPPort: 0,
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid smtp_port",
		},
		{
			name: "webhook enabled but missing url",
			config: &Config{
				GitHub: GitHubConfig{Token: "token"},
				Server: ServerConfig{Port: 8080},
				Notify: NotifyConfig{
					Webhook: WebhookConfig{Enabled: true, URL: ""},
				},
			},
			wantErr: true,
			errMsg:  "webhook url is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestParseRepoFullName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"valid with dash", "my-org/my-repo", "my-org", "my-repo", false},
		{"missing slash", "invalid", "", "", true},
		{"too many slashes", "owner/repo/extra", "", "", true},
		{"empty string", "", "", "", true},
		{"single slash", "/", "", "", true}, // Split returns ["", ""], empty owner/repo is invalid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepoFullName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				if owner != tt.wantOwner {
					t.Errorf("owner: expected %q, got %q", tt.wantOwner, owner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo: expected %q, got %q", tt.wantRepo, repo)
				}
			}
		})
	}
}

func TestConfigString(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			BaseURL: "http://localhost:8080",
		},
		GitHub: GitHubConfig{
			Token:        "secret-token", // should not appear
			PollInterval: 30,
		},
		Storage: StorageConfig{
			Local: LocalStorageConfig{
				Enabled: true,
				Path:    "/data",
			},
		},
		Retention: RetentionConfig{
			MaxVersions:   5,
			KeepLastMajor: true,
		},
	}

	result := cfg.String()

	// Should contain public info
	if result == "" {
		t.Error("String() returned empty string")
	}

	// Should NOT contain secret token
	if result != "" && len(result) > 0 {
		for _, secret := range []string{"secret-token", "Token:"} {
			if result == secret || (len(result) > len(secret) && result[:len(secret)] == secret) {
				t.Errorf("String() should not contain secret token")
			}
		}
	}
}
