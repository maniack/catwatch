package backend

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	chmw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/sirupsen/logrus"

	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/monitoring"
	"github.com/maniack/catwatch/internal/oauth"
	"github.com/maniack/catwatch/internal/sessions"
	"github.com/maniack/catwatch/internal/storage"
)

type MonitoringConfig struct {
	MetricsEndpoint string
	HealthzEndpoint string
}

type Config struct {
	Store           *storage.Store
	Logger          *logrus.Logger
	Version         string
	CORSAllowOrigin []string
	Monitoring      MonitoringConfig

	JWTSecret  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration

	OAuth        oauth.Config
	BotAPIKey    string
	SessionStore sessions.SessionStore

	DevLoginEnabled bool
	SkipWorkers     bool
}

type Server struct {
	Router   chi.Router
	store    *storage.Store
	log      *logrus.Logger
	cfg      Config
	sessions sessions.SessionStore
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		logging.Init(false, false)
		cfg.Logger = logging.L()
	}
	if cfg.Monitoring.MetricsEndpoint == "" {
		cfg.Monitoring.MetricsEndpoint = "/metrics"
	}
	if cfg.Monitoring.HealthzEndpoint == "" {
		cfg.Monitoring.HealthzEndpoint = "/healthz"
	}

	monitoring.Init()

	s := &Server{store: cfg.Store, log: cfg.Logger, cfg: cfg, sessions: cfg.SessionStore}
	r := chi.NewRouter()
	s.Router = r

	// Middlewares
	r.Use(chmw.RequestID)
	r.Use(chmw.RealIP)
	r.Use(chmw.Recoverer)
	r.Use(chmw.Logger)
	r.Use(RequestLogger(cfg.Logger, cfg.Monitoring.HealthzEndpoint+"/alive", cfg.Monitoring.HealthzEndpoint+"/ready"))
	r.Use(SecurityHeaders())
	r.Use(s.JWTAuth)
	r.Use(s.AuditMiddleware)

	co := cors.Options{
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}
	if len(cfg.CORSAllowOrigin) == 0 {
		co.AllowOriginFunc = func(r *http.Request, origin string) bool {
			if origin == "" {
				return false
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return u.Host == r.Host
		}
	} else {
		for _, o := range cfg.CORSAllowOrigin {
			if o == "*" {
				co.AllowCredentials = false
			}
		}
		co.AllowedOrigins = cfg.CORSAllowOrigin
	}
	r.Use(cors.Handler(co))

	// OAuth Handlers
	oa := oauth.NewHandler(s.store, s.log, cfg.OAuth, s.sessions, s.issueTokens)
	r.Route("/auth", func(r chi.Router) {
		r.Get("/google/login", oa.HandleGoogleLogin)
		r.Get("/google/callback", oa.HandleGoogleCallback)
		r.Get("/oidc/login", oa.HandleOIDCLogin)
		r.Get("/oidc/callback", oa.HandleOIDCCallback)
		r.Get("/dev/login", s.handleDevLoginGET)
	})

	// Healthz
	r.Route(cfg.Monitoring.HealthzEndpoint, func(r chi.Router) {
		r.Get("/alive", s.handleAlive)
		r.Get("/ready", s.handleReady)
	})
	// Metrics
	r.Handle(cfg.Monitoring.MetricsEndpoint, monitoring.Handler())

	// API
	r.Route("/api", func(r chi.Router) {
		r.Get("/config", s.handleGetConfig)
		r.Get("/records/planned", s.listAllPlannedRecords)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/dev-login", s.handleDevLogin)
			r.Post("/refresh", s.handleRefresh)
			r.Post("/logout", s.handleLogout)
			r.Group(func(r chi.Router) {
				r.Use(s.RequireAuth)
				r.Get("/user", s.handleGetUser)
			})
		})

		r.Route("/cats", func(r chi.Router) {
			r.Get("/", s.listCats)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.getCat)
				r.Get("/records", s.listRecords)
				r.Get("/images/{imgId}", s.getCatImageBinary)

				// Protected mutation routes
				r.Group(func(r chi.Router) {
					r.Use(s.RequireAuth)
					r.Put("/", s.updateCat)
					r.Delete("/", s.deleteCat)
					// Records
					r.Post("/records", s.createRecord)
					r.Route("/records/{rid}", func(r chi.Router) {
						r.Put("/", s.updateRecord)
						r.Post("/done", s.markRecordDone)
					})
					// Images
					r.Route("/images", func(r chi.Router) {
						r.Post("/", s.addCatImage)
						r.Delete("/{imgId}", s.deleteCatImage)
					})
					// Locations
					r.Post("/locations", s.addCatLocation)
				})
			})
			// Global cat creation (needs to be outside /{id} but inside /cats)
			r.Group(func(r chi.Router) {
				r.Use(s.RequireAuth)
				r.Post("/", s.createCat)
			})
		})

		// Global record/image routes (shorter URLs for bot callback data)
		r.Route("/records", func(r chi.Router) {
			r.Use(s.RequireAuth)
			r.Post("/{rid}/done", s.markRecordDone)
		})
		r.Route("/images", func(r chi.Router) {
			r.Use(s.RequireAuth)
			r.Delete("/{imgId}", s.deleteCatImage)
		})

		r.Route("/bot", func(r chi.Router) {
			r.Post("/register", s.registerBotUser)
			r.Post("/notifications", s.markNotificationSent)
			r.Get("/users", s.listBotUsers)
			r.Post("/token", s.handleBotToken)
			r.Post("/unlink", s.handleBotUnlink)
		})
	})

	// Start background workers
	if !cfg.SkipWorkers {
		s.startImageOptimizer()
		s.startImageCleanup(5, 10*time.Minute)
		s.startCatConditionCollector(30 * time.Second)
	}

	return s, nil
}

func (s *Server) handleAlive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.store == nil || s.store.DB == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// Simple ping via raw query
	if err := s.store.DB.WithContext(ctx).Exec("select 1").Error; err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// helpers
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseTimeRFC3339(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
