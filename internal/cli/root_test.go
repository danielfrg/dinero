package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/cli"
)

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	status := cli.Execute(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return status, out.String(), errOut.String()
}

func TestHelpAndVersionDoNotNeedConfig(t *testing.T) {
	t.Setenv("DINERO_CONFIG_FILE", filepath.Join(t.TempDir(), "missing.toml"))
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		status, out, errOut := run(args...)
		if status != 0 || out == "" || errOut != "" {
			t.Errorf("%v: status %d stdout %q stderr %q", args, status, out, errOut)
		}
	}
}

func TestExplicitMissingConfigFailsWithoutDSN(t *testing.T) {
	t.Setenv("DINERO_DATABASE_URL", "postgresql://user:sentinel-db-password@host/db")
	status, out, errOut := run("--config", filepath.Join(t.TempDir(), "missing.toml"), "config")
	if status == 0 || out != "" || strings.Contains(errOut, "sentinel-db-password") {
		t.Fatalf("status %d stdout %q stderr %q", status, out, errOut)
	}
}

func TestConfigOutputRedactsEverySecret(t *testing.T) {
	data, err := os.ReadFile("../../testdata/compat/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	status, out, errOut := run("--config", path, "config")
	if status != 0 {
		t.Fatalf("status %d stderr %q", status, errOut)
	}
	for _, secret := range []string{"sentinel-db-password", "sentinel-client-id", "sentinel-plaid-secret", "sentinel-access-token"} {
		if strings.Contains(out+errOut, secret) {
			t.Errorf("output leaked %q", secret)
		}
	}
	if !strings.Contains(out, "existing") || !strings.Contains(out, "UTC") {
		t.Fatalf("missing public config fields: %q", out)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("stdout must contain one JSON value: %v; output %q", err, out)
	}
	if payload["timezone"] != "UTC" {
		t.Fatalf("timezone = %v", payload["timezone"])
	}
	if !strings.Contains(errOut, path) {
		t.Fatalf("stderr omitted resolved config path: %q", errOut)
	}
}
