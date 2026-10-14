package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestReadTxDerivesThreeSecondDeadlineAndKeepsItAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deadline.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE transactions(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), path, true, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tx, readCtx, cancel, err := s.readTx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	defer tx.Rollback()
	deadline, ok := readCtx.Deadline()
	if !ok {
		t.Fatal("read context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 2500*time.Millisecond || remaining > 3*time.Second {
		t.Fatalf("read deadline in %s, want about 3s", remaining)
	}
	if err := readCtx.Err(); err != nil {
		t.Fatalf("read context canceled before scans: %v", err)
	}
	var count int
	if err := tx.QueryRowContext(readCtx, "SELECT COUNT(*) FROM transactions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
