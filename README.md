# CatWatch — Monitoring and Care for Homeless Cats

An application for volunteers and organizations dedicated to helping homeless cats. It allows maintaining a cat registry, tracking their locations, health status, and planning necessary procedures (feeding, examinations, vaccinations, etc.).


## Features
- Cat registry with photos, descriptions, tags, and service history.
- Cat location tracking (locations).
- Procedure calendar: planning one-time and recurring events.
- **Web UI**: Modern React-based single-page application for managing the registry from a browser.
- **Likes and Favorites**: Users can "like" cats, mark favorites, and see a popularity counter.
- **Personal Account**: View your profile and activity log (audit trail).
- **Notifications about upcoming procedures** in the Telegram bot.
- **Multi-domain support**: Flexible CORS and security headers for serving the application on multiple domains.
- **Success Redirect**: Configurable redirect URL after successful OAuth authentication.
- Localization support: English (EN) and Russian (RU) based on user's language.
- Image management: automatic optimization (WebP, resizing), multi-upload support (up to 5 photos).
- Sighting history: track multiple locations per cat with automatic observation logging.
- Health tracking: 1–5 scale condition system with automatic "Need attention" flagging.
- Support for SQLite and PostgreSQL via universal DSN.
- Prometheus metrics and health endpoints (Liveness/Readiness).
- JWT authorization (secret generated automatically) and OAuth2 (Google, OIDC).
- Session and refresh token storage in Redis (with in-memory fallback).
- **Audit system for all data changes.**
- **GDPR Compliance**: Data portability (export), right to be forgotten (account deletion), data minimization (minimal logging and audit trail), and transparent Privacy Policy.
- Public access to the cat list with sensitive information filtering.

## Architecture
- `internal/storage` — Data models (GORM), database operations (SQLite/Postgres), migrations.
- `internal/backend` — HTTP server (chi), API handlers, middleware.
- `internal/logging` — Logging (logrus).
- `internal/monitoring` — Metrics (Prometheus).
- `internal/bot` — Telegram bot logic (tgbotapi), HTTP client for API.
- `internal/frontend` — React-based Web UI (embedded).
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
- `/delete_me` — full account and data deletion.
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

## Web UI
The application includes a Web UI accessible at the same address as the API (default `http://localhost:8080`). 
Features:
- View cat list and details.
- Add and edit cats (requires authorization).
- Manage photos and journal events (requires authorization).
- View sightings history and log new locations.
- Responsive design for mobile and desktop.

Build and run with Web UI:
1. Ensure `esbuild` is installed (`brew install esbuild` or `npm i -g esbuild`).
2. Run `make generate` to build the frontend bundle.
3. Run `make build` to compile the application with embedded frontend.

## Model Context Protocol (MCP)
CatWatch supports [Model Context Protocol](https://modelcontextprotocol.io/) to allow AI agents (like Claude Desktop) to interact with the cat registry.

### Tools
- `list_cats`: Get a list of all cats with basic info.
- `get_cat`: Get detailed information about a specific cat by ID.
- `search_cats`: Search for cats by name.
- `get_cat_records`: Get feeding and medical history for a cat.

### MCP over HTTP (SSE)
MCP is now integrated into the main HTTP server and is available at the endpoint:
```
GET/POST /api/mcp
```
Access is protected: JWT is required in the `Authorization: Bearer <JWT>` header (or `access_token` cookie).

The transport uses HTTP with Server-Sent Events (SSE). The client opens an SSE connection with a GET request to `/api/mcp`. The server returns a session endpoint (with `sessionid=...` query) in the first event, after which the client sends JSON-RPC messages via POST to that same URL with the `sessionid` parameter.

Basic SSE check example (in browser/via curl):
- Open the event stream: `curl -N -H "Authorization: Bearer <JWT>" http://localhost:8080/api/mcp`
- In response, you will receive an `endpoint:` event with the URL to which you should POST JSON-RPC messages for this session.

Tips:
- You can get a JWT through standard OAuth login (Google/OIDC), then extract the `access_token` from the cookie or use an API client that includes the token in the header.
- In dev mode (`--devel`), you can get a test token via `POST /api/auth/dev-login` or click the "Dev Login" button on the sign-in page.

Note: Some external clients (e.g., current versions of desktop apps) may only support stdio transport. In such cases, use an MCP client or integration with HTTP(SSE) support.

The bot interface automatically adapts to your Telegram language (supports English and Russian).

## Configuration
The application can be configured via command-line flags or environment variables. Environment variables take precedence over default values but flags take precedence over environment variables.

### Global Settings (Common for Backend and Bot)
| Parameter | ENV | Default | Description |
|-----------|-----|---------|-------------|
| `--listen` | `LISTEN` | `:8080` | HTTP listen address. |
| `--debug` | `DEBUG` | `false` | Enable debug logging. |
| `--log-format`| `LOG_FORMAT`| `text` | Log format: `text` or `json`. |
| `--healthz-endpoint`| `HEALTHZ_ENDPOINT`| `/healthz` | Path for health check endpoints. |

### Backend Settings (`catwatch`)
| Parameter | ENV | Default | Description |
|-----------|-----|---------|-------------|
| `--db` | `DB_PATH` | `catwatch.db` | Database DSN (SQLite or Postgres). |
| `--jwt-secret` | `JWT_SECRET` | *(auto)* | **[Required for Production]** JWT signing secret. |
| `--bot-api-key`| `BOT_API_KEY`| | Secret key for bot authentication. |
| `--cors-origin`| `CORS_ORIGIN`| `*` | Allowed CORS origins (comma-separated or multiple flags). |
| `--devel` | `DEV_LOGIN` | `false` | Enable dev login (`POST /api/auth/dev-login`). |
| `--audit-log-ttl`| `AUDIT_LOG_TTL`| `720h` (30d)| Retention period for audit logs. |
| `--metrics-endpoint`| `METRICS_ENDPOINT`| `/metrics` | Prometheus metrics endpoint. |

#### Authentication (OAuth2 / OIDC)
| Parameter | ENV | Default | Description |
|-----------|-----|---------|-------------|
| `--google-client-id` | `GOOGLE_CLIENT_ID` | | **[Required for Google Auth]** |
| `--google-client-secret`| `GOOGLE_CLIENT_SECRET`| | **[Required for Google Auth]** |
| `--google-redirect-url`| `GOOGLE_REDIRECT_URL`| | Redirect URL for Google OAuth2. |
| `--oidc-issuer` | `OIDC_ISSUER` | | OIDC Provider Issuer URL. |
| `--oidc-client-id` | `OIDC_CLIENT_ID` | | OIDC Client ID. |
| `--oidc-client-secret`| `OIDC_CLIENT_SECRET`| | OIDC Client Secret. |
| `--oidc-redirect-url`| `OIDC_REDIRECT_URL` | | Redirect URL for OIDC. |
| `--auth-success-redirect`| `AUTH_SUCCESS_REDIRECT`| `/` | Redirect URL after successful login. |
| `--auth-access-ttl` | `AUTH_ACCESS_TTL` | `15m` | Access token (JWT) TTL. |
| `--auth-refresh-ttl`| `AUTH_REFRESH_TTL` | `720h`| Refresh token TTL. |

#### Session Storage (Redis)
If not specified, an in-memory storage is used (suitable for development only).

| Parameter | ENV | Default | Description |
|-----------|-----|---------|-------------|
| `--session-redis` | `SESSION_REDIS` | | Redis address (`host:port`). |
| `--session-redis-pass`| `SESSION_REDIS_PASS`| | Redis password. |
| `--session-redis-prefix`| `SESSION_REDIS_PREFIX`| `catwatch:oauth:`| Key prefix in Redis. |

### Telegram Bot Settings (`catwatch_bot`)
| Parameter | ENV | Default | Description |
|-----------|-----|---------|-------------|
| `--tg-token` | `TG_TOKEN` | | **[Required]** Telegram bot token from @BotFather. |
| `--api-url` | `API_URL` | `http://localhost:8080`| Internal API URL (accessible from bot). |
| `--public-api-url`| `PUBLIC_API_URL` | | Public API URL for auth links. |
| `--bot-api-key` | `BOT_API_KEY` | | **[Required]** Must match the key on the backend. |

## API Endpoints
### Authentication
- `GET /.well-known/oauth-authorization-server` — OAuth 2.0 Authorization Server Metadata (RFC 8414).
- `GET /.well-known/openid-configuration` — OpenID Connect Discovery.
- `GET /auth/login` — Centralized login page (Login Picker).
- `POST /api/auth/dev-login` — Dev login (only if `--devel` is enabled).
- `GET /auth/google/login` — Login via Google.
- `GET /auth/oidc/login` — Login via OIDC.
- `POST /api/auth/refresh` — Token refresh.
- `POST /api/auth/logout` — Logout.
- `GET /api/user` — Information about the current user (requires JWT).
- `PATCH /api/user` — Update user profile (name, email).
- `DELETE /api/user` — Delete user account and associated personal data.
- `GET /api/user/export` — Export all personal data in JSON format.
- `GET /api/user/likes` — List of cats liked by the current user.
- `GET /api/user/audit` — Activity log for the current user.

### Cats
- `GET /api/cats/` — List of all cats (public, limited data).
- `POST /api/cats/` — Add a new cat (requires JWT).
- `GET /api/cats/{id}/` — Cat details (public, limited data).
- `POST /api/cats/{id}/like` — Toggle like for a cat (requires JWT).
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
- `GET /api/mcp` — MCP SSE endpoint (requires JWT).

## GDPR and Privacy
CatWatch is designed with privacy in mind:
- **Data Minimization**: We only collect what's necessary (name, email from OAuth). IP addresses and User Agents are not stored in the database.
- **Transparency**: A Privacy Policy is available at `#/privacy`.
- **Consent**: A cookie consent banner informs users about strictly necessary cookies used for authentication.
- **Data Portability**: Users can export all their data via the profile page or API.
- **Right to Erasure**: Users can delete their accounts, which removes personal profiles, bot links, and anonymizes activity records.
- **Retention**: Audit logs are automatically pruned after the configured TTL (default 30 days).

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
