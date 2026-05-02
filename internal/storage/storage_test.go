package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maniack/catwatch/internal/logging"
)

func init() { logging.Init(false, false) }

func openTestDB(t *testing.T) *Store {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return store
}

// ---- FindOrCreateUser -------------------------------------------------------

func TestFindOrCreateUser_Create(t *testing.T) {
	s := openTestDB(t)
	u, err := s.FindOrCreateUser("google", "g-001", "a@example.com", "Alice", "")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Provider != "google" || u.ProviderID != "g-001" {
		t.Errorf("unexpected provider fields: %+v", u)
	}
	if u.Email != "a@example.com" || u.Name != "Alice" {
		t.Errorf("unexpected user fields: %+v", u)
	}
}

func TestFindOrCreateUser_Find(t *testing.T) {
	s := openTestDB(t)
	first, _ := s.FindOrCreateUser("google", "g-002", "b@example.com", "Bob", "")
	second, err := s.FindOrCreateUser("google", "g-002", "other@example.com", "Other", "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.ID != second.ID {
		t.Error("expected same user ID on second call")
	}
	// Original email preserved
	if second.Email != "b@example.com" {
		t.Errorf("expected original email, got %q", second.Email)
	}
}

func TestFindOrCreateUser_RequiresProviderFields(t *testing.T) {
	s := openTestDB(t)
	if _, err := s.FindOrCreateUser("", "x", "", "", ""); err == nil {
		t.Error("expected error for empty provider")
	}
	if _, err := s.FindOrCreateUser("google", "", "", "", ""); err == nil {
		t.Error("expected error for empty providerID")
	}
}

// ---- Likes ------------------------------------------------------------------

func TestLikes(t *testing.T) {
	s := openTestDB(t)

	catID := NewUUID()
	userID := NewUUID()
	s.DB.Create(&Cat{ID: catID, Name: "Fluffy"})
	s.DB.Create(&User{ID: userID, Provider: "test", ProviderID: userID})

	// Initially not liked
	liked, err := s.IsLikedByUser(catID, userID)
	if err != nil || liked {
		t.Fatalf("expected not liked initially, got liked=%v err=%v", liked, err)
	}
	n, _ := s.LikesCount(catID)
	if n != 0 {
		t.Errorf("expected count 0, got %d", n)
	}

	// Like
	if err := s.SetLike(catID, userID, true); err != nil {
		t.Fatalf("SetLike true: %v", err)
	}
	liked, _ = s.IsLikedByUser(catID, userID)
	if !liked {
		t.Error("expected liked=true after SetLike true")
	}
	n, _ = s.LikesCount(catID)
	if n != 1 {
		t.Errorf("expected count 1, got %d", n)
	}

	// Unlike
	if err := s.SetLike(catID, userID, false); err != nil {
		t.Fatalf("SetLike false: %v", err)
	}
	liked, _ = s.IsLikedByUser(catID, userID)
	if liked {
		t.Error("expected liked=false after SetLike false")
	}
	n, _ = s.LikesCount(catID)
	if n != 0 {
		t.Errorf("expected count 0 after unlike, got %d", n)
	}
}

func TestGetUserLikedCats(t *testing.T) {
	s := openTestDB(t)
	userID := NewUUID()
	catID := NewUUID()
	s.DB.Create(&User{ID: userID, Provider: "test", ProviderID: userID})
	s.DB.Create(&Cat{ID: catID, Name: "Liked Cat"})

	// No likes yet
	cats, err := s.GetUserLikedCats(userID)
	if err != nil || len(cats) != 0 {
		t.Fatalf("expected empty list, got %d cats, err=%v", len(cats), err)
	}

	s.DB.Create(&Like{ID: NewUUID(), CatID: catID, UserID: userID})
	cats, err = s.GetUserLikedCats(userID)
	if err != nil || len(cats) != 1 {
		t.Fatalf("expected 1 liked cat, got %d, err=%v", len(cats), err)
	}
	if cats[0].ID != catID {
		t.Errorf("expected catID %s, got %s", catID, cats[0].ID)
	}
}

// ---- BotLink ----------------------------------------------------------------

func TestBotLink(t *testing.T) {
	s := openTestDB(t)
	userID := NewUUID()
	s.DB.Create(&User{ID: userID, Provider: "test", ProviderID: userID})

	chatID := int64(9999)

	// Initially not linked
	if _, err := s.GetBotLink(chatID); err == nil {
		t.Error("expected error for non-existent link")
	}

	// Link
	if err := s.LinkBotChat(chatID, userID); err != nil {
		t.Fatalf("LinkBotChat: %v", err)
	}

	link, err := s.GetBotLink(chatID)
	if err != nil {
		t.Fatalf("GetBotLink: %v", err)
	}
	if link.UserID != userID {
		t.Errorf("expected userID %s, got %s", userID, link.UserID)
	}

	// Unlink
	if err := s.UnlinkBotChat(chatID); err != nil {
		t.Fatalf("UnlinkBotChat: %v", err)
	}
	if _, err := s.GetBotLink(chatID); err == nil {
		t.Error("expected error after unlink")
	}
}

// ---- OAuthClient ------------------------------------------------------------

func TestOAuthClient(t *testing.T) {
	s := openTestDB(t)

	client, err := s.CreateOAuthClient("TestApp", []string{"http://localhost/cb", "https://example.com/cb"})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	if client.ID == "" || client.Secret == "" {
		t.Error("expected non-empty ID and Secret")
	}

	got, err := s.GetOAuthClient(client.ID)
	if err != nil {
		t.Fatalf("GetOAuthClient: %v", err)
	}
	if got.Name != "TestApp" {
		t.Errorf("expected name 'TestApp', got %q", got.Name)
	}
	if !strings.Contains(got.RedirectURIs, "http://localhost/cb") {
		t.Errorf("redirect URIs missing: %q", got.RedirectURIs)
	}

	// Non-existent
	if _, err := s.GetOAuthClient("no-such-id"); err == nil {
		t.Error("expected error for missing client")
	}
}

// ---- DeleteUser -------------------------------------------------------------

func TestDeleteUser(t *testing.T) {
	s := openTestDB(t)
	userID := NewUUID()
	s.DB.Create(&User{ID: userID, Provider: "test", ProviderID: userID, Name: "Delete Me"})

	// Add associated data
	catID := NewUUID()
	s.DB.Create(&Cat{ID: catID, Name: "Cat"})
	s.DB.Create(&Like{ID: NewUUID(), CatID: catID, UserID: userID})
	s.DB.Create(&BotLink{ChatID: 1111, UserID: userID})

	if err := s.DeleteUser(userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	var u User
	if err := s.DB.First(&u, "id = ?", userID).Error; err == nil {
		t.Error("expected user to be deleted")
	}

	// Likes should be gone
	var n int64
	s.DB.Model(&Like{}).Where("user_id = ?", userID).Count(&n)
	if n != 0 {
		t.Errorf("expected likes deleted, found %d", n)
	}

	// BotLink should be gone
	s.DB.Model(&BotLink{}).Where("user_id = ?", userID).Count(&n)
	if n != 0 {
		t.Errorf("expected bot link deleted, found %d", n)
	}
}

// ---- AuditLog ---------------------------------------------------------------

func TestPruneAuditLogs(t *testing.T) {
	s := openTestDB(t)

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	s.DB.Create(&AuditLog{ID: NewUUID(), Timestamp: old, UserID: "u1"})
	s.DB.Create(&AuditLog{ID: NewUUID(), Timestamp: recent, UserID: "u1"})

	deleted, err := s.PruneAuditLogs(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("PruneAuditLogs: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	var remaining int64
	s.DB.Model(&AuditLog{}).Count(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 remaining, got %d", remaining)
	}
}

// ---- Image pruning ----------------------------------------------------------

func TestPruneOldCatImages(t *testing.T) {
	s := openTestDB(t)
	catID := NewUUID()
	s.DB.Create(&Cat{ID: catID, Name: "Pruned Cat"})

	for i := 0; i < 7; i++ {
		s.DB.Create(&Image{
			ID:        NewUUID(),
			CatID:     catID,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	deleted, err := s.PruneOldCatImages(catID, 5)
	if err != nil {
		t.Fatalf("PruneOldCatImages: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	var remaining int64
	s.DB.Model(&Image{}).Where("cat_id = ?", catID).Count(&remaining)
	if remaining != 5 {
		t.Errorf("expected 5 remaining, got %d", remaining)
	}
}
