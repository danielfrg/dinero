package plaid

import (
	"context"
	"errors"
	"strings"
	"time"

	sdk "github.com/plaid/plaid-go/v48/plaid"
)

type Transaction struct {
	AccountID, Date, Name string
	Amount                float64
	Pending               bool
}
type TransactionsPage struct {
	Total        int
	Transactions []Transaction
}
type Client interface {
	GetTransactions(context.Context, string, time.Time, time.Time, int) (TransactionsPage, error)
}

type ErrLoginRequired struct{}

func (*ErrLoginRequired) Error() string { return "Plaid login requires an update" }

type ErrProvider struct{ operation string }

func (e *ErrProvider) Error() string {
	if e.operation != "" {
		return "Plaid " + e.operation + " failed"
	}
	return "Plaid transactions/get failed"
}

type SDKClient struct{ api *sdk.APIClient }

func New(clientID, secret, environment string) (*SDKClient, error) {
	if clientID == "" || secret == "" {
		return nil, errors.New("Plaid client_id and secret are required")
	}
	cfg := sdk.NewConfiguration()
	switch strings.ToLower(environment) {
	case "", "production":
		cfg.UseEnvironment(sdk.Production)
	case "sandbox":
		cfg.UseEnvironment(sdk.Sandbox)
	case "development":
		return nil, errors.New("Plaid development environment is obsolete; use sandbox or production")
	default:
		return nil, errors.New("unsupported Plaid environment; use sandbox or production")
	}
	cfg.AddDefaultHeader("PLAID-CLIENT-ID", clientID)
	cfg.AddDefaultHeader("PLAID-SECRET", secret)
	return &SDKClient{api: sdk.NewAPIClient(cfg)}, nil
}

func (c *SDKClient) GetTransactions(ctx context.Context, token string, start, end time.Time, offset int) (TransactionsPage, error) {
	if offset < 0 {
		return TransactionsPage{}, errors.New("negative Plaid offset")
	}
	req := sdk.NewTransactionsGetRequest(token, start.Format("2006-01-02"), end.Format("2006-01-02"))
	opts := sdk.NewTransactionsGetRequestOptions()
	opts.SetCount(500)
	opts.SetOffset(int32(offset))
	req.SetOptions(*opts)
	resp, _, err := c.api.PlaidApi.TransactionsGet(ctx).TransactionsGetRequest(*req).Execute()
	if err != nil {
		if apiError, decodeErr := sdk.ToPlaidError(err); decodeErr == nil && apiError.GetErrorCode() == "ITEM_LOGIN_REQUIRED" {
			return TransactionsPage{}, &ErrLoginRequired{}
		}
		return TransactionsPage{}, &ErrProvider{}
	}
	result := TransactionsPage{Total: int(resp.GetTotalTransactions()), Transactions: make([]Transaction, 0, len(resp.GetTransactions()))}
	for _, item := range resp.GetTransactions() {
		result.Transactions = append(result.Transactions, Transaction{AccountID: item.GetAccountId(), Date: item.GetDate(), Name: item.GetName(), Amount: item.GetAmount(), Pending: item.GetPending()})
	}
	return result, nil
}
