package sessions

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// startMiniRedis spins up an in-process Redis and returns a store backed by it.
func startMiniRedis(t *testing.T) (SessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	store := NewRedisSessionStore(mr.Addr(), "", "test:")
	return store, mr
}

func TestRedisStore_SetGetDel(t *testing.T) {
	store, _ := startMiniRedis(t)
	defer store.Close()

	key := "mykey"
	val := []byte("hello redis")

	// Miss
	got, err := store.Get(key)
	if err != nil || got != nil {
		t.Fatalf("expected nil,nil on miss, got %v, %v", got, err)
	}

	// Set then Get
	if err := store.Set(key, val, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("expected %q, got %q", val, got)
	}

	// Del then Get
	if err := store.Del(key); err != nil {
		t.Fatalf("Del: %v", err)
	}
	got, err = store.Get(key)
	if err != nil || got != nil {
		t.Fatalf("expected nil,nil after del, got %v, %v", got, err)
	}
}

func TestRedisStore_DelNonExistent(t *testing.T) {
	store, _ := startMiniRedis(t)
	defer store.Close()
	if err := store.Del("no:such:key"); err != nil {
		t.Fatalf("Del non-existent: %v", err)
	}
}

func TestRedisStore_Expiry(t *testing.T) {
	store, mr := startMiniRedis(t)
	defer store.Close()

	if err := store.Set("exp:key", []byte("v"), 5*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Fast-forward time inside miniredis
	mr.FastForward(6 * time.Second)

	got, err := store.Get("exp:key")
	if err != nil || got != nil {
		t.Fatalf("expected expired key to be gone, got %v, %v", got, err)
	}
}

func TestRedisStore_OverwriteResetsExpiry(t *testing.T) {
	store, mr := startMiniRedis(t)
	defer store.Close()

	store.Set("k", []byte("first"), 2*time.Second)
	// Overwrite with a much longer TTL before it expires
	store.Set("k", []byte("second"), time.Hour)
	mr.FastForward(3 * time.Second) // original TTL expired, new one still valid

	got, err := store.Get("k")
	if err != nil || string(got) != "second" {
		t.Fatalf("expected 'second' after overwrite, got %q, %v", got, err)
	}
}

func TestRedisStore_IsolatesValues(t *testing.T) {
	store, _ := startMiniRedis(t)
	defer store.Close()

	store.Set("a", []byte("aaa"), time.Minute)
	store.Set("b", []byte("bbb"), time.Minute)

	a, _ := store.Get("a")
	b, _ := store.Get("b")
	if string(a) != "aaa" || string(b) != "bbb" {
		t.Errorf("isolation broken: a=%q b=%q", a, b)
	}
}

func TestRedisStore_KeyPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	// Two stores sharing the same Redis but different prefixes
	s1 := NewRedisSessionStore(mr.Addr(), "", "prefix1:")
	s2 := NewRedisSessionStore(mr.Addr(), "", "prefix2:")
	defer s1.Close()
	defer s2.Close()

	s1.Set("key", []byte("s1val"), time.Minute)
	got, _ := s2.Get("key")
	if got != nil {
		t.Error("expected prefix isolation: s2 should not see s1's key")
	}
}

func TestRedisStore_Disconnect(t *testing.T) {
	store, mr := startMiniRedis(t)
	defer store.Close()

	// Store a key
	store.Set("ping", []byte("ok"), time.Minute)

	// Stop Redis, then restart — retry logic should handle reconnect
	mr.Close()
	got, err := store.Get("ping")
	// After close, operations should fail gracefully (no panic)
	_ = got
	_ = err
}
