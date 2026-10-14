package httpapi

import (
	"context"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
)

type Account struct {
	Name       string  `json:"name"`
	LatestDate *string `json:"latest_date"`
}
type Accounts struct {
	Accounts []Account `json:"accounts"`
}
type Categories struct {
	Categories    []string       `json:"categories"`
	Subcategories []string       `json:"subcategories"`
	Pairs         []CategoryPair `json:"pairs"`
}
type CategoryPair struct {
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
}
type accountsOutput struct{ Body Accounts }
type categoriesOutput struct{ Body Categories }

func ListAccounts(ctx context.Context, s *ledger.Service) (Accounts, error) {
	rows, err := s.Accounts(ctx)
	if err != nil {
		return Accounts{}, err
	}
	out := Accounts{Accounts: make([]Account, 0, len(rows))}
	for _, r := range rows {
		out.Accounts = append(out.Accounts, Account{Name: r.Name, LatestDate: r.LatestDate})
	}
	return out, nil
}
func ListCategories(ctx context.Context, s *ledger.Service) (Categories, error) {
	c, err := s.Categories(ctx)
	if err != nil {
		return Categories{}, err
	}
	out := Categories{Categories: c.Categories, Subcategories: c.Subcategories, Pairs: make([]CategoryPair, 0, len(c.Pairs))}
	for _, p := range c.Pairs {
		out.Pairs = append(out.Pairs, CategoryPair{Category: p.Category, Subcategory: p.Subcategory})
	}
	return out, nil
}
func registerAccounts(api huma.API, s *ledger.Service) {
	huma.Register(api, huma.Operation{OperationID: "list-accounts", Method: "GET", Path: "/api/accounts", Summary: "List ledger accounts", Errors: []int{503}}, func(ctx context.Context, _ *struct{}) (*accountsOutput, error) {
		a, err := ListAccounts(ctx, s)
		if err != nil {
			return nil, apiError(err)
		}
		return &accountsOutput{Body: a}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "list-categories", Method: "GET", Path: "/api/categories", Summary: "List ledger categories", Errors: []int{503}}, func(ctx context.Context, _ *struct{}) (*categoriesOutput, error) {
		c, err := ListCategories(ctx, s)
		if err != nil {
			return nil, apiError(err)
		}
		return &categoriesOutput{Body: c}, nil
	})
}
