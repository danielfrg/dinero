package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/plaid"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinkCLIValidationAndNoSecretOutput(t *testing.T) {
	for _, args := range [][]string{{"link"}, {"link", "new", "--update", "old"}, {"link", "--resume", "some-file", "new"}, {"link", "new", "--timeout", "0s"}} {
		root := NewRoot(Dependencies{})
		root.SetArgs(args)
		if root.ExecuteContext(context.Background()) == nil {
			t.Fatal("invalid Link arguments accepted")
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("[database]\nconnection_string='sqlite:///synthetic.db'\n[plaid]\nclient_id='sentinel-client'\nsecret='sentinel-secret'\n[plaid.tokens]\nold='sentinel-access-private'\n"), 0600)
	var out, errOut bytes.Buffer
	root := NewRoot(Dependencies{})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"--config", p, "link", "old", "--no-browser"})
	e := root.ExecuteContext(context.Background())
	if e == nil || !strings.Contains(e.Error(), "alias") {
		t.Fatalf("expected alias collision: %v", e)
	}
	for _, s := range []string{"sentinel-client", "sentinel-secret", "sentinel-access-private"} {
		if strings.Contains(out.String()+errOut.String()+e.Error(), s) {
			t.Fatal("Link output leaked credentials")
		}
	}
}

type cliLinkProvider struct {
	creates, exchanges int
	existing           *string
	failCreate         bool
	pending            bool
}

func (f *cliLinkProvider) CreateHostedLink(_ context.Context, _ string, token *string) (plaid.LinkSession, error) {
	f.creates++
	f.existing = token
	if f.failCreate {
		return plaid.LinkSession{}, errors.New("sentinel-access-private sentinel-public-private sentinel-link-private")
	}
	return plaid.LinkSession{URL: "https://secure.plaid.com/hosted/session"}, nil
}
func (f *cliLinkProvider) GetLinkResult(context.Context, plaid.LinkSession) (plaid.LinkResult, error) {
	return plaid.LinkResult{Failed: !f.pending}, nil
}
func (f *cliLinkProvider) ExchangePublicToken(context.Context, string) (config.LinkCredentials, error) {
	f.exchanges++
	return config.LinkCredentials{}, errors.New("unexpected exchange")
}
func (f *cliLinkProvider) GetAccounts(context.Context, string) ([]plaid.LinkedAccount, error) {
	return nil, errors.New("unexpected accounts call")
}

type cliClock struct{ elapsed time.Duration }

func (c *cliClock) Now() time.Time                                  { return time.Unix(0, 0).Add(c.elapsed) }
func (c *cliClock) Wait(ctx context.Context, d time.Duration) error { c.elapsed += d; return ctx.Err() }

func TestLinkCLIFlagsFallbackAndProviderOutputRedaction(t *testing.T) {
	for _, kind := range []string{"no-browser", "browser-failure", "update", "provider-error", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("DINERO_STATE_DIR", t.TempDir())
			p := filepath.Join(t.TempDir(), "config.toml")
			if e := os.WriteFile(p, []byte("[database]\nconnection_string='sqlite:///synthetic.db'\n[plaid.tokens]\nold='sentinel-access-private'\n"), 0600); e != nil {
				t.Fatal(e)
			}
			var out, errOut, logs bytes.Buffer
			priorLog := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(priorLog)
			provider := &cliLinkProvider{}
			clock := &cliClock{}
			browserCalls := 0
			root := NewRoot(Dependencies{LinkProvider: provider, LinkClock: clock, OpenBrowser: func(context.Context, string) error { browserCalls++; return errors.New("sentinel-link-private") }})
			args := []string{"--config", p, "link", "new"}
			switch kind {
			case "no-browser":
				args = append(args, "--no-browser")
			case "update":
				args = []string{"--config", p, "link", "--update", "old", "--no-browser"}
			case "provider-error":
				provider.failCreate = true
			case "timeout":
				provider.pending = true
				args = append(args, "--no-browser", "--timeout", "1s")
			}
			root.SetArgs(args)
			root.SetOut(&out)
			root.SetErr(&errOut)
			e := root.ExecuteContext(context.Background())
			if e == nil || provider.creates != 1 || provider.exchanges != 0 {
				t.Fatal("invalid CLI provider flow")
			}
			if kind == "update" && (provider.existing == nil || *provider.existing != "sentinel-access-private") {
				t.Fatal("CLI failed to pass existing token")
			}
			if kind == "no-browser" && browserCalls != 0 {
				t.Fatal("no-browser opened a browser")
			}
			if kind == "browser-failure" && !strings.Contains(errOut.String(), "https://secure.plaid.com/hosted/session") {
				t.Fatal("browser failure did not print fallback URL")
			}
			if kind == "timeout" && !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal("timeout flag ignored")
			}
			captured := out.String() + errOut.String() + logs.String() + e.Error()
			for _, secret := range []string{"sentinel-access-private", "sentinel-public-private", "sentinel-link-private"} {
				if strings.Contains(captured, secret) {
					t.Fatal("CLI stdout/stderr/logs/error leaked token")
				}
			}
		})
	}
}

func TestLinkCLIResumeUsesLocalFileAndNoProvider(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if e := os.WriteFile(p, []byte("[database]\nconnection_string='sqlite:///synthetic.db'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	recovery := filepath.Join(t.TempDir(), "recovery.json")
	if e := os.WriteFile(recovery, []byte(`{"version":1,"alias":"new","token":"sentinel-access-private","item_id":"item","accounts":{"a":"Checking"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	f := &cliLinkProvider{}
	var out, errOut bytes.Buffer
	root := NewRoot(Dependencies{LinkProvider: f})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"--config", p, "link", "--resume", recovery})
	if e := root.ExecuteContext(context.Background()); e != nil {
		t.Fatal(e)
	}
	cfg, e := config.Load(p)
	if e != nil || cfg.Plaid.Tokens["new"] != "sentinel-access-private" || cfg.Plaid.AccountIDToName["a"] != "Checking" || f.creates != 0 || f.exchanges != 0 {
		t.Fatal("resume did not persist locally")
	}
	if _, e = os.Stat(recovery); !os.IsNotExist(e) {
		t.Fatal("successful resume retained recovery")
	}
	if strings.Contains(fmt.Sprint(&out, &errOut), "sentinel-access-private") {
		t.Fatal("resume output leaked credentials")
	}
}

type syntheticLinkTransport struct{ endpoint *url.URL }

func (s syntheticLinkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = s.endpoint.Scheme
	u.Host = s.endpoint.Host
	copy.URL = &u
	return http.DefaultTransport.RoundTrip(copy)
}
func TestLinkCLITerminalNamingAfterPersistence(t *testing.T) {
	for _, kind := range []string{"terminal", "nonterminal", "collision", "too-long", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("DINERO_STATE_DIR", t.TempDir())
			p := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(p, []byte("[database]\nconnection_string='sqlite:///synthetic.db'\n[plaid.account_id_to_name]\na='Preserved'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/link/token/create":
					fmt.Fprint(w, `{"link_token":"synthetic-link","hosted_link_url":"https://secure.plaid.com/hosted/session","expiration":"2026-10-14T12:00:00Z","request_id":"synthetic"}`)
				case "/link/token/get":
					fmt.Fprint(w, `{"link_token":"synthetic-link","link_sessions":[{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","results":{"item_add_results":[{"public_token":"synthetic-public"}]}}],"request_id":"synthetic"}`)
				case "/item/public_token/exchange":
					fmt.Fprint(w, `{"access_token":"synthetic-access","item_id":"synthetic-item","request_id":"synthetic"}`)
				case "/accounts/get":
					fmt.Fprint(w, `{"accounts":[{"account_id":"a","name":"Changed","mask":"9999"},{"account_id":"b","name":"Savings","mask":"1234"}],"item":{"institution_name":"Bank"},"request_id":"synthetic"}`)
				default:
					t.Errorf("unexpected provider path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer ts.Close()
			u, _ := url.Parse(ts.URL)
			prior := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: syntheticLinkTransport{endpoint: u}}
			defer func() { http.DefaultClient = prior }()
			provider, err := plaid.New("synthetic-client", "synthetic-secret", "sandbox")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := "Friendly Savings\n"
			if kind == "collision" {
				input = "Preserved\n"
			}
			if kind == "too-long" {
				input = strings.Repeat("x", 51) + "\n"
			}
			var in io.Reader = strings.NewReader(input)
			if kind == "cancel" {
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				defer writer.Close()
				in = reader
			}
			var out, errOut bytes.Buffer
			root := NewRoot(Dependencies{LinkProvider: provider, LinkClock: &cliClock{}, IsTerminal: func(io.Reader) bool {
				cfg, e := config.Load(p)
				if e != nil || cfg.Plaid.Tokens["new"] != "synthetic-access" {
					t.Fatal("terminal detected before durable credentials")
				}
				if kind == "cancel" {
					cancel()
				}
				return kind != "nonterminal"
			}})
			root.SetIn(in)
			root.SetOut(&out)
			root.SetErr(&errOut)
			root.SetArgs([]string{"--config", p, "link", "new", "--no-browser"})
			done := make(chan error, 1)
			go func() { done <- root.ExecuteContext(ctx) }()
			select {
			case err = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("naming cancellation unbounded")
			}
			cfg, e := config.Load(p)
			if e != nil || cfg.Plaid.Tokens["new"] != "synthetic-access" || cfg.Plaid.AccountIDToName["a"] != "Preserved" {
				t.Fatal("credentials or existing mapping lost", e)
			}
			switch kind {
			case "terminal":
				if err != nil || cfg.Plaid.AccountIDToName["b"] != "Friendly Savings" || !strings.Contains(errOut.String(), "Bank - Savings (1234)") || strings.Contains(out.String(), "Account name") {
					t.Fatalf("terminal naming %v %v %q", cfg.Plaid.AccountIDToName, err, errOut.String())
				}
			case "nonterminal":
				if err != nil || cfg.Plaid.AccountIDToName["b"] != "Bank - Savings (1234)" || strings.Contains(errOut.String(), "Account name") {
					t.Fatalf("nonterminal names %v %v", cfg.Plaid.AccountIDToName, err)
				}
			default:
				if err == nil || cfg.Plaid.AccountIDToName["b"] != "" {
					t.Fatalf("failed naming saved mapping %v %v", cfg.Plaid.AccountIDToName, err)
				}
			}
		})
	}
}

func TestAccountPromptDeadlineDoesNotWaitForInput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := readAccountName(ctx, reader); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked terminal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt ignored deadline")
	}
}
