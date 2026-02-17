package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/maniack/catwatch/internal/bot"
	"github.com/maniack/catwatch/internal/l10n"
	"github.com/maniack/catwatch/internal/logging"
	"github.com/urfave/cli/v3"
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
		Name:        "catwatch_bot",
		Usage:       "runs CatWatch Telegram bot",
		Description: "CatWatch Telegram bot — cat registry management via Telegram",
		Version:     fmt.Sprintf("%s @ %s", Version, buildTime.Format(time.RFC3339)),
		Authors:     []any{"Lorylin's Cats <cats@loryl.in>"},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tg-token", Usage: "Telegram bot token", Sources: cli.EnvVars("TG_TOKEN")},
			&cli.StringFlag{Name: "api-url", Usage: "Internal CatWatch API URL (reachable by bot)", Value: "http://localhost:8080", Sources: cli.EnvVars("API_URL")},
			&cli.StringFlag{Name: "public-api-url", Usage: "Public CatWatch API URL (reachable by user)", Sources: cli.EnvVars("PUBLIC_API_URL")},
			&cli.StringFlag{Name: "bot-api-key", Usage: "Static key for bot authentication", Sources: cli.EnvVars("BOT_API_KEY")},
			&cli.StringFlag{Name: "listen", Usage: "HTTP listen address for health checks", Value: ":8080", Sources: cli.EnvVars("LISTEN")},
			&cli.StringFlag{Name: "healthz-endpoint", Usage: "Healthz endpoint path", Value: "/healthz", Sources: cli.EnvVars("HEALTHZ_ENDPOINT")},
			&cli.BoolFlag{Name: "debug", Usage: "Enable debug logging", Sources: cli.EnvVars("DEBUG")},
			&cli.StringFlag{Name: "log-format", Usage: "Log format (text or json)", Value: "text", Sources: cli.EnvVars("LOG_FORMAT")},
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
							resp, err := clnt.Get(livenessEndpoint)
							if err != nil || resp.StatusCode != http.StatusOK {
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
							resp, err := clnt.Get(readinessEndpoint)
							if err != nil || resp.StatusCode != http.StatusOK {
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

			token := c.String("tg-token")
			if token == "" {
				log.Fatal("Telegram token is required (use --tg-token or TG_TOKEN env)")
			}

			cfg := bot.Config{
				Token:            token,
				API:              bot.NewAPIClient(c.String("api-url"), c.String("public-api-url"), c.String("bot-api-key"), log),
				Logger:           log,
				Debug:            c.Bool("debug"),
				HealthListenAddr: c.String("listen"),
			}

			b, err := bot.NewBot(cfg)
			if err != nil {
				log.Fatalf("init bot: %v", err)
			}

			log.Infof("CatWatch bot starting...")
			if err := b.Start(ctx); err != nil {
				log.Fatalf("run bot: %v", err)
			}

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
