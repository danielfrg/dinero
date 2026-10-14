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
	return httpapi.New(ledger.NewService(fx.Open(t, "US/Central"), cfg), httpapi.Options{})
}
func request(t *testing.T, h http.Handler, path, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "localhost"
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
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

func TestRootWelcomeKeepsExactRoutingAndRequestPolicy(t *testing.T) {
	h := handler(t)
	w := request(t, h, "/", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("root: %d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Dinero API") || !strings.Contains(body, "/api/docs") || !strings.Contains(body, "/mcp") || strings.Count(body, "\n") != 1 {
		t.Fatalf("root does not identify the service and endpoints in one line: %q", body)
	}
	if !strings.Contains(w.Body.String(), "<title>Dinero</title>") {
		t.Fatalf("root is missing its browser tab title: %q", w.Body.String())
	}
	for _, tc := range []struct {
		method, path, host, origin string
		want                       int
	}{
		{http.MethodGet, "/unknown", "localhost", "", http.StatusNotFound},
		{http.MethodPost, "/", "localhost", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", "attacker.example", "", http.StatusForbidden},
		{http.MethodGet, "/", "localhost", "https://attacker.example", http.StatusForbidden},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s host=%s origin=%s: %d %q", tc.method, tc.path, tc.host, tc.origin, w.Code, w.Body.String())
		}
	}
}

func TestSixReadsAndTypedMoney(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/api/accounts", "/api/categories", "/api/transactions", "/api/balance?account=checking&through=2026-03-09", "/api/summary?after=2026-03-08&before=2026-03-09", "/api/status"} {
		w := request(t, h, path, "")
		if w.Code != 200 {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	p := bodyMap(t, request(t, h, "/api/transactions?sort=description&limit=4", ""))
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
	b := bodyMap(t, request(t, h, "/api/balance?account=checking&through=2026-03-09", ""))
	if b["amount"] != "11.48" || b["currency"] != nil || b["incomplete_amount_count"] != float64(2) {
		t.Fatalf("balance=%v", b)
	}
	s := bodyMap(t, request(t, h, "/api/summary?after=2026-03-08&before=2026-03-09", ""))
	groups := s["groups"].([]any)
	g := groups[0].(map[string]any)
	if g["key"] != "Food" || g["signed_total"] != "10.25" || g["outflows"] != "12.25" || g["inflows"] != "-2.00" || g["count"] != float64(4) || s["incomplete_amount_count"] != float64(2) {
		t.Fatalf("summary=%v", s)
	}
	if currency, ok := g["currency"]; !ok || currency != nil {
		t.Fatalf("unknown summary currency=%v", g)
	}
	status := bodyMap(t, request(t, h, "/api/status", ""))
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
	h := httpapi.New(ledger.NewService(fx.Open(t, "US/Central"), config.Config{Timezone: "US/Central"}), httpapi.Options{})
	w := request(t, h, "/api/transactions?limit=500", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := bodyMap(t, w)
	if len(p["transactions"].([]any)) != 500 || p["has_more"] != true {
		t.Fatalf("page=%v", p)
	}
	s := bodyMap(t, request(t, h, "/api/summary?after=2026-03-08&before=2026-03-08", ""))
	g := s["groups"].([]any)[0].(map[string]any)
	if g["count"] != float64(501) || g["signed_total"] != "501.00" {
		t.Fatalf("summary=%v", s)
	}
	for _, path := range []string{"/api/transactions?limit=501", "/api/transactions?limit=0", "/api/transactions?limit=-1", "/api/transactions?offset=-1", "/api/transactions?year=0", "/api/transactions?month=0", "/api/summary?before=2026-03-08", "/api/summary?after=2026-03-08&before=2026-03-08&group_by=merchant"} {
		w := request(t, h, path, "")
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

func TestSummaryGroupsAndFiltersUseMatchingRows(t *testing.T) {
	h := handler(t)
	for group, key := range map[string]string{"account": "checking", "category": "Food", "subcategory": "Cafe", "month": "2026-03"} {
		path := "/api/summary?after=2026-03-08&before=2026-03-09&group_by=" + group
		w := request(t, h, path, "")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", group, w.Code, w.Body.String())
		}
		g := bodyMap(t, w)["groups"].([]any)[0].(map[string]any)
		if g["key"] != key || g["count"] != float64(4) {
			t.Errorf("%s: %v", group, g)
		}
	}
	w := request(t, h, "/api/summary?after=2026-03-08&before=2026-03-09&account=checking&category=Food&description=coffee", "")
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
	h := httpapi.New(ledger.NewService(db, config.Config{Timezone: "US/Central"}), httpapi.Options{})
	w := request(t, h, "/api/transactions", "")
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

func TestNoAuthenticationAndSanitizedFailure(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/api/accounts", "/api/openapi", "/api/docs"} {
		for _, authorization := range []string{"", "Bearer wrong", "Bearer plaid-secret-sentinel", "Basic ignored", "Bearer"} {
			w := request(t, h, path, authorization)
			if w.Code != 200 {
				t.Errorf("%s authorization %q: %d", path, authorization, w.Code)
			}
			if challenge := w.Header().Get("WWW-Authenticate"); challenge != "" {
				t.Errorf("%s returned an authentication challenge: %q", path, challenge)
			}
		}
	}
	if w := request(t, h, "/api/missing", ""); w.Code != 404 {
		t.Errorf("missing route: %d", w.Code)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		w := request(t, h, path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "checking") {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/accounts", nil)
	r.Host = "attacker.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("host=%d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/accounts", nil)
	r.Host = "localhost"
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("origin=%d", w.Code)
	}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })
	_ = request(t, h, "/api/accounts?secret=query-secret-sentinel", "")
	_ = request(t, h, "/api/path-secret-sentinel", "")
	_ = request(t, h, "/healthz", "")
	_ = request(t, h, "/", "")
	if strings.Contains(logs.String(), "secret-sentinel") {
		t.Fatalf("secret in logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "path=/healthz") {
		t.Fatalf("health request was not logged: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "path=/ status=200") {
		t.Fatalf("root request was not logged with its path: %s", logs.String())
	}
	fx := testutil.NewSQLite(t)
	db := fx.Open(t, "US/Central")
	db.Close()
	broken := httpapi.New(ledger.NewService(db, config.Config{Timezone: "US/Central"}), httpapi.Options{})
	w = request(t, broken, "/api/accounts", "")
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
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOpenAPIHasNoAuthenticationRequirements(t *testing.T) {
	w := request(t, handler(t), "/api/openapi", "")
	if w.Code != http.StatusOK {
		t.Fatalf("OpenAPI: %d %s", w.Code, w.Body.String())
	}
	spec := bodyMap(t, w)
	if security, ok := spec["security"].([]any); ok && len(security) != 0 {
		t.Fatalf("OpenAPI requires authentication: %v", security)
	}
	components := spec["components"].(map[string]any)
	if schemes, ok := components["securitySchemes"].(map[string]any); ok && len(schemes) != 0 {
		t.Fatalf("OpenAPI advertises authentication: %v", schemes)
	}
	for path, raw := range spec["paths"].(map[string]any) {
		for method, operation := range raw.(map[string]any) {
			if fields, ok := operation.(map[string]any); ok {
				if security, ok := fields["security"].([]any); ok && len(security) != 0 {
					t.Errorf("%s %s requires authentication: %v", method, path, security)
				}
			}
		}
	}
}
