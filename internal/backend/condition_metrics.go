package backend

import (
	"time"

	"github.com/maniack/catwatch/internal/monitoring"
	"github.com/maniack/catwatch/internal/storage"
)

// startCatMetricsCollector periodically exports cat metrics (condition, likes) to Prometheus gauges
func (s *Server) startCatMetricsCollector(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s.log.WithField("interval", interval.String()).Info("metrics: starting cat metrics collector")
	go func() {
		for {
			var cats []storage.Cat
			if err := s.store.DB.Select("id, condition").Find(&cats).Error; err == nil {
				for _, c := range cats {
					monitoring.SetCatCondition(c.ID, c.Condition)
				}

				// Export likes
				type likesResult struct {
					CatID string
					Count int
				}
				var lcounts []likesResult
				if err := s.store.DB.Table("likes").Select("cat_id, count(*) as count").Group("cat_id").Scan(&lcounts).Error; err == nil {
					likesMap := make(map[string]int)
					for _, r := range lcounts {
						likesMap[r.CatID] = r.Count
					}
					for _, c := range cats {
						monitoring.SetCatLikes(c.ID, likesMap[c.ID])
					}
				} else {
					s.log.WithError(err).Warn("metrics: failed to load likes for export")
				}
			} else {
				s.log.WithError(err).Warn("metrics: failed to load cats for metrics export")
			}
			time.Sleep(interval)
		}
	}()
}
