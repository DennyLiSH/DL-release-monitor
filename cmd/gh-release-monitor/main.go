package main

import (
	"context"
	"fmt"
	"log"
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

func main() {
	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Initialize database
	db, err := database.Init(cfg.Storage.Local.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		sqlDB, err := db.DB()
		if err != nil {
			log.Printf("Failed to get underlying DB: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			log.Printf("Failed to close database: %v", err)
		}
	}()

	// Create GitHub client
	ghClient := github.NewClient(cfg.GitHub.Token)

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
		log.Printf("Server starting on port %d", cfg.Server.Port)
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
		log.Println("Shutting down server...")
	case err := <-serverErr:
		log.Printf("Server failed: %v", err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
