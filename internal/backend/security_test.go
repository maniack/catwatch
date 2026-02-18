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

func TestUnauthorizedMutations(t *testing.T) {
	s := newTestServer(t)
	r := s.Router

	methods := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/cats/", map[string]string{"name": "Anonymous"}},
		{http.MethodPut, "/api/cats/123/", map[string]string{"name": "Anonymous"}},
		{http.MethodDelete, "/api/cats/123/", nil},
		{http.MethodPost, "/api/cats/123/records", map[string]string{"type": "feeding"}},
		{http.MethodPut, "/api/cats/123/records/rid/", map[string]string{"note": "hack"}},
		{http.MethodPost, "/api/cats/123/records/rid/done", nil},
	}

	for _, m := range methods {
		t.Run(m.method+" "+m.path, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if m.body != nil {
				b, _ := json.Marshal(m.body)
				bodyReader = bytes.NewReader(b)
			} else {
				bodyReader = bytes.NewReader([]byte{})
			}
			req := httptest.NewRequest(m.method, m.path, bodyReader)
			if m.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestPublicPIIFiltering(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "test-user")

	// 1. Create a cat with a record
	catIn := map[string]any{
		"name":        "Secret Cat",
		"description": "Super private cat",
	}
	body, _ := json.Marshal(catIn)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create cat: %s", w.Body.String())
	}
	var cat storage.Cat
	json.Unmarshal(w.Body.Bytes(), &cat)

	// 2. Add a record for that cat
	recordIn := map[string]any{
		"type": "medical",
		"note": "Secret medical info",
	}
	bodyRec, _ := json.Marshal(recordIn)
	reqRec := httptest.NewRequest(http.MethodPost, "/api/cats/"+cat.ID+"/records", bytes.NewReader(bodyRec))
	reqRec.Header.Set("Content-Type", "application/json")
	reqRec.Header.Set("Authorization", "Bearer "+token)
	wRec := httptest.NewRecorder()
	r.ServeHTTP(wRec, reqRec)
	if wRec.Code != http.StatusCreated {
		t.Fatalf("failed to create record: %s", wRec.Body.String())
	}

	// 3. Mark record as done
	var record storage.Record
	json.Unmarshal(wRec.Body.Bytes(), &record)
	reqDone := httptest.NewRequest(http.MethodPost, "/api/cats/"+cat.ID+"/records/"+record.ID+"/done", nil)
	reqDone.Header.Set("Authorization", "Bearer "+token)
	wDone := httptest.NewRecorder()
	r.ServeHTTP(wDone, reqDone)
	if wDone.Code != http.StatusOK {
		t.Fatalf("failed to mark record as done: %s", wDone.Body.String())
	}

	// 4. GET /api/cats/ as anonymous
	reqList := httptest.NewRequest(http.MethodGet, "/api/cats/", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("failed to list cats: %s", wList.Body.String())
	}
	var publicCats []map[string]any
	json.Unmarshal(wList.Body.Bytes(), &publicCats)

	for _, c := range publicCats {
		// Should NOT have 'Records' or sensitive fields not in PublicCat DTO
		if _, ok := c["records"]; ok {
			t.Errorf("public cat should not contain 'records' field")
		}
	}

	// 5. GET /api/cats/{id}/records as anonymous
	reqRecs := httptest.NewRequest(http.MethodGet, "/api/cats/"+cat.ID+"/records", nil)
	wRecs := httptest.NewRecorder()
	r.ServeHTTP(wRecs, reqRecs)
	if wRecs.Code != http.StatusOK {
		t.Fatalf("failed to list records: %s", wRecs.Body.String())
	}
	var publicRecs []map[string]any
	json.Unmarshal(wRecs.Body.Bytes(), &publicRecs)

	for _, rec := range publicRecs {
		// PublicRecord should NOT have 'note' or 'user_id'
		if _, ok := rec["note"]; ok {
			t.Errorf("public record should not contain 'note' field")
		}
		if _, ok := rec["user_id"]; ok {
			t.Errorf("public record should not contain 'user_id' field")
		}
		if _, ok := rec["user"]; ok {
			t.Errorf("public record should not contain 'user' field")
		}
	}
}

func TestAuditLogs(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "audit-user")

	// 1. Initial count of audit logs
	var countBefore int64
	s.store.DB.Model(&storage.AuditLog{}).Count(&countBefore)

	// 2. Perform a mutation
	catIn := map[string]any{"name": "Audited Cat"}
	body, _ := json.Marshal(catIn)
	req := httptest.NewRequest(http.MethodPost, "/api/cats/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create cat: %s", w.Body.String())
	}

	// 3. Check audit log count increased
	var countAfter int64
	s.store.DB.Model(&storage.AuditLog{}).Count(&countAfter)
	if countAfter != countBefore+1 {
		t.Errorf("expected audit log count to increase by 1, got %d -> %d", countBefore, countAfter)
	}

	// 4. Verify log entry content
	var entry storage.AuditLog
	s.store.DB.Order("timestamp DESC").First(&entry)
	if entry.UserID != "audit-user" {
		t.Errorf("expected user_id audit-user, got %s", entry.UserID)
	}
	if entry.TargetType != "cat" {
		t.Errorf("expected target_type cat, got %s", entry.TargetType)
	}
	if entry.Status != "success" {
		t.Errorf("expected status success, got %s", entry.Status)
	}

	// 5. Verify API endpoint GET /api/user/audit
	reqAudit := httptest.NewRequest(http.MethodGet, "/api/user/audit", nil)
	reqAudit.Header.Set("Authorization", "Bearer "+token)
	wAudit := httptest.NewRecorder()
	r.ServeHTTP(wAudit, reqAudit)
	if wAudit.Code != http.StatusOK {
		t.Fatalf("failed to get user audit: %s", wAudit.Body.String())
	}
	var auditList []storage.AuditLog
	json.Unmarshal(wAudit.Body.Bytes(), &auditList)
	if len(auditList) == 0 {
		t.Errorf("expected audit logs in list, got empty")
	}
	if auditList[0].ID != entry.ID {
		t.Errorf("expected first log ID %s, got %s", entry.ID, auditList[0].ID)
	}
}

func TestBotTokenAndUnlink(t *testing.T) {
	s := newTestServer(t)
	r := s.Router

	// Set Bot API Key
	s.cfg.BotAPIKey = "secret-bot-key"

	// 1. Create a user and a bot link
	uid := storage.NewUUID()
	s.store.DB.Create(&storage.User{ID: uid, Provider: "google", ProviderID: "g123"})
	s.store.DB.Create(&storage.BotLink{ChatID: 12345, UserID: uid})

	// 2. Try to get token with wrong bot key
	in := map[string]any{"chat_id": 12345}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/bot/token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bot-Key", "wrong-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong bot key, got %d", w.Code)
	}

	// 3. Get token with correct bot key
	req2 := httptest.NewRequest(http.MethodPost, "/api/bot/token", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Bot-Key", "secret-bot-key")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for correct bot key, got %d: %s", w2.Code, w2.Body.String())
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(w2.Body.Bytes(), &out)
	if out.AccessToken == "" {
		t.Error("expected access_token in response")
	}

	// 4. Unlink
	req3 := httptest.NewRequest(http.MethodPost, "/api/bot/unlink", bytes.NewReader(body))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("X-Bot-Key", "secret-bot-key")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 for unlink, got %d: %s", w3.Code, w3.Body.String())
	}

	// Verify unlinked
	var link storage.BotLink
	err := s.store.DB.First(&link, "chat_id = ?", 12345).Error
	if err == nil {
		t.Error("expected bot link to be deleted")
	}
}

func TestAuthRefresh(t *testing.T) {
	s := newTestServer(t)
	r := s.Router

	// 1. Create session in store
	uid := "refresh-user"
	refreshToken := s.generateOpaqueToken()
	payload, _ := json.Marshal(map[string]any{"uid": uid, "iat": time.Now().Unix()})
	s.sessions.Set("sess:"+refreshToken, payload, 1*time.Hour)

	// 2. Call refresh with refresh_token cookie
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("refresh code = %d, body=%s", w.Code, w.Body.String())
	}

	// Verify new cookies set (access_token and refresh_token)
	cookies := w.Result().Cookies()
	var newAccess, newRefresh bool
	for _, c := range cookies {
		if c.Name == "access_token" && c.Value != "" {
			newAccess = true
		}
		if c.Name == "refresh_token" && c.Value != "" {
			newRefresh = true
			if c.Value == refreshToken {
				t.Error("expected new refresh_token, got same")
			}
		}
	}
	if !newAccess {
		t.Error("expected new access_token cookie")
	}
	if !newRefresh {
		t.Error("expected new refresh_token cookie")
	}
}
