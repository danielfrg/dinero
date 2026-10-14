package mcp

import (
	"context"
	"errors"

	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type noInput struct{}

type searchInput struct {
	Account     string `json:"account,omitempty"`
	Category    string `json:"category,omitempty"`
	Subcategory string `json:"subcategory,omitempty"`
	Description string `json:"description,omitempty"`
	After       string `json:"after,omitempty"`
	Before      string `json:"before,omitempty"`
	Year        *int   `json:"year,omitempty"`
	Month       *int   `json:"month,omitempty"`
	Sort        string `json:"sort,omitempty"`
	Desc        bool   `json:"desc,omitempty"`
	Limit       *int   `json:"limit,omitempty"`
	Offset      *int   `json:"offset,omitempty"`
}

type balanceInput struct {
	Account string `json:"account"`
	Through string `json:"through"`
}

type summaryInput struct {
	After       string `json:"after"`
	Before      string `json:"before"`
	GroupBy     string `json:"group_by,omitempty"`
	Account     string `json:"account,omitempty"`
	Category    string `json:"category,omitempty"`
	Subcategory string `json:"subcategory,omitempty"`
	Description string `json:"description,omitempty"`
}

func readTool(name, description string) *sdk.Tool {
	falseValue := false
	return &sdk.Tool{Name: name, Description: description, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &falseValue, OpenWorldHint: &falseValue}}
}

func toolError(err error) error {
	if err == nil {
		return nil
	}
	var invalid *ledger.ErrInvalidInput
	if errors.As(err, &invalid) {
		return errors.New("Invalid query input")
	}
	var unknown *ledger.ErrUnknownAccount
	if errors.As(err, &unknown) {
		return errors.New("Unknown account")
	}
	return errors.New("Data source unavailable")
}

func registerTools(s *sdk.Server, service *ledger.Service) {
	sdk.AddTool(s, readTool("list_accounts", "List ledger accounts"), func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, httpapi.Accounts, error) {
		out, err := httpapi.ListAccounts(ctx, service)
		return nil, out, toolError(err)
	})
	sdk.AddTool(s, readTool("list_categories", "List ledger categories"), func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, httpapi.Categories, error) {
		out, err := httpapi.ListCategories(ctx, service)
		return nil, out, toolError(err)
	})
	sdk.AddTool(s, readTool("search_transactions", "Search ledger transactions with stable pagination"), func(ctx context.Context, _ *sdk.CallToolRequest, in searchInput) (*sdk.CallToolResult, httpapi.TransactionPage, error) {
		if in.Year != nil && (*in.Year < 1 || *in.Year > 9999) || in.Month != nil && (*in.Month < 1 || *in.Month > 12) || in.Limit != nil && (*in.Limit < 1 || *in.Limit > 500) || in.Offset != nil && *in.Offset < 0 {
			return nil, httpapi.TransactionPage{}, errors.New("Invalid query input")
		}
		input := httpapi.SearchInput{Account: in.Account, Category: in.Category, Subcategory: in.Subcategory, Description: in.Description, After: in.After, Before: in.Before, Sort: in.Sort, Desc: in.Desc}
		if in.Year != nil {
			input.Year = *in.Year
		}
		if in.Month != nil {
			input.Month = *in.Month
		}
		if in.Limit != nil {
			input.Limit = *in.Limit
		}
		if in.Offset != nil {
			input.Offset = *in.Offset
		}
		out, err := httpapi.Search(ctx, service, input)
		return nil, out, toolError(err)
	})
	sdk.AddTool(s, readTool("get_balance", "Get cumulative ledger balance through a date"), func(ctx context.Context, _ *sdk.CallToolRequest, in balanceInput) (*sdk.CallToolResult, httpapi.Balance, error) {
		out, err := httpapi.GetBalance(ctx, service, httpapi.BalanceInput{Account: in.Account, Through: in.Through})
		return nil, out, toolError(err)
	})
	sdk.AddTool(s, readTool("summarize_transactions", "Summarize all matching transactions in a date range"), func(ctx context.Context, _ *sdk.CallToolRequest, in summaryInput) (*sdk.CallToolResult, httpapi.Summary, error) {
		out, err := httpapi.Summarize(ctx, service, httpapi.SummaryInput{After: in.After, Before: in.Before, GroupBy: in.GroupBy, Account: in.Account, Category: in.Category, Subcategory: in.Subcategory, Description: in.Description})
		return nil, out, toolError(err)
	})
	sdk.AddTool(s, readTool("get_status", "Get read-only ledger status"), func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, httpapi.Status, error) {
		out, err := httpapi.GetStatus(ctx, service)
		return nil, out, toolError(err)
	})
}
