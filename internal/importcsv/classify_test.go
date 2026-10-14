package importcsv

import (
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
)

func TestKeyNormalizesAwareDateToConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 3, 4, 30, 0, 0, time.UTC)
	amount := 4.25
	got, ok := key(ledger.Transaction{Date: &date, Description: "Coffee", Account: "Test Account", Amount: &amount}, loc)
	if !ok || got.date != "2026-09-02" {
		t.Fatalf("key %+v valid %v", got, ok)
	}
}
