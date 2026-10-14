package httpapi

import (
	"context"
	"strconv"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
)

type Transaction struct {
	ID            int64   `json:"id"`
	Date          *string `json:"date"`
	Description   string  `json:"description"`
	Category      string  `json:"category"`
	Subcategory   string  `json:"subcategory"`
	Amount        *string `json:"amount"`
	Currency      *string `json:"currency"`
	InvalidAmount bool    `json:"invalid_amount"`
	Account       string  `json:"account"`
	Notes         string  `json:"notes"`
}
type TransactionPage struct {
	Transactions []Transaction `json:"transactions"`
	Limit        int           `json:"limit"`
	Offset       int           `json:"offset"`
	HasMore      bool          `json:"has_more"`
}
type SearchInput struct {
	Account     string `query:"account"`
	Category    string `query:"category"`
	Subcategory string `query:"subcategory"`
	Description string `query:"description"`
	After       string `query:"after"`
	Before      string `query:"before"`
	Year        int    `query:"year" minimum:"1" maximum:"9999"`
	Month       int    `query:"month" minimum:"1" maximum:"12"`
	Sort        string `query:"sort"`
	Desc        bool   `query:"desc"`
	Limit       int    `query:"limit" default:"50" minimum:"1" maximum:"500"`
	Offset      int    `query:"offset" minimum:"0"`
}
type transactionOutput struct{ Body TransactionPage }

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// NormalizeSearch is shared by REST and MCP and enforces the transport page contract.
func NormalizeSearch(in SearchInput) (ledger.Query, error) {
	q := ledger.Query{Account: optional(in.Account), Category: optional(in.Category), Subcategory: optional(in.Subcategory), Description: optional(in.Description), After: optional(in.After), Before: optional(in.Before), Sort: in.Sort, Desc: in.Desc, Limit: in.Limit, Offset: in.Offset}
	if in.Year != 0 {
		q.Year = &in.Year
	}
	if in.Month != 0 {
		q.Month = &in.Month
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 500 {
		return q, &ledger.ErrInvalidInput{Field: "limit", Reason: "must be 1 through 500"}
	}
	if q.Offset < 0 {
		return q, &ledger.ErrInvalidInput{Field: "offset", Reason: "must be nonnegative"}
	}
	return q, nil
}
func ConvertTransaction(t ledger.Transaction) Transaction {
	out := Transaction{ID: t.ID, Description: t.Description, Category: t.Category, Subcategory: t.Subcategory, Account: t.Account, Notes: t.Notes}
	if t.Date != nil {
		d := t.Date.Format("2006-01-02")
		out.Date = &d
	}
	if t.Amount != nil {
		if ledger.ValidAmount(t.Amount) {
			v, err := ledger.FormatMoney(strconv.FormatFloat(*t.Amount, 'f', -1, 64))
			if err == nil {
				out.Amount = &v
			} else {
				out.InvalidAmount = true
			}
		} else {
			out.InvalidAmount = true
		}
	} else {
		out.InvalidAmount = true
	}
	return out
}
func Search(ctx context.Context, service *ledger.Service, in SearchInput) (TransactionPage, error) {
	q, err := NormalizeSearch(in)
	if err != nil {
		return TransactionPage{}, err
	}
	p, err := service.Search(ctx, q)
	if err != nil {
		return TransactionPage{}, err
	}
	out := TransactionPage{Transactions: make([]Transaction, 0, len(p.Transactions)), Limit: p.Limit, Offset: p.Offset, HasMore: p.HasMore}
	for _, t := range p.Transactions {
		out.Transactions = append(out.Transactions, ConvertTransaction(t))
	}
	return out, nil
}
func registerTransactions(api huma.API, service *ledger.Service) {
	huma.Register(api, huma.Operation{OperationID: "search-transactions", Method: "GET", Path: "/api/transactions", Summary: "Search ledger transactions", Errors: []int{422, 503}}, func(ctx context.Context, in *SearchInput) (*transactionOutput, error) {
		p, err := Search(ctx, service, *in)
		if err != nil {
			return nil, apiError(err)
		}
		return &transactionOutput{Body: p}, nil
	})
}
