package backend

import (
	"time"

	"github.com/maniack/catwatch/internal/storage"
)

type PublicCat struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description,omitempty"`
	Condition     int                   `json:"condition"`
	NeedAttention bool                  `json:"need_attention"`
	Tags          []storage.Tag         `json:"tags,omitempty"`
	LastSeen      *time.Time            `json:"last_seen,omitempty"`
	Locations     []storage.CatLocation `json:"locations,omitempty"`
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
		Condition:     c.Condition,
		NeedAttention: c.NeedAttention,
		Tags:          c.Tags,
		LastSeen:      c.LastSeen,
		Locations:     c.Locations,
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
