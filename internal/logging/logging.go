package logging

import (
	"os"
	"runtime"
	"sort"
	"strings"

	chmw "github.com/go-chi/chi/v5/middleware"
	"github.com/sirupsen/logrus"
)

var logger = logrus.New()

// CtxKey is a typed key for storing values in context without collisions across packages.
type CtxKey string

// Standard context keys used by logging hooks.
const (
	ContextRequestID CtxKey = "request_id"
	ContextUserID    CtxKey = "user_id"
	ContextLang      CtxKey = "lang"
)

// contextHook injects common fields from context into every log entry, if present.
type contextHook struct{}

func (contextHook) Levels() []logrus.Level { return logrus.AllLevels }

func (contextHook) Fire(e *logrus.Entry) error {
	if e.Context == nil {
		return nil
	}
	// request_id: use chi's native request id when available
	if _, exists := e.Data["request_id"]; !exists {
		if rid := chmw.GetReqID(e.Context); rid != "" {
			e.Data["request_id"] = rid
		}
	}
	// user_id from context
	if v := e.Context.Value(ContextUserID); v != nil {
		if s, ok := v.(string); ok && s != "" {
			if _, exists := e.Data["user_id"]; !exists {
				e.Data["user_id"] = s
			}
		}
	}
	return nil
}

// moduleHook injects the module name (top-level package path like "internal/backend")
// based on the callsite stack frame. It does not override an explicitly set module.
// It skips frames from logrus and the logging package itself.
type moduleHook struct{}

func (moduleHook) Levels() []logrus.Level { return logrus.AllLevels }

func (moduleHook) Fire(e *logrus.Entry) error {
	if _, exists := e.Data["module"]; exists {
		return nil
	}
	if m := computeModule(); m != "" {
		e.Data["module"] = m
	}
	return nil
}

// computeModule walks the call stack to find the first frame within this repo
// (excluding logging and logrus) and derives a module like "internal/backend" or
// "internal/oauth" from the file path. Falls back to empty string if not found.
func computeModule() string {
	const repoMarker = "github.com/maniack/catwatch/"
	pcs := make([]uintptr, 32)
	n := runtime.Callers(4, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if fn != "" {
			if strings.Contains(fn, "github.com/sirupsen/logrus") || strings.Contains(fn, "github.com/maniack/catwatch/internal/logging") {
				if !more {
					break
				}
				continue
			}
		}
		file := frame.File
		if i := strings.Index(file, repoMarker); i >= 0 {
			rel := file[i+len(repoMarker):]
			// Expect rel like: internal/backend/..., internal/oauth/..., cmd/...
			parts := strings.Split(rel, "/")
			if len(parts) > 0 {
				if parts[0] == "internal" {
					if len(parts) >= 2 {
						return parts[0] + "/" + parts[1]
					}
					return parts[0]
				}
				if parts[0] == "cmd" {
					if len(parts) >= 2 {
						return parts[0] + "/" + parts[1]
					}
					return parts[0]
				}
				// Skip vendor and other directories; otherwise report top-level
				if parts[0] != "vendor" && parts[0] != "vendor-mod" {
					return parts[0]
				}
			}
		}
		if !more {
			break
		}
	}
	return ""
}

// canonicalFieldOrder defines our preferred order for log fields.
var canonicalFieldOrder = []string{
	"time",
	"module",
	"level",
	"handler",
	"method",
	"path",
	"route",
	"status",
	"size",
	// DB / GORM
	"rows",
	"sql",
	"slow",
	"threshold_ms",
	// Misc
	"cat_id",
	"record_id",
	"user_id",
	"request_id",
	"duration_ms",
}

// sortKeysCanonical sorts keys using the canonical order first, then alpha for the rest.
func sortKeysCanonical(keys []string) {
	if len(keys) <= 1 {
		return
	}
	priority := map[string]int{}
	for i, k := range canonicalFieldOrder {
		priority[k] = i
	}
	sort.Slice(keys, func(i, j int) bool {
		iKey, jKey := keys[i], keys[j]
		pi, iok := priority[iKey]
		pj, jok := priority[jKey]
		if iok && jok {
			return pi < pj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		// neither has priority: sort alpha, but keep error last if present
		// ensure stable behavior regardless of Go's map iteration order
		iLower, jLower := strings.ToLower(iKey), strings.ToLower(jKey)
		if iLower == "error" && jLower != "error" {
			return false
		}
		if jLower == "error" && iLower != "error" {
			return true
		}
		return iLower < jLower
	})
}

// Init configures global logger. If debug is true, sets debug level.
// If jsonFormat is true, uses JSON formatter, otherwise text with full timestamp.
func Init(debug bool, jsonFormat bool) {
	level := logrus.InfoLevel
	if debug {
		level = logrus.DebugLevel
	}
	logger.SetLevel(level)
	logger.SetOutput(os.Stdout)
	// Hooks: enrich with request/user context and module
	logger.AddHook(moduleHook{})
	logger.AddHook(contextHook{})

	if jsonFormat {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, SortingFunc: sortKeysCanonical})
	}
}

// L returns the configured global logger.
func L() *logrus.Logger { return logger }
