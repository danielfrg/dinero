package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/cli"
	"github.com/danielfrg/dinero/internal/plaid"
	"github.com/danielfrg/dinero/internal/testutil"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type cliFakePlaid struct{}

func (cliFakePlaid) GetTransactions(_ context.Context, _ string, _, _ time.Time, offset int) (plaid.TransactionsPage, error) {
	if offset != 0 {
		return plaid.TransactionsPage{}, nil
	}
	return plaid.TransactionsPage{Total: 1, Transactions: []plaid.Transaction{{AccountID: "missing-map", Date: "2026-10-14", Name: "Coffee", Amount: 4.25}}}, nil
}

type cliFailPlaid struct{}

func (cliFailPlaid) GetTransactions(context.Context, string, time.Time, time.Time, int) (plaid.TransactionsPage, error) {
	return plaid.TransactionsPage{}, errors.New("sentinel-secret-token")
}

func TestMaintenanceJSONAndConfirmation(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	state := t.TempDir()
	t.Setenv("DINERO_STATE_DIR", state)
	fx.InsertNullable(t, "2026-09-01 08:00:00", "Coffee", "Food", "Cafe", 4.25, "Checking", "")
	for i := 0; i < 2; i++ {
		fx.InsertNullable(t, "2026-09-01 08:00:00", "Coffee", "Food", "Cafe", 4.25, "Checking", "")
	}
	status, out, stderr := run("mkrules", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("preview %d %q %q", status, out, stderr)
	}
	var rules map[string][2]string
	if err := json.Unmarshal([]byte(out), &rules); err != nil || rules["Coffee"] != [2]string{"Food", "Cafe"} {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(state, "category_rules.json")); !os.IsNotExist(err) {
		t.Fatal("preview wrote rules")
	}
	status, out, stderr = run("mkrules")
	if status != 0 || stderr != "" || !strings.Contains(out, "Coffee  Food  Cafe  3") {
		t.Fatalf("human preview %d %q %q", status, out, stderr)
	}
	status, out, stderr = run("mkrules", "--commit", "--yes", "--json")
	if status != 0 || stderr != "" || !strings.HasPrefix(out, "{") {
		t.Fatalf("commit %d %q %q", status, out, stderr)
	}
	status, out, stderr = run("build-cache", "--json")
	if status != 0 || stderr != "" || !strings.HasPrefix(out, "{") {
		t.Fatalf("cache %d %q %q", status, out, stderr)
	}
	status, out, stderr = run("mkdataset", "--json", "--directory", t.TempDir())
	if status != 0 || stderr != "" || !strings.HasPrefix(out, "{") {
		t.Fatalf("export %d %q %q", status, out, stderr)
	}
}

func TestInitDBRequiresExplicitApprovalForJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.sqlite")
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("timezone = \"UTC\"\n[database]\nconnection_string = \"sqlite:///"+path+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", cfg)
	t.Setenv("DINERO_DATABASE_URL", "")
	os.Unsetenv("DINERO_DATABASE_URL")
	status, out, stderr := run("init-db", "--dry-run", "--json")
	if status != 0 || stderr != "" || !strings.Contains(out, "dry-run") {
		t.Fatalf("dry run %d %q %q", status, out, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry-run created database")
	}
	status, out, _ = run("init-db", "--json")
	if status == 0 || out != "" {
		t.Fatalf("expected confirmation error %d %q", status, out)
	}
	status, out, stderr = run("init-db", "--yes", "--json")
	if status != 0 || stderr != "" || !strings.Contains(out, "created") {
		t.Fatalf("init %d %q %q", status, out, stderr)
	}
}

func TestTransactionsJSONAndLegacyYes(t *testing.T) {
	fx := testutil.NewSQLite(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	body := "timezone = \"UTC\"\n[database]\nconnection_string = \"sqlite:///" + fx.DSN + "\"\n[plaid.tokens]\nbank = \"synthetic\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", cfg)
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	t.Setenv("DINERO_DATABASE_URL", "")
	os.Unsetenv("DINERO_DATABASE_URL")
	execute := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		root := cli.NewRoot(cli.Dependencies{PlaidClient: cliFakePlaid{}})
		root.SetArgs(args)
		root.SetOut(&out)
		root.SetErr(&errOut)
		err := root.ExecuteContext(context.Background())
		if err != nil {
			errOut.WriteString(err.Error())
			return 1, out.String(), errOut.String()
		}
		return 0, out.String(), errOut.String()
	}
	code, out, stderr := execute("transactions", "--through", "2026-10-14", "--json")
	if code != 0 || !strings.Contains(stderr, "missing-map") || !strings.Contains(out, "\"new_transactions\":1") {
		t.Fatalf("preview %d %q %q", code, out, stderr)
	}
	var n int
	fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n)
	if n != 0 {
		t.Fatal("preview wrote")
	}
	code, out, stderr = execute("transactions", "--through", "2026-10-14", "--dry-run", "--yes", "--json")
	if code != 0 || !strings.Contains(out, "\"new_transactions\":1") {
		t.Fatalf("explicit dry run %d %q %q", code, out, stderr)
	}
	fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n)
	if n != 0 {
		t.Fatal("--dry-run --yes wrote")
	}
	code, out, stderr = execute("transactions", "--through", "2026-10-14", "--yes", "--json")
	if code != 0 || !strings.Contains(out, "\"status\":\"imported\"") {
		t.Fatalf("commit %d %q %q", code, out, stderr)
	}
	fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	code, out, stderr = execute("transactions", "--through", "2026-10-14", "--yes", "--json")
	if code != 0 || !strings.Contains(out, "\"new_transactions\":0") {
		t.Fatalf("repeat %d %q %q", code, out, stderr)
	}
	fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n)
	if n != 1 {
		t.Fatal("repeat wrote duplicate", n)
	}
}

func TestTransactionsProviderErrorDoesNotLeakSecret(t *testing.T) {
	fx := testutil.NewSQLite(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	body := "timezone = \"UTC\"\n[database]\nconnection_string = \"sqlite:///" + fx.DSN + "\"\n[plaid.tokens]\nbank = \"synthetic\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", cfg)
	t.Setenv("DINERO_STATE_DIR", t.TempDir())
	t.Setenv("DINERO_DATABASE_URL", "")
	os.Unsetenv("DINERO_DATABASE_URL")
	var out, stderr bytes.Buffer
	root := cli.NewRoot(cli.Dependencies{PlaidClient: cliFailPlaid{}})
	root.SetArgs([]string{"transactions", "--through", "2026-10-14", "--json"})
	root.SetOut(&out)
	root.SetErr(&stderr)
	err := root.ExecuteContext(context.Background())
	if err == nil || strings.Contains(err.Error()+stderr.String()+out.String(), "sentinel-secret-token") {
		t.Fatalf("provider secret leaked: %v %q %q", err, out.String(), stderr.String())
	}
}

func TestInitDBCreatesMissingPostgresDatabase(t *testing.T) {
	adminDSN := os.Getenv("DINERO_TEST_POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("synthetic PostgreSQL DSN unset")
	}
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	name := "dinero_t6_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
	u.Path = "/" + name
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("timezone = \"UTC\"\n[database]\nconnection_string = \""+u.String()+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", cfg)
	t.Setenv("DINERO_DATABASE_URL", "")
	os.Unsetenv("DINERO_DATABASE_URL")
	status, out, stderr := run("init-db", "--yes", "--json")
	if status != 0 || stderr != "" || !strings.Contains(out, "created") {
		t.Fatalf("init %d %q %q", status, out, stderr)
	}
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='transactions'").Scan(&n); err != nil || n != 8 {
		t.Fatalf("schema columns %d %v", n, err)
	}
}
