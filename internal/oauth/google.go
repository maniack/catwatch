package oauth

import (
	"net/http"
)

const googleIssuer = "https://accounts.google.com"

func (h *Handler) googleLogin(w http.ResponseWriter, r *http.Request) {
	h.doOIDCLogin(w, r, providerParams{
		name:         "google",
		issuer:       googleIssuer,
		clientID:     h.cfg.GoogleClientID,
		clientSecret: h.cfg.GoogleClientSecret,
		redirectURL:  h.cfg.GoogleRedirectURL,
	})
}

func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) {
	h.doOIDCCallback(w, r, providerParams{
		name:         "google",
		issuer:       googleIssuer,
		clientID:     h.cfg.GoogleClientID,
		clientSecret: h.cfg.GoogleClientSecret,
		redirectURL:  h.cfg.GoogleRedirectURL,
	})
}
