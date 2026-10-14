package ledger

import (
	"context"
	"github.com/danielfrg/dinero/internal/config"
	"math"
	"sort"
	"strconv"
	"time"
)

type Service struct {
	repo Repository
	cfg  config.Config
}

func NewService(repo Repository, cfg config.Config) *Service { return &Service{repo: repo, cfg: cfg} }
func (s *Service) validateQuery(q Query) error {
	if q.Limit < 0 || q.Offset < 0 {
		return &ErrInvalidInput{Field: "limit/offset", Reason: "must be nonnegative"}
	}
	switch q.Sort {
	case "", "date", "amount", "description", "account", "category", "subcategory":
	default:
		return &ErrInvalidInput{Field: "sort", Reason: "unsupported column"}
	}
	if q.Month != nil && (*q.Month < 1 || *q.Month > 12) {
		return &ErrInvalidInput{Field: "month", Reason: "must be 1 through 12"}
	}
	if q.Year != nil && (*q.Year < 1 || *q.Year > 9999) {
		return &ErrInvalidInput{Field: "year", Reason: "out of range"}
	}
	loc, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		return &ErrInvalidInput{Field: "timezone", Reason: "unknown timezone"}
	}
	var after, before time.Time
	if q.After != nil {
		after, err = ParseDay(*q.After, loc)
		if err != nil {
			return err
		}
	}
	if q.Before != nil {
		before, err = ParseDay(*q.Before, loc)
		if err != nil {
			return err
		}
	}
	if q.After != nil && q.Before != nil && after.After(before) {
		return &ErrInvalidInput{Field: "range", Reason: "after is later than before"}
	}
	return nil
}
func (s *Service) Search(ctx context.Context, q Query) (Page, error) {
	if err := s.validateQuery(q); err != nil {
		return Page{}, err
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Sort == "" {
		q.Sort = "date"
	}
	p, err := s.repo.Search(ctx, q)
	if p.Transactions == nil {
		p.Transactions = []Transaction{}
	}
	return p, err
}

// SearchLegacy preserves the CLI's explicit SQL LIMIT 0 behavior.
// Transport adapters continue to normalize omitted limits and reject zero.
func (s *Service) SearchLegacy(ctx context.Context, q Query) (Page, error) {
	if q.Limit == 0 {
		if err := s.validateQuery(q); err != nil {
			return Page{}, err
		}
		return Page{Transactions: []Transaction{}, Limit: 0, Offset: q.Offset}, nil
	}
	return s.Search(ctx, q)
}
func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.repo.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]Account{}
	for _, a := range rows {
		if a.Name != "" {
			byName[a.Name] = a
		}
	}
	for _, name := range s.cfg.Plaid.AccountIDToName {
		if name != "" {
			if _, ok := byName[name]; !ok {
				byName[name] = Account{Name: name}
			}
		}
	}
	out := make([]Account, 0, len(byName))
	for _, a := range byName {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Service) Categories(ctx context.Context) (Categories, error) {
	c, err := s.repo.Categories(ctx)
	if c.Categories == nil {
		c.Categories = []string{}
	}
	if c.Subcategories == nil {
		c.Subcategories = []string{}
	}
	if c.Pairs == nil {
		c.Pairs = []CategoryPair{}
	}
	return c, err
}
func (s *Service) Balance(ctx context.Context, account, through string) (Balance, error) {
	return s.balance(ctx, account, through, true)
}

// BalanceLegacy retains the CLI decimal-string contract and validation.
func (s *Service) BalanceLegacy(ctx context.Context, account, through string) (Balance, error) {
	return s.balance(ctx, account, through, false)
}
func (s *Service) balance(ctx context.Context, account, through string, nullable bool) (Balance, error) {
	if account == "" {
		return Balance{}, &ErrInvalidInput{Field: "account", Reason: "required"}
	}
	loc, err := time.LoadLocation(s.cfg.Timezone)
	if err != nil {
		return Balance{}, &ErrInvalidInput{Field: "timezone", Reason: "unknown timezone"}
	}
	if _, err = ParseDay(through, loc); err != nil {
		return Balance{}, err
	}
	accounts, err := s.Accounts(ctx)
	if err != nil {
		return Balance{}, err
	}
	known := false
	for _, a := range accounts {
		if a.Name == account {
			known = true
			break
		}
	}
	if !known {
		return Balance{}, &ErrUnknownAccount{Account: account}
	}
	b, err := s.repo.Balance(ctx, account, through)
	if err != nil {
		return b, err
	}
	b.Account = account
	b.Through = through
	if b.Amount == "" {
		b.Amount = "0"
	}
	if nullable {
		if b.Unrepresentable {
			b.Amount = ""
			return b, nil
		}
		if value, e := strconv.ParseFloat(b.Amount, 64); e == nil && (math.IsNaN(value) || math.IsInf(value, 0)) {
			b.Amount = ""
			return b, nil
		}
	}
	b.Amount, err = FormatMoney(b.Amount)
	return b, err
}
func (s *Service) Summary(ctx context.Context, q SummaryQuery) (Summary, error) {
	if err := s.validateQuery(q.Query); err != nil {
		return Summary{}, err
	}
	switch q.GroupBy {
	case "", "account", "category", "subcategory", "month":
	default:
		return Summary{}, &ErrInvalidInput{Field: "group_by", Reason: "unsupported group"}
	}
	out, err := s.repo.Summary(ctx, q)
	if err != nil {
		return out, err
	}
	if out.Groups == nil {
		out.Groups = []SummaryGroup{}
	}
	for i := range out.Groups {
		g := &out.Groups[i]
		for _, field := range []*string{&g.SignedTotal, &g.Outflows, &g.Inflows} {
			if *field == "" {
				*field = "0"
			}
			v, e := FormatMoney(*field)
			if e != nil {
				return Summary{}, e
			}
			*field = v
		}
	}
	return out, nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	a, err := s.Accounts(ctx)
	if err != nil {
		return Status{}, err
	}
	aliasSet := make(map[string]struct{}, len(s.cfg.Plaid.ItemIDs)+len(s.cfg.Plaid.Tokens))
	for alias := range s.cfg.Plaid.ItemIDs {
		aliasSet[alias] = struct{}{}
	}
	for alias := range s.cfg.Plaid.Tokens {
		aliasSet[alias] = struct{}{}
	}
	aliases := make([]string, 0, len(aliasSet))
	for alias := range aliasSet {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return Status{QueriedAt: time.Now().UTC(), Timezone: s.cfg.Timezone, Accounts: a, ItemAliases: aliases}, nil
}
