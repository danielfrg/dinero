package store_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestWriteTxRollsBackSecondInsert(t *testing.T) {
	for _, tc := range []struct {
		name  string
		newFX func(testing.TB) *testutil.Fixture
	}{{"sqlite", testutil.NewSQLite}, {"postgres", testutil.NewPostgres}} {
		t.Run(tc.name, func(t *testing.T) {
			fx := tc.newFX(t)
			s, err := store.Open(context.Background(), fx.DSN, false, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if tc.name == "sqlite" {
				_, err = fx.DB.Exec("CREATE TRIGGER reject_tea BEFORE INSERT ON transactions WHEN NEW.description = 'Tea' BEGIN SELECT RAISE(FAIL, 'synthetic second insert failure'); END")
			} else {
				_, err = fx.DB.Exec("CREATE FUNCTION reject_tea() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.description = 'Tea' THEN RAISE EXCEPTION 'synthetic second insert failure'; END IF; RETURN NEW; END; $$")
				if err == nil {
					_, err = fx.DB.Exec("CREATE TRIGGER reject_tea BEFORE INSERT ON transactions FOR EACH ROW EXECUTE FUNCTION reject_tea()")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			d := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
			v := 4.25
			records := []ledger.Transaction{{Date: &d, Description: "Coffee", Amount: &v, Account: "Test Account"}, {Date: &d, Description: "Tea", Amount: &v, Account: "Test Account"}}
			err = s.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
				return s.InsertTransactionsTx(context.Background(), tx, records)
			})
			if err == nil || !strings.Contains(err.Error(), "synthetic second insert failure") {
				t.Fatalf("error %v", err)
			}
			var n int
			if err := fx.DB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("rollback left %d rows", n)
			}
		})
	}
}
