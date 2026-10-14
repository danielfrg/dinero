package ledger

import "time"

type Transaction struct {
	ID                                                 int64
	Date                                               *time.Time
	Description, Category, Subcategory, Account, Notes string
	Amount                                             *float64
}
type Query struct {
	Account, Category, Subcategory, Description *string
	After, Before                               *string
	Year, Month                                 *int
	Sort                                        string
	Desc                                        bool
	Limit, Offset                               int
}
type Page struct {
	Transactions  []Transaction
	Limit, Offset int
	HasMore       bool
}
type Account struct {
	Name       string
	LatestDate *string
}
type Categories struct {
	Categories, Subcategories []string
	Pairs                     []CategoryPair
}
type CategoryPair struct{ Category, Subcategory string }
type Balance struct {
	Account, Through, Amount string
	IncompleteAmountCount    int64
}
type SummaryQuery struct {
	Query   Query
	GroupBy string
}
type Summary struct {
	Groups                []SummaryGroup
	IncompleteAmountCount int64
}
type SummaryGroup struct {
	Key, SignedTotal, Outflows, Inflows string
	Count, IncompleteAmountCount        int64
}
type Status struct {
	QueriedAt   time.Time
	Timezone    string
	Accounts    []Account
	ItemAliases []string
}
