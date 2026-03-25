package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gh-release-monitor/internal/api"
	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/database"
	"gh-release-monitor/internal/github"
	"gh-release-monitor/internal/scheduler"
)

// getEnvOrDefault returns the value of the environment variable or the default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	// Initialize structured logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Parse command line flags
	configPath := flag.String("config", getEnvOrDefault("CONFIG_PATH", "config.yaml"), "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	// Initialize database
	db, err := database.Init(cfg.Storage.Local.Path)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer func() {
		sqlDB, err := db.DB()
		if err != nil {
			slog.Error("Failed to get underlying DB", "error", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			slog.Error("Failed to close database", "error", err)
		}
	}()

	// Create GitHub client
	ghClient := github.NewClient(cfg.GitHub.Token)
	ghClient.SetAPIDelay(time.Duration(cfg.GitHub.APIDelay) * time.Millisecond)

	// Create scheduler
	sched := scheduler.New(db, ghClient, cfg)
	sched.Start()
	defer sched.Stop()

	// Setup API routes
	router := api.NewRouter(db, ghClient, sched, cfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			select {
			case serverErr <- err:
			default:
			}
		}
	}()

	// Wait for interrupt signal or server error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		slog.Info("Shutting down server...")
	case err := <-serverErr:
		slog.Error("Server failed", "error", err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exited")
}
