package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/storage"
)

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	logging.Init(false, false)
	dsn := "file:mcp_test?mode=memory&cache=shared"
	st, err := storage.Open(dsn)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	return st
}

func TestMCPMutationsBasicFlow(t *testing.T) {
	st := newTestStore(t)
	s, err := New(st)
	if err != nil {
		t.Fatalf("new MCP server: %v", err)
	}
	ctx := context.WithValue(context.Background(), logging.ContextUserID, "u-1")

	// 1. Create a cat
	cat := storage.Cat{Name: "MCP Cat"}
	_, outAny, err := s.createCat(ctx, nil, cat)
	if err != nil {
		t.Fatalf("createCat: %v", err)
	}
	created := outAny.(storage.Cat)
	if created.ID == "" {
		t.Fatalf("expected cat ID")
	}

	// 2. Toggle like
	_, likeAny, err := s.toggleLike(ctx, nil, ToggleLikeArgs{CatID: created.ID})
	if err != nil {
		t.Fatalf("toggleLike: %v", err)
	}
	liked := likeAny.(map[string]any)["liked"].(bool)
	if !liked {
		t.Fatalf("expected liked=true after first toggle")
	}

	// 3. Create record (feeding)
	rec := storage.Record{CatID: created.ID, Type: "feeding"}
	_, recAny, err := s.createRecord(ctx, nil, rec)
	if err != nil {
		t.Fatalf("createRecord: %v", err)
	}
	createdRec := recAny.(storage.Record)
	if createdRec.ID == "" || createdRec.UserID == "" {
		t.Fatalf("expected record id and user id set")
	}

	// 4. Mark record done
	_, doneAny, err := s.markRecordDone(ctx, nil, MarkRecordDoneArgs{ID: createdRec.ID, CatID: created.ID})
	if err != nil {
		t.Fatalf("markRecordDone: %v", err)
	}
	doneRec := doneAny.(storage.Record)
	if doneRec.DoneAt == nil {
		t.Fatalf("expected done_at to be set")
	}

	// 5. Verify cat last_seen is updated
	var gotCat storage.Cat
	if err := st.DB.First(&gotCat, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load cat: %v", err)
	}
	if gotCat.LastSeen == nil || gotCat.LastSeen.IsZero() {
		t.Fatalf("expected last_seen to be set after done record")
	}

	// 6. Add location
	_, locAny, err := s.addCatLocation(ctx, nil, AddCatLocationArgs{CatID: created.ID, Latitude: 1.23, Longitude: 4.56})
	if err != nil {
		t.Fatalf("addCatLocation: %v", err)
	}
	loc := locAny.(storage.CatLocation)
	if loc.ID == "" || loc.CatID != created.ID {
		t.Fatalf("bad location response")
	}

	// 7. Add image by URL and delete it
	_, imgAny, err := s.addCatImageByURL(ctx, nil, AddCatImageByURLArgs{CatID: created.ID, URL: "https://example.com/x.jpg"})
	if err != nil {
		t.Fatalf("addCatImageByURL: %v", err)
	}
	img := imgAny.(storage.Image)
	if img.ID == "" || img.URL == "" {
		t.Fatalf("expected image created")
	}
	_, _, err = s.deleteImage(ctx, nil, DeleteImageArgs{ImageID: img.ID})
	if err != nil {
		t.Fatalf("deleteImage: %v", err)
	}
}

func TestMCPVirtualRecordDone(t *testing.T) {
	st := newTestStore(t)
	s, _ := New(st)
	ctx := context.WithValue(context.Background(), logging.ContextUserID, "u-2")
	// Prepare a recurring record
	cat := storage.Cat{ID: storage.NewUUID(), Name: "RR Cat"}
	if err := st.DB.Create(&cat).Error; err != nil {
		t.Fatalf("create cat: %v", err)
	}
	planned := time.Now().AddDate(0, 0, -2).Truncate(time.Hour)
	rec := storage.Record{ID: storage.NewUUID(), CatID: cat.ID, Type: "vet_visit", PlannedAt: &planned, Recurrence: "daily", Interval: 1}
	if err := st.DB.Create(&rec).Error; err != nil {
		t.Fatalf("create recurring record: %v", err)
	}
	// Compose virtual id for yesterday
	virtID := "virtual-" + rec.ID + "-" + time.Now().AddDate(0, 0, -1).Format("20060102")
	_, anyOut, err := s.markRecordDone(ctx, nil, MarkRecordDoneArgs{ID: virtID, CatID: cat.ID})
	if err != nil {
		t.Fatalf("markRecordDone(virtual): %v", err)
	}
	inst := anyOut.(storage.Record)
	if inst.ID == rec.ID || inst.DoneAt == nil {
		t.Fatalf("expected new instance with done_at")
	}
}
