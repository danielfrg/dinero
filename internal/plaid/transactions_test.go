package plaid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/plaid/plaid-go/v48/plaid"
)

func TestSDKTransactionsGetTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transactions/get" || r.Header.Get("PLAID-CLIENT-ID") != "synthetic-id" || r.Header.Get("PLAID-SECRET") != "synthetic-secret" {
			t.Errorf("request path/header %s %v", r.URL.Path, r.Header)
		}
		var body struct {
			AccessToken string `json:"access_token"`
			StartDate   string `json:"start_date"`
			EndDate     string `json:"end_date"`
			Options     struct {
				Count  int `json:"count"`
				Offset int `json:"offset"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.StartDate != "2026-07-17" || body.EndDate != "2026-10-14" || body.Options.Offset != 2 || body.Options.Count != 500 {
			t.Errorf("request %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"accounts":[],"transactions":[{"account_id":"a","date":"2026-10-14","name":"Coffee","amount":4.25,"pending":false}],"total_transactions":3,"item":{},"request_id":"synthetic"}`))
	}))
	defer server.Close()
	cfg := sdk.NewConfiguration()
	cfg.UseEnvironment(sdk.Environment(server.URL))
	cfg.AddDefaultHeader("PLAID-CLIENT-ID", "synthetic-id")
	cfg.AddDefaultHeader("PLAID-SECRET", "synthetic-secret")
	c := &SDKClient{api: sdk.NewAPIClient(cfg)}
	start, _ := time.Parse("2006-01-02", "2026-07-17")
	end, _ := time.Parse("2006-01-02", "2026-10-14")
	page, err := c.GetTransactions(context.Background(), "synthetic-token", start, end, 2)
	if err != nil || page.Total != 3 || len(page.Transactions) != 1 || page.Transactions[0].Name != "Coffee" {
		t.Fatalf("%+v %v", page, err)
	}
}

func TestRejectDevelopmentEnvironment(t *testing.T) {
	if _, err := New("synthetic-id", "synthetic-secret", "development"); err == nil {
		t.Fatal("development must be rejected")
	}
}
