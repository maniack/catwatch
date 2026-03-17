package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// handleProtectedResourceMetadata serves RFC 9728 Protected Resource Metadata.
// It supports both the recommended well-known path (/.well-known/oauth-protected-resource{suffix})
// and a compatibility alias (/.well-known/protected-resource-metadata).
func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	baseURL := s.getBaseURL(r)

	// Derive resource ID from request path per RFC 9728 §3 discovery rules.
	// The metadata URL is formed as: https://host/.well-known/oauth-protected-resource{resource-path}
	// So here we reconstruct resource identifier as baseURL + {resource-path}.
	p := r.URL.Path
	suffix := ""
	if strings.HasPrefix(p, "/.well-known/oauth-protected-resource") {
		suffix = strings.TrimPrefix(p, "/.well-known/oauth-protected-resource")
	} else if strings.HasPrefix(p, "/.well-known/protected-resource-metadata") {
		// legacy alias without suffix
		suffix = ""
	}
	resourceID := baseURL + suffix

	metadata := oauthex.ProtectedResourceMetadata{
		Resource:               resourceID,
		AuthorizationServers:   []string{baseURL},
		BearerMethodsSupported: []string{"header", "cookie"},
		ResourceName:           "CatWatch API",
	}

	writeJSON(w, http.StatusOK, metadata)
}

// OAuthAuthorizationServerMetadata represents OAuth 2.0 Authorization Server Metadata (RFC 8414).
type OAuthAuthorizationServerMetadata struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint          string   `json:"token_endpoint,omitempty"`
	RegistrationEndpoint   string   `json:"registration_endpoint,omitempty"`
	UserinfoEndpoint       string   `json:"userinfo_endpoint,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported    []string `json:"grant_types_supported,omitempty"`

	// Custom CatWatch metadata to replace /api/config
	Config struct {
		GoogleEnabled bool `json:"google_enabled"`
		OIDCEnabled   bool `json:"oidc_enabled"`
		DevLogin      bool `json:"dev_login"`
	} `json:"x_catwatch_config"`
}

// handleAuthorizationServerMetadata serves RFC 8414 Authorization Server Metadata.
// It also serves as OpenID Connect Discovery (openid-configuration).
func (s *Server) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	baseURL := s.getBaseURL(r)

	metadata := OAuthAuthorizationServerMetadata{
		Issuer:                 baseURL,
		AuthorizationEndpoint:  baseURL + "/auth/login",
		TokenEndpoint:          baseURL + "/api/auth/token",
		RegistrationEndpoint:   baseURL + "/api/auth/register",
		UserinfoEndpoint:       baseURL + "/api/user",
		ScopesSupported:        []string{"openid", "profile", "email"},
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code", "refresh_token"},
	}

	metadata.Config.GoogleEnabled = s.GoogleEnabled()
	metadata.Config.OIDCEnabled = s.OIDCEnabled()
	metadata.Config.DevLogin = s.DevEnabled()

	writeJSON(w, http.StatusOK, metadata)
}

// handleRegisterClient handles RFC 7591 Dynamic Client Registration.
func (s *Server) handleRegisterClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// RFC 7591 expects specific error responses, but 400 is a good start.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	if len(req.RedirectURIs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
		return
	}

	client, err := s.store.CreateOAuthClient(req.ClientName, req.RedirectURIs)
	if err != nil {
		s.log.Errorf("Failed to register client: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}

	// RFC 7591 Response
	response := map[string]any{
		"client_id":                  client.ID,
		"client_secret":              client.Secret,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none", // Assuming public client for now
	}

	writeJSON(w, http.StatusCreated, response)
}

// handleTokenExchange handles RFC 6749 Token Endpoint.
func (s *Server) handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}

	grantType := r.Form.Get("grant_type")
	if grantType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "grant_type is required"})
		return
	}

	if grantType == "authorization_code" {
		code := r.Form.Get("code")
		if code == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "code is required"})
			return
		}

		val, err := s.sessions.Get("auth_code:" + code)
		if err != nil || val == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "invalid or expired code"})
			return
		}
		_ = s.sessions.Del("auth_code:" + code)

		var sessData map[string]any
		_ = json.Unmarshal(val, &sessData)
		uid, _ := sessData["uid"].(string)

		// Check client credentials if provided (RFC 6749 §3.2.1)
		// For MCP/Claude it might be a public client or it might provide client_id/client_secret.
		// If they provide it, we should validate.
		clientID := r.Form.Get("client_id")
		clientSecret := r.Form.Get("client_secret")
		if clientID != "" {
			client, err := s.store.GetOAuthClient(clientID)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
				return
			}
			if client.Secret != "" && clientSecret != "" && client.Secret != clientSecret {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
				return
			}
		}

		// Issue tokens
		accessTTL := s.cfg.AccessTTL
		if accessTTL == 0 {
			accessTTL = 15 * time.Minute
		}
		refreshTTL := s.cfg.RefreshTTL
		if refreshTTL == 0 {
			refreshTTL = 30 * 24 * time.Hour
		}

		accessToken, err := s.generateAccessToken(uid, accessTTL)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
		refreshToken := s.generateOpaqueToken()
		rsessData := map[string]any{"uid": uid, "iat": time.Now().Unix()}
		rsessBytes, _ := json.Marshal(rsessData)
		_ = s.sessions.Set("sess:"+refreshToken, rsessBytes, refreshTTL)

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  accessToken,
			"token_type":    "Bearer",
			"expires_in":    int(accessTTL.Seconds()),
			"refresh_token": refreshToken,
		})
		return
	}

	if grantType == "refresh_token" {
		// Use existing refresh logic
		s.handleRefresh(w, r)
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
}

func (s *Server) getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func (s *Server) getMetadataURLForResource(r *http.Request, resourcePath string) string {
	// Build metadata URL per RFC 9728: /.well-known/oauth-protected-resource{resourcePath}
	if !strings.HasPrefix(resourcePath, "/") {
		resourcePath = "/" + resourcePath
	}
	return s.getBaseURL(r) + "/.well-known/oauth-protected-resource" + resourcePath
}
