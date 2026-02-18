package backend

import (
	"time"

	"github.com/maniack/catwatch/internal/storage"
)

type PublicCat struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description,omitempty"`
	Color         string                `json:"color,omitempty"`
	BirthDate     *time.Time            `json:"birth_date,omitempty"`
	Gender        string                `json:"gender"`
	IsSterilized  bool                  `json:"is_sterilized"`
	Condition     int                   `json:"condition"`
	NeedAttention bool                  `json:"need_attention"`
	Tags          []storage.Tag         `json:"tags,omitempty"`
	LastSeen      *time.Time            `json:"last_seen,omitempty"`
	Locations     []storage.CatLocation `json:"locations,omitempty"`
	Images        []storage.Image       `json:"images,omitempty"`
	Likes         int64                 `json:"likes"`
	Liked         bool                  `json:"liked"`
	CreatedAt     time.Time             `json:"created_at"`
	Records       any                   `json:"records,omitempty"`
}

type PublicRecord struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Timestamp time.Time  `json:"timestamp"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

func ToPublicCat(c storage.Cat) PublicCat {
	return PublicCat{
		ID:            c.ID,
		Name:          c.Name,
		Description:   c.Description,
		Color:         c.Color,
		BirthDate:     c.BirthDate,
		Gender:        c.Gender,
		IsSterilized:  c.IsSterilized,
		Condition:     c.Condition,
		NeedAttention: c.NeedAttention,
		Tags:          c.Tags,
		LastSeen:      c.LastSeen,
		Locations:     c.Locations,
		Images:        c.Images,
		Likes:         c.Likes,
		Liked:         c.Liked,
		CreatedAt:     c.CreatedAt,
	}
}

func ToPublicRecord(r storage.Record) PublicRecord {
	return PublicRecord{
		ID:        r.ID,
		Type:      r.Type,
		Timestamp: r.Timestamp,
		DoneAt:    r.DoneAt,
	}
}
