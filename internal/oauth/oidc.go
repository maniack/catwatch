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
	// RFC 6749 Authorization Parameters
	OAuthState       string `json:"oauth_state,omitempty"`
	OAuthRedirectURI string `json:"oauth_redirect_uri,omitempty"`
	// RFC 7636 PKCE
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
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
	if p.issuer == "" || p.clientID == "" || p.clientSecret == "" {
		return nil, nil, fmt.Errorf("OIDC not configured")
	}

	provider, err := oidc.NewProvider(ctx, p.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get provider: %v", err)
	}

	redirectURL := p.redirectURL
	if redirectURL == "" {
		redirectURL = "/auth/" + p.name + "/callback"
	}
	absRedirectURL := GetAbsoluteURL(r, redirectURL)

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

	// Capture OAuth context if present
	q := r.URL.Query()
	meta.OAuthState = q.Get("state")
	meta.OAuthRedirectURI = q.Get("redirect_uri")
	meta.CodeChallenge = q.Get("code_challenge")
	meta.CodeChallengeMethod = q.Get("code_challenge_method")
	if meta.OAuthRedirectURI != "" {
		h.logger.Infof("OAuth login started for %s, with client_id: %s and redirect_uri: %s", p.name, q.Get("client_id"), meta.OAuthRedirectURI)
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
	v, err := h.sessions.Get(sessionKey)
	if err == nil && v != nil {
		_ = json.Unmarshal(v, &meta)
		_ = h.sessions.Del(sessionKey)
	} else {
		h.logger.Warnf("OAuth session lost for %s (state=%s), err=%v", p.name, stateQ[:min(8, len(stateQ))], err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "session_expired",
			"error_description": "OAuth session expired, please try the login link again",
		})
		return
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
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		fmt.Fprintf(w, "<h2>Authorization successful!</h2><p>You can now return to the Telegram bot.</p>")
	} else if meta.OAuthRedirectURI != "" {
		// RFC 6749 §4.1.2. Authorization Response
		// We issue a code for the OAuth client
		code := randomState() // Reuse same helper for random string
		sessData := map[string]any{
			"uid":                   u.ID,
			"iat":                   time.Now().Unix(),
			"code_challenge":        meta.CodeChallenge,
			"code_challenge_method": meta.CodeChallengeMethod,
		}
		sessBytes, _ := json.Marshal(sessData)
		_ = h.sessions.Set("auth_code:"+code, sessBytes, 5*time.Minute)

		target, err := url.Parse(meta.OAuthRedirectURI)
		if err != nil {
			h.logger.Errorf("Failed to parse redirect_uri '%s': %v", meta.OAuthRedirectURI, err)
			http.Redirect(w, r, h.appRedirect, http.StatusFound)
			return
		}
		q := target.Query()
		q.Set("code", code)
		if meta.OAuthState != "" {
			q.Set("state", meta.OAuthState)
		}
		target.RawQuery = q.Encode()
		h.logger.Infof("OAuth authorization successful for %s, redirecting to: %s", p.name, target.String())
		http.Redirect(w, r, target.String(), http.StatusFound)
	} else {
		// For SPA, redirect to home
		h.logger.Infof("OAuth login successful for %s, no redirect_uri, going to home", p.name)
		http.Redirect(w, r, h.appRedirect, http.StatusFound)
	}
}
