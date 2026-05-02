package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maniack/catwatch/internal/storage"
)

// ---- helpers ----------------------------------------------------------------

func createTestCat(t *testing.T, s *Server, token string) storage.Cat {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "Test Cat"})
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestCat: %d %s", w.Code, w.Body.String())
	}
	var cat storage.Cat
	json.Unmarshal(w.Body.Bytes(), &cat)
	return cat
}

func createTestRecord(t *testing.T, s *Server, token, catID string) storage.Record {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"type": "feeding", "note": "initial note"})
	req := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestRecord: %d %s", w.Code, w.Body.String())
	}
	var rec storage.Record
	json.Unmarshal(w.Body.Bytes(), &rec)
	return rec
}

// ---- tests ------------------------------------------------------------------

func TestToggleLike(t *testing.T) {
	s := newTestServer(t)
	uid := "like-user"
	token := issueTestToken(t, s, uid)
	cat := createTestCat(t, s, token)

	like := func() (int64, bool, int) {
		req := httptest.NewRequest(http.MethodPost, "/api/cats/"+cat.ID+"/like", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Router.ServeHTTP(w, req)
		var out struct {
			Likes int64 `json:"likes"`
			Liked bool  `json:"liked"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		return out.Likes, out.Liked, w.Code
	}

	// First toggle: like
	likes, liked, code := like()
	if code != http.StatusOK {
		t.Fatalf("toggle like code = %d", code)
	}
	if !liked || likes != 1 {
		t.Fatalf("expected liked=true, likes=1, got liked=%v, likes=%d", liked, likes)
	}

	// Second toggle: unlike
	likes, liked, code = like()
	if code != http.StatusOK {
		t.Fatalf("toggle unlike code = %d", code)
	}
	if liked || likes != 0 {
		t.Fatalf("expected liked=false, likes=0, got liked=%v, likes=%d", liked, likes)
	}
}

func TestToggleLikeUnauthenticated(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "owner")
	cat := createTestCat(t, s, token)

	req := httptest.NewRequest(http.MethodPost, "/api/cats/"+cat.ID+"/like", nil)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestGetCatImageBinary(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "img-user")
	cat := createTestCat(t, s, token)

	// Create image with binary data directly in DB
	imgData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	img := storage.Image{
		ID:    storage.NewUUID(),
		CatID: cat.ID,
		MIME:  "image/jpeg",
		Data:  imgData,
	}
	if err := s.store.DB.Create(&img).Error; err != nil {
		t.Fatalf("create image: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cats/"+cat.ID+"/images/"+img.ID, nil)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %s", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), imgData) {
		t.Errorf("image data mismatch")
	}
}

func TestGetCatImageBinaryNotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cats/nonexistent/images/no-image", nil)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetCatImageBinaryNoData(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "img-user2")
	cat := createTestCat(t, s, token)

	// Image with URL only, no binary data
	img := storage.Image{
		ID:    storage.NewUUID(),
		CatID: cat.ID,
		URL:   "https://example.com/cat.jpg",
	}
	s.store.DB.Create(&img)

	req := httptest.NewRequest(http.MethodGet, "/api/cats/"+cat.ID+"/images/"+img.ID, nil)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for URL-only image, got %d", w.Code)
	}
}

func TestUpdateRecord(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "rec-user")
	cat := createTestCat(t, s, token)
	rec := createTestRecord(t, s, token, cat.ID)

	update := map[string]any{"note": "updated note", "type": "medical"}
	body, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPut, "/api/cats/"+cat.ID+"/records/"+rec.ID+"/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update record code = %d: %s", w.Code, w.Body.String())
	}
	var out storage.Record
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Note != "updated note" {
		t.Errorf("expected note 'updated note', got %q", out.Note)
	}
	if out.Type != "medical" {
		t.Errorf("expected type 'medical', got %q", out.Type)
	}
}

func TestUpdateRecordUnauthorized(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "owner")
	cat := createTestCat(t, s, token)
	rec := createTestRecord(t, s, token, cat.ID)

	body, _ := json.Marshal(map[string]any{"note": "hack"})
	req := httptest.NewRequest(http.MethodPut, "/api/cats/"+cat.ID+"/records/"+rec.ID+"/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	s := newTestServer(t)

	// Create a session first
	uid := "logout-user"
	refreshToken := s.generateOpaqueToken()
	payload, _ := json.Marshal(map[string]any{"uid": uid})
	s.sessions.Set("sess:"+refreshToken, payload, 1*time.Hour)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	req.Header.Set("X-CSRF-Token", "test") // CSRF middleware: header presence required for cookie-based requests
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("logout code = %d", w.Code)
	}

	// Session must be deleted
	val, _ := s.sessions.Get("sess:" + refreshToken)
	if val != nil {
		t.Error("expected session to be deleted after logout")
	}

	// Cookies must be cleared
	var accessCleared, refreshCleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "access_token" && c.MaxAge < 0 {
			accessCleared = true
		}
		if c.Name == "refresh_token" && c.MaxAge < 0 {
			refreshCleared = true
		}
	}
	if !accessCleared {
		t.Error("expected access_token cookie to be cleared")
	}
	if !refreshCleared {
		t.Error("expected refresh_token cookie to be cleared")
	}
}

func TestGetUser(t *testing.T) {
	s := newTestServer(t)
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{
		ID:         uid,
		Provider:   "test",
		ProviderID: uid,
		Name:       "Test User",
		Email:      "test@example.com",
	})
	token := issueTestToken(t, s, uid)

	req := httptest.NewRequest(http.MethodGet, "/api/user/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get user code = %d: %s", w.Code, w.Body.String())
	}
	var u storage.User
	json.Unmarshal(w.Body.Bytes(), &u)
	if u.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %q", u.Name)
	}
	if u.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %q", u.Email)
	}
}

func TestUpdateUser(t *testing.T) {
	s := newTestServer(t)
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{
		ID:         uid,
		Provider:   "test",
		ProviderID: uid,
		Name:       "Old Name",
		Email:      "old@example.com",
	})
	token := issueTestToken(t, s, uid)

	body, _ := json.Marshal(map[string]string{"name": "New Name", "email": "new@example.com"})
	req := httptest.NewRequest(http.MethodPatch, "/api/user/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update user code = %d: %s", w.Code, w.Body.String())
	}
	var u storage.User
	json.Unmarshal(w.Body.Bytes(), &u)
	if u.Name != "New Name" {
		t.Errorf("expected name 'New Name', got %q", u.Name)
	}
	if u.Email != "new@example.com" {
		t.Errorf("expected email 'new@example.com', got %q", u.Email)
	}
}

func TestDeleteUser(t *testing.T) {
	s := newTestServer(t)
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{
		ID:         uid,
		Provider:   "test",
		ProviderID: uid,
		Name:       "Deletable User",
	})
	token := issueTestToken(t, s, uid)

	req := httptest.NewRequest(http.MethodDelete, "/api/user/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete user code = %d: %s", w.Code, w.Body.String())
	}

	// User must be gone
	var u storage.User
	err := s.store.DB.First(&u, "id = ?", uid).Error
	if err == nil {
		t.Error("expected user to be deleted from DB")
	}
}

func TestGetUserLikes(t *testing.T) {
	s := newTestServer(t)
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{ID: uid, Provider: "test", ProviderID: uid})
	token := issueTestToken(t, s, uid)

	// Create a cat and like it
	cat := createTestCat(t, s, token)
	s.store.DB.Create(&storage.Like{ID: storage.NewUUID(), CatID: cat.ID, UserID: uid})

	req := httptest.NewRequest(http.MethodGet, "/api/user/likes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get likes code = %d: %s", w.Code, w.Body.String())
	}
	var cats []PublicCat
	json.Unmarshal(w.Body.Bytes(), &cats)
	if len(cats) != 1 {
		t.Fatalf("expected 1 liked cat, got %d", len(cats))
	}
	if cats[0].ID != cat.ID {
		t.Errorf("expected cat id %s, got %s", cat.ID, cats[0].ID)
	}
	if !cats[0].Liked {
		t.Error("expected Liked=true")
	}
}

func TestExportUser(t *testing.T) {
	s := newTestServer(t)
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{ID: uid, Provider: "test", ProviderID: uid, Name: "Export Me"})
	token := issueTestToken(t, s, uid)

	req := httptest.NewRequest(http.MethodGet, "/api/user/export", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("export user code = %d: %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd == "" {
		t.Error("expected Content-Disposition header")
	}
	var export map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &export); err != nil {
		t.Fatalf("invalid export JSON: %v", err)
	}
}

func TestAddCatImageURLValidation(t *testing.T) {
	s := newTestServer(t)
	token := issueTestToken(t, s, "img-url-user")
	cat := createTestCat(t, s, token)

	cases := []struct {
		url      string
		wantCode int
	}{
		{"https://example.com/cat.jpg", http.StatusCreated},
		{"http://example.com/cat.jpg", http.StatusCreated},
		{"javascript:alert(1)", http.StatusBadRequest},
		{"ftp://example.com/cat.jpg", http.StatusBadRequest},
		{"data:image/png;base64,abc", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	}

	for _, tc := range cases {
		body, _ := json.Marshal(map[string]string{"url": tc.url})
		req := httptest.NewRequest(http.MethodPost, "/api/cats/"+cat.ID+"/images/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Router.ServeHTTP(w, req)
		if w.Code != tc.wantCode {
			t.Errorf("url=%q: expected %d, got %d (%s)", tc.url, tc.wantCode, w.Code, w.Body.String())
		}
	}
}

func TestBotKeyAuthMiddleware(t *testing.T) {
	s := newTestServer(t)
	// No BotAPIKey set — all bot endpoints must be closed
	req := httptest.NewRequest(http.MethodGet, "/api/bot/users", nil)
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when BotAPIKey not configured, got %d", w.Code)
	}

	// Set key — correct key must be accepted
	s.cfg.BotAPIKey = "mykey"
	req2 := httptest.NewRequest(http.MethodGet, "/api/bot/users", nil)
	req2.Header.Set("X-Bot-Key", "mykey")
	w2 := httptest.NewRecorder()
	s.Router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct key, got %d: %s", w2.Code, w2.Body.String())
	}

	// Wrong key must be rejected
	req3 := httptest.NewRequest(http.MethodGet, "/api/bot/users", nil)
	req3.Header.Set("X-Bot-Key", "wrongkey")
	w3 := httptest.NewRecorder()
	s.Router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong key, got %d", w3.Code)
	}
}

func TestOAuthTokenRefreshGrant(t *testing.T) {
	s := newTestServer(t)

	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{ID: uid, Provider: "test", ProviderID: uid})

	refreshToken := s.generateOpaqueToken()
	payload, _ := json.Marshal(map[string]any{"uid": uid, "iat": time.Now().Unix()})
	s.sessions.Set("sess:"+refreshToken, payload, 1*time.Hour)

	// POST /api/auth/token with grant_type=refresh_token (form-encoded, OAuth2 client flow)
	form := "grant_type=refresh_token&refresh_token=" + refreshToken
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("token refresh code = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == "" {
		t.Error("expected access_token in response")
	}
	newRefresh, _ := resp["refresh_token"].(string)
	if newRefresh == "" {
		t.Error("expected refresh_token in response")
	}
	if newRefresh == refreshToken {
		t.Error("expected rotated refresh_token, got same value")
	}

	// Old token must be invalidated
	val, _ := s.sessions.Get("sess:" + refreshToken)
	if val != nil {
		t.Error("expected old refresh token to be deleted after rotation")
	}

	// New token must be usable
	form2 := "grant_type=refresh_token&refresh_token=" + newRefresh
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewBufferString(form2))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	s.Router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second refresh code = %d: %s", w2.Code, w2.Body.String())
	}
}

func TestOAuthTokenRefreshGrantInvalidToken(t *testing.T) {
	s := newTestServer(t)

	form := "grant_type=refresh_token&refresh_token=nosuchtoken"
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid refresh token, got %d", w.Code)
	}
}

func TestOAuthTokenRefreshGrantMissingToken(t *testing.T) {
	s := newTestServer(t)

	form := "grant_type=refresh_token"
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", bytes.NewBufferString(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing refresh_token, got %d", w.Code)
	}
}
