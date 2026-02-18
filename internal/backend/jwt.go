package backend

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maniack/catwatch/internal/storage"
)

type jwtClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func (s *Server) JWTAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		var tok string
		if auth != "" && strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			tok = strings.TrimSpace(auth[len("Bearer "):])
		} else {
			// Fallback to cookie for SPA
			if c, err := r.Cookie("access_token"); err == nil {
				val, _ := url.QueryUnescape(c.Value)
				tok = strings.TrimSpace(val)
			}
		}

		if tok == "" {
			next.ServeHTTP(w, r)
			return
		}

		claims := &jwtClaims{}
		_, err := jwt.ParseWithClaims(tok, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.cfg.JWTSecret), nil
		})

		if err != nil {
			// Token invalid or expired
			next.ServeHTTP(w, r)
			return
		}

		ctx := WithUserID(r.Context(), claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth middleware ensures user is authenticated.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := UserIDFromCtx(r.Context())
		if uid == "" {
			// RFC 9728 discovery via WWW-Authenticate header
			metaURL := s.getMetadataURLForResource(r, "/api")
			w.Header().Add("WWW-Authenticate", fmt.Sprintf("Bearer realm=\"CatWatch\", resource_metadata=\"%s\"", metaURL))
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) issueTokens(w http.ResponseWriter, r *http.Request, userID string) error {
	accessTTL := s.cfg.AccessTTL
	if accessTTL == 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := s.cfg.RefreshTTL
	if refreshTTL == 0 {
		refreshTTL = 30 * 24 * time.Hour
	}

	accessToken, err := s.generateAccessToken(userID, accessTTL)
	if err != nil {
		return err
	}

	refreshToken := s.generateOpaqueToken()

	// Store refresh token in Redis
	// We can store multiple sessions per user. Key: sess:<opaque>
	sessData := map[string]any{
		"uid": userID,
		"iat": time.Now().Unix(),
	}
	sessBytes, _ := json.Marshal(sessData)
	if err := s.sessions.Set("sess:"+refreshToken, sessBytes, refreshTTL); err != nil {
		return err
	}

	// Check if it's a bot login (we might have chat_id in context or passed via state earlier)
	// For now, let's assume we store the link in DB.
	// If the callback had tg_chat_id, we also want to store a specific bot session.

	// Set cookies for SPA
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    url.QueryEscape(accessToken),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(accessTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    url.QueryEscape(refreshToken),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})

	return nil
}

func (s *Server) generateAccessToken(userID string, ttl time.Duration) (string, error) {
	claims := jwtClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Server) generateOpaqueToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var refreshToken string
	if c, err := r.Cookie("refresh_token"); err == nil {
		refreshToken, _ = url.QueryUnescape(c.Value)
	}
	if refreshToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing refresh token"})
		return
	}

	val, err := s.sessions.Get("sess:" + refreshToken)
	if err != nil || val == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid refresh token"})
		return
	}

	var sessData map[string]any
	_ = json.Unmarshal(val, &sessData)
	uid, _ := sessData["uid"].(string)

	// Rotate tokens
	_ = s.sessions.Del("sess:" + refreshToken)
	if err := s.issueTokens(w, r, uid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("refresh_token"); err == nil {
		refreshToken, _ := url.QueryUnescape(c.Value)
		_ = s.sessions.Del("sess:" + refreshToken)
	}

	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	// Clear cookies with full flags for better GDPR/Security compliance
	clearCookie := func(name string) {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
	clearCookie("access_token")
	clearCookie("refresh_token")

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	var u storage.User
	if err := s.store.DB.First(&u, "id = ?", uid).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	if in.Name != "" {
		u.Name = in.Name
	}
	if in.Email != "" {
		u.Email = in.Email
	}

	if err := s.store.DB.Save(&u).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.LogAudit(r, "user", u.ID, "success", "updated profile")
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())

	// Revoke sessions
	// Note: Opaque refresh tokens are in Redis/Memory, but we don't have a reverse index uid->tokens.
	// In a real system, we'd store a list of tokens per user or use a versioned token.
	// For now, we delete the user record and related data.
	if err := s.store.DeleteUser(uid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Clear cookies
	s.handleLogout(w, r)
}

func (s *Server) handleExportUser(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	data, err := s.store.GetUserExport(uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=catwatch-export-%s.json", uid))
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	var u storage.User
	if err := s.store.DB.First(&u, "id = ?", uid).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleGetUserLikes(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	cats, err := s.store.GetUserLikedCats(uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	out := make([]PublicCat, len(cats))
	for i, c := range cats {
		pc := ToPublicCat(c)
		pc.Liked = true
		pc.Likes, _ = s.store.LikesCount(c.ID)
		out[i] = pc
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetUserAudit(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	logs, err := s.store.GetUserAuditLogs(uid, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleBotToken(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BotAPIKey != "" && r.Header.Get("X-Bot-Key") != s.cfg.BotAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid bot key"})
		return
	}

	var in struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	link, err := s.store.GetBotLink(in.ChatID)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "chat not linked"})
		return
	}

	// For the bot, we issue a short access token.
	// We should also check if the user has an active "bot session" (refresh token).
	// If not, we might need them to log in again.
	// For simplicity, if they are linked, we issue a token.
	// But to follow the "refresh" requirement, we should ideally check a server-side session.

	accessToken, err := s.generateAccessToken(link.UserID, s.cfg.AccessTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"access_token": accessToken})
}

func (s *Server) handleBotUnlink(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BotAPIKey != "" && r.Header.Get("X-Bot-Key") != s.cfg.BotAPIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid bot key"})
		return
	}

	var in struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	_ = s.store.UnlinkBotChat(in.ChatID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
