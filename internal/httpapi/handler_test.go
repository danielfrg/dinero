package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/testutil"
)

func handler(t *testing.T) http.Handler {
	t.Helper()
	fx := testutil.NewSQLite(t)
	fx.InsertNullable(t, "2026-03-08 12:00:00", "coffee", "Food", "Cafe", 12.25, "checking", "")
	fx.InsertNullable(t, "2026-03-09 12:00:00", "refund", "Food", "Cafe", -2.00, "checking", "")
	fx.InsertNullable(t, "2026-03-09 12:00:00", "unknown", "Food", "Cafe", nil, "checking", "")
	fx.InsertNullable(t, "2026-03-09 12:00:00", "fraction", "Food", "Cafe", 1.234, "checking", "")
	cfg := config.Config{Timezone: "US/Central", Plaid: config.Plaid{Tokens: map[string]string{"bank": "plaid-secret-sentinel"}, ItemIDs: map[string]string{"bank": "item-secret-sentinel"}}}
	return httpapi.New(ledger.NewService(fx.Open(t, "US/Central"), cfg), httpapi.Options{Token: []byte("read-secret-sentinel")})
}
func request(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "localhost"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func bodyMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return out
}

func TestSixReadsAndTypedMoney(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/api/accounts", "/api/categories", "/api/transactions", "/api/balance?account=checking&through=2026-03-09", "/api/summary?after=2026-03-08&before=2026-03-09", "/api/status"} {
		w := request(t, h, path, "read-secret-sentinel")
		if w.Code != 200 {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	p := bodyMap(t, request(t, h, "/api/transactions?sort=description&limit=4", "read-secret-sentinel"))
	rows := p["transactions"].([]any)
	if len(rows) != 4 {
		t.Fatalf("rows=%d", len(rows))
	}
	seenNull := false
	seenInvalid := false
	for _, raw := range rows {
		r := raw.(map[string]any)
		if r["currency"] != nil {
			t.Fatalf("currency=%v", r["currency"])
		}
		if r["date"] == nil {
			t.Fatal("date missing")
		}
		if r["amount"] == nil {
			seenNull = true
		}
		if r["invalid_amount"] == true {
			seenInvalid = true
		}
		if v, ok := r["amount"].(float64); ok {
			t.Fatalf("numeric amount: %v", v)
		}
	}
	if !seenNull || !seenInvalid {
		t.Fatalf("null/invalid missing: %v", p)
	}
	b := bodyMap(t, request(t, h, "/api/balance?account=checking&through=2026-03-09", "read-secret-sentinel"))
	if b["amount"] != "11.48" || b["currency"] != nil || b["incomplete_amount_count"] != float64(2) {
		t.Fatalf("balance=%v", b)
	}
	s := bodyMap(t, request(t, h, "/api/summary?after=2026-03-08&before=2026-03-09", "read-secret-sentinel"))
	groups := s["groups"].([]any)
	g := groups[0].(map[string]any)
	if g["key"] != "Food" || g["signed_total"] != "10.25" || g["outflows"] != "12.25" || g["inflows"] != "-2.00" || g["count"] != float64(4) || s["incomplete_amount_count"] != float64(2) {
		t.Fatalf("summary=%v", s)
	}
	if currency, ok := g["currency"]; !ok || currency != nil {
		t.Fatalf("unknown summary currency=%v", g)
	}
	status := bodyMap(t, request(t, h, "/api/status", "read-secret-sentinel"))
	encoded, _ := json.Marshal(status)
	if bytes.Contains(encoded, []byte("plaid-secret-sentinel")) || bytes.Contains(encoded, []byte("item-secret-sentinel")) {
		t.Fatalf("secret in status: %s", encoded)
	}
}

func TestPaginationAndSummaryFullDataset(t *testing.T) {
	fx := testutil.NewSQLite(t)
	for i := 0; i < 501; i++ {
		fx.InsertNullable(t, "2026-03-08 12:00:00", "row", "Food", "", 1.0, "checking", "")
	}
	h := httpapi.New(ledger.NewService(fx.Open(t, "US/Central"), config.Config{Timezone: "US/Central"}), httpapi.Options{Token: []byte("read")})
	w := request(t, h, "/api/transactions?limit=500", "read")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := bodyMap(t, w)
	if len(p["transactions"].([]any)) != 500 || p["has_more"] != true {
		t.Fatalf("page=%v", p)
	}
	s := bodyMap(t, request(t, h, "/api/summary?after=2026-03-08&before=2026-03-08", "read"))
	g := s["groups"].([]any)[0].(map[string]any)
	if g["count"] != float64(501) || g["signed_total"] != "501.00" {
		t.Fatalf("summary=%v", s)
	}
	for _, path := range []string{"/api/transactions?limit=501", "/api/transactions?limit=0", "/api/transactions?limit=-1", "/api/transactions?offset=-1", "/api/transactions?year=0", "/api/transactions?month=0", "/api/summary?before=2026-03-08", "/api/summary?after=2026-03-08&before=2026-03-08&group_by=merchant"} {
		w := request(t, h, path, "read")
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

func TestSummaryGroupsAndFiltersUseMatchingRows(t *testing.T) {
	h := handler(t)
	for group, key := range map[string]string{"account": "checking", "category": "Food", "subcategory": "Cafe", "month": "2026-03"} {
		path := "/api/summary?after=2026-03-08&before=2026-03-09&group_by=" + group
		w := request(t, h, path, "read-secret-sentinel")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", group, w.Code, w.Body.String())
		}
		g := bodyMap(t, w)["groups"].([]any)[0].(map[string]any)
		if g["key"] != key || g["count"] != float64(4) {
			t.Errorf("%s: %v", group, g)
		}
	}
	w := request(t, h, "/api/summary?after=2026-03-08&before=2026-03-09&account=checking&category=Food&description=coffee", "read-secret-sentinel")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	g := bodyMap(t, w)["groups"].([]any)[0].(map[string]any)
	if g["count"] != float64(1) || g["signed_total"] != "12.25" {
		t.Fatalf("filtered summary=%v", g)
	}
}

func TestNullDateAndReadinessFailure(t *testing.T) {
	fx := testutil.NewSQLite(t)
	fx.InsertNullable(t, "", "undated", "", "", nil, "checking", "")
	db := fx.Open(t, "US/Central")
	h := httpapi.New(ledger.NewService(db, config.Config{Timezone: "US/Central"}), httpapi.Options{Token: []byte("read")})
	w := request(t, h, "/api/transactions", "read")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	row := bodyMap(t, w)["transactions"].([]any)[0].(map[string]any)
	if row["date"] != nil || row["amount"] != nil || row["invalid_amount"] != true {
		t.Fatalf("nullable row=%v", row)
	}
	db.Close()
	w = request(t, h, "/readyz", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), fx.DSN) {
		t.Fatalf("readiness=%d %s", w.Code, w.Body.String())
	}
}

func TestAuthenticationPolicyAndSanitizedFailure(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/api/accounts", "/api/openapi", "/api/docs", "/api/missing"} {
		for _, token := range []string{"", "wrong", "plaid-secret-sentinel"} {
			w := request(t, h, path, token)
			if w.Code != 401 {
				t.Errorf("%s token %q: %d", path, token, w.Code)
			}
		}
	}
	for _, path := range []string{"/api/openapi", "/api/docs"} {
		if w := request(t, h, path, "read-secret-sentinel"); w.Code != 200 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		w := request(t, h, path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "checking") {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/accounts", nil)
	r.Host = "attacker.example"
	r.Header.Set("Authorization", "Bearer read-secret-sentinel")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("host=%d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/accounts", nil)
	r.Host = "localhost"
	r.Header.Set("Origin", "https://attacker.example")
	r.Header.Set("Authorization", "Bearer read-secret-sentinel")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("origin=%d", w.Code)
	}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })
	_ = request(t, h, "/api/accounts?secret=query-secret-sentinel", "read-secret-sentinel")
	_ = request(t, h, "/api/path-secret-sentinel", "read-secret-sentinel")
	_ = request(t, h, "/healthz", "")
	if strings.Contains(logs.String(), "secret-sentinel") {
		t.Fatalf("secret in logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "path=/healthz") {
		t.Fatalf("health request was not logged: %s", logs.String())
	}
	fx := testutil.NewSQLite(t)
	db := fx.Open(t, "US/Central")
	db.Close()
	broken := httpapi.New(ledger.NewService(db, config.Config{Timezone: "US/Central"}), httpapi.Options{Token: []byte("read")})
	w = request(t, broken, "/api/accounts", "read")
	if w.Code != 503 || strings.Contains(w.Body.String(), fx.DSN) {
		t.Fatalf("unavailable=%d %s", w.Code, w.Body.String())
	}
}

func TestCanceledRead(t *testing.T) {
	h := handler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/api/accounts", nil).WithContext(ctx)
	r.Host = "localhost"
	r.Header.Set("Authorization", "Bearer read-secret-sentinel")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
