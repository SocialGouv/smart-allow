package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type cacheEntry struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func cacheKey(command, policyHash, model string) string {
	h := sha256.Sum256([]byte(command + "::" + policyHash + "::" + model))
	return hex.EncodeToString(h[:])[:16]
}

func cacheGet(dir, key string, ttl time.Duration) *cacheEntry {
	f := filepath.Join(dir, key+".json")
	st, err := os.Stat(f)
	if err != nil {
		return nil
	}
	if time.Since(st.ModTime()) > ttl {
		return nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	var e cacheEntry
	if json.Unmarshal(b, &e) != nil {
		return nil
	}
	return &e
}

func cacheSet(dir, key string, e cacheEntry) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, key+".json"), b, 0o644)
}

func policyHash(policy string) string {
	h := sha256.Sum256([]byte(policy))
	return hex.EncodeToString(h[:])[:12]
}
