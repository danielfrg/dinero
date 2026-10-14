// server_smoke prepares synthetic container data and verifies anonymous REST
// access. Run this file directly; mcp_smoke.go exercises the MCP transport.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: server_smoke.go fixture OUTPUT_DIR | rest BASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch os.Args[1] {
	case "fixture":
		return createFixture(ctx, os.Args[2])
	case "rest":
		return verifyREST(ctx, os.Args[2])
	default:
		return fmt.Errorf("unknown smoke mode %q", os.Args[1])
	}
}

func createFixture(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "input.sqlite")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE transactions (
		id INTEGER PRIMARY KEY, date DATETIME, description VARCHAR(1000),
		category VARCHAR(50), subcategory VARCHAR(50), amount DOUBLE,
		notes VARCHAR(1000), account VARCHAR(50))`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO transactions VALUES (1,?,?,?,?,?,?,?)`,
		"2026-09-02 09:00:00", "Coffee", "Food", "Cafe", 4.25, "synthetic", "Test Account"); err != nil {
		return err
	}
	const config = `timezone = "UTC"
[database]
connection_string = "sqlite:////run/dinero/input.sqlite"
[server]
allowed_hosts = ["127.0.0.1", "localhost"]
`
	configFile, err := os.OpenFile(filepath.Join(dir, "config.toml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := configFile.WriteString(config); err != nil {
		configFile.Close()
		return err
	}
	return configFile.Close()
}

func verifyREST(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/accounts", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("REST accounts: got HTTP %d, want 200", resp.StatusCode)
	}
	var body struct {
		Accounts []struct {
			Name string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if len(body.Accounts) != 1 || body.Accounts[0].Name != "Test Account" {
		return fmt.Errorf("REST accounts: unexpected synthetic fixture response %+v", body.Accounts)
	}
	fmt.Println("Anonymous REST accounts passed: Test Account")
	return nil
}
