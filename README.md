# CatWatch — Monitoring and Care for Homeless Cats

An application for volunteers and organizations dedicated to helping homeless cats. It allows maintaining a cat registry, tracking their locations, health status, and planning necessary procedures (feeding, examinations, vaccinations, etc.).


## Features
- Cat registry with photos, descriptions, tags, and service history.
- Cat location tracking (locations).
- Procedure calendar: planning one-time and recurring events.
- Notifications about upcoming procedures in the Telegram bot.
- Automatic tracking of the last discovery date (`LastSeen`).
- Localization support: English (EN) and Russian (RU) based on user's language.
- Image management: automatic optimization (WebP, resizing), multi-upload support (up to 5 photos).
- Sighting history: track multiple locations per cat with automatic observation logging.
- Health tracking: 1–5 scale condition system with automatic "Need attention" flagging.
- Support for SQLite and PostgreSQL via universal DSN.
- Prometheus metrics and health endpoints (Liveness/Readiness).
- JWT authorization (secret generated automatically) and OAuth2 (Google, OIDC).
- Session and refresh token storage in Redis (with in-memory fallback).
- Audit system for all data changes.
- Public access to the cat list with sensitive information filtering.

## Architecture
- `internal/storage` — Data models (GORM), database operations (SQLite/Postgres), migrations.
- `internal/backend` — HTTP server (chi), API handlers, middleware.
- `internal/logging` — Logging (logrus).
- `internal/monitoring` — Metrics (Prometheus).
- `internal/bot` — Telegram bot logic (tgbotapi), HTTP client for API.
- `cmd/catwatch` — Main application entry point.
- `cmd/catwatch_bot` — Telegram bot entry point.

## Requirements
- Go 1.24+
- Docker (optional)

## Quick Start
1. Build binaries:
   ```bash
   make build
   ```
   Results in `./bin/catwatch` and `./bin/catwatch_bot`.
2. Run:
   ```bash
   make run
   ```
   A `catwatch.db` file is created by default.
3. Run in debug mode:
   ```bash
   make dev
   ```

## Telegram Bot
The bot allows viewing the list of cats, adding new cats, editing their data, and quickly adding feeding or observation records. The bot also automatically sends reminders 30 minutes before planned procedures to all registered users (those who pressed `/start`). The bot interacts with the application via API.

Main bot commands:
- `/start` — registration and receiving an authorization link.
- `/stop` — logging out and unlinking account.
- `/cats` — list of cats.
- `/add_cat` — add a new cat.
- `/help` — detailed user guide.
- `/cancel` — cancel current action.

*Features:*
- **Seen**: Quickly mark a cat as seen, optionally sharing coordinates.
- **Feed**: Log a feeding event.
- **Observe**: Detailed observation (condition rating, new photos, location).
- **Schedule**: View last 2 past events and next 3 upcoming events for a cat.
- **Upcoming**: Global weekly schedule for all cats.
- **Photos**: Manage cat gallery (upload albums up to 5 photos).

For full bot functionality (adding and editing), you must click the link in the welcome message and authorize via Google. The bot will automatically gain access to the API on your behalf.

Starting the bot:
```bash
./bin/catwatch_bot --tg-token <YOUR_TOKEN> --api-url http://localhost:8080 --bot-api-key <SECRET_KEY>
```

The bot interface automatically adapts to your Telegram language (supports English and Russian).

Bot configuration:
- `--tg-token` (ENV: `TG_TOKEN`) — Telegram bot token.
- `--api-url` (ENV: `API_URL`, default: `http://localhost:8080`) — Internal CatWatch API URL.
- `--public-api-url` (ENV: `PUBLIC_API_URL`) — Public CatWatch API URL for authorization links (useful for Docker).
- `--bot-api-key` (ENV: `BOT_API_KEY`) — Secret key for bot authentication in API.
- `--debug` (ENV: `DEBUG`) — Enable debug mode.

## Configuration
Parameters are set via flags or environment variables:
- `--listen` (ENV: `LISTEN`, default: `:8080`) — Server address.
- `--db` (ENV: `DB_PATH`, default: `catwatch.db`) — Database DSN.
  - SQLite: `catwatch.db` or `file::memory:?cache=shared`.
  - Postgres: `postgres://user:pass@host:5432/dbname?sslmode=disable`.
- `--debug` (ENV: `DEBUG`) — Enable extended logging.
- `--cors-origin` (ENV: `CORS_ORIGIN`) — Allowed CORS origins.
- `--bot-api-key` (ENV: `BOT_API_KEY`) — Secret key for the bot.

### Authentication (OAuth2 / OIDC)
- `--devel` (ENV: `DEV_LOGIN=1`) — enables dev login for testing (`POST /api/auth/dev-login`).
- `--google-client-id`, `--google-client-secret`, `--google-redirect-url` — Settings for Google OAuth2.
- `--oidc-issuer`, `--oidc-client-id`, `--oidc-client-secret`, `--oidc-redirect-url` — Settings for a custom OIDC provider.
- `--session-redis`, `--session-redis-pass`, `--session-redis-prefix` — Redis settings for session storage. If not specified, in-memory storage is used (development only).
- `--auth-access-ttl` — Access token (JWT) TTL, default 15 minutes.
- `--auth-refresh-ttl` — Refresh token TTL, default 30 days.

## API Endpoints
### Authentication
- `GET /api/config` — Public configuration (available login methods).
- `POST /api/auth/dev-login` — Dev login (only if `--devel` is enabled).
- `GET /auth/google/login` — Login via Google.
- `GET /auth/oidc/login` — Login via OIDC.
- `POST /api/auth/refresh` — Token refresh.
- `POST /api/auth/logout` — Logout.
- `GET /api/user` — Information about the current user (requires JWT).

### Cats
- `GET /api/cats/` — List of all cats (public, limited data).
- `POST /api/cats/` — Add a new cat (requires JWT).
- `GET /api/cats/{id}/` — Cat details (public, limited data).
- `PUT /api/cats/{id}/` — Update cat data (requires JWT).
- `DELETE /api/cats/{id}/` — Delete cat (requires JWT).

### Service Journal and Planning
- `GET /api/cats/{id}/records` — History (public, done only) and planned procedures (requires JWT).
  - Parameters: `status=planned` or `status=done`.
  - Calendar: `start=RFC3339&end=RFC3339` for expanding recurring events.
- `POST /api/cats/{id}/records` — Add a record (event or plan).
- `POST /api/cats/{id}/records/{rid}/done` — Mark procedure as done.

### Bot and Reminders
- `GET /api/records/planned` — All planned records (supports `start` and `end`).
- `GET /api/bot/users` — List of registered bot users.
- `POST /api/bot/register` — Bot user registration.
- `POST /api/bot/notifications` — Confirming notification delivery.

### Utilities
- `GET /healthz/alive` — Liveness check (Backend & Bot).
- `GET /healthz/ready` — Readiness check (Backend: DB connection; Bot: Telegram connection).
- `GET /metrics` — Prometheus metrics (Backend).

## Docker
### Build image
```bash
docker build -t catwatch .
```

### Run single container
```bash
docker run -p 8080:8080 -v $(pwd)/data:/data catwatch --db /data/catwatch.db
```

### Run with Docker Compose
1. Copy `.env.example` to `.env` and fill in your secrets.
2. Run `docker-compose up -d`.

## Kubernetes
Manifests are available in the `k8s/` directory. Deployment uses PostgreSQL by default.

1. Configure your secrets in `k8s/secrets.yaml` (including `db-password`).
2. Apply manifests:
   ```bash
   kubectl apply -f k8s/secrets.yaml
   kubectl apply -f k8s/redis.yaml
   kubectl apply -f k8s/postgres.yaml
   kubectl apply -f k8s/backend.yaml
   kubectl apply -f k8s/bot.yaml
   ```

## License
MIT
