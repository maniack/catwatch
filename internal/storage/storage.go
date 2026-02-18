package storage

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/maniack/catwatch/internal/logging"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// User represents an application user (volunteer/caretaker).
type User struct {
	ID        string `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Provider   string `json:"provider"`
	ProviderID string `gorm:"uniqueIndex" json:"provider_id"`
	Email      string `gorm:"index" json:"email"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url"`
}

// Cat represents a homeless cat.
type Cat struct {
	ID        string         `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Color         string     `json:"color"`
	BirthDate     *time.Time `json:"birth_date,omitempty"`
	Gender        string     `json:"gender"` // male, female, unknown
	IsSterilized  bool       `json:"is_sterilized"`
	NeedAttention bool       `json:"need_attention"`
	Condition     int        `gorm:"type:integer;default:3" json:"condition"` // 1..5 scale

	LastSeen *time.Time `json:"last_seen,omitempty"`

	Locations []CatLocation `gorm:"constraint:OnDelete:CASCADE;" json:"locations"`

	Images  []Image  `gorm:"constraint:OnDelete:CASCADE;" json:"images"`
	Records []Record `gorm:"constraint:OnDelete:CASCADE;" json:"records"`
	Tags    []Tag    `gorm:"many2many:cat_tags;" json:"tags"`

	// Virtual fields for API (populated in handlers)
	Likes int64 `gorm:"-" json:"likes"`
	Liked bool  `gorm:"-" json:"liked"`
}

type Tag struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	Name string `gorm:"uniqueIndex" json:"name"`
}

// CatLocation represents a place where a cat was seen.
type CatLocation struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	CatID string `gorm:"type:char(36);index" json:"cat_id"`

	Name        string  `json:"name"`
	Description string  `json:"description"`
	Latitude    float64 `json:"lat"`
	Longitude   float64 `json:"lon"`
}

// Image represents a cat photo.
type Image struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	CatID string `gorm:"type:char(36);index" json:"cat_id"`
	URL   string `json:"url"`
	Data  []byte `json:"-"` // Optional: embedded image data (BLOB for SQLite, BYTEA for Postgres)
	MIME  string `json:"mime"`
	Title string `json:"title"`
	// Optimized marks that the optimizer has processed this image (resized/recompressed)
	Optimized bool `gorm:"index;default:false" json:"-"`
}

// Record represents a service event for a cat (feeding, medical, etc.)
type Record struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	CatID  string `gorm:"type:char(36);index" json:"cat_id"`
	UserID string `gorm:"type:char(36);index" json:"user_id"`
	User   User   `json:"user"`

	Type      string     `json:"type"` // feeding, medical, observation, sterilization, vaccination, vet_visit, medication
	Note      string     `json:"note"`
	Timestamp time.Time  `json:"timestamp"`
	PlannedAt *time.Time `json:"planned_at,omitempty"`
	DoneAt    *time.Time `json:"done_at,omitempty"`

	// Recurrence fields
	Recurrence string     `json:"recurrence,omitempty"` // daily, weekly, monthly
	Interval   int        `json:"interval,omitempty"`   // e.g. every 2 days
	EndDate    *time.Time `json:"end_date,omitempty"`
}

// AuditLog tracks all mutating actions.
// GDPR: Minimizing data. We keep only what's necessary for operation.
type AuditLog struct {
	ID         string    `gorm:"type:char(36);primaryKey" json:"id"`
	Timestamp  time.Time `gorm:"index" json:"ts"`
	UserID     string    `gorm:"type:char(36);index" json:"user_id"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	TargetType string    `gorm:"index" json:"target_type"`
	TargetID   string    `gorm:"index" json:"target_id"`
	Status     string    `json:"status"` // success, error
	RequestID  string    `json:"request_id"`
	Delta      string    `json:"delta"`
}

// BotLink associates a Telegram chat with a User.
type BotLink struct {
	ChatID    int64     `gorm:"primaryKey" json:"chat_id"`
	UserID    string    `gorm:"type:char(36);index" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type BotNotification struct {
	RecordID string    `gorm:"primaryKey;index" json:"record_id"`
	ChatID   int64     `gorm:"primaryKey;index" json:"chat_id"`
	SentAt   time.Time `json:"sent_at"`
}

type Setting struct {
	Key       string `gorm:"primaryKey" json:"key"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Value string `json:"value"`
}

// Like represents a like from a user to a cat.
type Like struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	CatID  string `gorm:"type:char(36);index:idx_cat_user,unique" json:"cat_id"`
	UserID string `gorm:"type:char(36);index:idx_cat_user,unique" json:"user_id"`
}

type Store struct {
	DB *gorm.DB
}

// Open initializes the database (SQLite or PostgreSQL based on DSN) and runs auto-migrations.
// If the provided string looks like a PostgreSQL DSN (starts with postgres:// or postgresql://,
// or contains key=val pairs like host=/user=/dbname=), Postgres driver will be used.
// Otherwise, it's treated as a SQLite path/DSN.
func Open(dsn string) (*Store, error) {
	log := logging.L()

	isPg := isPostgresDSN(dsn)
	var db *gorm.DB
	var err error
	if isPg {
		log.Infof("Opening PostgreSQL database...")
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logging.NewGormLogger(log, 100*time.Millisecond)})
	} else {
		log.Infof("Opening SQLite database (path: %s)...", dsn)
		// default to SQLite (supports file paths and memory dsn like file::memory:?cache=shared)
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logging.NewGormLogger(log, 100*time.Millisecond)})
	}
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	if !isPg {
		// SQLite works best with a single writer connection
		sqlDB.SetMaxOpenConns(1)
	}

	if err = db.AutoMigrate(
		&User{},
		&Cat{},
		&Tag{},
		&CatLocation{},
		&Image{},
		&Record{},
		&BotNotification{},
		&AuditLog{},
		&BotLink{},
		&Setting{},
		&Like{},
	); err != nil {
		return nil, fmt.Errorf("auto-migrate: %w", err)
	}
	log.Infof("Database auto-migration completed successfully")

	return &Store{DB: db}, nil
}

func isPostgresDSN(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://") {
		return true
	}
	// Key-value DSN commonly used by lib/pq/pgx: host=... user=... dbname=...
	if strings.Contains(s, "host=") || strings.Contains(s, "user=") || strings.Contains(s, "dbname=") {
		return true
	}
	return false
}

// NewUUID generates a new UUID v4.
func NewUUID() string {
	return uuid.New().String()
}

func (s *Store) GetJWTSecret() (string, error) {
	var sett Setting
	if err := s.DB.First(&sett, "key = ?", "jwt_secret").Error; err != nil {
		return "", err
	}
	return sett.Value, nil
}

func (s *Store) SaveJWTSecret(secret string) error {
	sett := Setting{
		Key:   "jwt_secret",
		Value: secret,
	}
	return s.DB.Save(&sett).Error
}

func (s *Store) FindOrCreateUser(provider, providerID, email, name, avatar string) (*User, error) {
	if provider == "" || providerID == "" {
		return nil, fmt.Errorf("provider and providerID required")
	}
	u := &User{Provider: provider, ProviderID: providerID}
	res := s.DB.Where("provider = ? AND provider_id = ?", provider, providerID).First(u)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		u.ID = NewUUID()
		u.Email = email
		u.Name = name
		u.AvatarURL = avatar
		if err := s.DB.Create(u).Error; err != nil {
			return nil, err
		}
		return u, nil
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return u, nil
}

func (s *Store) LinkBotChat(chatID int64, userID string) error {
	link := BotLink{
		ChatID: chatID,
		UserID: userID,
	}
	return s.DB.Save(&link).Error
}

func (s *Store) GetBotLink(chatID int64) (*BotLink, error) {
	var link BotLink
	if err := s.DB.First(&link, "chat_id = ?", chatID).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *Store) UnlinkBotChat(chatID int64) error {
	return s.DB.Delete(&BotLink{}, "chat_id = ?", chatID).Error
}

// Optimizer helpers and image maintenance
// ListImagesToOptimize returns up to 'limit' images that haven't been optimized yet.
func (s *Store) ListImagesToOptimize(limit int) ([]Image, error) {
	var imgs []Image
	err := s.DB.Where("optimized = ? OR optimized IS NULL", false).Order("created_at").Limit(limit).Find(&imgs).Error
	return imgs, err
}

// MarkImageOptimizedEmpty marks an image as optimized when there is no image data to process.
func (s *Store) MarkImageOptimizedEmpty(id string) error {
	return s.DB.Model(&Image{}).Where("id = ?", id).Update("optimized", true).Error
}

// MarkImageOptimizedNoChange marks an image as optimized without changing its data.
func (s *Store) MarkImageOptimizedNoChange(id string) error {
	return s.DB.Model(&Image{}).Where("id = ?", id).Updates(map[string]any{"optimized": true, "updated_at": time.Now()}).Error
}

// UpdateImageOptimizedData stores optimized image bytes and MIME, marking it optimized.
func (s *Store) UpdateImageOptimizedData(id string, data []byte, mime string) error {
	return s.DB.Model(&Image{}).Where("id = ?", id).Updates(map[string]any{"data": data, "mime": mime, "optimized": true, "updated_at": time.Now()}).Error
}

// PruneOldCatImages keeps only the newest 'keepN' images for the given cat and deletes the rest.
// Returns the number of deleted images and an error if occurred.
func (s *Store) PruneOldCatImages(catID string, keepN int) (int64, error) {
	if keepN <= 0 {
		keepN = 5
	}
	// Select IDs ordered by CreatedAt desc, keep first keepN
	var ids []string
	if err := s.DB.Model(&Image{}).Where("cat_id = ?", catID).Order("created_at DESC, id DESC").Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) <= keepN {
		return 0, nil
	}
	toDelete := ids[keepN:]
	res := s.DB.Where("id IN ?", toDelete).Delete(&Image{})
	return res.RowsAffected, res.Error
}

// PruneAllCatsImages iterates over all cats and prunes images to keep 'keepN' each.
// Returns total deleted count.
func (s *Store) PruneAllCatsImages(keepN int) (int64, error) {
	var catIDs []string
	if err := s.DB.Model(&Cat{}).Order("id").Pluck("id", &catIDs).Error; err != nil {
		return 0, err
	}
	var total int64
	for _, id := range catIDs {
		n, err := s.PruneOldCatImages(id, keepN)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// Like helpers

func (s *Store) IsLikedByUser(catID, userID string) (bool, error) {
	var n int64
	err := s.DB.Model(&Like{}).Where("cat_id = ? AND user_id = ?", catID, userID).Count(&n).Error
	return n > 0, err
}

func (s *Store) SetLike(catID, userID string, like bool) error {
	if like {
		l := &Like{ID: NewUUID(), CatID: catID, UserID: userID}
		return s.DB.Create(l).Error
	}
	return s.DB.Where("cat_id = ? AND user_id = ?", catID, userID).Delete(&Like{}).Error
}

func (s *Store) LikesCount(catID string) (int64, error) {
	var n int64
	err := s.DB.Model(&Like{}).Where("cat_id = ?", catID).Count(&n).Error
	return n, err
}

func (s *Store) GetUserLikedCats(userID string) ([]Cat, error) {
	var cats []Cat
	err := s.DB.Joins("JOIN likes ON likes.cat_id = cats.id").
		Where("likes.user_id = ?", userID).
		Preload("Images").Preload("Tags").
		Find(&cats).Error
	return cats, err
}

func (s *Store) GetUserAuditLogs(userID string, limit int) ([]AuditLog, error) {
	var logs []AuditLog
	err := s.DB.Where("user_id = ?", userID).
		Order("timestamp DESC").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

func (s *Store) PruneAuditLogs(before time.Time) (int64, error) {
	res := s.DB.Where("timestamp < ?", before).Delete(&AuditLog{})
	return res.RowsAffected, res.Error
}

func (s *Store) DeleteUser(userID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Delete related data first
		if err := tx.Where("user_id = ?", userID).Delete(&Like{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&BotLink{}).Error; err != nil {
			return err
		}
		// Records and AuditLogs are kept but de-identified
		if err := tx.Model(&Record{}).Where("user_id = ?", userID).Update("user_id", "").Error; err != nil {
			return err
		}
		// Finally delete the user
		if err := tx.Delete(&User{}, "id = ?", userID).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) GetUserExport(userID string) (map[string]any, error) {
	var user User
	if err := s.DB.First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}

	var likes []Like
	s.DB.Where("user_id = ?", userID).Find(&likes)

	var botLinks []BotLink
	s.DB.Where("user_id = ?", userID).Find(&botLinks)

	var records []Record
	s.DB.Where("user_id = ?", userID).Find(&records)

	var auditLogs []AuditLog
	s.DB.Where("user_id = ?", userID).Order("timestamp DESC").Find(&auditLogs)

	return map[string]any{
		"user":       user,
		"likes":      likes,
		"bot_links":  botLinks,
		"records":    records,
		"audit_logs": auditLogs,
	}, nil
}
