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

func TestDefaultConfigAndStatePathsHonorXDGConfigHome(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DINERO_CONFIG_FILE", "unused")
	if err := os.Unsetenv("DINERO_CONFIG_FILE"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_STATE_DIR", "")
	wantDir := filepath.Join(xdg, "dinero")
	if got := config.DefaultDir(); got != wantDir {
		t.Errorf("DefaultDir = %q, want %q", got, wantDir)
	}
	if got := config.ResolvePath(""); got != filepath.Join(wantDir, "config.toml") {
		t.Errorf("ResolvePath = %q", got)
	}
	if got := config.StateDir(); got != wantDir {
		t.Errorf("StateDir = %q, want %q", got, wantDir)
	}
}

func TestDefaultDirFallsBackToHomeConfigWhenXDGEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "dinero")
	if got := config.DefaultDir(); got != want {
		t.Fatalf("DefaultDir = %q, want %q", got, want)
	}
}
