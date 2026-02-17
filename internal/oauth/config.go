package oauth

// Config holds OAuth/OIDC provider configuration.
type Config struct {
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	AuthSuccessRedirect string
}
