package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/sessions"
	"github.com/maniack/catwatch/internal/storage"
)

type catIn struct {
	Name      string        `json:"name"`
	BirthDate *time.Time    `json:"birth_date"`
	Tags      []storage.Tag `json:"tags"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	logging.Init(false, false)
	store, err := storage.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	s, err := NewServer(Config{
		Store:        store,
		Logger:       logging.L(),
		Monitoring:   MonitoringConfig{},
		JWTSecret:    "test-secret",
		SessionStore: sessions.NewMemorySessionStore(),
		SkipWorkers:  true,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s
}

func issueTestToken(t *testing.T, s *Server, uid string) string {
	tok, err := s.generateAccessToken(uid, 1*time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	// alive
	req := httptest.NewRequest(http.MethodGet, "/healthz/alive", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("alive code = %d", w.Code)
	}
	// ready
	req2 := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("ready code = %d", w2.Code)
	}
}

func TestCreateAndGetCat(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	in := catIn{Name: "Barsik"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create code = %d, body=%s", w.Code, w.Body.String())
	}
	// decode id
	var cat map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cat)
	id, _ := cat["id"].(string)
	if id == "" {
		t.Fatalf("no id in response: %s", w.Body.String())
	}
	// get
	req2 := httptest.NewRequest(http.MethodGet, "/api/cats/"+id+"/", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get code = %d, body=%s", w2.Code, w2.Body.String())
	}
}

func TestPlanAndCompleteProcedure(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// create cat
	in := catIn{Name: "Murzik"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create cat code = %d, body=%s", w.Code, w.Body.String())
	}
	var cat map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cat)
	catID, _ := cat["id"].(string)
	if catID == "" {
		t.Fatalf("no id in response: %s", w.Body.String())
	}

	// plan vet visit in 1 hour
	planned := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	recIn := map[string]any{
		"type":       "vet_visit",
		"note":       "Initial inspection",
		"planned_at": planned,
	}
	recBody, _ := json.Marshal(recIn)
	req2 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(recBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("create record code = %d, body=%s", w2.Code, w2.Body.String())
	}
	var rec map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &rec)
	recID, _ := rec["id"].(string)
	if recID == "" {
		t.Fatalf("no record id in response: %s", w2.Body.String())
	}

	// list planned
	req3 := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/records?status=planned", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("list planned code = %d, body=%s", w3.Code, w3.Body.String())
	}
	var plannedList []map[string]any
	_ = json.Unmarshal(w3.Body.Bytes(), &plannedList)
	if len(plannedList) == 0 {
		t.Fatalf("expected planned records, got 0: %s", w3.Body.String())
	}

	// mark done
	req4 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records/"+recID+"/done", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("mark done code = %d, body=%s", w4.Code, w4.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(w4.Body.Bytes(), &updated)
	if updated["done_at"] == nil {
		t.Fatalf("expected done_at to be set: %s", w4.Body.String())
	}

	// list done
	req5 := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/records?status=done", nil)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("list done code = %d, body=%s", w5.Code, w5.Body.String())
	}
	var doneList []map[string]any
	_ = json.Unmarshal(w5.Body.Bytes(), &doneList)
	if len(doneList) == 0 {
		t.Fatalf("expected done records, got 0: %s", w5.Body.String())
	}

	// test quick record creation (like Feed button in Web UI) - now with DoneAt
	now := time.Now()
	quickIn := map[string]any{
		"type":    "feeding",
		"note":    "Quick feed with done_at",
		"done_at": now.UTC().Format(time.RFC3339),
	}
	quickBody, _ := json.Marshal(quickIn)
	qReq := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(quickBody))
	qReq.Header.Set("Authorization", "Bearer "+token)
	qW := httptest.NewRecorder()
	r.ServeHTTP(qW, qReq)
	if qW.Code != http.StatusCreated {
		t.Fatalf("expected 201 for quick record, got %d", qW.Code)
	}

	// verify it appears in list even for anonymous (because it has done_at)
	anonReq := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/", nil)
	anonW := httptest.NewRecorder()
	r.ServeHTTP(anonW, anonReq)
	var anonCat PublicCat
	json.Unmarshal(anonW.Body.Bytes(), &anonCat)

	records, ok := anonCat.Records.([]any)
	if !ok {
		t.Fatalf("expected records to be slice in public cat, got %T", anonCat.Records)
	}

	found := false
	for _, recAny := range records {
		rec := recAny.(map[string]any)
		if rec["type"] == "feeding" {
			found = true
			if rec["done_at"] == nil {
				t.Error("expected done_at to be present in public record")
			}
		}
	}
	if !found {
		t.Error("quick record not found in anonymous cat view")
	}
}

func TestBotAPI(t *testing.T) {
	s := newTestServer(t)
	r := s.Router

	// Test Register
	regIn := map[string]any{
		"chat_id": 123456,
		"name":    "Test User",
	}
	body, _ := json.Marshal(regIn)
	req := httptest.NewRequest(http.MethodPost, "/api/bot/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register code = %d", w.Code)
	}

	// Test List Users
	req2 := httptest.NewRequest(http.MethodGet, "/api/bot/users", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list users code = %d", w2.Code)
	}
	var users []map[string]any
	json.Unmarshal(w2.Body.Bytes(), &users)
	if len(users) != 1 || users[0]["provider_id"] != "123456" {
		t.Fatalf("unexpected users list: %s", w2.Body.String())
	}

	// Test List All Planned
	req3 := httptest.NewRequest(http.MethodGet, "/api/records/planned", nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("list all planned code = %d", w3.Code)
	}

	// Test Mark Notification
	notifIn := map[string]any{
		"record_id": "rec1",
		"chat_id":   123456,
	}
	body2, _ := json.Marshal(notifIn)
	req4 := httptest.NewRequest(http.MethodPost, "/api/bot/notifications", bytes.NewReader(body2))
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("mark notif code = %d", w4.Code)
	}

	// Duplicate mark should fail with 409
	req5 := httptest.NewRequest(http.MethodPost, "/api/bot/notifications", bytes.NewReader(body2))
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusConflict {
		t.Fatalf("duplicate mark code = %d, expected 409", w5.Code)
	}
}

func TestDevLogin(t *testing.T) {
	logging.Init(false, false)
	store, _ := storage.Open("file::memory:?cache=shared")
	s, _ := NewServer(Config{
		Store:           store,
		Logger:          logging.L(),
		Monitoring:      MonitoringConfig{},
		JWTSecret:       "test-secret",
		SessionStore:    sessions.NewMemorySessionStore(),
		DevLoginEnabled: true,
	})
	r := s.Router

	// 1. Try dev login
	in := map[string]string{
		"email": "dev@example.com",
		"name":  "Dev User",
	}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("dev login failed: %d, %s", w.Code, w.Body.String())
	}

	// Check cookies
	var accessToken, refreshToken string
	for _, c := range w.Result().Cookies() {
		if c.Name == "access_token" {
			accessToken = c.Value
		}
		if c.Name == "refresh_token" {
			refreshToken = c.Value
		}
	}
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("missing tokens in cookies")
	}

	// 2. Try to get user with token
	req2 := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get user failed: %d, %s", w2.Code, w2.Body.String())
	}
	var user storage.User
	json.Unmarshal(w2.Body.Bytes(), &user)
	if user.Email != "dev@example.com" {
		t.Fatalf("wrong user email: %s", user.Email)
	}

	// 3. Try protected route WITHOUT token (should auto-login in dev mode)
	in3 := catIn{Name: "AutoDevCat"}
	body3, _ := json.Marshal(in3)
	req3 := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusCreated {
		t.Fatalf("auto dev login failed: expected 201, got %d, body=%s", w3.Code, w3.Body.String())
	}
}

func TestNeedAttentionAutoSet(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// 1. Create cat with condition 2
	catIn := map[string]any{
		"name":      "SickCat",
		"condition": 2,
	}
	body, _ := json.Marshal(catIn)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create cat failed: %d", w.Code)
	}

	var cat storage.Cat
	json.Unmarshal(w.Body.Bytes(), &cat)
	if !cat.NeedAttention {
		t.Errorf("expected need_attention to be true for condition 2")
	}

	// 2. Update cat to condition 4, manually setting need_attention=true (should stay true as we only enforce true if < 3)
	// Actually, usually users might want it to stay true if manually set.
	// But if I change it to condition 1, it MUST be true.

	// Let's test update to condition 1
	cat.Condition = 1
	cat.NeedAttention = false // try to override
	body2, _ := json.Marshal(cat)
	req2 := httptest.NewRequest(http.MethodPut, "/api/cats/"+cat.ID+"/", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("update cat failed: %d", w2.Code)
	}
	var cat2 storage.Cat
	json.Unmarshal(w2.Body.Bytes(), &cat2)
	if !cat2.NeedAttention {
		t.Errorf("expected need_attention to be true after update to condition 1")
	}
}

func TestBotDevLogin(t *testing.T) {
	logging.Init(false, false)
	store, _ := storage.Open("file::memory:?cache=shared")
	s, _ := NewServer(Config{
		Store:           store,
		Logger:          logging.L(),
		Monitoring:      MonitoringConfig{},
		JWTSecret:       "test-secret",
		SessionStore:    sessions.NewMemorySessionStore(),
		DevLoginEnabled: true,
	})
	r := s.Router

	// 1. Link chat via dev login GET
	chatID := int64(987654)
	email := "bot-dev@example.com"
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/auth/dev/login?tg_chat_id=%d&email=%s", chatID, email), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("bot dev login failed: %d, %s", w.Code, w.Body.String())
	}

	// 2. Check if linked in DB
	link, err := store.GetBotLink(chatID)
	if err != nil {
		t.Fatalf("link not found in DB: %v", err)
	}
	if link.UserID == "" {
		t.Fatalf("empty user id in link")
	}

	// 3. Try to get bot token
	req2In := map[string]any{"chat_id": chatID}
	body2, _ := json.Marshal(req2In)
	req2 := httptest.NewRequest(http.MethodPost, "/api/bot/token", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get bot token failed: %d, %s", w2.Code, w2.Body.String())
	}
	var res struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(w2.Body.Bytes(), &res)
	if res.AccessToken == "" {
		t.Fatalf("empty access token")
	}

	// 4. Try unlinked chat (should work automatically in dev mode)
	unlinkedChatID := int64(112233)
	in4 := map[string]int64{"chat_id": unlinkedChatID}
	body4, _ := json.Marshal(in4)
	req4 := httptest.NewRequest(http.MethodPost, "/api/bot/token", bytes.NewReader(body4))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)

	if w4.Code != http.StatusOK {
		t.Fatalf("auto bot token failed: expected 200, got %d, body=%s", w4.Code, w4.Body.String())
	}
}

func TestCatImageUploadDelete(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// create cat
	in := catIn{Name: "Cat"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create code = %d, body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	catID, _ := created["id"].(string)
	if catID == "" {
		t.Fatalf("cat id empty")
	}

	// upload image multipart
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.jpg")
	// Minimal valid JPEG header (SOI + APP0 + DQT + SOF + SOS)
	fw.Write([]byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01, 0x00, 0x48, 0x00, 0x48, 0x00, 0x00, // APP0
		0xFF, 0xDB, 0x00, 0x43, 0x00, // DQT
		0x08, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
		0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
		0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
		0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
		0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0x01, 0x00, 0x01, 0x03, 0x01, 0x11, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01, // SOF0 (1x1)
		0xFF, 0xDA, 0x00, 0x0C, 0x03, 0x01, 0x00, 0x02, 0x11, 0x03, 0x11, 0x00, 0x3F, 0x00, // SOS
		0xFF, 0xD9, // EOI
	})
	mw.Close()

	uReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/cats/%s/images", catID), &buf)
	uReq.Header.Set("Content-Type", mw.FormDataContentType())
	uReq.Header.Set("Authorization", "Bearer "+token)
	uW := httptest.NewRecorder()
	r.ServeHTTP(uW, uReq)
	if uW.Code != http.StatusCreated {
		t.Fatalf("upload code = %d, body=%s", uW.Code, uW.Body.String())
	}
	var img storage.Image
	_ = json.Unmarshal(uW.Body.Bytes(), &img)
	if img.ID == "" || img.CatID != catID {
		t.Fatalf("bad image response: %+v", img)
	}

	// delete image
	dReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/cats/%s/images/%s", catID, img.ID), nil)
	dReq.Header.Set("Authorization", "Bearer "+token)
	dW := httptest.NewRecorder()
	r.ServeHTTP(dW, dReq)
	if dW.Code != http.StatusOK {
		t.Fatalf("delete code = %d, body=%s", dW.Code, dW.Body.String())
	}
}

func TestImagePruneKeepFive(t *testing.T) {
	s := newTestServer(t)
	// create cat directly via storage for speed
	cat := storage.Cat{ID: storage.NewUUID(), Name: "PruneCat"}
	if err := s.store.DB.Create(&cat).Error; err != nil {
		t.Fatalf("create cat: %v", err)
	}
	// add 7 images with increasing CreatedAt
	for i := 0; i < 7; i++ {
		img := storage.Image{
			ID:        storage.NewUUID(),
			CatID:     cat.ID,
			MIME:      "image/jpeg",
			Data:      []byte{1, 2, 3, byte(i)},
			CreatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		}
		if err := s.store.DB.Create(&img).Error; err != nil {
			t.Fatalf("create image %d: %v", i, err)
		}
	}
	// prune to 5
	deleted, err := s.store.PruneOldCatImages(cat.ID, 5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected deleted 2, got %d", deleted)
	}
	// count remaining
	var cnt int64
	if err := s.store.DB.Model(&storage.Image{}).Where("cat_id = ?", cat.ID).Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 5 {
		t.Fatalf("expected 5 images remain, got %d", cnt)
	}
}

func TestLastSeenAndRecurrence(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// create cat
	in := catIn{Name: "Cat"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var cat map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cat)
	catID := cat["id"].(string)

	// Add a record with timestamp
	ts := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	recIn := map[string]any{
		"type":      "feeding",
		"timestamp": ts.Format(time.RFC3339),
	}
	recBody, _ := json.Marshal(recIn)
	req2 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(recBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Check LastSeen on cat
	req3 := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	var updatedCat map[string]any
	_ = json.Unmarshal(w3.Body.Bytes(), &updatedCat)
	lastSeen, _ := updatedCat["last_seen"].(string)
	if lastSeen == "" {
		t.Fatalf("expected last_seen to be set")
	}

	// Recurring record: plan it for 2 days ago
	planned := time.Now().AddDate(0, 0, -2).Truncate(time.Hour)
	recRecur := map[string]any{
		"type":       "vet_visit",
		"planned_at": planned.Format(time.RFC3339),
		"recurrence": "daily",
		"interval":   1,
	}
	recurBody, _ := json.Marshal(recRecur)
	req4 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(recurBody))
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("Authorization", "Bearer "+token)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusCreated {
		t.Fatalf("failed to create recurring record: %s", w4.Body.String())
	}

	// List with expansion: from 3 days ago to 3 days in future
	startQuery := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)
	endQuery := time.Now().AddDate(0, 0, 3).UTC().Format(time.RFC3339)
	req5 := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/records?start="+startQuery+"&end="+endQuery, nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	var list []map[string]any
	_ = json.Unmarshal(w5.Body.Bytes(), &list)

	// DEBUG: print all IDs
	for _, l := range list {
		t.Logf("Found record ID: %v, PlannedAt: %v", l["id"], l["planned_at"])
	}

	// Should have at least 2 occurrences (original + 2 virtual = 3 total)
	if len(list) < 3 {
		t.Logf("List: %v", list)
		t.Fatalf("expected at least 3 recurring records, got %d", len(list))
	}
}

func TestReminderAPI(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// 1. Create cat
	in := catIn{Name: "ReminderCat"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var cat map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cat)
	catID := cat["id"].(string)

	// 2. Plan event in 10 minutes
	planned := time.Now().Add(10 * time.Minute).UTC()
	recIn := map[string]any{
		"type":       "feeding",
		"planned_at": planned.Format(time.RFC3339),
	}
	recBody, _ := json.Marshal(recIn)
	req2 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/records", bytes.NewReader(recBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// 3. Check ListAllPlannedRecords
	start := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	end := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	req3 := httptest.NewRequest(http.MethodGet, "/api/records/planned?start="+start+"&end="+end, nil)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("list planned failed: %d", w3.Code)
	}

	var recs []storage.Record
	json.Unmarshal(w3.Body.Bytes(), &recs)

	found := false
	for _, rec := range recs {
		if rec.CatID == catID && rec.Type == "feeding" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("planned record not found in reminder list")
	}
}

func TestLocationAutoRecord(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// 1. Create cat
	in := catIn{Name: "LocCat"}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var cat map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &cat)
	catID := cat["id"].(string)

	// 2. Add location
	locIn := map[string]any{
		"lat": 1.23,
		"lon": 4.56,
	}
	locBody, _ := json.Marshal(locIn)
	req2 := httptest.NewRequest(http.MethodPost, "/api/cats/"+catID+"/locations", bytes.NewReader(locBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("add location failed: %d", w2.Code)
	}

	// 3. Check records
	req3 := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/records?status=done", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("list records failed: %d", w3.Code)
	}

	var recs []storage.Record
	json.Unmarshal(w3.Body.Bytes(), &recs)

	found := false
	for _, rec := range recs {
		if rec.Type == "observation" && strings.Contains(rec.Note, "Location") {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("automatic observation record for location not found")
	}
}

func TestUpdateCatWithUnknownFields(t *testing.T) {
	s := newTestServer(t)
	r := s.Router

	// 1. Create a cat
	cat := storage.Cat{
		ID:   storage.NewUUID(),
		Name: "Test Cat",
	}
	s.store.DB.Create(&cat)

	// 2. Try to update it with a JSON that includes "likes" and "liked"
	// These fields are returned by GET /api/cats/{id} (as part of PublicCat)
	// and frontend might send them back in PUT /api/cats/{id}
	updateData := map[string]interface{}{
		"name":  "Updated Cat Name",
		"likes": 5,
		"liked": true,
	}
	body, _ := json.Marshal(updateData)
	req := httptest.NewRequest(http.MethodPut, "/api/cats/"+cat.ID, bytes.NewReader(body))
	// We need to be authorized as mutations are protected
	token := issueTestToken(t, s, "test-user")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var out storage.Cat
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if out.Name != "Updated Cat Name" {
		t.Errorf("Expected name 'Updated Cat Name', got '%s'", out.Name)
	}
}
