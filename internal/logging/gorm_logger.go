package logging

import (
	"context"
	"errors"
	"time"

	chmw "github.com/go-chi/chi/v5/middleware"
	"github.com/sirupsen/logrus"
	gormlogger "gorm.io/gorm/logger"
)

// NewGormLogger creates a GORM logger that forwards messages into the provided logrus logger.
// It maps GORM levels to logrus levels and respects the current logrus level for verbosity.
func NewGormLogger(l *logrus.Logger, slowThreshold time.Duration) gormlogger.Interface {
	lvl := gormlogger.Warn
	if l.IsLevelEnabled(logrus.DebugLevel) {
		// In debug mode, show full SQL traces
		lvl = gormlogger.Info
	}
	return &gormLogrus{
		log: l,
		cfg: gormlogger.Config{
			SlowThreshold:             slowThreshold,
			IgnoreRecordNotFoundError: false,
			ParameterizedQueries:      false,
			LogLevel:                  lvl,
		},
	}
}

type gormLogrus struct {
	log *logrus.Logger
	cfg gormlogger.Config
}

func (g *gormLogrus) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	ng := *g
	ng.cfg.LogLevel = level
	return &ng
}

// entry returns a logrus.Entry with context attached and request_id field if available.
func (g *gormLogrus) entry(ctx context.Context) *logrus.Entry {
	if ctx == nil {
		return g.log.WithContext(context.Background())
	}
	e := g.log.WithContext(ctx)
	if rid := chmw.GetReqID(ctx); rid != "" {
		return e.WithField("request_id", rid)
	}
	return e
}

func (g *gormLogrus) Info(ctx context.Context, msg string, data ...interface{}) {
	if g.cfg.LogLevel >= gormlogger.Info {
		g.entry(ctx).Infof("gorm: "+msg, data...)
	}
}

func (g *gormLogrus) Warn(ctx context.Context, msg string, data ...interface{}) {
	if g.cfg.LogLevel >= gormlogger.Warn {
		g.entry(ctx).Warnf("gorm: "+msg, data...)
	}
}

func (g *gormLogrus) Error(ctx context.Context, msg string, data ...interface{}) {
	if g.cfg.LogLevel >= gormlogger.Error {
		g.entry(ctx).Errorf("gorm: "+msg, data...)
	}
}

// Trace implements the GORM logger's Trace method with level and slow query handling.
func (g *gormLogrus) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if g.cfg.LogLevel <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	elapsedMs := float64(elapsed.Nanoseconds()) / 1e6

	switch {
	case err != nil && g.cfg.LogLevel >= gormlogger.Error && (!errors.Is(err, gormlogger.ErrRecordNotFound) || !g.cfg.IgnoreRecordNotFoundError):
		sql, rows := fc()
		g.entry(ctx).WithFields(logrus.Fields{
			"duration_ms": elapsedMs,
			"rows":        rows,
			"sql":         sql,
		}).WithError(err).Error("gorm trace")
	case elapsed > g.cfg.SlowThreshold && g.cfg.SlowThreshold != 0 && g.cfg.LogLevel >= gormlogger.Warn:
		sql, rows := fc()
		g.entry(ctx).WithFields(logrus.Fields{
			"duration_ms":  elapsedMs,
			"rows":         rows,
			"sql":          sql,
			"slow":         true,
			"threshold_ms": float64(g.cfg.SlowThreshold.Nanoseconds()) / 1e6,
		}).Warn("gorm slow query")
	case g.cfg.LogLevel == gormlogger.Info:
		sql, rows := fc()
		g.entry(ctx).WithFields(logrus.Fields{
			"duration_ms": elapsedMs,
			"rows":        rows,
			"sql":         sql,
		}).Debug("gorm trace")
	}
}
