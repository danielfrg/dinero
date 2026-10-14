package httpapi

import (
	"context"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
)

type SummaryInput struct {
	After       string `query:"after"`
	Before      string `query:"before"`
	GroupBy     string `query:"group_by" default:"category" enum:"account,category,subcategory,month"`
	Account     string `query:"account"`
	Category    string `query:"category"`
	Subcategory string `query:"subcategory"`
	Description string `query:"description"`
}
type SummaryGroup struct {
	Key                   string  `json:"key"`
	SignedTotal           string  `json:"signed_total"`
	Outflows              string  `json:"outflows"`
	Inflows               string  `json:"inflows"`
	Currency              *string `json:"currency"`
	Count                 int64   `json:"count"`
	IncompleteAmountCount int64   `json:"incomplete_amount_count"`
}
type Summary struct {
	Groups                []SummaryGroup `json:"groups"`
	IncompleteAmountCount int64          `json:"incomplete_amount_count"`
}
type summaryOutput struct{ Body Summary }

func NormalizeSummary(in SummaryInput) (ledger.SummaryQuery, error) {
	if in.After == "" || in.Before == "" {
		return ledger.SummaryQuery{}, &ledger.ErrInvalidInput{Field: "after/before", Reason: "required"}
	}
	group := in.GroupBy
	if group == "" {
		group = "category"
	}
	switch group {
	case "account", "category", "subcategory", "month":
	default:
		return ledger.SummaryQuery{}, &ledger.ErrInvalidInput{Field: "group_by", Reason: "unsupported group"}
	}
	return ledger.SummaryQuery{Query: ledger.Query{After: &in.After, Before: &in.Before, Account: optional(in.Account), Category: optional(in.Category), Subcategory: optional(in.Subcategory), Description: optional(in.Description)}, GroupBy: group}, nil
}
func Summarize(ctx context.Context, s *ledger.Service, in SummaryInput) (Summary, error) {
	q, err := NormalizeSummary(in)
	if err != nil {
		return Summary{}, err
	}
	sum, err := s.Summary(ctx, q)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Groups: make([]SummaryGroup, 0, len(sum.Groups)), IncompleteAmountCount: sum.IncompleteAmountCount}
	for _, g := range sum.Groups {
		out.Groups = append(out.Groups, SummaryGroup{Key: g.Key, SignedTotal: g.SignedTotal, Outflows: g.Outflows, Inflows: g.Inflows, Count: g.Count, IncompleteAmountCount: g.IncompleteAmountCount})
	}
	return out, nil
}
func registerSummary(api huma.API, s *ledger.Service) {
	huma.Register(api, huma.Operation{OperationID: "summarize-transactions", Method: "GET", Path: "/api/summary", Summary: "Summarize ledger transactions", Errors: []int{422, 503}}, func(ctx context.Context, in *SummaryInput) (*summaryOutput, error) {
		sum, err := Summarize(ctx, s, *in)
		if err != nil {
			return nil, apiError(err)
		}
		return &summaryOutput{Body: sum}, nil
	})
}
