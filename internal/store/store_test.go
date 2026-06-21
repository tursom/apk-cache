package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tursom/apk-cache/internal/config"
)

func TestEnsureRuntimeConfigImportsAndDatabaseWins(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Cache.Root = filepath.Join(root, "cache")
	cfg.Cache.DataRoot = filepath.Join(root, "data")
	cfg.APK.VerifySignature = false

	s, err := Open(filepath.Join(root, "data", "apk-cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	loaded, imported, err := s.EnsureRuntimeConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("expected first ensure to import runtime config")
	}
	if loaded.Cache.IndexTTL != cfg.Cache.IndexTTL {
		t.Fatalf("index ttl=%s", loaded.Cache.IndexTTL)
	}

	raw, _ := json.Marshal("48h")
	if _, _, err := s.UpdateSettings(context.Background(), loaded, map[string]json.RawMessage{"cache.index_ttl": raw}); err != nil {
		t.Fatal(err)
	}

	cfg.Cache.IndexTTL = "1h"
	loaded, imported, err = s.EnsureRuntimeConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("second ensure should not import")
	}
	if loaded.Cache.IndexTTL != "48h" {
		t.Fatalf("database should win, index ttl=%s", loaded.Cache.IndexTTL)
	}
}
