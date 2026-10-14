package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
)

func fixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/compat/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPreservesLegacyFieldsWithoutPlaidValidation(t *testing.T) {
	path := fixture(t)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timezone != "UTC" || cfg.Plaid.Tokens["existing"] != "sentinel-access-token" || cfg.Plaid.ItemIDs["existing"] != "item-existing" || cfg.Plaid.AccountIDToName["account_existing"] != "Checking" || cfg.Server.Port != 8080 {
		t.Fatalf("legacy fields lost: %+v", cfg.Redacted())
	}
	data := "[database]\nconnection_string = \"sqlite:///data/dinero.db\"\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("read-only service config should load: %v", err)
	}
	if cfg.Timezone != "US/Central" {
		t.Fatalf("timezone = %q", cfg.Timezone)
	}
}

func TestNormalizeDSNRecognizedPrefixes(t *testing.T) {
	cases := map[string]string{
		"postgresql+psycopg://user:pass@localhost/db": "postgresql://user:pass@localhost/db",
		"postgresql://user:pass@localhost/db":         "postgresql://user:pass@localhost/db",
		"sqlite:///data/dinero.db":                    "data/dinero.db",
		"sqlite:////tmp/dinero.db":                    "/tmp/dinero.db",
	}
	for input, want := range cases {
		got, err := config.NormalizeDSN(input)
		if err != nil || got != want {
			t.Errorf("NormalizeDSN(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	_, err := config.NormalizeDSN("mysql://user:sentinel-db-password@host/db")
	if err == nil || strings.Contains(err.Error(), "sentinel-db-password") {
		t.Fatalf("unsafe unsupported DSN error: %v", err)
	}
}

func TestDatabaseURLFileOverridesConfigWithoutLeaking(t *testing.T) {
	path := fixture(t)
	secretPath := filepath.Join(t.TempDir(), "database-url")
	if err := os.WriteFile(secretPath, []byte("postgresql://file:sentinel-file-password@host/db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_DATABASE_URL_FILE", secretPath)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.DSN()
	if err != nil || got != "postgresql://file:sentinel-file-password@host/db" {
		t.Fatalf("DSN = %q, %v", got, err)
	}
	if strings.Contains(cfg.Redacted(), "sentinel-file-password") || strings.Contains(cfg.Redacted(), "sentinel-db-password") {
		t.Fatal("redacted output leaked database password")
	}
}

func TestDatabaseURLOverrideSuppliesDatabaseForServiceConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("timezone = \"UTC\"\n[server]\nhost = \"127.0.0.1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_DATABASE_URL", "sqlite:///data/service.db")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.DSN()
	if err != nil || got != "data/service.db" {
		t.Fatalf("DSN = %q, %v", got, err)
	}
}

func TestLoadMalformedConfigDoesNotEchoSecrets(t *testing.T) {
	path := fixture(t)
	if err := os.WriteFile(path, []byte("[database]\nconnection_string = \"sentinel-db-password\"\ninvalid = [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil || strings.Contains(err.Error(), "sentinel-db-password") {
		t.Fatalf("unsafe parse error: %v", err)
	}
}
