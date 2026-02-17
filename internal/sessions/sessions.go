package sessions

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/maniack/catwatch/internal/logging"
	redis "github.com/redis/go-redis/v9"
)

type SessionStore interface {
	Set(key string, value []byte, ttl time.Duration) error
	Get(key string) ([]byte, error)
	Del(key string) error
	Close() error
}

// ---- In-memory implementation ----

type memItem struct {
	v   []byte
	exp time.Time
}

type memorySessionStore struct {
	mu   sync.Mutex
	data map[string]memItem
	stop chan struct{}
}

func NewMemorySessionStore() SessionStore {
	logging.L().WithField("impl", "memory").Info("sessions: initialized")
	m := &memorySessionStore{data: make(map[string]memItem), stop: make(chan struct{})}
	// background cleanup to prevent unbounded growth
	go m.cleanupLoop()
	return m
}

func (m *memorySessionStore) Set(key string, value []byte, ttl time.Duration) error {
	logging.L().WithFields(map[string]any{
		"impl":    "memory",
		"op":      "set",
		"key":     redactKey(key),
		"ttl":     ttl.String(),
		"val_len": len(value),
	}).Debug("sessions")
	m.mu.Lock()
	m.data[key] = memItem{v: append([]byte(nil), value...), exp: time.Now().Add(ttl)}
	m.mu.Unlock()
	return nil
}

func (m *memorySessionStore) Get(key string) ([]byte, error) {
	m.mu.Lock()
	item, ok := m.data[key]
	m.mu.Unlock()
	if !ok {
		logging.L().WithFields(map[string]any{
			"impl":  "memory",
			"op":    "get",
			"key":   redactKey(key),
			"state": "miss",
		}).Debug("sessions")
		return nil, nil
	}
	if time.Now().After(item.exp) {
		logging.L().WithFields(map[string]any{
			"impl":  "memory",
			"op":    "get",
			"key":   redactKey(key),
			"state": "expired",
		}).Debug("sessions")
		return nil, nil
	}
	logging.L().WithFields(map[string]any{
		"impl":    "memory",
		"op":      "get",
		"key":     redactKey(key),
		"state":   "hit",
		"val_len": len(item.v),
	}).Debug("sessions")
	return item.v, nil
}

func (m *memorySessionStore) Del(key string) error {
	m.mu.Lock()
	_, existed := m.data[key]
	delete(m.data, key)
	m.mu.Unlock()
	logging.L().WithFields(map[string]any{
		"impl":    "memory",
		"op":      "del",
		"key":     redactKey(key),
		"existed": existed,
	}).Debug("sessions")
	return nil
}

func (m *memorySessionStore) Close() error {
	close(m.stop)
	logging.L().WithField("impl", "memory").Debug("sessions: closed")
	return nil
}

func (m *memorySessionStore) cleanupLoop() {
	logging.L().WithField("impl", "memory").Debug("sessions: cleanup loop start")
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			logging.L().WithField("impl", "memory").Debug("sessions: cleanup loop stop")
			return
		case <-t.C:
			m.mu.Lock()
			now := time.Now()
			removed := 0
			for k, it := range m.data {
				if now.After(it.exp) {
					delete(m.data, k)
					removed++
				}
			}
			m.mu.Unlock()
			if removed > 0 {
				logging.L().WithFields(map[string]any{"impl": "memory", "removed": removed}).Debug("sessions: cleanup removed expired")
			}
		}
	}
}

type redisSessionStore struct {
	client *redis.Client
	prefix string
}

// NewRedisSessionStore creates a Redis-backed store using go-redis.
func NewRedisSessionStore(addr, password, prefix string) SessionStore {
	if prefix == "" {
		prefix = "catwatch:oauth:"
	} else if !strings.HasSuffix(prefix, ":") {
		prefix = prefix + ":"
	}
	logging.L().WithFields(map[string]any{"impl": "redis", "addr": addr, "prefix": prefix}).Info("sessions: initializing redis store")
	opts := &redis.Options{
		Addr:            addr,
		Password:        password,
		DB:              0,
		MaxRetries:      3,
		MinRetryBackoff: 50 * time.Millisecond,
		MaxRetryBackoff: 250 * time.Millisecond,
		DialTimeout:     1 * time.Second,
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		OnConnect: func(ctx context.Context, cn *redis.Conn) error {
			logging.L().WithFields(map[string]any{"impl": "redis", "addr": addr}).Debug("sessions: redis connected")
			return nil
		},
	}
	cl := redis.NewClient(opts)
	// Best-effort ping
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cl.Ping(ctx).Err(); err != nil {
		logging.L().WithFields(map[string]any{"impl": "redis", "addr": addr}).WithError(err).Info("sessions: redis ping failed (will retry on demand)")
	} else {
		logging.L().WithFields(map[string]any{"impl": "redis", "addr": addr}).Debug("sessions: redis ping ok")
	}
	return &redisSessionStore{client: cl, prefix: prefix}
}

func (r *redisSessionStore) Set(key string, value []byte, ttl time.Duration) error {
	start := time.Now()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := r.client.Set(ctx, r.prefix+key, value, ttl).Err()
		cancel()
		if err == nil {
			logging.L().WithFields(map[string]any{
				"impl":    "redis",
				"op":      "set",
				"key":     redactKey(r.prefix + key),
				"ttl":     ttl.String(),
				"val_len": len(value),
				"took":    time.Since(start).String(),
				"attempt": attempt + 1,
			}).Debug("sessions")
			return nil
		}
		lastErr = err
		if attempt < 2 {
			ctxPing, cancelPing := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_ = r.client.Ping(ctxPing).Err()
			cancelPing()
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}
	}
	return lastErr
}

func (r *redisSessionStore) Get(key string) ([]byte, error) {
	start := time.Now()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		b, err := r.client.Get(ctx, r.prefix+key).Bytes()
		cancel()
		if err == nil {
			logging.L().WithFields(map[string]any{
				"impl":    "redis",
				"op":      "get",
				"key":     redactKey(r.prefix + key),
				"state":   "hit",
				"val_len": len(b),
				"took":    time.Since(start).String(),
				"attempt": attempt + 1,
			}).Debug("sessions")
			return b, nil
		}
		if err == redis.Nil {
			logging.L().WithFields(map[string]any{
				"impl":    "redis",
				"op":      "get",
				"key":     redactKey(r.prefix + key),
				"state":   "miss",
				"took":    time.Since(start).String(),
				"attempt": attempt + 1,
			}).Debug("sessions")
			return nil, nil
		}
		lastErr = err
		if attempt < 2 {
			ctxPing, cancelPing := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_ = r.client.Ping(ctxPing).Err()
			cancelPing()
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}
	}
	return nil, lastErr
}

func (r *redisSessionStore) Del(key string) error {
	start := time.Now()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := r.client.Del(ctx, r.prefix+key).Result()
		cancel()
		if err == nil {
			logging.L().WithFields(map[string]any{
				"impl":    "redis",
				"op":      "del",
				"key":     redactKey(r.prefix + key),
				"took":    time.Since(start).String(),
				"attempt": attempt + 1,
			}).Debug("sessions")
			return nil
		}
		lastErr = err
		if attempt < 2 {
			ctxPing, cancelPing := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_ = r.client.Ping(ctxPing).Err()
			cancelPing()
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}
	}
	return lastErr
}

func (r *redisSessionStore) Close() error { return r.client.Close() }

func redactKey(key string) string {
	if key == "" {
		return ""
	}
	const keep = 4
	n := len(key)
	if idx := strings.IndexByte(key, ':'); idx >= 0 && n > idx+keep {
		return key[:idx+1] + "***" + key[n-keep:]
	}
	if n > keep {
		return "***" + key[n-keep:]
	}
	return "***"
}
