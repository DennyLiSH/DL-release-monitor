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
	ghClient  *github.Client
	sched     *scheduler.Scheduler
	cfg       *config.Config
	cfgMu     sync.RWMutex // protects cfg access
	startTime time.Time
}

// NewRouter creates a new API router
func NewRouter(db *gorm.DB, ghClient *github.Client, sched *scheduler.Scheduler, cfg *config.Config) *Router {
	r := chi.NewRouter()

	router := &Router{
		Mux:       r,
		db:        db,
		ghClient:  ghClient,
		sched:     sched,
		cfg:       cfg,
		startTime: time.Now(),
	}

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Routes
	r.Route("/api", func(r chi.Router) {
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

	// Serve static files (web UI)
	r.Get("/*", router.ServeIndex)

	return router
}
