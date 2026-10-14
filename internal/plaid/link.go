package plaid

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	sdk "github.com/plaid/plaid-go/v48/plaid"
)

const DefaultLinkTimeout = 10 * time.Minute

// Provider identifiers are private and redact when formatted.
type LinkSession struct {
	URL                  string
	linkToken, sessionID string
	update               bool
}

func (LinkSession) String() string     { return "LinkSession{[REDACTED]}" }
func (s LinkSession) GoString() string { return s.String() }

type LinkResult struct {
	Done, Failed           bool
	sessionID, publicToken string
	itemCount              int
}

func (r LinkResult) String() string {
	return fmt.Sprintf("LinkResult{Done:%t, Failed:%t}", r.Done, r.Failed)
}
func (r LinkResult) GoString() string { return r.String() }

type LinkedAccount struct{ ID, Name string }
type LinkProvider interface {
	CreateHostedLink(context.Context, string, *string) (LinkSession, error)
	GetLinkResult(context.Context, LinkSession) (LinkResult, error)
	ExchangePublicToken(context.Context, string) (config.LinkCredentials, error)
	GetAccounts(context.Context, string) ([]LinkedAccount, error)
}
type LinkClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type realLinkClock struct{}

func (realLinkClock) Now() time.Time { return time.Now() }
func (realLinkClock) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// LinkStore allows durable storage failures to be exercised independently of banks.
type LinkStore interface {
	Lock(context.Context, string) (func(), error)
	Load(string) (config.Config, error)
	Preflight(string) error
	PreflightRecovery(string) error
	Save(context.Context, string, config.LinkCredentials) error
	Protect(string, config.LinkCredentials) (string, error)
}
type LinkOptions struct {
	ConfigPath, Alias, StateDir string
	Update, NoBrowser           bool
	Timeout                     time.Duration
	Provider                    LinkProvider
	Store                       LinkStore
	Clock                       LinkClock
	OpenBrowser                 func(string) error
	ShowURL                     func(string) error
	NameAccounts                func([]LinkedAccount, map[string]string) (map[string]string, error)
}
type SafeLinkResult struct {
	Alias     string            `json:"alias"`
	ItemID    string            `json:"item_id"`
	Accounts  map[string]string `json:"accounts"`
	Persisted bool              `json:"persisted"`
}

func linkError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var login *ErrLoginRequired
	if errors.As(err, &login) {
		return login
	}
	return fmt.Errorf("Plaid %s failed", operation)
}
func (c *SDKClient) CreateHostedLink(ctx context.Context, alias string, existingToken *string) (LinkSession, error) {
	req := sdk.NewLinkTokenCreateRequest("Dinero", "en", []sdk.CountryCode{sdk.COUNTRYCODE_US})
	req.SetUser(sdk.LinkTokenCreateRequestUser{ClientUserId: alias})
	req.SetHostedLink(*sdk.NewLinkTokenCreateHostedLink())
	if existingToken != nil {
		req.SetAccessToken(*existingToken)
	} else {
		req.SetProducts([]sdk.Products{sdk.PRODUCTS_TRANSACTIONS})
	}
	resp, _, err := c.api.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*req).Execute()
	if err != nil {
		return LinkSession{}, safeSDKLinkError(ctx, "link/token/create", err)
	}
	session := LinkSession{URL: resp.GetHostedLinkUrl(), linkToken: resp.GetLinkToken(), update: existingToken != nil}
	if session.linkToken == "" || !safeHostedURL(session.URL, session.linkToken) {
		return LinkSession{}, errors.New("Plaid returned an invalid Hosted Link session")
	}
	return session, nil
}
func safeSDKLinkError(ctx context.Context, operation string, err error) error {
	if apiError, decodeErr := sdk.ToPlaidError(err); decodeErr == nil && apiError.GetErrorCode() == "ITEM_LOGIN_REQUIRED" {
		return &ErrLoginRequired{}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &ErrProvider{operation: operation}
}
func (c *SDKClient) GetLinkResult(ctx context.Context, session LinkSession) (LinkResult, error) {
	resp, _, err := c.api.PlaidApi.LinkTokenGet(ctx).LinkTokenGetRequest(*sdk.NewLinkTokenGetRequest(session.linkToken)).Execute()
	if err != nil {
		return LinkResult{}, safeSDKLinkError(ctx, "link/token/get", err)
	}
	if resp.GetLinkToken() != session.linkToken {
		return LinkResult{}, errors.New("Plaid returned a different Link session")
	}
	sessions := resp.GetLinkSessions()
	if len(sessions) == 0 {
		return LinkResult{}, nil
	}
	if len(sessions) != 1 {
		return LinkResult{}, errors.New("Plaid returned multiple Link sessions")
	}
	s := sessions[0]
	id := s.GetLinkSessionId()
	if id == "" || (session.sessionID != "" && id != session.sessionID) {
		return LinkResult{}, errors.New("Plaid returned an invalid Link session")
	}
	result := LinkResult{sessionID: id}
	exit, _ := s.GetExitOk()
	oldExit, _ := s.GetOnExitOk()
	result.Failed = exit != nil || oldExit != nil
	success, _ := s.GetOnSuccessOk()
	positive := success != nil
	for _, event := range s.GetEvents() {
		switch event.GetEventName() {
		case "HANDOFF":
			positive = true
		case "EXIT":
			result.Failed = true
		}
	}
	if result.Failed {
		return result, nil
	}
	finished, _ := s.GetFinishedAtOk()
	if finished == nil {
		return result, nil
	}
	adds := s.GetResults().ItemAddResults
	result.itemCount = len(adds)
	if len(adds) > 1 {
		return LinkResult{}, errors.New("Plaid returned multiple Items")
	}
	if session.update {
		result.Done = positive
		return result, nil
	}
	if len(adds) == 1 {
		result.publicToken = adds[0].GetPublicToken()
		result.Done = result.publicToken != ""
	}
	if !result.Done {
		return LinkResult{}, errors.New("Plaid completed Link without a new Item")
	}
	return result, nil
}
func (c *SDKClient) ExchangePublicToken(ctx context.Context, token string) (config.LinkCredentials, error) {
	resp, _, err := c.api.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*sdk.NewItemPublicTokenExchangeRequest(token)).Execute()
	if err != nil {
		return config.LinkCredentials{}, safeSDKLinkError(ctx, "item/public_token/exchange", err)
	}
	result := config.LinkCredentials{Token: resp.GetAccessToken(), ItemID: resp.GetItemId()}
	if result.Token == "" || result.ItemID == "" {
		return config.LinkCredentials{}, errors.New("Plaid returned incomplete Item credentials")
	}
	return result, nil
}
func (c *SDKClient) GetAccounts(ctx context.Context, token string) ([]LinkedAccount, error) {
	resp, _, err := c.api.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*sdk.NewAccountsGetRequest(token)).Execute()
	if err != nil {
		return nil, safeSDKLinkError(ctx, "accounts/get", err)
	}
	result := make([]LinkedAccount, 0, len(resp.GetAccounts()))
	seen := map[string]bool{}
	for _, a := range resp.GetAccounts() {
		id := a.GetAccountId()
		if id == "" || seen[id] {
			return nil, errors.New("Plaid returned invalid account IDs")
		}
		seen[id] = true
		result = append(result, LinkedAccount{ID: id, Name: a.GetName()})
	}
	return result, nil
}
func safeHostedURL(value string, secrets ...string) bool {
	u, e := url.Parse(value)
	if e != nil || u.Scheme != "https" || u.User != nil || (u.Hostname() != "plaid.com" && !strings.HasSuffix(u.Hostname(), ".plaid.com")) {
		return false
	}
	decoded, e := url.QueryUnescape(value)
	if e != nil {
		return false
	}
	for _, secret := range secrets {
		if secret != "" && (strings.Contains(value, secret) || strings.Contains(decoded, secret)) {
			return false
		}
	}
	return true
}

// OpenBrowser executes a platform opener with an argument vector, never a shell.
func OpenBrowser(value string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", value)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", value)
	default:
		cmd = exec.Command("xdg-open", value)
	}
	if err := cmd.Run(); err != nil {
		return errors.New("browser could not be opened")
	}
	return nil
}

func RunLink(ctx context.Context, opts LinkOptions) (SafeLinkResult, error) {
	result := SafeLinkResult{Alias: opts.Alias, Accounts: map[string]string{}}
	if strings.TrimSpace(opts.Alias) == "" || opts.Timeout < 0 {
		return result, errors.New("Link alias and a positive timeout are required")
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultLinkTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if opts.Clock == nil {
		opts.Clock = realLinkClock{}
	}
	if opts.Store == nil {
		opts.Store = fileLinkStore{}
	}
	if opts.StateDir == "" {
		opts.StateDir = filepath.Join(config.StateDir(), "link-recovery")
	}
	path := config.ResolvePath(opts.ConfigPath)
	unlock, err := opts.Store.Lock(ctx, path)
	if err != nil {
		return result, errors.New("cannot lock Link configuration")
	}
	defer unlock()
	if err := opts.Store.Preflight(path); err != nil {
		return result, errors.New("Link configuration must be writable")
	}
	cfg, err := opts.Store.Load(path)
	if err != nil {
		return result, errors.New("cannot load Link configuration")
	}
	token, exists := cfg.Plaid.Tokens[opts.Alias]
	if opts.Update && !exists {
		return result, errors.New("Link update alias does not exist")
	}
	if !opts.Update && exists {
		return result, errors.New("Link alias already exists")
	}
	if err := opts.Store.PreflightRecovery(opts.StateDir); err != nil {
		return result, errors.New("Link recovery directory must be protected and writable")
	}
	provider := opts.Provider
	if provider == nil {
		provider, err = New(cfg.Plaid.ClientID, cfg.Plaid.Secret, cfg.Plaid.Env)
		if err != nil {
			return result, errors.New("invalid Plaid Link configuration")
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	var existing *string
	if opts.Update {
		existing = &token
	}
	session, err := provider.CreateHostedLink(ctx, opts.Alias, existing)
	if err != nil {
		return result, linkError(ctx, "link/token/create", err)
	}
	if !safeHostedURL(session.URL, session.linkToken, token) {
		return result, errors.New("Plaid returned an unsafe Hosted Link URL")
	}
	if opts.OpenBrowser == nil {
		opts.OpenBrowser = OpenBrowser
	}
	show := opts.NoBrowser
	if !show {
		show = opts.OpenBrowser(session.URL) != nil
	}
	if show && opts.ShowURL != nil {
		if err := opts.ShowURL(session.URL); err != nil {
			return result, errors.New("cannot display Hosted Link URL")
		}
	}
	deadline := opts.Clock.Now().Add(opts.Timeout)
	delay := 2 * time.Second
	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if !opts.Clock.Now().Before(deadline) {
			return result, context.DeadlineExceeded
		}
		poll, e := provider.GetLinkResult(ctx, session)
		if e != nil {
			return result, linkError(ctx, "link/token/get", e)
		}
		if poll.Failed || poll.itemCount > 1 || (poll.Done && (poll.sessionID == "" || (session.sessionID != "" && poll.sessionID != session.sessionID))) {
			return result, errors.New("Link did not complete a valid single Item session")
		}
		if poll.sessionID != "" {
			if session.sessionID != "" && session.sessionID != poll.sessionID {
				return result, errors.New("Plaid returned a different Link session")
			}
			session.sessionID = poll.sessionID
		}
		if poll.Done {
			credentials := config.LinkCredentials{Alias: opts.Alias, ItemID: cfg.Plaid.ItemIDs[opts.Alias]}
			if !opts.Update {
				if poll.publicToken == "" {
					return result, errors.New("Link completed without a public token")
				}
				credentials, e = provider.ExchangePublicToken(ctx, poll.publicToken)
				if e != nil {
					return result, linkError(ctx, "item/public_token/exchange", e)
				}
				credentials.Alias = opts.Alias
				if credentials.Token == "" || credentials.ItemID == "" {
					return result, errors.New("Plaid returned incomplete Item credentials")
				}
				token = credentials.Token
				// A successful exchange must survive caller cancellation. The independent
				// deadline bounds local fsync/rename while the configuration lock is held.
				durable, durableCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				e = opts.Store.Save(durable, path, credentials)
				durableCancel()
				if e != nil {
					return result, protectFailure(opts.Store, opts.StateDir, credentials)
				}
			}
			result.ItemID = credentials.ItemID
			result.Persisted = true
			if err = ctx.Err(); err != nil {
				return result, err
			}
			accounts, e := provider.GetAccounts(ctx, token)
			if e != nil {
				return result, linkError(ctx, "accounts/get", e)
			}
			mappings := defaultAccountNames(opts.Alias, accounts, cfg.Plaid.AccountIDToName)
			if opts.NameAccounts != nil {
				mappings, e = opts.NameAccounts(accounts, cfg.Plaid.AccountIDToName)
				if e != nil {
					return result, errors.New("Link credentials saved; account naming did not complete")
				}
			}
			credentials.Accounts = mappings
			if e = opts.Store.Save(ctx, path, credentials); e != nil {
				if opts.Update {
					return result, errors.New("Link update account names were not saved")
				}
				result.Persisted = false
				return result, protectFailure(opts.Store, opts.StateDir, credentials)
			}
			result.Accounts = mappings
			return result, nil
		}
		wait := delay
		if remaining := deadline.Sub(opts.Clock.Now()); wait > remaining {
			wait = remaining
		}
		if err = opts.Clock.Wait(ctx, wait); err != nil {
			return result, err
		}
		delay *= 2
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
	}
}
func defaultAccountNames(alias string, accounts []LinkedAccount, prior map[string]string) map[string]string {
	result := map[string]string{}
	used := map[string]bool{}
	for _, name := range prior {
		used[name] = true
	}
	ordered := append([]LinkedAccount(nil), accounts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, a := range ordered {
		if name := prior[a.ID]; name != "" {
			result[a.ID] = name
			continue
		}
		base := alias + " - " + strings.TrimSpace(a.Name)
		if strings.TrimSpace(a.Name) == "" {
			base = alias + " - Account"
		}
		name := base
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s (%d)", base, n)
		}
		used[name] = true
		result[a.ID] = name
	}
	return result
}
