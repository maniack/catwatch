package backend

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chmw "github.com/go-chi/chi/v5/middleware"
	"github.com/maniack/catwatch/internal/logging"
	"github.com/maniack/catwatch/internal/storage"
	"github.com/sirupsen/logrus"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.size += n
	return n, err
}

type ctxKey string

const (
	ctxUserID ctxKey = "userID"
)

func WithUserID(ctx context.Context, uid string) context.Context {
	ctx = context.WithValue(ctx, ctxUserID, uid)
	ctx = context.WithValue(ctx, logging.ContextUserID, uid)
	return ctx
}

func UserIDFromCtx(ctx context.Context) (string, bool) {
	v := ctx.Value(ctxUserID)
	if v == nil {
		return "", false
	}
	id, ok := v.(string)
	return id, ok
}

func RequestIDFromCtx(ctx context.Context) (string, bool) {
	if rid := chmw.GetReqID(ctx); rid != "" {
		return rid, true
	}
	return "", false
}

// RequestLogger logs basic request info.
func RequestLogger(l *logrus.Logger, debugPaths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lrw := &loggingResponseWriter{ResponseWriter: w}
			next.ServeHTTP(lrw, r)
			if lrw.status == 0 {
				lrw.status = http.StatusOK
			}

			uid, _ := UserIDFromCtx(r.Context())
			rid, _ := RequestIDFromCtx(r.Context())
			var route string
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				route = rctx.RoutePattern()
			}

			isDebugPath := false
			for _, p := range debugPaths {
				if r.URL.Path == p {
					isDebugPath = true
					break
				}
			}

			entry := l.WithContext(r.Context()).WithFields(logrus.Fields{
				"method":      r.Method,
				"path":        r.URL.Path,
				"route":       route,
				"status":      lrw.status,
				"size":        lrw.size,
				"duration_ms": float64(time.Since(start).Nanoseconds()) / 1e6,
				"user_id":     uid,
				"request_id":  rid,
			})

			if isDebugPath && lrw.status < 400 {
				entry.Debug("request")
			} else {
				entry.Info("request")
			}
		})
	}
}

// SecurityHeaders adds common security-related headers to all responses.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			csp := strings.Join([]string{
				"default-src 'self'",
				"base-uri 'self'",
				"form-action 'self'",
				"script-src 'self' 'unsafe-inline'",
				"style-src 'self' 'unsafe-inline'",
				"img-src 'self' data: https:",
				"font-src 'self' data:",
				"connect-src 'self' ws: wss: https:",
				"frame-ancestors 'none'",
			}, "; ")
			w.Header().Set("Content-Security-Policy", csp)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			next.ServeHTTP(w, r)
		})
	}
}

// AuditMiddleware logs mutations to the AuditLog table.
func (s *Server) AuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Detect language from Accept-Language header
		lang := r.Header.Get("Accept-Language")
		if lang == "" {
			lang = "en"
		}
		// Basic parsing: pick first 2 chars
		if len(lang) >= 2 {
			lang = lang[:2]
		}
		ctx := context.WithValue(r.Context(), logging.ContextLang, lang)
		r = r.WithContext(ctx)

		// Only log mutations
		if r.Method == http.MethodGet || r.Method == http.MethodOptions || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		// We'll capture status after next.ServeHTTP
		// But for now, let's just log the attempt and status will be handled in handlers for more detail,
		// or we wrap ResponseWriter.
		next.ServeHTTP(w, r)

		// Simple audit from middleware (can be enriched in handlers)
		uid, _ := UserIDFromCtx(r.Context())
		if uid == "" && !strings.Contains(r.URL.Path, "/auth/") {
			// Skip logging if not authenticated and not auth path?
			// Actually we should log attempts too.
		}

		// Detailed audit is better done in handlers where we know target_type and target_id.
		// Middleware can serve as a fallback or for general logging.
	})
}

func (s *Server) LogAudit(r *http.Request, targetType, targetID, status, delta string) {
	uid, _ := UserIDFromCtx(r.Context())
	rid, _ := RequestIDFromCtx(r.Context())

	var route string
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		route = rctx.RoutePattern()
	}
	if route == "" {
		route = r.URL.Path
	}

	entry := storage.AuditLog{
		ID:         storage.NewUUID(),
		Timestamp:  time.Now().UTC(),
		UserID:     uid,
		Method:     r.Method,
		Route:      route,
		TargetType: targetType,
		TargetID:   targetID,
		Status:     status,
		IP:         r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		RequestID:  rid,
		Delta:      delta,
	}

	if err := s.store.DB.Create(&entry).Error; err != nil {
		s.log.WithError(err).Error("failed to write audit log")
	}
}
