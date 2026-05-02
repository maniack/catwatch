package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"crypto/rand"
	"crypto/rsa"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/sessions"
	"github.com/maniack/catwatch/internal/storage"
)

func init() { logging.Init(false, false) }

// ---- fake OIDC provider -----------------------------------------------------

type fakeOIDC struct {
	*httptest.Server
	priv     *rsa.PrivateKey
	kid      string
	clientID string
	// ExtraClaims are merged into the ID token
	ExtraClaims map[string]any
}

func newFakeOIDC(t *testing.T, clientID string) *fakeOIDC {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	f := &fakeOIDC{
		priv:     priv,
		kid:      "k1",
		clientID: clientID,
		ExtraClaims: map[string]any{
			"sub":   "sub-001",
			"email": "user@fakeoidc.test",
			"name":  "Fake User",
		},
	}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	f.Server = srv
	t.Cleanup(srv.Close)

	issuer := srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := &priv.PublicKey
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": f.kid,
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
				},
			},
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idToken := f.makeIDToken(issuer)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
			"expires_in":   3600,
		})
	})

	return f
}

func (f *fakeOIDC) makeIDToken(issuer string) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": jwt.ClaimStrings{f.clientID},
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	for k, v := range f.ExtraClaims {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = f.kid
	signed, err := tok.SignedString(f.priv)
	if err != nil {
		panic(fmt.Sprintf("sign: %v", err))
	}
	return signed
}

// ---- test helpers -----------------------------------------------------------

func newTestOAuthHandler(t *testing.T, cfg Config) (*Handler, *storage.Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	store, err := storage.Open(dsn)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	sess := sessions.NewMemorySessionStore()
	issued := ""
	h := NewHandler(store, logging.L(), cfg, sess, func(w http.ResponseWriter, r *http.Request, uid string) error {
		issued = uid
		_ = issued
		return nil
	})
	return h, store
}

// storeState saves a stateMeta in the handler's session store, simulating what doOIDCLogin does.
func storeState(h *Handler, provider, stateVal string, meta stateMeta) {
	b, _ := json.Marshal(meta)
	_ = h.sessions.Set("state:"+provider+":"+stateVal, b, 15*time.Minute)
}

// ---- tests ------------------------------------------------------------------

func TestGetAbsoluteURL(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"https://already.absolute/path", "https://already.absolute/path"},
		{"http://already.absolute/path", "http://already.absolute/path"},
		{"/relative/path", "http://example.com/relative/path"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		got := GetAbsoluteURL(req, tc.path)
		if got != tc.want {
			t.Errorf("GetAbsoluteURL(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestGetAbsoluteURL_ForwardedProto(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	got := GetAbsoluteURL(req, "/api")
	if got != "https://example.com/api" {
		t.Errorf("expected https scheme from X-Forwarded-Proto, got %q", got)
	}
}

// TestOIDCLogin_NotConfigured: if OIDC is not set up, login returns 501.
func TestOIDCLogin_NotConfigured(t *testing.T) {
	h, _ := newTestOAuthHandler(t, Config{})
	req := httptest.NewRequest("GET", "/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	h.HandleOIDCLogin(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for unconfigured OIDC, got %d", w.Code)
	}
}

// TestGoogleLogin_NotConfigured mirrors the OIDC case for Google.
func TestGoogleLogin_NotConfigured(t *testing.T) {
	h, _ := newTestOAuthHandler(t, Config{})
	req := httptest.NewRequest("GET", "/auth/google/login", nil)
	w := httptest.NewRecorder()
	h.HandleGoogleLogin(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for unconfigured Google, got %d", w.Code)
	}
}

// TestOIDCLogin_RedirectsToProvider: login with a valid OIDC config must redirect to provider.
func TestOIDCLogin_RedirectsToProvider(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)

	cfg := Config{
		OIDCIssuer:       fake.URL,
		OIDCClientID:     clientID,
		OIDCClientSecret: "secret",
		OIDCRedirectURL:  "http://localhost/callback",
	}
	h, _ := newTestOAuthHandler(t, cfg)

	req := httptest.NewRequest("GET", "/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	h.HandleOIDCLogin(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, fake.URL+"/authorize") {
		t.Errorf("expected redirect to fake authorize endpoint, got %q", loc)
	}

	// State cookie must be set
	var stateCookie bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "oauth_oidc_state" {
			stateCookie = true
		}
	}
	if !stateCookie {
		t.Error("expected oauth_oidc_state cookie to be set")
	}
}

// TestOIDCCallback_MissingState: callback with no state param → 400.
func TestOIDCCallback_MissingState(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{OIDCIssuer: fake.URL, OIDCClientID: clientID, OIDCClientSecret: "secret"}
	h, _ := newTestOAuthHandler(t, cfg)

	req := httptest.NewRequest("GET", "/auth/oidc/callback", nil)
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing state, got %d", w.Code)
	}
}

// TestOIDCCallback_SessionExpired: state param present but not in sessions → 400.
func TestOIDCCallback_SessionExpired(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{OIDCIssuer: fake.URL, OIDCClientID: clientID, OIDCClientSecret: "secret"}
	h, _ := newTestOAuthHandler(t, cfg)

	req := httptest.NewRequest("GET", "/auth/oidc/callback?state=unknown-state", nil)
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for expired session, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "session_expired" {
		t.Errorf("expected error=session_expired, got %q", resp["error"])
	}
}

// TestOIDCCallback_MissingCode: state valid but no code → 400.
func TestOIDCCallback_MissingCode(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{OIDCIssuer: fake.URL, OIDCClientID: clientID, OIDCClientSecret: "secret"}
	h, _ := newTestOAuthHandler(t, cfg)

	storeState(h, "oidc", "mystate", stateMeta{})
	req := httptest.NewRequest("GET", "/auth/oidc/callback?state=mystate", nil)
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing code, got %d: %s", w.Code, w.Body.String())
	}
}

// TestOIDCCallback_HappyPath: full callback flow creates a user and redirects home.
func TestOIDCCallback_HappyPath(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{
		OIDCIssuer:          fake.URL,
		OIDCClientID:        clientID,
		OIDCClientSecret:    "secret",
		OIDCRedirectURL:     "http://localhost/callback",
		AuthSuccessRedirect: "/home",
	}
	h, store := newTestOAuthHandler(t, cfg)

	storeState(h, "oidc", "goodstate", stateMeta{})

	q := url.Values{"state": {"goodstate"}, "code": {"anycode"}}
	req := httptest.NewRequest("GET", "/auth/oidc/callback?"+q.Encode(), nil)
	req.Host = "localhost"
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/home" {
		t.Errorf("expected redirect to /home, got %q", loc)
	}

	// User must be created in DB
	var u storage.User
	if err := store.DB.Where("provider = ? AND provider_id = ?", "oidc", "sub-001").First(&u).Error; err != nil {
		t.Fatalf("user not found in DB: %v", err)
	}
	if u.Email != "user@fakeoidc.test" {
		t.Errorf("unexpected email: %q", u.Email)
	}
	if u.Name != "Fake User" {
		t.Errorf("unexpected name: %q", u.Name)
	}
}

// TestOIDCCallback_WithOAuthRedirectURI: when OAuth client redirect_uri is in meta,
// the callback issues an auth code and redirects to the client.
func TestOIDCCallback_WithOAuthRedirectURI(t *testing.T) {
	const clientID = "my-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{
		OIDCIssuer:       fake.URL,
		OIDCClientID:     clientID,
		OIDCClientSecret: "secret",
	}
	h, _ := newTestOAuthHandler(t, cfg)

	meta := stateMeta{
		OAuthState:       "client-state-xyz",
		OAuthRedirectURI: "https://myapp.example.com/callback",
	}
	storeState(h, "oidc", "st2", meta)

	q := url.Values{"state": {"st2"}, "code": {"code2"}}
	req := httptest.NewRequest("GET", "/auth/oidc/callback?"+q.Encode(), nil)
	req.Host = "localhost"
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if !strings.HasPrefix(loc, "https://myapp.example.com/callback") {
		t.Errorf("expected redirect to myapp, got %q", loc)
	}
	if parsed.Query().Get("state") != "client-state-xyz" {
		t.Errorf("expected state=client-state-xyz, got %q", parsed.Query().Get("state"))
	}
	if parsed.Query().Get("code") == "" {
		t.Error("expected auth code in redirect URL")
	}

	// Auth code must be stored in sessions
	code := parsed.Query().Get("code")
	val, _ := h.sessions.Get("auth_code:" + code)
	if val == nil {
		t.Error("expected auth_code session to be created")
	}
}

// TestOIDCCallback_WithTelegramChatID: when Telegram chat_id is in meta,
// the bot link is created.
func TestOIDCCallback_WithTelegramChatID(t *testing.T) {
	const clientID = "tg-client"
	fake := newFakeOIDC(t, clientID)
	cfg := Config{
		OIDCIssuer:          fake.URL,
		OIDCClientID:        clientID,
		OIDCClientSecret:    "secret",
		AuthSuccessRedirect: "/",
	}
	h, store := newTestOAuthHandler(t, cfg)

	const chatID = int64(77777)
	storeState(h, "oidc", "tgstate", stateMeta{ChatID: chatID})

	q := url.Values{"state": {"tgstate"}, "code": {"tgcode"}}
	req := httptest.NewRequest("GET", "/auth/oidc/callback?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	h.HandleOIDCCallback(w, req)

	// Telegram path returns 200 HTML, not a redirect
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for Telegram flow, got %d: %s", w.Code, w.Body.String())
	}

	// Bot link must exist
	link, err := store.GetBotLink(chatID)
	if err != nil {
		t.Fatalf("bot link not found: %v", err)
	}
	if link.UserID == "" {
		t.Error("expected non-empty UserID in bot link")
	}
}
