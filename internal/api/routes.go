package api

import (
	"sync"
	"time"

	"gh-release-monitor/internal/config"
	"gh-release-monitor/internal/github"
	"gh-release-monitor/internal/scheduler"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"gorm.io/gorm"
)

// Router wraps the chi router
type Router struct {
	*chi.Mux
	db        *gorm.DB
	ghClient  github.ClientInterface
	sched     *scheduler.Scheduler
	cfgHolder *config.AtomicConfig
	cfgMu     sync.Mutex // serializes config updates (reads are lock-free via atomic)
	startTime time.Time
}

// NewRouter creates a new API router
func NewRouter(db *gorm.DB, ghClient github.ClientInterface, sched *scheduler.Scheduler, cfgHolder *config.AtomicConfig) *Router {
	r := chi.NewRouter()

	cfg := cfgHolder.Load()

	router := &Router{
		Mux:       r,
		db:        db,
		ghClient:  ghClient,
		sched:     sched,
		cfgHolder: cfgHolder,
		startTime: time.Now(),
	}

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Routes
	r.Route("/api", func(r chi.Router) {
		// Health check endpoints (no auth required)
		r.Get("/health", router.HealthCheck)
		r.Get("/ready", router.ReadyCheck)

		// Protected API endpoints (auth required if auth_key is configured)
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(cfg.Server.AuthKey))

			// Repository endpoints
			r.Get("/repos", router.ListRepos)
			r.Post("/repos", router.CreateRepo)
			r.Get("/repos/{id}", router.GetRepo)
			r.Put("/repos/{id}", router.UpdateRepo)
			r.Delete("/repos/{id}", router.DeleteRepo)

			// Release endpoints
			r.Get("/releases", router.ListReleases)

			// Download endpoints
			r.Get("/downloads", router.ListDownloads)

			// Check endpoint
			r.Post("/check", router.TriggerCheck)
			r.Post("/check/{id}", router.TriggerRepoCheck)

			// Config endpoints
			r.Get("/config", router.GetConfig)
			r.Put("/config", router.UpdateConfig)

			// Status endpoint
			r.Get("/status", router.GetStatus)
		})
	})

	// Serve static files (web UI)
	r.Get("/*", router.ServeIndex)

	return router
}
