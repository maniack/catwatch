package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

type Server struct {
	mcpServer *mcp.Server
	store     *storage.Store
}

func New(store *storage.Store) (*Server, error) {
	s := &Server{
		store: store,
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "catwatch",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_cats",
		Description: "Get a list of all cats with basic information",
	}, s.listCats)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_cat",
		Description: "Get detailed information about a specific cat by its ID",
	}, s.getCat)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "search_cats",
		Description: "Search for cats by name",
	}, s.searchCats)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "get_cat_records",
		Description: "Get feeding and medical history for a cat",
	}, s.getCatRecords)

	// Mutating tools (require authorized MCP session)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "create_cat",
		Description: "Create a new cat with optional fields and tags",
	}, s.createCat)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "update_cat",
		Description: "Update cat fields by ID (full save incl. tags)",
	}, s.updateCat)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "delete_cat",
		Description: "Delete cat by ID",
	}, s.deleteCat)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "create_record",
		Description: "Create a record for a cat (feeding, medical, etc.)",
	}, s.createRecord)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "update_record",
		Description: "Update a record by ID",
	}, s.updateRecord)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "mark_record_done",
		Description: "Mark a record (including virtual recurrence) as done",
	}, s.markRecordDone)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "toggle_like",
		Description: "Toggle like for a cat by current user",
	}, s.toggleLike)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "add_cat_location",
		Description: "Add a location to a cat (lat/lon, name, description)",
	}, s.addCatLocation)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "add_cat_image_by_url",
		Description: "Attach an external image URL to a cat",
	}, s.addCatImageByURL)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "delete_image",
		Description: "Delete image by ID",
	}, s.deleteImage)

	s.mcpServer = mcpServer
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) HandleSSE() http.Handler {
	return mcp.NewSSEHandler(func(request *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)
}

type ListCatsArgs struct {
	Limit int `json:"limit"`
}

func (s *Server) listCats(ctx context.Context, request *mcp.CallToolRequest, input ListCatsArgs) (*mcp.CallToolResult, any, error) {
	var cats []storage.Cat
	query := s.store.DB.Preload("Tags").Order("created_at DESC")
	if input.Limit > 0 {
		query = query.Limit(input.Limit)
	}
	if err := query.Find(&cats).Error; err != nil {
		return nil, nil, err
	}
	return nil, cats, nil
}

type GetCatArgs struct {
	ID string `json:"id"`
}

func (s *Server) getCat(ctx context.Context, request *mcp.CallToolRequest, input GetCatArgs) (*mcp.CallToolResult, any, error) {
	var cat storage.Cat
	if err := s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").Preload("Records").First(&cat, "id = ?", input.ID).Error; err != nil {
		return nil, nil, err
	}
	return nil, cat, nil
}

type SearchCatsArgs struct {
	Query string `json:"query"`
}

func (s *Server) searchCats(ctx context.Context, request *mcp.CallToolRequest, input SearchCatsArgs) (*mcp.CallToolResult, any, error) {
	var cats []storage.Cat
	if err := s.store.DB.Preload("Tags").Where("name LIKE ?", "%"+input.Query+"%").Find(&cats).Error; err != nil {
		return nil, nil, err
	}
	return nil, cats, nil
}

type GetCatRecordsArgs struct {
	CatID string `json:"cat_id"`
	Limit int    `json:"limit"`
}

func (s *Server) getCatRecords(ctx context.Context, request *mcp.CallToolRequest, input GetCatRecordsArgs) (*mcp.CallToolResult, any, error) {
	var records []storage.Record
	query := s.store.DB.Where("cat_id = ?", input.CatID).Order("timestamp DESC")
	if input.Limit > 0 {
		query = query.Limit(input.Limit)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, nil, err
	}
	return nil, records, nil
}

// --- Helpers ---
func uidFromCtx(ctx context.Context) string {
	if v := ctx.Value(logging.ContextUserID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (s *Server) updateCatLastSeenFromRecord(rec storage.Record) {
	var lastSeen *time.Time
	if rec.DoneAt != nil {
		lastSeen = rec.DoneAt
	} else if !rec.Timestamp.IsZero() {
		lastSeen = &rec.Timestamp
	}
	if lastSeen != nil {
		s.store.DB.Model(&storage.Cat{}).Where("id = ?", rec.CatID).
			Where("last_seen IS NULL OR last_seen < ?", lastSeen).
			Update("last_seen", lastSeen)
	}
}

// --- Mutating tools ---

// createCat creates a new cat. Input mirrors storage.Cat fields; ID will be generated if empty.
func (s *Server) createCat(ctx context.Context, request *mcp.CallToolRequest, in storage.Cat) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		in.ID = storage.NewUUID()
	}
	// normalize condition 1..5 (default 3)
	if in.Condition < 1 || in.Condition > 5 {
		in.Condition = 3
	}
	if in.Condition > 0 && in.Condition < 3 {
		in.NeedAttention = true
	}
	// Handle tags: ensure IDs; reuse existing by name when present
	for i := range in.Tags {
		if in.Tags[i].ID == "" {
			var existing storage.Tag
			if err := s.store.DB.Where("name = ?", in.Tags[i].Name).First(&existing).Error; err == nil {
				in.Tags[i] = existing
			} else {
				in.Tags[i].ID = storage.NewUUID()
			}
		}
	}
	if err := s.store.DB.Create(&in).Error; err != nil {
		return nil, nil, err
	}
	return nil, in, nil
}

// updateCat updates existing cat by full save (handles tags M2M correctly).
func (s *Server) updateCat(ctx context.Context, request *mcp.CallToolRequest, in storage.Cat) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return nil, nil, gorm.ErrMissingWhereClause
	}
	if in.Condition < 1 || in.Condition > 5 {
		in.Condition = 3
	}
	if in.Condition > 0 && in.Condition < 3 {
		in.NeedAttention = true
	}
	for i := range in.Tags {
		if in.Tags[i].ID == "" {
			var existing storage.Tag
			if err := s.store.DB.Where("name = ?", in.Tags[i].Name).First(&existing).Error; err == nil {
				in.Tags[i] = existing
			} else {
				in.Tags[i].ID = storage.NewUUID()
			}
		}
	}
	if err := s.store.DB.Save(&in).Error; err != nil {
		return nil, nil, err
	}
	var out storage.Cat
	_ = s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").First(&out, "id = ?", in.ID).Error
	return nil, out, nil
}

// deleteCat removes a cat by ID.
type DeleteCatArgs struct {
	ID string `json:"id"`
}

func (s *Server) deleteCat(ctx context.Context, request *mcp.CallToolRequest, input DeleteCatArgs) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return nil, nil, gorm.ErrMissingWhereClause
	}
	if err := s.store.DB.Delete(&storage.Cat{}, "id = ?", input.ID).Error; err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"status": "ok"}, nil
}

// createRecord creates a new record for a cat and updates cat's last_seen when applicable.
func (s *Server) createRecord(ctx context.Context, request *mcp.CallToolRequest, in storage.Record) (*mcp.CallToolResult, any, error) {
	uid := uidFromCtx(ctx)
	if in.ID == "" {
		in.ID = storage.NewUUID()
	}
	if in.CatID == "" {
		return nil, nil, gorm.ErrMissingWhereClause
	}
	in.UserID = uid
	if in.PlannedAt == nil && in.DoneAt == nil && in.Timestamp.IsZero() {
		in.Timestamp = time.Now()
	}
	if err := s.store.DB.Create(&in).Error; err != nil {
		return nil, nil, err
	}
	s.updateCatLastSeenFromRecord(in)
	return nil, in, nil
}

// updateRecord updates fields of an existing record.
func (s *Server) updateRecord(ctx context.Context, request *mcp.CallToolRequest, in storage.Record) (*mcp.CallToolResult, any, error) {
	if in.ID == "" {
		return nil, nil, gorm.ErrMissingWhereClause
	}
	if err := s.store.DB.Model(&storage.Record{ID: in.ID}).Updates(in).Error; err != nil {
		return nil, nil, err
	}
	var out storage.Record
	if err := s.store.DB.First(&out, "id = ?", in.ID).Error; err != nil {
		return nil, nil, err
	}
	s.updateCatLastSeenFromRecord(out)
	return nil, out, nil
}

// markRecordDone sets done_at to now for the record, supports virtual-* IDs for recurrences.
type MarkRecordDoneArgs struct {
	ID    string `json:"id"`
	CatID string `json:"cat_id"`
}

func (s *Server) markRecordDone(ctx context.Context, request *mcp.CallToolRequest, input MarkRecordDoneArgs) (*mcp.CallToolResult, any, error) {
	now := time.Now()
	if strings.HasPrefix(input.ID, "virtual-") {
		idAndDate := strings.TrimPrefix(input.ID, "virtual-")
		lastDash := strings.LastIndex(idAndDate, "-")
		if lastDash != -1 {
			originalID := idAndDate[:lastDash]
			dateStr := idAndDate[lastDash+1:]
			var orig storage.Record
			if err := s.store.DB.First(&orig, "id = ?", originalID).Error; err == nil {
				occurrenceDate, _ := time.Parse("20060102", dateStr)
				if orig.PlannedAt != nil {
					planned := time.Date(occurrenceDate.Year(), occurrenceDate.Month(), occurrenceDate.Day(),
						orig.PlannedAt.Hour(), orig.PlannedAt.Minute(), orig.PlannedAt.Second(), orig.PlannedAt.Nanosecond(), orig.PlannedAt.Location())
					newRec := orig
					newRec.ID = storage.NewUUID()
					newRec.PlannedAt = &planned
					newRec.DoneAt = &now
					newRec.Recurrence = ""
					newRec.CreatedAt = now
					if err := s.store.DB.Create(&newRec).Error; err != nil {
						return nil, nil, err
					}
					s.updateCatLastSeenFromRecord(newRec)
					return nil, newRec, nil
				}
			}
		}
	}
	// Non-virtual: update existing
	db := s.store.DB.Model(&storage.Record{}).Where("id = ?", input.ID)
	if input.CatID != "" {
		db = db.Where("cat_id = ?", input.CatID)
	}
	if err := db.Update("done_at", &now).Error; err != nil {
		return nil, nil, err
	}
	var out storage.Record
	if err := s.store.DB.First(&out, "id = ?", input.ID).Error; err != nil {
		return nil, nil, err
	}
	s.updateCatLastSeenFromRecord(out)
	return nil, out, nil
}

// toggleLike toggles like for current user on a cat and returns likes count and state.
type ToggleLikeArgs struct {
	CatID string `json:"cat_id"`
}

func (s *Server) toggleLike(ctx context.Context, request *mcp.CallToolRequest, input ToggleLikeArgs) (*mcp.CallToolResult, any, error) {
	uid := uidFromCtx(ctx)
	if uid == "" {
		return nil, nil, gorm.ErrInvalidData
	}
	cur, err := s.store.IsLikedByUser(input.CatID, uid)
	if err != nil {
		return nil, nil, err
	}
	if err := s.store.SetLike(input.CatID, uid, !cur); err != nil {
		return nil, nil, err
	}
	n, _ := s.store.LikesCount(input.CatID)
	return nil, map[string]any{"likes": n, "liked": !cur}, nil
}

// addCatLocation adds a location and creates an automatic observation record.
type AddCatLocationArgs struct {
	CatID       string     `json:"cat_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Latitude    float64    `json:"lat"`
	Longitude   float64    `json:"lon"`
	CreatedAt   *time.Time `json:"created_at"`
}

func (s *Server) addCatLocation(ctx context.Context, request *mcp.CallToolRequest, in AddCatLocationArgs) (*mcp.CallToolResult, any, error) {
	if in.CatID == "" {
		return nil, nil, gorm.ErrMissingWhereClause
	}
	loc := storage.CatLocation{
		ID:          storage.NewUUID(),
		CatID:       in.CatID,
		Name:        in.Name,
		Description: in.Description,
		Latitude:    in.Latitude,
		Longitude:   in.Longitude,
	}
	if in.CreatedAt != nil {
		loc.CreatedAt = *in.CreatedAt
	} else {
		loc.CreatedAt = time.Now()
	}
	if err := s.store.DB.Create(&loc).Error; err != nil {
		return nil, nil, err
	}
	// Auto observation record
	uid := uidFromCtx(ctx)
	obs := storage.Record{
		ID:     storage.NewUUID(),
		CatID:  in.CatID,
		UserID: uid,
		Type:   "observation",
		Note: "Location updated: " +
			fmt.Sprintf("%.6f, %.6f", loc.Latitude, loc.Longitude),
		Timestamp: loc.CreatedAt,
		DoneAt:    &loc.CreatedAt,
	}
	// Add fmt import via fully qualified call requires fmt, so ensure import - use fmt.Sprintf here
	if err := s.store.DB.Create(&obs).Error; err == nil {
		// ignore errors silently for auto record
		s.updateCatLastSeenFromRecord(obs)
	}
	// Update Cat.LastSeen if this is fresh
	s.store.DB.Model(&storage.Cat{}).Where("id = ?", in.CatID).
		Where("last_seen IS NULL OR last_seen < ?", loc.CreatedAt).
		Update("last_seen", loc.CreatedAt)
	return nil, loc, nil
}

// addCatImageByURL attaches an external image URL to a cat.
type AddCatImageByURLArgs struct {
	CatID string `json:"cat_id"`
	URL   string `json:"url"`
	MIME  string `json:"mime"`
	Title string `json:"title"`
}

func (s *Server) addCatImageByURL(ctx context.Context, request *mcp.CallToolRequest, in AddCatImageByURLArgs) (*mcp.CallToolResult, any, error) {
	if in.CatID == "" || in.URL == "" {
		return nil, nil, gorm.ErrInvalidData
	}
	img := storage.Image{ID: storage.NewUUID(), CatID: in.CatID, URL: in.URL, MIME: in.MIME, Title: in.Title}
	if err := s.store.DB.Create(&img).Error; err != nil {
		return nil, nil, err
	}
	_ = s.store.DB.First(&img, "id = ?", img.ID).Error
	return nil, img, nil
}

// deleteImage deletes an image by id (optionally we could verify cat ownership on client side).
type DeleteImageArgs struct {
	ImageID string `json:"image_id"`
}

func (s *Server) deleteImage(ctx context.Context, request *mcp.CallToolRequest, in DeleteImageArgs) (*mcp.CallToolResult, any, error) {
	if in.ImageID == "" {
		return nil, nil, gorm.ErrInvalidData
	}
	// Return the image row prior to deletion if exists
	var img storage.Image
	if err := s.store.DB.First(&img, "id = ?", in.ImageID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, err
		}
	}
	if err := s.store.DB.Delete(&storage.Image{}, "id = ?", in.ImageID).Error; err != nil {
		return nil, nil, err
	}
	return nil, img, nil
}
