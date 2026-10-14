package plaid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	sdk "github.com/plaid/plaid-go/v48/plaid"
)

const accessSentinel = "sentinel-access-private"
const publicSentinel = "sentinel-public-private"
const linkSentinel = "sentinel-link-private"

type fakeLink struct {
	creates, exchanges, polls int
	existing                  *string
	results                   []LinkResult
	accounts                  []LinkedAccount
	err                       error
	exchanged                 func()
	accountsHook              func()
}

func (f *fakeLink) CreateHostedLink(_ context.Context, _ string, token *string) (LinkSession, error) {
	f.creates++
	f.existing = token
	return LinkSession{linkToken: linkSentinel, sessionID: "session", URL: "https://secure.plaid.com/hosted/session"}, f.err
}
func (f *fakeLink) GetLinkResult(context.Context, LinkSession) (LinkResult, error) {
	f.polls++
	if f.err != nil {
		return LinkResult{}, f.err
	}
	if len(f.results) == 0 {
		return LinkResult{}, nil
	}
	r := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return r, nil
}
func (f *fakeLink) ExchangePublicToken(_ context.Context, p string) (config.LinkCredentials, error) {
	f.exchanges++
	if p != publicSentinel {
		return config.LinkCredentials{}, errors.New("wrong public token")
	}
	if f.exchanged != nil {
		f.exchanged()
	}
	return config.LinkCredentials{Token: accessSentinel, ItemID: "item"}, f.err
}
func (f *fakeLink) GetAccounts(context.Context, string) ([]LinkedAccount, error) {
	if f.accountsHook != nil {
		f.accountsHook()
	}
	return f.accounts, f.err
}

type fakeClock struct {
	waits   []time.Duration
	elapsed time.Duration
}

func (f *fakeClock) Wait(ctx context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	f.elapsed += d
	return ctx.Err()
}
func (f *fakeClock) Now() time.Time { return time.Unix(0, 0).Add(f.elapsed) }
func linkFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[database]\nconnection_string='sqlite:///synthetic.db'\n[custom]\nvalue='keep-me'\n[plaid.tokens]\nold='sentinel-old'\n[plaid.item_ids]\nold='old-item'\n[plaid.account_id_to_name]\na='Existing'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func successfulLink() *fakeLink {
	return &fakeLink{results: []LinkResult{{Done: true, sessionID: "session", publicToken: publicSentinel}}, accounts: []LinkedAccount{{ID: "a", Name: "Changed"}, {ID: "b", Name: "Savings"}, {ID: "c", Name: "Savings"}}}
}
func options(t *testing.T, p string, f *fakeLink) LinkOptions {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return LinkOptions{ConfigPath: p, Alias: "new", Provider: f, StateDir: dir, NoBrowser: true, Clock: &fakeClock{}}
}
func assertNoSecrets(t *testing.T, v any) {
	t.Helper()
	s := fmt.Sprintf("%+v", v)
	for _, secret := range []string{accessSentinel, publicSentinel, linkSentinel} {
		if strings.Contains(s, secret) {
			t.Fatal("credential disclosure")
		}
	}
}

func TestRunLinkNewPersistsBeforeNamingAndPreservesAccounts(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	o := options(t, p, f)
	o.NameAccounts = func(a []LinkedAccount, prior map[string]string) (map[string]string, error) {
		cfg, e := config.Load(p)
		if e != nil || cfg.Plaid.Tokens["new"] != accessSentinel || cfg.Plaid.ItemIDs["new"] != "item" {
			t.Fatal("credentials not durable before naming")
		}
		return map[string]string{"a": "Existing", "b": "Savings", "c": "Other Savings"}, nil
	}
	r, e := RunLink(context.Background(), o)
	if e != nil || !r.Persisted || f.exchanges != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	cfg, _ := config.Load(p)
	if cfg.Plaid.AccountIDToName["a"] != "Existing" || cfg.Plaid.AccountIDToName["c"] != "Other Savings" {
		t.Fatal("mappings lost")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "keep-me") {
		t.Fatal("unrelated configuration lost")
	}
	assertNoSecrets(t, r)
	assertNoSecrets(t, f.results[0])
	assertNoSecrets(t, LinkSession{linkToken: linkSentinel})
}
func TestRunLinkUpdateNeverExchanges(t *testing.T) {
	p := linkFixture(t)
	before, _ := config.Load(p)
	f := successfulLink()
	o := options(t, p, f)
	o.Alias = "old"
	o.Update = true
	r, e := RunLink(context.Background(), o)
	if e != nil || !r.Persisted || f.exchanges != 0 || f.existing == nil || *f.existing != "sentinel-old" {
		t.Fatalf("update failed: %v", e)
	}
	after, _ := config.Load(p)
	if after.Plaid.Tokens["old"] != before.Plaid.Tokens["old"] {
		t.Fatal("update replaced token")
	}
}
func TestRunLinkPreflightBeforeNetwork(t *testing.T) {
	for _, kind := range []string{"collision", "unknown-update", "readonly", "readonly-state"} {
		t.Run(kind, func(t *testing.T) {
			p := linkFixture(t)
			f := successfulLink()
			o := options(t, p, f)
			switch kind {
			case "collision":
				o.Alias = "old"
			case "unknown-update":
				o.Update = true
			case "readonly":
				os.Chmod(p, 0400)
			case "readonly-state":
				os.Chmod(o.StateDir, 0500)
			}
			_, e := RunLink(context.Background(), o)
			if e == nil || f.creates != 0 {
				t.Fatal("network happened before preflight rejection")
			}
		})
	}
}
func TestRunLinkRejectsInvalidResults(t *testing.T) {
	for _, r := range []LinkResult{{Done: true}, {Done: true, sessionID: "wrong", publicToken: publicSentinel}, {Failed: true, sessionID: "session"}, {Done: true, sessionID: "session", publicToken: publicSentinel, itemCount: 2}} {
		t.Run(fmt.Sprintf("%+v", r), func(t *testing.T) {
			p := linkFixture(t)
			f := successfulLink()
			f.results = []LinkResult{r}
			o := options(t, p, f)
			_, e := RunLink(context.Background(), o)
			if e == nil || f.exchanges != 0 {
				t.Fatal("invalid result exchanged")
			}
			assertNoSecrets(t, e)
		})
	}
}
func TestRunLinkTimeoutBackoffAndBrowserFallback(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	f.results = nil
	o := options(t, p, f)
	o.Timeout = 35 * time.Second
	c := &fakeClock{}
	o.Clock = c
	o.NoBrowser = false
	o.OpenBrowser = func(string) error { return errors.New(accessSentinel) }
	var url string
	o.ShowURL = func(s string) error { url = s; return nil }
	_, e := RunLink(context.Background(), o)
	if !errors.Is(e, context.DeadlineExceeded) || url == "" || !reflect.DeepEqual(c.waits, []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second, time.Second}) {
		t.Fatalf("timeout %v waits %v", e, c.waits)
	}
	assertNoSecrets(t, e)
}

type failingStore struct {
	fileLinkStore
	saves        int
	failAt       int
	recoveryFail bool
}

func (s *failingStore) Save(ctx context.Context, p string, c config.LinkCredentials) error {
	s.saves++
	if s.saves >= s.failAt {
		return errors.New(accessSentinel)
	}
	return s.fileLinkStore.Save(ctx, p, c)
}
func (s *failingStore) Protect(dir string, c config.LinkCredentials) (string, error) {
	if s.recoveryFail {
		return "", errors.New(publicSentinel)
	}
	return s.fileLinkStore.Protect(dir, c)
}
func TestRunLinkRecoveryAndResume(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			p := linkFixture(t)
			f := successfulLink()
			o := options(t, p, f)
			o.Store = &failingStore{failAt: failAt}
			r, e := RunLink(context.Background(), o)
			var recovery *RecoveryError
			if e == nil || !errors.As(e, &recovery) || r.Persisted || recovery.Path == "" {
				t.Fatalf("expected protected recovery: %+v %v", r, e)
			}
			assertNoSecrets(t, e)
			info, _ := os.Stat(recovery.Path)
			if info.Mode().Perm() != 0600 {
				t.Fatal("unprotected recovery")
			}
			r, e = ResumeLink(context.Background(), recovery.Path, p)
			if e != nil || !r.Persisted || f.exchanges != 1 {
				t.Fatalf("resume %v", e)
			}
			if _, e = os.Stat(recovery.Path); !os.IsNotExist(e) {
				t.Fatal("recovery not deleted")
			}
		})
	}
}
func TestRunLinkRecoveryFailureDoesNotClaimSuccess(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	o := options(t, p, f)
	o.Store = &failingStore{failAt: 1, recoveryFail: true}
	r, e := RunLink(context.Background(), o)
	var recovery *RecoveryError
	if e == nil || r.Persisted || errors.As(e, &recovery) {
		t.Fatal("fabricated recovery/success")
	}
	assertNoSecrets(t, e)
}
func TestRunLinkCancellation(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			p := linkFixture(t)
			before, _ := os.ReadFile(p)
			f := successfulLink()
			ctx, cancel := context.WithCancel(context.Background())
			o := options(t, p, f)
			if after {
				f.exchanged = cancel
			} else {
				o.Alias = "old"
				o.Update = true
				cancel()
			}
			r, e := RunLink(ctx, o)
			if after {
				cfg, _ := config.Load(p)
				if cfg.Plaid.Tokens["new"] != accessSentinel || !r.Persisted {
					t.Fatalf("cancel lost exchanged credentials: %v", e)
				}
			} else {
				b, _ := os.ReadFile(p)
				if e == nil || string(b) != string(before) || f.exchanges != 0 {
					t.Fatal("cancelled update changed configuration")
				}
			}
		})
	}
}
func TestResumeRejectsConflictAndUnsafeFile(t *testing.T) {
	p := linkFixture(t)
	s := fileLinkStore{}
	dir := t.TempDir()
	path, e := s.Protect(dir, config.LinkCredentials{Alias: "old", Token: accessSentinel, ItemID: "item"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ResumeLink(context.Background(), path, p); e == nil {
		t.Fatal("resume overwrote conflicting alias")
	}
	os.Chmod(path, 0644)
	if _, e = ResumeLink(context.Background(), path, p); e == nil {
		t.Fatal("accepted exposed credentials")
	}
}

func sdkLinkFixture(t *testing.T, handler http.HandlerFunc) *SDKClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := sdk.NewConfiguration()
	cfg.UseEnvironment(sdk.Environment(server.URL))
	cfg.AddDefaultHeader("PLAID-CLIENT-ID", "synthetic-client")
	cfg.AddDefaultHeader("PLAID-SECRET", "synthetic-secret")
	return &SDKClient{api: sdk.NewAPIClient(cfg)}
}

func TestSDKHostedLinkNewAndUpdateContracts(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			p := linkFixture(t)
			exchanges := 0
			provider := sdkLinkFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				switch r.URL.Path {
				case "/link/token/create":
					if _, ok := body["hosted_link"]; !ok {
						t.Error("Hosted Link object missing")
					}
					_, hasProducts := body["products"]
					if update {
						if hasProducts || body["access_token"] != "sentinel-old" {
							t.Error("update must pass existing token and omit products")
						}
					} else {
						if !hasProducts || body["access_token"] != nil {
							t.Error("new mode request malformed")
						}
					}
					fmt.Fprintf(w, `{"link_token":%q,"hosted_link_url":"https://secure.plaid.com/hosted/session","expiration":"2026-10-14T12:00:00Z","request_id":"synthetic"}`, linkSentinel)
				case "/link/token/get":
					if body["link_token"] != linkSentinel {
						t.Error("wrong polling token")
					}
					session := `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","on_success":null,"exit":null,"on_exit":null,"events":[{"event_name":"HANDOFF"}]}`
					if !update {
						session = `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","on_success":null,"exit":null,"on_exit":null,"results":{"item_add_results":[{"public_token":"sentinel-public-private"}]}}`
					}
					fmt.Fprintf(w, `{"link_token":%q,"link_sessions":[%s],"request_id":"synthetic"}`, linkSentinel, session)
				case "/item/public_token/exchange":
					exchanges++
					if body["public_token"] != publicSentinel {
						t.Error("wrong public token")
					}
					fmt.Fprintf(w, `{"access_token":%q,"item_id":"item","request_id":"synthetic"}`, accessSentinel)
				case "/accounts/get":
					expected := accessSentinel
					if update {
						expected = "sentinel-old"
					}
					if body["access_token"] != expected {
						t.Error("wrong accounts token")
					}
					fmt.Fprint(w, `{"accounts":[{"account_id":"b","name":"Savings"}],"item":{},"request_id":"synthetic"}`)
				default:
					t.Error("unexpected SDK endpoint")
					w.WriteHeader(404)
				}
			})
			o := options(t, p, successfulLink())
			o.Provider = provider
			o.Update = update
			if update {
				o.Alias = "old"
			}
			r, e := RunLink(context.Background(), o)
			expected := 1
			if update {
				expected = 0
			}
			if e != nil || !r.Persisted || exchanges != expected {
				t.Fatalf("SDK flow: %v exchanges %d", e, exchanges)
			}
			assertNoSecrets(t, r)
		})
	}
}

func TestSDKPollNullableSuccessExitAndSessionValidation(t *testing.T) {
	for _, tc := range []struct {
		name, session, token          string
		update, done, failed, wantErr bool
	}{
		{"empty", "", linkSentinel, true, false, false, false},
		{"null-finished", `{"link_session_id":"session","finished_at":null,"on_success":{"public_token":"unused"},"events":[{"event_name":"HANDOFF"}]}`, linkSentinel, true, false, false, false},
		{"finished-only", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","on_success":null,"exit":null,"on_exit":null}`, linkSentinel, true, false, false, false},
		{"handoff", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","on_success":null,"exit":null,"on_exit":null,"events":[{"event_name":"HANDOFF"}]}`, linkSentinel, true, true, false, false},
		{"legacy-success", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","on_success":{"public_token":"unused"}}`, linkSentinel, true, true, false, false},
		{"exit-event", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","events":[{"event_name":"HANDOFF"},{"event_name":"EXIT"}]}`, linkSentinel, true, false, true, false},
		{"exit-object", `{"link_session_id":"session","exit":{}}`, linkSentinel, true, false, true, false},
		{"legacy-exit", `{"link_session_id":"session","on_exit":{}}`, linkSentinel, true, false, true, false},
		{"wrong-token", `{"link_session_id":"session"}`, "different", true, false, false, true},
		{"wrong-session", `{"link_session_id":"different"}`, linkSentinel, true, false, false, true},
		{"multiple-sessions", `{"link_session_id":"session"},{"link_session_id":"second"}`, linkSentinel, true, false, false, true},
		{"multiple-items", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","results":{"item_add_results":[{"public_token":"a"},{"public_token":"b"}]}}`, linkSentinel, false, false, false, true},
		{"new-empty", `{"link_session_id":"session","finished_at":"2026-10-14T11:00:00Z","results":{"item_add_results":[]}}`, linkSentinel, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := sdkLinkFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"link_token":%q,"link_sessions":[%s]}`, tc.token, tc.session)
			})
			r, e := provider.GetLinkResult(context.Background(), LinkSession{linkToken: linkSentinel, sessionID: "session", update: tc.update})
			if (e != nil) != tc.wantErr || r.Done != tc.done || r.Failed != tc.failed {
				t.Fatalf("poll result %+v %v", r, e)
			}
			assertNoSecrets(t, e)
		})
	}
}

func TestSDKLinkErrorsNeverExposeProviderBody(t *testing.T) {
	for _, code := range []string{"ITEM_LOGIN_REQUIRED", "INVALID_ACCESS_TOKEN"} {
		provider := sdkLinkFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"error_type":"ITEM_ERROR","error_code":%q,"error_message":%q}`, code, accessSentinel+publicSentinel+linkSentinel)
		})
		calls := []func() error{func() error { _, e := provider.CreateHostedLink(context.Background(), "new", nil); return e }, func() error {
			_, e := provider.GetLinkResult(context.Background(), LinkSession{linkToken: linkSentinel})
			return e
		}, func() error { _, e := provider.ExchangePublicToken(context.Background(), publicSentinel); return e }, func() error { _, e := provider.GetAccounts(context.Background(), accessSentinel); return e }}
		for _, call := range calls {
			e := call()
			if e == nil {
				t.Fatal("missing SDK error")
			}
			var login *ErrLoginRequired
			if errors.As(e, &login) != (code == "ITEM_LOGIN_REQUIRED") {
				t.Fatal("wrong SDK error classification")
			}
			var providerError *ErrProvider
			if code != "ITEM_LOGIN_REQUIRED" && !errors.As(e, &providerError) {
				t.Fatal("missing safe typed provider error")
			}
			assertNoSecrets(t, e)
		}
	}
}

func TestHostedURLRejectsEncodedCredentialsAndUntrustedHost(t *testing.T) {
	for _, u := range []string{"https://secure.plaid.com/hosted?state=%73entinel-link-private", "https://example.com/hosted"} {
		if safeHostedURL(u, linkSentinel) {
			t.Fatal("unsafe Hosted Link URL accepted")
		}
	}
}

func TestRunLinkDefaultAccountNamesAvoidCollision(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	o := options(t, p, f)
	r, e := RunLink(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	if r.Accounts["a"] != "Existing" || r.Accounts["b"] != "new - Savings" || r.Accounts["c"] != "new - Savings (2)" {
		t.Fatalf("unsafe mappings %+v", r.Accounts)
	}
}

func TestRunLinkNamingCollisionPreservesCredentials(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	o := options(t, p, f)
	o.NameAccounts = func([]LinkedAccount, map[string]string) (map[string]string, error) {
		return map[string]string{"b": "Existing"}, nil
	}
	_, e := RunLink(context.Background(), o)
	var recovery *RecoveryError
	if !errors.As(e, &recovery) {
		t.Fatalf("collision lacked recovery: %v", e)
	}
	cfg, _ := config.Load(p)
	if cfg.Plaid.Tokens["new"] != accessSentinel || cfg.Plaid.AccountIDToName["b"] != "" {
		t.Fatal("collision damaged config")
	}
	if _, e = ResumeLink(context.Background(), recovery.Path, p); e == nil {
		t.Fatal("resume accepted conflicting name")
	}
	if _, e = os.Stat(recovery.Path); e != nil {
		t.Fatal("failed resume deleted credentials")
	}
}

func TestProviderIdentifiersRedactGoSyntaxFormatting(t *testing.T) {
	for _, v := range []any{LinkSession{linkToken: linkSentinel, sessionID: publicSentinel}, LinkResult{publicToken: publicSentinel}, config.LinkCredentials{Token: accessSentinel}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			s := fmt.Sprintf(format, v)
			for _, secret := range []string{linkSentinel, publicSentinel, accessSentinel} {
				if strings.Contains(s, secret) {
					t.Fatalf("unsafe formatting %s", format)
				}
			}
		}
	}
}

func TestRunLinkUpdateCancellationAfterSuccessLeavesConfigUnchanged(t *testing.T) {
	p := linkFixture(t)
	before, _ := os.ReadFile(p)
	f := successfulLink()
	ctx, cancel := context.WithCancel(context.Background())
	f.accountsHook = cancel
	o := options(t, p, f)
	o.Alias = "old"
	o.Update = true
	_, e := RunLink(ctx, o)
	after, _ := os.ReadFile(p)
	if e == nil || string(before) != string(after) || f.exchanges != 0 {
		t.Fatal("cancelled update changed config")
	}
}

func TestResumeRejectsSymlinkAndMalformedRecord(t *testing.T) {
	p := linkFixture(t)
	for _, kind := range []string{"symlink", "malformed", "empty", "extra-value"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "recovery.json")
			data := `{"version":1,"alias":"new","token":"sentinel-access-private","item_id":"item"}`
			switch kind {
			case "malformed":
				data = `{"token":"sentinel-access-private"`
			case "empty":
				data = `{}`
			case "extra-value":
				data += ` {}`
			}
			if e := os.WriteFile(path, []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			if kind == "symlink" {
				target := path
				path = filepath.Join(dir, "link")
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			}
			_, e := ResumeLink(context.Background(), path, p)
			if e == nil {
				t.Fatal("unsafe record accepted")
			}
			assertNoSecrets(t, e)
		})
	}
}

func TestRunLinkCancelledExchangeProtectsRecoveryOnDiskFailure(t *testing.T) {
	for _, doubleFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(doubleFailure), func(t *testing.T) {
			p := linkFixture(t)
			f := successfulLink()
			ctx, cancel := context.WithCancel(context.Background())
			f.exchanged = cancel
			o := options(t, p, f)
			o.Store = &failingStore{failAt: 1, recoveryFail: doubleFailure}
			r, e := RunLink(ctx, o)
			if e == nil || r.Persisted || f.exchanges != 1 {
				t.Fatal("cancelled exchange claimed success")
			}
			var recovery *RecoveryError
			if errors.As(e, &recovery) == doubleFailure {
				t.Fatal("incorrect recovery-path claim")
			}
			assertNoSecrets(t, e)
			assertNoSecrets(t, r)
			if !doubleFailure {
				saved, e := ResumeLink(context.Background(), recovery.Path, p)
				if e != nil || !saved.Persisted || f.exchanges != 1 {
					t.Fatalf("cancelled exchange recovery: %v", e)
				}
			}
		})
	}
}

func TestRunLinkDefaultDeadlineIsTenMinutes(t *testing.T) {
	p := linkFixture(t)
	f := successfulLink()
	f.results = nil
	o := options(t, p, f)
	c := &fakeClock{}
	o.Clock = c
	_, e := RunLink(context.Background(), o)
	if !errors.Is(e, context.DeadlineExceeded) || c.elapsed != 10*time.Minute {
		t.Fatalf("default deadline: %v %s", e, c.elapsed)
	}
	for _, d := range c.waits {
		if d > 10*time.Second {
			t.Fatal("poll interval exceeds maximum")
		}
	}
}
