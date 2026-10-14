package httpapi

import (
	"context"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
	"time"
)

type Status struct {
	QueriedAt   time.Time `json:"queried_at"`
	Timezone    string    `json:"timezone"`
	Accounts    []Account `json:"accounts"`
	ItemAliases []string  `json:"item_aliases"`
	ItemCount   int       `json:"item_count"`
}
type statusOutput struct{ Body Status }

func GetStatus(ctx context.Context, s *ledger.Service) (Status, error) {
	v, err := s.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	out := Status{QueriedAt: v.QueriedAt, Timezone: v.Timezone, Accounts: make([]Account, 0, len(v.Accounts)), ItemAliases: v.ItemAliases, ItemCount: len(v.ItemAliases)}
	for _, a := range v.Accounts {
		out.Accounts = append(out.Accounts, Account{Name: a.Name, LatestDate: a.LatestDate})
	}
	return out, nil
}
func registerStatus(api huma.API, s *ledger.Service) {
	huma.Register(api, huma.Operation{OperationID: "get-status", Method: "GET", Path: "/api/status", Summary: "Get ledger status", Errors: []int{503}}, func(ctx context.Context, _ *struct{}) (*statusOutput, error) {
		v, err := GetStatus(ctx, s)
		if err != nil {
			return nil, apiError(err)
		}
		return &statusOutput{Body: v}, nil
	})
}
