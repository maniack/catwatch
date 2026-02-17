package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/maniack/catwatch/internal/backend"
	"github.com/maniack/catwatch/internal/l10n"
	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/oauth"
	"github.com/maniack/catwatch/internal/sessions"
	"github.com/maniack/catwatch/internal/storage"
	"github.com/urfave/cli/v3"
	"gorm.io/gorm"
)

var (
	Version   string = "v0.0.0-dev"
	BuildTime string = ""
)

func init() {
	if BuildTime == "" || len(BuildTime) == 0 {
		BuildTime = time.Now().Format(time.RFC3339)
	}
}

func main() {
	buildTime, err := time.Parse(time.RFC3339, BuildTime)
	if err != nil {
		buildTime = time.Now()
	}

	cmd := &cli.Command{
		Name:        "catwatch",
		Usage:       "runs CatWatch web application",
		Description: "CatWatch — homeless cats monitoring and care",
		Version:     fmt.Sprintf("%s @ %s", Version, buildTime.Format(time.RFC3339)),
		Authors:     []any{"Lorylin's Cats <cats@loryl.in>"},
		Copyright:   fmt.Sprintf("Lorylin's Cats © %d", buildTime.Year()),
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "listen", Usage: "HTTP listen address", Value: ":8080", Sources: cli.EnvVars("LISTEN")},
			&cli.StringFlag{Name: "db", Usage: "Database DSN (SQLite or Postgres). Examples: 'catwatch.db', 'file::memory:?cache=shared', 'postgres://user:pass@host:5432/dbname'", Value: "catwatch.db", Sources: cli.EnvVars("DB_PATH")},
			&cli.StringSliceFlag{Name: "cors-origin", Usage: "CORS allowed origin", Value: []string{"*"}, Sources: cli.EnvVars("CORS_ORIGIN")},
			&cli.BoolFlag{Name: "debug", Usage: "Enable debug logging", Sources: cli.EnvVars("DEBUG")},
			&cli.StringFlag{Name: "log-format", Usage: "Log format (text or json)", Value: "text", Sources: cli.EnvVars("LOG_FORMAT")},
			&cli.StringFlag{Name: "metrics-endpoint", Usage: "", Value: "/metrics", Sources: cli.EnvVars("METRICS_ENDPOINT")},
			&cli.StringFlag{Name: "healthz-endpoint", Usage: "", Value: "/healthz", Sources: cli.EnvVars("HEALTHZ_ENDPOINT")},
			&cli.BoolFlag{Category: "development", Name: "devel", Usage: "Enable dev login endpoint", Sources: cli.EnvVars("DEV_LOGIN")},
			&cli.StringFlag{Category: "authentication", Name: "google-client-id", Usage: "Google OAuth client ID", Sources: cli.EnvVars("GOOGLE_CLIENT_ID")},
			&cli.StringFlag{Category: "authentication", Name: "google-client-secret", Usage: "Google OAuth client secret", Sources: cli.EnvVars("GOOGLE_CLIENT_SECRET")},
			&cli.StringFlag{Category: "authentication", Name: "google-redirect-url", Usage: "Google OAuth redirect URL", Sources: cli.EnvVars("GOOGLE_REDIRECT_URL")},
			&cli.StringFlag{Category: "authentication", Name: "oidc-issuer", Usage: "OIDC Issuer URL", Sources: cli.EnvVars("OIDC_ISSUER")},
			&cli.StringFlag{Category: "authentication", Name: "oidc-client-id", Usage: "OIDC Client ID", Sources: cli.EnvVars("OIDC_CLIENT_ID")},
			&cli.StringFlag{Category: "authentication", Name: "oidc-client-secret", Usage: "OIDC Client Secret", Sources: cli.EnvVars("OIDC_CLIENT_SECRET")},
			&cli.StringFlag{Category: "authentication", Name: "oidc-redirect-url", Usage: "OIDC Redirect URL", Sources: cli.EnvVars("OIDC_REDIRECT_URL")},
			&cli.StringFlag{Category: "authentication", Name: "session-redis", Usage: "Redis address for auth sessions", Sources: cli.EnvVars("SESSION_REDIS")},
			&cli.StringFlag{Category: "authentication", Name: "session-redis-pass", Usage: "Redis password", Sources: cli.EnvVars("SESSION_REDIS_PASS")},
			&cli.StringFlag{Category: "authentication", Name: "session-redis-prefix", Usage: "Redis key prefix", Value: "catwatch:oauth:", Sources: cli.EnvVars("SESSION_REDIS_PREFIX")},
			&cli.StringFlag{Category: "authentication", Name: "bot-api-key", Usage: "Static key for bot authentication", Sources: cli.EnvVars("BOT_API_KEY")},
			&cli.DurationFlag{Category: "authentication", Name: "auth-access-ttl", Usage: "Access token TTL", Value: 15 * time.Minute, Sources: cli.EnvVars("AUTH_ACCESS_TTL")},
			&cli.DurationFlag{Category: "authentication", Name: "auth-refresh-ttl", Usage: "Refresh token TTL", Value: 720 * time.Hour, Sources: cli.EnvVars("AUTH_REFRESH_TTL")},
			&cli.StringFlag{Category: "authentication", Name: "auth-success-redirect", Usage: "Redirect URL after successful OAuth", Value: "/", Sources: cli.EnvVars("AUTH_SUCCESS_REDIRECT")},
		},
		Commands: []*cli.Command{
			{
				Name:  "healthz",
				Usage: "health checks",
				Commands: []*cli.Command{
					{
						Name:  "alive",
						Usage: "shows application liveness",
						Action: func(ctx context.Context, c *cli.Command) error {
							livenessEndpoint := "http://localhost" + c.String("listen") + c.String("healthz-endpoint") + "/alive"
							clnt := &http.Client{}
							_, err := clnt.Get(livenessEndpoint)
							if err != nil {
								fmt.Println("FAIL")
								return err
							}
							fmt.Println("ALIVE")
							return nil
						},
					},
					{
						Name:  "ready",
						Usage: "shows application readiness",
						Action: func(ctx context.Context, c *cli.Command) error {
							readinessEndpoint := "http://localhost" + c.String("listen") + c.String("healthz-endpoint") + "/ready"
							clnt := &http.Client{}
							_, err := clnt.Get(readinessEndpoint)
							if err != nil {
								fmt.Println("FAIL")
								return err
							}
							fmt.Println("READY")
							return nil
						},
					},
				},
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			logging.Init(c.Bool("debug"), c.String("log-format") == "json")
			log := logging.L()

			// Init l10n
			if err := l10n.Init(); err != nil {
				log.WithError(err).Error("failed to initialize l10n")
			}

			store, err := storage.Open(c.String("db"))
			if err != nil {
				log.Fatalf("open storage: %v", err)
			}

			jwtSecret, err := store.GetJWTSecret()
			if err != nil {
				// If secret not found, generate and persist
				if errors.Is(err, gorm.ErrRecordNotFound) {
					jwtSecret = randomSecret()
					if err := store.SaveJWTSecret(jwtSecret); err != nil {
						log.Fatalf("save jwt secret: %v", err)
					}
					log.Infof("Generated new JWT secret and stored in DB")
				} else {
					log.Fatalf("get jwt secret: %v", err)
				}
			}

			var sessStore sessions.SessionStore
			if c.String("session-redis") != "" {
				sessStore = sessions.NewRedisSessionStore(c.String("session-redis"), c.String("session-redis-pass"), c.String("session-redis-prefix"))
			} else {
				sessStore = sessions.NewMemorySessionStore()
			}

			cfg := backend.Config{
				Store:           store,
				Logger:          log,
				Version:         c.Version,
				CORSAllowOrigin: c.StringSlice("cors-origin"),
				Monitoring: backend.MonitoringConfig{
					MetricsEndpoint: c.String("metrics-endpoint"),
					HealthzEndpoint: c.String("healthz-endpoint"),
				},
				DevLoginEnabled: c.Bool("devel"),
				JWTSecret:       jwtSecret,
				AccessTTL:       c.Duration("auth-access-ttl"),
				RefreshTTL:      c.Duration("auth-refresh-ttl"),
				BotAPIKey:       c.String("bot-api-key"),
				SessionStore:    sessStore,
				OAuth: oauth.Config{
					GoogleClientID:      c.String("google-client-id"),
					GoogleClientSecret:  c.String("google-client-secret"),
					GoogleRedirectURL:   c.String("google-redirect-url"),
					OIDCIssuer:          c.String("oidc-issuer"),
					OIDCClientID:        c.String("oidc-client-id"),
					OIDCClientSecret:    c.String("oidc-client-secret"),
					OIDCRedirectURL:     c.String("oidc-redirect-url"),
					AuthSuccessRedirect: c.String("auth-success-redirect"),
				},
			}

			srv, err := backend.NewServer(cfg)
			if err != nil {
				log.Fatalf("init server: %v", err)
			}

			addr := c.String("listen")
			web := &http.Server{
				Addr:              addr,
				Handler:           srv.Router,
				ReadTimeout:       15 * time.Second,
				ReadHeaderTimeout: 10 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       120 * time.Second,
			}
			log.Infof("CatWatch listening on %s", addr)
			if err := web.ListenAndServe(); err != nil {
				log.Fatalf("http server: %v", err)
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		// If initialization failed and didn't fatal, print error
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
