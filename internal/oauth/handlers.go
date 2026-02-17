package oauth

import (
	"net/http"
	"time"

	"github.com/maniack/catwatch/internal/sessions"
	"github.com/maniack/catwatch/internal/storage"
	"github.com/sirupsen/logrus"
)

// IssueTokenFunc issues app JWT and refresh token for the given user ID.
type IssueTokenFunc func(w http.ResponseWriter, r *http.Request, userID string) error

// Handler bundles OAuth handlers for providers.
type Handler struct {
	store       *storage.Store
	logger      *logrus.Logger
	cfg         Config
	sessions    sessions.SessionStore
	issueTokens IssueTokenFunc
	tokenTTL    time.Duration
}

// NewHandler constructs Handler.
func NewHandler(store *storage.Store, logger *logrus.Logger, cfg Config, sessStore sessions.SessionStore, issue IssueTokenFunc) *Handler {
	if sessStore == nil {
		sessStore = sessions.NewMemorySessionStore()
	}
	return &Handler{
		store:       store,
		logger:      logger,
		cfg:         cfg,
		sessions:    sessStore,
		issueTokens: issue,
		tokenTTL:    24 * time.Hour,
	}
}

func (h *Handler) GoogleEnabled() bool {
	return h.cfg.GoogleClientID != "" && h.cfg.GoogleClientSecret != "" && h.cfg.GoogleRedirectURL != ""
}
func (h *Handler) OIDCEnabled() bool {
	return h.cfg.OIDCIssuer != "" && h.cfg.OIDCClientID != "" && h.cfg.OIDCClientSecret != "" && h.cfg.OIDCRedirectURL != ""
}

func (h *Handler) HandleGoogleLogin(w http.ResponseWriter, r *http.Request) { h.googleLogin(w, r) }
func (h *Handler) HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	h.googleCallback(w, r)
}
func (h *Handler) HandleOIDCLogin(w http.ResponseWriter, r *http.Request)    { h.oidcLogin(w, r) }
func (h *Handler) HandleOIDCCallback(w http.ResponseWriter, r *http.Request) { h.oidcCallback(w, r) }
