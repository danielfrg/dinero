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
	"github.com/danielfrg/dinero/internal/testutil"
)

func importSetup(t *testing.T, dsn string) string {
	t.Helper()
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	t.Setenv("DINERO_DATABASE_URL_FILE", "")
	t.Setenv("DINERO_DATABASE_URL", "")
	os.Unsetenv("DINERO_DATABASE_URL_FILE")
	os.Unsetenv("DINERO_DATABASE_URL")
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := "timezone = \"America/Chicago\"\n[database]\nconnection_string = \"sqlite:///" + dsn + "\"\n[plaid.account_id_to_name]\nfixture = \"Test Account\"\n"
	if strings.HasPrefix(dsn, "postgres") {
		body = "timezone = \"America/Chicago\"\n[database]\nconnection_string = \"" + dsn + "\"\n[plaid.account_id_to_name]\nfixture = \"Test Account\"\n"
	}
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", cfg)
	file := filepath.Join(t.TempDir(), "transactions.csv")
	if err := os.WriteFile(file, []byte("date,description,amount\n2026-09-02,Coffee,4.25\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}
func importRun(input string, args ...string) (int, string, string) {
	var out, stderr bytes.Buffer
	status := cli.Execute(context.Background(), args, strings.NewReader(input), &out, &stderr)
	return status, out.String(), stderr.String()
}
func TestImportCLIConsentAndJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		fx   func(*testing.T) *testutil.Fixture
	}{{"sqlite", func(t *testing.T) *testutil.Fixture { return testutil.NewSQLite(t) }}, {"postgres", func(t *testing.T) *testutil.Fixture { return testutil.NewPostgres(t) }}} {
		t.Run(tc.name, func(t *testing.T) {
			fx := tc.fx(t)
			file := importSetup(t, fx.DSN)
			status, out, stderr := importRun("", "import-csv", file, "Test Account", "--json")
			if status != 0 || stderr != "" {
				t.Fatalf("preview %d %q %q", status, out, stderr)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["status"] != "dry-run" || len(payload) != 9 {
				t.Fatalf("payload %v", payload)
			}
			status, out, stderr = importRun("", "import-csv", file, "Test Account", "--dry-run", "--json")
			if status != 0 || stderr != "" || json.Unmarshal([]byte(out), &payload) != nil || payload["status"] != "dry-run" {
				t.Fatalf("explicit dry-run %d %q %q", status, out, stderr)
			}
			status, _, stderr = importRun("", "import-csv", file, "Test Account", "--yes")
			if status != 2 || !strings.Contains(stderr, "--yes requires --commit") {
				t.Fatalf("yes %d %q", status, stderr)
			}
			status, _, stderr = importRun("\n", "import-csv", file, "Test Account", "--commit")
			if status != 1 || !strings.Contains(stderr, "Import these transactions? [y/N]") {
				t.Fatalf("confirm %d %q", status, stderr)
			}
			status, _, stderr = importRun("", "import-csv", file, "Test Account", "--json", "--commit")
			if status != 2 || !strings.Contains(stderr, "--json --commit requires --yes") {
				t.Fatalf("JSON consent %d %q", status, stderr)
			}
			status, out, stderr = importRun("", "import-csv", file, "Test Account", "--json", "--commit", "--yes")
			if status != 0 || stderr != "" {
				t.Fatalf("commit %d %q %q", status, out, stderr)
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil || payload["status"] != "imported" {
				t.Fatalf("commit payload %v err %v", payload, err)
			}
			status, out, stderr = importRun("", "import-csv", file, "Test Account", "--json", "--commit")
			if status != 0 || stderr != "" || json.Unmarshal([]byte(out), &payload) != nil || payload["status"] != "no-new-transactions" {
				t.Fatalf("repeat %d %q %q", status, out, stderr)
			}
		})
	}
}
func TestImportAmbiguousJSONAndMissingFile(t *testing.T) {
	fx := testutil.NewSQLite(t)
	file := importSetup(t, fx.DSN)
	fx.Insert(t, "2026-09-02 00:00:00", "Coffee", 4.25, "Test Account")
	fx.Insert(t, "2026-09-02 00:00:00", "Coffee", 4.25, "Test Account")
	status, out, stderr := importRun("", "import-csv", file, "Test Account", "--json")
	var payload map[string]any
	if status != 1 || stderr != "" || json.Unmarshal([]byte(out), &payload) != nil || payload["status"] != "ambiguous" {
		t.Fatalf("ambiguous %d %q %q", status, out, stderr)
	}
	status, out, stderr = importRun("", "import-csv", file+"-missing", "Test Account")
	if status != 1 || out != "" || !strings.Contains(stderr, "File not found:") || strings.Contains(stderr, "configuration file") {
		t.Fatalf("missing %d %q %q", status, out, stderr)
	}
}
func TestImportPreviewDoesNotCreateMissingSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.sqlite")
	file := importSetup(t, path)
	status, _, _ := importRun("", "import-csv", file, "Test Account", "--json")
	if status == 0 {
		t.Fatal("preview unexpectedly succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preview created storage: %v", err)
	}
}
