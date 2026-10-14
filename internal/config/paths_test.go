package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
)

func TestResolvePathExplicitMissingOverrideIsReturned(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	t.Setenv("DINERO_CONFIG_FILE", missing)
	got := config.ResolvePath("")
	if got != missing {
		t.Fatalf("path = %q", got)
	}
	if _, err := config.Load(got); !os.IsNotExist(err) {
		t.Fatalf("missing override error = %v", err)
	}
}

func TestStateDirIndependentOfConfigOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "override", "config.toml")
	t.Setenv("DINERO_CONFIG_FILE", configPath)
	t.Setenv("DINERO_STATE_DIR", "")
	if got := config.StateDir(); got == filepath.Dir(configPath) {
		t.Fatalf("state dir followed config override: %q", got)
	}
	statePath := filepath.Join(t.TempDir(), "state")
	t.Setenv("DINERO_STATE_DIR", statePath)
	if got := config.StateDir(); got != statePath {
		t.Fatalf("state dir = %q", got)
	}
}
