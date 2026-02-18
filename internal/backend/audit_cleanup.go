package backend

import (
	"time"
)

// startAuditLogCleanup launches a periodic cleanup of the AuditLog table based on TTL.
func (s *Server) startAuditLogCleanup(interval time.Duration) {
	ttl := s.cfg.AuditLogTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour // Default 30 days
	}
	if interval <= 0 {
		interval = 1 * time.Hour
	}

	s.log.WithField("ttl", ttl.String()).WithField("interval", interval.String()).Info("audit: starting cleanup worker")
	go func() {
		for {
			before := time.Now().Add(-ttl)
			deleted, err := s.store.PruneAuditLogs(before)
			if err != nil {
				s.log.WithError(err).Warn("audit: cleanup failed")
			} else if deleted > 0 {
				s.log.WithField("deleted", deleted).Infof("audit: cleaned up old logs (older than %s)", before.Format(time.RFC3339))
			}
			time.Sleep(interval)
		}
	}()
}
