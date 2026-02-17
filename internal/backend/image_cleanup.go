package backend

import (
	"time"

	"github.com/maniack/catwatch/internal/monitoring"
)

// startImageCleanup launches a periodic cleanup that keeps only last N images per cat.
func (s *Server) startImageCleanup(keepN int, interval time.Duration) {
	if keepN <= 0 {
		keepN = 5
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	s.log.WithField("keep", keepN).WithField("interval", interval.String()).Info("images: starting cleanup worker")
	go func() {
		for {
			deleted, err := s.store.PruneAllCatsImages(keepN)
			if err != nil {
				s.log.WithError(err).Warn("images: cleanup failed")
			} else if deleted > 0 {
				s.log.WithField("deleted", deleted).Info("images: cleanup pruned old images")
				for i := int64(0); i < deleted; i++ {
					monitoring.ImagesPrunedTotal.Inc()
				}
			}
			time.Sleep(interval)
		}
	}()
}
