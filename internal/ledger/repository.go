package ledger

import "context"

type Repository interface {
	Search(context.Context, Query) (Page, error)
	Accounts(context.Context) ([]Account, error)
	Categories(context.Context) (Categories, error)
	Balance(context.Context, string, string) (Balance, error)
	Summary(context.Context, SummaryQuery) (Summary, error)
}
