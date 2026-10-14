package httpapi

import (
	"context"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
)

type BalanceInput struct {
	Account string `query:"account"`
	Through string `query:"through"`
}
type Balance struct {
	Account               string  `json:"account"`
	Through               string  `json:"through"`
	Amount                *string `json:"amount"`
	Currency              *string `json:"currency"`
	IncompleteAmountCount int64   `json:"incomplete_amount_count"`
}
type balanceOutput struct{ Body Balance }

func GetBalance(ctx context.Context, s *ledger.Service, in BalanceInput) (Balance, error) {
	b, err := s.Balance(ctx, in.Account, in.Through)
	if err != nil {
		return Balance{}, err
	}
	var amount *string
	if b.Amount != "" {
		amount = &b.Amount
	}
	return Balance{Account: b.Account, Through: b.Through, Amount: amount, IncompleteAmountCount: b.IncompleteAmountCount}, nil
}
func registerBalance(api huma.API, s *ledger.Service) {
	huma.Register(api, huma.Operation{OperationID: "get-balance", Method: "GET", Path: "/api/balance", Summary: "Get cumulative ledger balance", Errors: []int{404, 422, 503}}, func(ctx context.Context, in *BalanceInput) (*balanceOutput, error) {
		b, err := GetBalance(ctx, s, *in)
		if err != nil {
			return nil, apiError(err)
		}
		return &balanceOutput{Body: b}, nil
	})
}
