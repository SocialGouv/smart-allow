package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key := cacheKey("ls", "pol123", "qwen:7b")
	if key == "" || len(key) != 16 {
		t.Fatalf("key should be 16 hex chars, got %q", key)
	}

	if cacheGet(dir, key, time.Second) != nil {
		t.Fatalf("cache should be empty initially")
	}

	cacheSet(dir, key, cacheEntry{Decision: "ask", Reason: "because"})
	got := cacheGet(dir, key, time.Second)
	if got == nil {
		t.Fatalf("cache miss after set")
	}
	if got.Decision != "ask" || got.Reason != "because" {
		t.Fatalf("unexpected cache entry: %+v", got)
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	dir := t.TempDir()
	key := cacheKey("ls -la", "pol", "qwen")
	cacheSet(dir, key, cacheEntry{Decision: "approve"})

	// Push mtime to the past so the entry is older than TTL.
	f := filepath.Join(dir, key+".json")
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if cacheGet(dir, key, time.Minute) != nil {
		t.Fatalf("cache should be expired")
	}
}
