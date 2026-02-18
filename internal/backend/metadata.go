package backend

import (
	"fmt"
	"net/http"
	"strings"

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
		TokenEndpoint:          baseURL + "/api/auth/refresh",
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
