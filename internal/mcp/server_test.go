package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	adapter "github.com/danielfrg/dinero/internal/mcp"
	"github.com/danielfrg/dinero/internal/server"
	"github.com/danielfrg/dinero/internal/testutil"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const token = "synthetic-read-token-123456"

type bearerTransport struct{ base http.RoundTripper }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return b.base.RoundTrip(r)
}

func fixture(t *testing.T) (*ledger.Service, func()) {
	t.Helper()
	fx := testutil.NewSQLite(t)
	fx.InsertNullable(t, "2026-03-08 12:00:00", "coffee", "Food", "Cafe", 12.25, "checking", "")
	fx.InsertNullable(t, "2026-03-09 12:00:00", "refund", "Food", "Cafe", -2.00, "checking", "")
	fx.InsertNullable(t, "2026-03-09 12:00:00", "unknown", "Food", "Cafe", nil, "checking", "")
	db := fx.Open(t, "US/Central")
	cfg := config.Config{Timezone: "US/Central", Plaid: config.Plaid{Tokens: map[string]string{"bank": "plaid-secret-sentinel"}, ItemIDs: map[string]string{"bank": "item-secret-sentinel"}}}
	return ledger.NewService(db, cfg), func() { db.Close() }
}

func connect(t *testing.T, endpoint string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "integration-test", Version: "0.1.0"}, nil)
	transport := &sdk.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport}}}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: result=%+v error=%v", name, result, err)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rest(t *testing.T, base, path string) map[string]any {
	t.Helper()
	r, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("REST %s: %d %s", path, resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRealHTTPClientListsAndCallsSixReadTools(t *testing.T) {
	svc, _ := fixture(t)
	opts := httpapi.Options{Token: []byte(token)}
	ts := httptest.NewServer(server.NewHandler(svc, opts, adapter.NewTransportHandler(svc)))
	defer ts.Close()
	session := connect(t, ts.URL+"/mcp")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"get_balance", "get_status", "list_accounts", "list_categories", "search_transactions", "summarize_transactions"}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not annotated read-only", tool.Name)
		}
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Errorf("%s lacks typed schema", tool.Name)
		}
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools=%v", names)
	}
	for _, tc := range []struct {
		name, path string
		args       map[string]any
	}{
		{"list_accounts", "/api/accounts", map[string]any{}},
		{"list_categories", "/api/categories", map[string]any{}},
		{"search_transactions", "/api/transactions", map[string]any{}},
		{"get_balance", "/api/balance?account=checking&through=2026-03-09", map[string]any{"account": "checking", "through": "2026-03-09"}},
		{"summarize_transactions", "/api/summary?after=2026-03-08&before=2026-03-09", map[string]any{"after": "2026-03-08", "before": "2026-03-09"}},
		{"get_status", "/api/status", map[string]any{}},
	} {
		got := call(t, session, tc.name, tc.args)
		expected := rest(t, ts.URL, tc.path)
		if tc.name == "get_status" {
			delete(got, "queried_at")
			delete(expected, "queried_at")
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s MCP=%v REST=%v", tc.name, got, expected)
		}
		b, _ := json.Marshal(got)
		if strings.Contains(string(b), "plaid-secret-sentinel") || strings.Contains(string(b), "item-secret-sentinel") {
			t.Errorf("%s leaked credentials", tc.name)
		}
	}
	if p := call(t, session, "search_transactions", map[string]any{}); p["limit"] != float64(50) || p["offset"] != float64(0) {
		t.Fatalf("pagination defaults=%v", p)
	}
}

func TestMCPRejectsUnauthorizedAndUntrustedRequests(t *testing.T) {
	svc, _ := fixture(t)
	h := adapter.NewHandler(svc, adapter.Options{Token: []byte(token)})
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range []struct {
				host, origin, auth string
				want               int
			}{
				{"localhost", "", "", 401},
				{"attacker.example", "", "Bearer " + token, 403},
				{"localhost", "https://attacker.example", "Bearer " + token, 403},
			} {
				r := httptest.NewRequest(method, "/mcp", nil)
				r.Host = tc.host
				r.Header.Set("Origin", tc.origin)
				r.Header.Set("Authorization", tc.auth)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != tc.want {
					t.Errorf("host=%s origin=%s auth=%s: %d", tc.host, tc.origin, tc.auth, w.Code)
				}
			}
		})
	}
	r := httptest.NewRequest("GET", "/mcp", nil)
	r.Host = "localhost"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("authenticated stateless GET: %d %s", w.Code, w.Body.String())
	}
}

func TestMCPInvalidUnknownCanceledAndUnavailable(t *testing.T) {
	svc, closeDB := fixture(t)
	ts := httptest.NewServer(server.NewHandler(svc, httpapi.Options{Token: []byte(token)}, adapter.NewTransportHandler(svc)))
	defer ts.Close()
	session := connect(t, ts.URL+"/mcp")
	for _, args := range []map[string]any{{"limit": 0}, {"year": 0}, {"month": 0}, {"limit": 501}, {"offset": -1}, {"sort": "description; DROP TABLE transactions"}} {
		result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "search_transactions", Arguments: args})
		if err == nil && (result == nil || !result.IsError) {
			t.Errorf("invalid args %v accepted: %+v", args, result)
		}
	}
	if got := call(t, session, "search_transactions", map[string]any{}); len(got["transactions"].([]any)) != 3 {
		t.Fatalf("invalid input reached SQL or changed table: %v", got)
	}
	if _, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "delete_all_transactions", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "list_accounts", Arguments: map[string]any{}}); err == nil {
		t.Fatal("canceled call succeeded")
	}
	closeDB()
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "list_accounts", Arguments: map[string]any{}})
	if err != nil || !result.IsError {
		t.Fatalf("unavailable: result=%+v err=%v", result, err)
	}
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), "sqlite") || strings.Contains(string(b), "ledger.sqlite") || strings.Contains(string(b), "plaid-secret") {
		t.Fatalf("database detail leaked: %s", b)
	}
}
