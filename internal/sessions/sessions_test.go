package sessions

import (
	"testing"
	"time"

	"github.com/maniack/catwatch/internal/logging"
)

func init() { logging.Init(false, false) }

func TestMemoryStore_SetGetDel(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Close()

	key := "test:key"
	val := []byte("hello")

	// Miss before set
	got, err := store.Get(key)
	if err != nil || got != nil {
		t.Fatalf("expected nil,nil on miss, got %v, %v", got, err)
	}

	// Set then Get
	if err := store.Set(key, val, 1*time.Minute); err != nil {
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

func TestMemoryStore_DelNonExistent(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Close()
	// Should not error on missing key
	if err := store.Del("no:such:key"); err != nil {
		t.Fatalf("Del non-existent: %v", err)
	}
}

func TestMemoryStore_Expiry(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Close()

	key := "expiry:key"
	// TTL of 1ms — expires immediately
	store.Set(key, []byte("v"), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	got, err := store.Get(key)
	if err != nil || got != nil {
		t.Fatalf("expected expired item to be gone, got %v, %v", got, err)
	}
}

func TestMemoryStore_OverwriteResetsExpiry(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Close()

	key := "overwrite:key"
	store.Set(key, []byte("first"), 1*time.Millisecond)
	// Overwrite with longer TTL before it expires
	store.Set(key, []byte("second"), 1*time.Hour)
	time.Sleep(5 * time.Millisecond)

	got, err := store.Get(key)
	if err != nil || string(got) != "second" {
		t.Fatalf("expected 'second' after overwrite, got %q, %v", got, err)
	}
}

func TestMemoryStore_IsolatesValues(t *testing.T) {
	store := NewMemorySessionStore()
	defer store.Close()

	store.Set("a", []byte("aaa"), time.Minute)
	store.Set("b", []byte("bbb"), time.Minute)

	a, _ := store.Get("a")
	b, _ := store.Get("b")
	if string(a) != "aaa" || string(b) != "bbb" {
		t.Errorf("value isolation broken: a=%q b=%q", a, b)
	}
}

func TestRedactKey(t *testing.T) {
	cases := []struct {
		in   string
		want string // just check it doesn't contain the full key
	}{
		{"sess:abcdefghijklmnop", ""},
		{"state:oidc:xyz123456", ""},
		{"", ""},
	}
	for _, tc := range cases {
		out := redactKey(tc.in)
		if tc.in != "" && out == tc.in {
			t.Errorf("redactKey(%q) = %q, expected redacted", tc.in, out)
		}
	}
}
