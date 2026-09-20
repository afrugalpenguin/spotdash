package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afrugalpenguin/spotdash/agent/internal/config"
)

// loadExample loads agent/config.example.json with the placeholder token replaced.
func loadExample(t *testing.T) *config.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	copied := strings.ReplaceAll(string(data), config.PlaceholderToken, "a-token-of-my-own")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(copied), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestTheExampleConfigHasABlockForEverySource(t *testing.T) {
	cfg := loadExample(t)

	for name := range factories {
		if _, ok := cfg.Sources[name]; !ok {
			t.Errorf("config.example.json has no %q block", name)
		}
	}
}

// The example is documentation people edit, so every block must build when enabled.
func TestEveryBlockInTheExampleBuildsOnceSwitchedOn(t *testing.T) {
	cfg := loadExample(t)
	for name, src := range cfg.Sources {
		src.Enabled = true
		cfg.Sources[name] = src
	}

	built, err := Build(cfg)

	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built) != len(factories) {
		t.Errorf("built %d sources, want %d", len(built), len(factories))
	}
}
