package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/maniack/catwatch/internal/l10n"
	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/monitoring"
	"github.com/maniack/catwatch/internal/storage"
)

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	lang := "en"
	if l, ok := r.Context().Value(logging.ContextLang).(string); ok {
		lang = l
	}

	data := struct {
		Lang          string
		GoogleEnabled bool
		OIDCEnabled   bool
		DevEnabled    bool
		Query         string
	}{
		Lang:          lang,
		GoogleEnabled: s.cfg.OAuth.GoogleClientID != "" && s.cfg.OAuth.GoogleClientSecret != "" && s.cfg.OAuth.GoogleRedirectURL != "",
		OIDCEnabled:   s.cfg.OAuth.OIDCIssuer != "" && s.cfg.OAuth.OIDCClientID != "" && s.cfg.OAuth.OIDCRedirectURL != "",
		DevEnabled:    s.cfg.DevLoginEnabled,
		Query:         r.URL.RawQuery,
	}

	tmpl := `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login - CatWatch</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background-color: #f9fafb; margin: 0; padding: 20px; }
        .card { background: white; padding: 2.5rem; border-radius: 16px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); width: 100%; max-width: 400px; text-align: center; }
        .logo { font-size: 3rem; margin-bottom: 1rem; }
        h2 { margin-bottom: 1.5rem; color: #111827; font-size: 1.5rem; }
        .btn { display: flex; align-items: center; justify-content: center; width: 100%; padding: 14px; margin-bottom: 16px; border-radius: 10px; text-decoration: none; font-weight: 600; text-align: center; border: none; cursor: pointer; transition: transform 0.1s; }
        .btn:active { transform: scale(0.98); }
        .btn-google { background-color: #4285F4; color: white; }
        .btn-oidc { background-color: #6366f1; color: white; }
        .btn-dev { background-color: #ef4444; color: white; }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">🐈‍⬛</div>
        <h2>CatWatch Login</h2>
        {{if .GoogleEnabled}}
        <a href="/auth/google/login?{{.Query}}" class="btn btn-google">Login with Google</a>
        {{end}}
        {{if .OIDCEnabled}}
        <a href="/auth/oidc/login?{{.Query}}" class="btn btn-oidc">Login with OIDC</a>
        {{end}}
        {{if .DevEnabled}}
        <a href="/auth/dev/login?{{.Query}}" class="btn btn-dev">Login with Dev User</a>
        {{end}}
        {{if not (or .GoogleEnabled .OIDCEnabled .DevEnabled)}}
        <p>No login methods enabled.</p>
        {{end}}
    </div>
</body>
</html>`

	t, _ := htmltemplate.New("login").Parse(tmpl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = t.Execute(w, data)
}

func (s *Server) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DevLoginEnabled {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "dev login disabled"})
		return
	}
	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := jsonNewDecoder(r).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email required"})
		return
	}

	name := req.Name
	if name == "" {
		name = req.Email
	}

	u, err := s.store.FindOrCreateUser("dev", req.Email, req.Email, name, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create user: " + err.Error()})
		return
	}

	if err := s.issueTokens(w, r, u.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) handleDevLoginGET(w http.ResponseWriter, r *http.Request) {
	lang := "en"
	if l, ok := r.Context().Value(logging.ContextLang).(string); ok {
		lang = l
	}

	if !s.cfg.DevLoginEnabled {
		http.Error(w, "dev login disabled", http.StatusForbidden)
		return
	}
	email := r.URL.Query().Get("email")
	if email == "" {
		email = "dev@catwatch.local"
	}
	tgChatID, _ := strconv.ParseInt(r.URL.Query().Get("tg_chat_id"), 10, 64)

	u, err := s.store.FindOrCreateUser("dev", email, email, email, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if tgChatID != 0 {
		_ = s.store.LinkBotChat(tgChatID, u.ID)
	}

	// Issue tokens (mainly for cookies if user visits via browser)
	_ = s.issueTokens(w, r, u.ID)

	if tgChatID != 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		title := l10n.T(lang, "auth_success_title")
		msg := l10n.T(lang, "auth_success_msg", map[string]int64{"ChatID": tgChatID})
		fmt.Fprintf(w, "<h2>%s</h2><p>%s</p>", title, msg)
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (s *Server) listCats(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	var cats []storage.Cat
	if err := s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").Order("created_at DESC").Find(&cats).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	out := make([]PublicCat, len(cats))
	for i, c := range cats {
		pc := ToPublicCat(c)
		pc.Likes, _ = s.store.LikesCount(c.ID)
		if uid != "" {
			pc.Liked, _ = s.store.IsLikedByUser(c.ID, uid)
		}
		out[i] = pc
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCat(w http.ResponseWriter, r *http.Request) {
	var in storage.Cat
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
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

	// Handle tags
	for i := range in.Tags {
		if in.Tags[i].ID == "" {
			// Find existing tag by name or create new
			var existing storage.Tag
			if err := s.store.DB.Where("name = ?", in.Tags[i].Name).First(&existing).Error; err == nil {
				in.Tags[i] = existing
			} else {
				in.Tags[i].ID = storage.NewUUID()
			}
		}
	}

	if err := s.store.DB.Create(&in).Error; err != nil {
		s.LogAudit(r, "cat", in.ID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.LogAudit(r, "cat", in.ID, "success", "create")
	writeJSON(w, http.StatusCreated, ToPublicCat(in))
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

func (s *Server) getCat(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	var cat storage.Cat
	if err := s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").Preload("Records").First(&cat, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	pc := ToPublicCat(cat)
	pc.Likes, _ = s.store.LikesCount(cat.ID)
	if uid != "" {
		pc.Liked, _ = s.store.IsLikedByUser(cat.ID, uid)
		pc.Records = cat.Records
	} else {
		// Anonymous users can only see done records
		var publicRecs []PublicRecord
		for _, rec := range cat.Records {
			if rec.DoneAt != nil {
				publicRecs = append(publicRecs, ToPublicRecord(rec))
			}
		}
		pc.Records = publicRecs
	}

	writeJSON(w, http.StatusOK, pc)
}

func (s *Server) updateCat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in storage.Cat
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	in.ID = id
	// normalize condition 1..5 (default 3)
	if in.Condition < 1 || in.Condition > 5 {
		in.Condition = 3
	}
	if in.Condition > 0 && in.Condition < 3 {
		in.NeedAttention = true
	}

	// Handle tags
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

	// Full save to handle many-to-many tags correctly
	if err := s.store.DB.Save(&in).Error; err != nil {
		s.LogAudit(r, "cat", id, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.LogAudit(r, "cat", id, "success", "update")
	var out storage.Cat
	_ = s.store.DB.Preload("Locations").Preload("Images").Preload("Tags").First(&out, "id = ?", id).Error

	uid, _ := UserIDFromCtx(r.Context())
	pc := ToPublicCat(out)
	pc.Likes, _ = s.store.LikesCount(out.ID)
	if uid != "" {
		pc.Liked, _ = s.store.IsLikedByUser(out.ID, uid)
	}
	writeJSON(w, http.StatusOK, pc)
}

func (s *Server) deleteCat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DB.Delete(&storage.Cat{}, "id = ?", id).Error; err != nil {
		s.LogAudit(r, "cat", id, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.LogAudit(r, "cat", id, "success", "delete")
	w.WriteHeader(http.StatusNoContent)
}

// addCatImage uploads a new image for a cat. Supports multipart/form-data (file) or JSON {"url":"..."}.
func (s *Server) addCatImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Ensure cat exists
	var cat storage.Cat
	if err := s.store.DB.First(&cat, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "cat not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	ct := r.Header.Get("Content-Type")
	img := storage.Image{ID: storage.NewUUID(), CatID: id}
	if strings.HasPrefix(ct, "multipart/form-data") {
		// Limit memory usage for multipart parsing
		if err := r.ParseMultipartForm(12 << 20); err != nil { // 12MB
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart: " + err.Error()})
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field required"})
			return
		}
		defer file.Close()
		data := make([]byte, hdr.Size)
		n, _ := file.Read(data)
		data = data[:n]
		img.Data = data
		if m := http.DetectContentType(data); m != "application/octet-stream" {
			img.MIME = m
		}
		img.URL = ""
	} else {
		// Try JSON with URL
		var in struct {
			URL string `json:"url"`
		}
		if err := jsonNewDecoder(r).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported content type or bad json"})
			return
		}
		if in.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url required"})
			return
		}
		img.URL = in.URL
	}
	if err := s.store.DB.Create(&img).Error; err != nil {
		s.LogAudit(r, "image", img.ID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Reload with timestamps
	_ = s.store.DB.First(&img, "id = ?", img.ID).Error

	s.LogAudit(r, "image", img.ID, "success", "create")
	writeJSON(w, http.StatusCreated, img)
}

// deleteCatImage removes an image by id for a cat.
func (s *Server) deleteCatImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	imgID := chi.URLParam(r, "imgId")

	// Ensure image exists and optionally belongs to cat
	db := s.store.DB.Model(&storage.Image{}).Where("id = ?", imgID)
	if id != "" {
		db = db.Where("cat_id = ?", id)
	}

	var img storage.Image
	if err := db.First(&img).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := s.store.DB.Delete(&storage.Image{}, "id = ?", imgID).Error; err != nil {
		s.LogAudit(r, "image", imgID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.LogAudit(r, "image", imgID, "success", "delete")
	writeJSON(w, http.StatusOK, img)
}

func (s *Server) addCatLocation(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	catID := chi.URLParam(r, "id")
	var loc storage.CatLocation
	if err := jsonNewDecoder(r).Decode(&loc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if loc.ID == "" {
		loc.ID = storage.NewUUID()
	}
	loc.CatID = catID
	if loc.CreatedAt.IsZero() {
		loc.CreatedAt = time.Now()
	}

	if err := s.store.DB.Create(&loc).Error; err != nil {
		s.LogAudit(r, "location", loc.ID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.LogAudit(r, "location", loc.ID, "success", "create")

	// Create an observation record as well
	obsRec := storage.Record{
		ID:        storage.NewUUID(),
		CatID:     catID,
		UserID:    uid,
		Type:      "observation",
		Note:      fmt.Sprintf("Location updated: %.6f, %.6f", loc.Latitude, loc.Longitude),
		Timestamp: loc.CreatedAt,
		DoneAt:    &loc.CreatedAt,
	}
	if err := s.store.DB.Create(&obsRec).Error; err != nil {
		s.log.WithError(err).Warn("failed to create automatic observation record for location")
	} else {
		s.LogAudit(r, "record", obsRec.ID, "success", "create-auto-loc")
		monitoring.IncRecord(obsRec.Type, catID)
	}

	// Also update Cat.LastSeen if this location is fresh
	s.store.DB.Model(&storage.Cat{}).Where("id = ?", catID).
		Where("last_seen IS NULL OR last_seen < ?", loc.CreatedAt).
		Update("last_seen", loc.CreatedAt)

	writeJSON(w, http.StatusCreated, loc)
}

func (s *Server) updateRecord(w http.ResponseWriter, r *http.Request) {
	catID := chi.URLParam(r, "id")
	rid := chi.URLParam(r, "rid")
	var in storage.Record
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	in.ID = rid
	in.CatID = catID
	if err := s.store.DB.Model(&storage.Record{ID: rid}).Updates(in).Error; err != nil {
		s.LogAudit(r, "record", rid, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.LogAudit(r, "record", rid, "success", "update")
	var out storage.Record
	if err := s.store.DB.First(&out, "id = ? AND cat_id = ?", rid, catID).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Update LastSeen
	s.updateCatLastSeenFromRecord(out)

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) markRecordDone(w http.ResponseWriter, r *http.Request) {
	catID := chi.URLParam(r, "id")
	rid := chi.URLParam(r, "rid")
	now := time.Now()

	// Handle virtual records from recurring events
	if strings.HasPrefix(rid, "virtual-") {
		// virtual-originalID-date
		idAndDate := strings.TrimPrefix(rid, "virtual-")
		lastDash := strings.LastIndex(idAndDate, "-")
		if lastDash != -1 {
			originalID := idAndDate[:lastDash]
			dateStr := idAndDate[lastDash+1:]

			var orig storage.Record
			if err := s.store.DB.First(&orig, "id = ?", originalID).Error; err == nil {
				// Create a real record for this occurrence
				occurrenceDate, _ := time.Parse("20060102", dateStr)
				// keep original time
				plannedAt := time.Date(occurrenceDate.Year(), occurrenceDate.Month(), occurrenceDate.Day(),
					orig.PlannedAt.Hour(), orig.PlannedAt.Minute(), orig.PlannedAt.Second(), orig.PlannedAt.Nanosecond(), orig.PlannedAt.Location())

				newRec := orig
				newRec.ID = storage.NewUUID()
				newRec.PlannedAt = &plannedAt
				newRec.DoneAt = &now
				newRec.Recurrence = "" // this instance is done, no recurrence
				newRec.CreatedAt = now

				if err := s.store.DB.Create(&newRec).Error; err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create record instance: " + err.Error()})
					return
				}
				s.LogAudit(r, "record", newRec.ID, "success", "done-virtual")
				monitoring.IncRecord(newRec.Type, newRec.CatID)
				s.updateCatLastSeenFromRecord(newRec)
				writeJSON(w, http.StatusOK, newRec)
				return
			}
		}
	}

	db := s.store.DB.Model(&storage.Record{}).Where("id = ?", rid)
	if catID != "" {
		db = db.Where("cat_id = ?", catID)
	}

	if err := db.Update("done_at", &now).Error; err != nil {
		s.LogAudit(r, "record", rid, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.LogAudit(r, "record", rid, "success", "done")
	var out storage.Record
	if err := s.store.DB.First(&out, "id = ?", rid).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Update LastSeen
	s.updateCatLastSeenFromRecord(out)

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	catID := chi.URLParam(r, "id")
	status := r.URL.Query().Get("status") // planned|done|all("")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var recs []storage.Record

	db := s.store.DB.Model(&storage.Record{}).Where("cat_id = ?", catID).Preload("User")
	if uid == "" {
		// Anonymous users can only see done records
		db = db.Where("done_at IS NOT NULL")
	}

	switch status {
	case "planned":
		if uid == "" {
			writeJSON(w, http.StatusOK, []PublicRecord{})
			return
		}
		db = db.Where("planned_at IS NOT NULL AND done_at IS NULL").Order("planned_at ASC")
	case "done":
		db = db.Where("done_at IS NOT NULL").Order("COALESCE(done_at, created_at) DESC")
	default:
		db = db.Order("created_at DESC")
	}
	if err := db.Find(&recs).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// If start/end provided, expand recurring records
	if uid != "" && startStr != "" && endStr != "" {
		start, startErr := time.Parse(time.RFC3339, startStr)
		end, endErr := time.Parse(time.RFC3339, endStr)
		if startErr == nil && endErr == nil {
			recs = s.expandRecurringRecords(recs, start, end)
		}
	}

	if uid == "" {
		publicRecs := make([]PublicRecord, len(recs))
		for i, rec := range recs {
			publicRecs[i] = ToPublicRecord(rec)
		}
		writeJSON(w, http.StatusOK, publicRecs)
		return
	}

	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) expandRecurringRecords(recs []storage.Record, start, end time.Time) []storage.Record {
	var expanded []storage.Record
	for _, r := range recs {
		// Add the original record if it's within range
		if isWithinRange(r, start, end) {
			expanded = append(expanded, r)
		}

		if r.Recurrence == "" || r.PlannedAt == nil || r.DoneAt != nil {
			continue
		}

		// Generate virtual occurrences starting from PlannedAt
		curr := *r.PlannedAt
		interval := r.Interval
		if interval <= 0 {
			interval = 1
		}

		count := 0
		for {
			// Save current position
			prev := curr

			switch r.Recurrence {
			case "daily":
				curr = curr.AddDate(0, 0, interval)
			case "weekly":
				curr = curr.AddDate(0, 0, 7*interval)
			case "monthly":
				curr = curr.AddDate(0, interval, 0)
			case "yearly":
				curr = curr.AddDate(interval, 0, 0)
			default:
				goto nextRecord
			}

			// Infinite loop protection if AddDate doesn't advance (shouldn't happen with positive interval)
			if !curr.After(prev) {
				break
			}

			// If current occurrence is past the end of the search range or past the record's EndDate, stop
			if curr.After(end) || (r.EndDate != nil && curr.After(*r.EndDate)) {
				break
			}

			// If current occurrence is within the search range, add it
			if curr.After(start) || curr.Equal(start) {
				// Create a virtual instance
				instance := r
				instance.ID = "virtual-" + r.ID + "-" + curr.Format("20060102")
				newPlanned := curr
				instance.PlannedAt = &newPlanned
				expanded = append(expanded, instance)
				count++
			}

			// Safety break to prevent infinite loops (or too many occurrences)
			if count > 1000 || curr.Year() > 2100 {
				break
			}
		}
	nextRecord:
	}
	return expanded
}

func isWithinRange(r storage.Record, start, end time.Time) bool {
	t := r.Timestamp
	if r.PlannedAt != nil {
		t = *r.PlannedAt
	}
	if r.DoneAt != nil {
		t = *r.DoneAt
	}
	return (t.After(start) || t.Equal(start)) && (t.Before(end) || t.Equal(end))
}

func (s *Server) createRecord(w http.ResponseWriter, r *http.Request) {
	uid, _ := UserIDFromCtx(r.Context())
	catID := chi.URLParam(r, "id")
	var in storage.Record
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if in.ID == "" {
		in.ID = storage.NewUUID()
	}
	in.CatID = catID
	in.UserID = uid
	// If neither planned nor done provided, treat as immediate event and set timestamp
	if in.PlannedAt == nil && in.DoneAt == nil && in.Timestamp.IsZero() {
		in.Timestamp = time.Now()
	}
	if err := s.store.DB.Create(&in).Error; err != nil {
		s.LogAudit(r, "record", in.ID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.LogAudit(r, "record", in.ID, "success", "create")
	monitoring.IncRecord(in.Type, catID)
	// Update LastSeen if record is done or has timestamp
	s.updateCatLastSeenFromRecord(in)

	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) listAllPlannedRecords(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var recs []storage.Record
	// Only planned records (planned_at set, done_at null)
	db := s.store.DB.Model(&storage.Record{}).Where("planned_at IS NOT NULL AND done_at IS NULL").Preload("User")

	if err := db.Find(&recs).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if startStr != "" && endStr != "" {
		start, startErr := time.Parse(time.RFC3339, startStr)
		end, endErr := time.Parse(time.RFC3339, endStr)
		if startErr == nil && endErr == nil {
			recs = s.expandRecurringRecords(recs, start, end)
		}
	}

	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) registerBotUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChatID int64  `json:"chat_id"`
		Name   string `json:"name"`
	}
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	providerID := fmt.Sprintf("%d", in.ChatID)
	var user storage.User
	err := s.store.DB.Where("provider = ? AND provider_id = ?", "telegram", providerID).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			user = storage.User{
				ID:         storage.NewUUID(),
				Provider:   "telegram",
				ProviderID: providerID,
				Name:       in.Name,
			}
			if err := s.store.DB.Create(&user).Error; err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		// Update name if changed
		if user.Name != in.Name {
			user.Name = in.Name
			s.store.DB.Save(&user)
		}
	}

	writeJSON(w, http.StatusOK, user)
}

func (s *Server) markNotificationSent(w http.ResponseWriter, r *http.Request) {
	var in storage.BotNotification
	if err := jsonNewDecoder(r).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if in.SentAt.IsZero() {
		in.SentAt = time.Now()
	}

	// Use Create to fail if already exists
	if err := s.store.DB.Create(&in).Error; err != nil {
		// Check if it's a duplicate key error
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already sent or database error"})
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) listBotUsers(w http.ResponseWriter, r *http.Request) {
	// We want to return all users that should receive notifications.
	// This includes users with provider='telegram' AND users linked via BotLink.

	var users []storage.User
	// 1. Users from 'telegram' provider
	if err := s.store.DB.Where("provider = ?", "telegram").Find(&users).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 2. Users linked via BotLink (those who logged in via Google/OIDC in bot)
	var linkedUsers []storage.User
	if err := s.store.DB.Joins("JOIN bot_links ON bot_links.user_id = users.id").Find(&linkedUsers).Error; err == nil {
		// Append unique users
		seen := make(map[string]bool)
		for _, u := range users {
			seen[u.ProviderID] = true
		}
		for _, u := range linkedUsers {
			// Find corresponding chat_id for this user
			var link storage.BotLink
			if err := s.store.DB.First(&link, "user_id = ?", u.ID).Error; err == nil {
				chatIDStr := fmt.Sprintf("%d", link.ChatID)
				if !seen[chatIDStr] {
					// We need to return storage.User where ProviderID is chat_id for the reminder loop to work
					u.ProviderID = chatIDStr
					users = append(users, u)
					seen[chatIDStr] = true
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, users)
}

func (s *Server) getCatImageBinary(w http.ResponseWriter, r *http.Request) {
	imgID := chi.URLParam(r, "imgId")
	var img storage.Image
	if err := s.store.DB.First(&img, "id = ?", imgID).Error; err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}
	if len(img.Data) == 0 {
		http.Error(w, "no data", http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", img.MIME)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	_, _ = w.Write(img.Data)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	f, err := s.assets.Open("index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl, err := htmltemplate.New("index").Parse(string(data))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	copyright := fmt.Sprintf("CatWatch © %d", time.Now().Year())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = tmpl.Execute(w, map[string]any{
		"Version":   s.cfg.Version,
		"Copyright": copyright,
		"Dev":       s.cfg.DevLoginEnabled,
		"Canonical": baseURL + r.URL.Path,
		"OGURL":     baseURL + r.URL.Path,
		"OGImage":   baseURL + "/images/cat_watch_icon.png", // We should have one
	})
	if err != nil {
		s.log.WithError(err).Error("failed to execute index template")
	}
}

func (s *Server) handleToggleLike(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFromCtx(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	cur, err := s.store.IsLikedByUser(id, uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	if err := s.store.SetLike(id, uid, !cur); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}
	n, _ := s.store.LikesCount(id)
	writeJSON(w, http.StatusOK, map[string]any{"likes": n, "liked": !cur})
}

// minimal local json decoder helper with sane defaults
func jsonNewDecoder(r *http.Request) *json.Decoder {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec
}
