package backend

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maniack/catwatch/internal/storage"
)

func TestCatSoftDelete(t *testing.T) {
	s := newTestServer(t)
	r := s.Router
	token := issueTestToken(t, s, "admin-user")

	// 1. Create a cat
	in := catIn{Name: "SoftDeleteCat"}
	body, _ := json.Marshal(in)
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
	catID := cat.ID

	// 2. Delete the cat
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/cats/"+catID+"/", nil)
	reqDel.Header.Set("Authorization", "Bearer "+token)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("delete cat failed: %d", wDel.Code)
	}

	// 3. Verify cat is NOT in the list
	reqList := httptest.NewRequest(http.MethodGet, "/api/cats/", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	var cats []PublicCat
	json.Unmarshal(wList.Body.Bytes(), &cats)
	for _, c := range cats {
		if c.ID == catID {
			t.Errorf("cat still present in public list after delete")
		}
	}

	// 4. Verify cat is NOT accessible by ID
	reqGet := httptest.NewRequest(http.MethodGet, "/api/cats/"+catID+"/", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusNotFound {
		t.Errorf("expected 404 for deleted cat, got %d", wGet.Code)
	}

	// 5. Verify cat record still exists in DB but with DeletedAt set (Soft Delete)
	var dbCat storage.Cat
	// We use Unscoped to see soft-deleted records
	if err := s.store.DB.Unscoped().First(&dbCat, "id = ?", catID).Error; err != nil {
		t.Fatalf("cat record missing from DB entirely: %v", err)
	}
	if !dbCat.DeletedAt.Valid {
		t.Errorf("expected DeletedAt to be valid for soft-deleted cat")
	}
}
