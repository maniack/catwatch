package backend

import (
	"time"

	"github.com/maniack/catwatch/internal/monitoring"
	"github.com/maniack/catwatch/internal/storage"
)

// startCatConditionCollector periodically exports cat conditions to Prometheus gauges
func (s *Server) startCatConditionCollector(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s.log.WithField("interval", interval.String()).Info("metrics: starting cat condition collector")
	go func() {
		for {
			var cats []storage.Cat
			if err := s.store.DB.Select("id, name, condition").Find(&cats).Error; err == nil {
				for _, c := range cats {
					monitoring.SetCatCondition(c.ID, c.Name, c.Condition)
				}
			} else {
				s.log.WithError(err).Warn("metrics: failed to load cats for condition export")
			}
			time.Sleep(interval)
		}
	}()
}
