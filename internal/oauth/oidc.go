package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type providerParams struct {
	name         string
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
}

type stateMeta struct {
	ChatID int64  `json:"chat_id,omitempty"`
	Nonce  string `json:"nonce,omitempty"`
}

func (h *Handler) oidcLogin(w http.ResponseWriter, r *http.Request) {
	h.doOIDCLogin(w, r, providerParams{
		name:         "oidc",
		issuer:       h.cfg.OIDCIssuer,
		clientID:     h.cfg.OIDCClientID,
		clientSecret: h.cfg.OIDCClientSecret,
		redirectURL:  h.cfg.OIDCRedirectURL,
	})
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	h.doOIDCCallback(w, r, providerParams{
		name:         "oidc",
		issuer:       h.cfg.OIDCIssuer,
		clientID:     h.cfg.OIDCClientID,
		clientSecret: h.cfg.OIDCClientSecret,
		redirectURL:  h.cfg.OIDCRedirectURL,
	})
}

func (h *Handler) oidcConfig(ctx context.Context, r *http.Request, p providerParams) (*oidc.Provider, *oauth2.Config, error) {
	if p.issuer == "" || p.clientID == "" || p.clientSecret == "" || p.redirectURL == "" {
		return nil, nil, fmt.Errorf("OIDC not configured")
	}

	provider, err := oidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get provider: %v", err)
	}

	absRedirectURL := GetAbsoluteURL(r, p.redirectURL)

	conf := &oauth2.Config{
		ClientID:     p.clientID,
		ClientSecret: p.clientSecret,
		RedirectURL:  absRedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return provider, conf, nil
}

func (h *Handler) doOIDCLogin(w http.ResponseWriter, r *http.Request, p providerParams) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	_, conf, err := h.oidcConfig(ctx, r, p)
	if err != nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": fmt.Sprintf("%s not configured", p.name)})
		return
	}

	state := randomState()
	sessionKey := "state:" + p.name + ":" + state

	// Capture Telegram metadata if present
	meta := stateMeta{}
	if tgID := r.URL.Query().Get("tg_chat_id"); tgID != "" {
		fmt.Sscanf(tgID, "%d", &meta.ChatID)
		meta.Nonce = r.URL.Query().Get("tg_nonce")
	}

	metaBytes, _ := json.Marshal(meta)
	_ = h.sessions.Set(sessionKey, metaBytes, 15*time.Minute)

	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_" + p.name + "_state",
		Value:    url.QueryEscape(state),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   900,
	})

	authURL := conf.AuthCodeURL(state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) doOIDCCallback(w http.ResponseWriter, r *http.Request, p providerParams) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	provider, conf, err := h.oidcConfig(ctx, r, p)
	if err != nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": fmt.Sprintf("%s not configured", p.name)})
		return
	}

	stateQ := r.URL.Query().Get("state")
	if stateQ == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state"})
		return
	}

	var meta stateMeta
	sessionKey := "state:" + p.name + ":" + stateQ
	if v, _ := h.sessions.Get(sessionKey); v != nil {
		_ = json.Unmarshal(v, &meta)
		_ = h.sessions.Del(sessionKey)
	} else {
		// Fallback to cookie for non-telegram logins if session is lost (best effort)
		if c, err := r.Cookie("oauth_" + p.name + "_state"); err == nil {
			if cv, _ := url.QueryUnescape(c.Value); cv != stateQ {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state (cookie)"})
				return
			}
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid state (no session)"})
			return
		}
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:    "oauth_" + p.name + "_state",
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing code"})
		return
	}

	oauth2Token, err := conf.Exchange(ctx, code)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exchange failed"})
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no id_token"})
		return
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: p.clientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		writeJSON(http.ResponseWriter(w), http.StatusUnauthorized, map[string]string{"error": "verification failed"})
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	_ = idToken.Claims(&claims)

	u, err := h.store.FindOrCreateUser(p.name, claims.Subject, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "user error"})
		return
	}

	// Link telegram if meta present
	if meta.ChatID != 0 {
		_ = h.store.LinkBotChat(meta.ChatID, u.ID)
	}

	// Issue tokens and handle response (redirect or show success)
	if err := h.issueTokens(w, r, u.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}

	if meta.ChatID != 0 {
		// For telegram, we can show a success message
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<h2>Authorization successful!</h2><p>You can now return to the Telegram bot.</p>")
	} else {
		// For SPA, redirect to home
		http.Redirect(w, r, "/", http.StatusFound)
	}
}
