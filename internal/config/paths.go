package config

import (
	"os"
	"path/filepath"
)

func DefaultDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "dinero")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "dinero")
	}
	return filepath.Join(home, ".config", "dinero")
}

func ResolvePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if override, set := os.LookupEnv("DINERO_CONFIG_FILE"); set {
		return override
	}
	return filepath.Join(DefaultDir(), "config.toml")
}

func StateDir() string {
	if override := os.Getenv("DINERO_STATE_DIR"); override != "" {
		return override
	}
	return DefaultDir()
}
