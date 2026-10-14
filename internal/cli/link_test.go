package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/plaid"
	"log"
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
			root := NewRoot(Dependencies{LinkProvider: provider, LinkClock: clock, OpenBrowser: func(string) error { browserCalls++; return errors.New("sentinel-link-private") }})
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
