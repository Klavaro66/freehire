package telegram

import (
	"path/filepath"
	"testing"

	"github.com/strelov1/freehire/internal/platform/modroot"
)

// The committed sources/telegram.yml must always load and validate — a broken seed
// file would otherwise only surface at the next scheduled crawl.
func TestSeedChannelsFileIsValid(t *testing.T) {
	root, err := modroot.Find()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "sources", "telegram.yml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(cfg.Channels) < 30 {
		t.Errorf("channels = %d, want the curated tier-1 list (>=30)", len(cfg.Channels))
	}
}
