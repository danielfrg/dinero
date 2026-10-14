package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/testutil"
)

func queryConfig(t *testing.T, dsn string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "timezone = \"UTC\"\n[database]\nconnection_string = \"sqlite:///" + dsn + "\"\n[plaid.account_id_to_name]\nfixture = \"Test Account\"\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_CONFIG_FILE", path)
	t.Setenv("DINERO_DATABASE_URL", "sqlite:///"+dsn)
}
func TestBalanceIncludesEntireThroughDateAndKnownEmptyAccount(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	fx.Insert(t, "2026-09-02 00:00:00", "Midnight", 10.0, "Test Account")
	fx.Insert(t, "2026-09-02 23:59:59.999999", "End of day", 5.25, "Test Account")
	fx.Insert(t, "2026-09-03 00:00:00", "Next day", 7.0, "Test Account")
	status, out, stderr := run("balance", "--account", "Test Account", "--through", "2026-09-02", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("status %d stderr %q", status, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["amount"] != "15.25" || got["through"] != "2026-09-02" || got["account"] != "Test Account" || len(got) != 3 {
		t.Fatalf("payload %v", got)
	}
	compatData, err := os.ReadFile("../../testdata/compat/query.json")
	if err != nil {
		t.Fatal(err)
	}
	var compat struct {
		Balance map[string]any   `json:"balance"`
		Search  []map[string]any `json:"search"`
	}
	if err = json.Unmarshal(compatData, &compat); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, compat.Balance) {
		t.Fatalf("balance contract %v, want %v", got, compat.Balance)
	}
	status, out, stderr = run("search", "--description", "Midnight", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("search status %d stderr %q", status, stderr)
	}
	var searched []map[string]any
	if err = json.Unmarshal([]byte(out), &searched); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(searched, compat.Search) {
		t.Fatalf("search contract %v, want %v", searched, compat.Search)
	}
	status, out, _ = run("balance", "--account", "Test Account", "--through", "2026-09-01", "--json")
	if status != 0 || !strings.Contains(out, `"amount":"0.00"`) {
		t.Fatalf("empty balance %d %s", status, out)
	}
}
func TestSearchInclusiveBeforeAndEmptyJSON(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	status, out, stderr := run("search", "--json")
	if status != 0 || strings.TrimSpace(out) != "[]" || stderr != "" {
		t.Fatalf("empty %d %q %q", status, out, stderr)
	}
	fx.Insert(t, "2026-09-02 12:00:00", "Closing day", 3.50, "Test Account")
	fx.Insert(t, "2026-09-03 00:00:00", "Next day", 4.50, "Test Account")
	status, out, stderr = run("search", "--before", "2026-09-02", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("status %d stderr %q", status, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["description"] != "Closing day" || rows[0]["amount"] != 3.5 || len(rows[0]) != 8 {
		t.Fatalf("rows %v", rows)
	}
}
func TestSearchNullFieldsAndStableSort(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	fx.InsertNullable(t, "", "null row", "", "", nil, "Test Account", "")
	fx.Insert(t, "2026-09-02 12:00:00", "same", 2.0, "Test Account")
	fx.Insert(t, "2026-09-02 12:00:00", "same", 3.0, "Test Account")
	status, out, stderr := run("search", "--sort", "description", "--asc", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("status %d stderr %q", status, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["date"] != "" || rows[0]["amount"] != nil {
		t.Fatalf("null row %v", rows[0])
	}
	if rows[1]["id"].(float64) >= rows[2]["id"].(float64) {
		t.Fatalf("unstable IDs %v", rows)
	}
}
func TestInvalidSearchOptionsDoNotWriteJSON(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	for _, args := range [][]string{{"--sort", "bad"}, {"--month", "13"}, {"--limit", "-1"}, {"--after", "2026-09-03", "--before", "2026-09-02"}} {
		status, out, stderr := run(append([]string{"search", "--json"}, args...)...)
		if status == 0 || out != "" || stderr == "" {
			t.Errorf("args %v: %d %q %q", args, status, out, stderr)
		}
	}
}

func TestSearchPreservesLiteralSQLWildcardsAndAllowsMoreThan500Rows(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	fx.Insert(t, "2026-09-02 12:00:00", "coffee", 1, "Test Account")
	fx.Insert(t, "2026-09-02 12:00:00", "cocoa", 1, "Test Account")
	fx.Insert(t, "2026-09-02 12:00:00", "tea", 1, "Test Account")
	status, out, stderr := run("search", "--description", "co%", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("status %d stderr %q", status, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("wildcard returned %d rows: %s", len(rows), out)
	}
	for i := 0; i < 498; i++ {
		fx.Insert(t, "2026-09-02 12:00:00", "filler", 1, "Test Account")
	}
	status, out, stderr = run("search", "--limit", "501", "--json")
	if status != 0 || stderr != "" {
		t.Fatalf("status %d stderr %q", status, stderr)
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 501 {
		t.Fatalf("limit 501 returned %d rows", len(rows))
	}
}

func TestBalanceUsesHalfEvenOnSQLSum(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	fx.Insert(t, "2026-09-02 12:00:00", "tie", 2.225, "Test Account")
	status, out, stderr := run("balance", "--account", "Test Account", "--through", "2026-09-02", "--json")
	if status != 0 || stderr != "" || !strings.Contains(out, `"amount":"2.22"`) {
		t.Fatalf("half-even %d %q %q", status, out, stderr)
	}
}
