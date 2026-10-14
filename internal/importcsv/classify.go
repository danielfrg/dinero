package importcsv

import (
	"strconv"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/shopspring/decimal"
)

type transactionKey struct{ date, account, description, amount string }

func key(tr ledger.Transaction, loc *time.Location) (transactionKey, bool) {
	if tr.Date == nil || tr.Amount == nil || tr.Account == "" || tr.Description == "" {
		return transactionKey{}, false
	}
	d, e := decimal.NewFromString(strconv.FormatFloat(*tr.Amount, 'f', -1, 64))
	if e != nil {
		return transactionKey{}, false
	}
	return transactionKey{tr.Date.In(loc).Format("2006-01-02"), tr.Account, tr.Description, d.Round(2).StringFixed(2)}, true
}
func duplicateRows(records []record, loc *time.Location) [][]int {
	byKey := map[transactionKey][]int{}
	order := []transactionKey{}
	for _, r := range records {
		k, _ := key(r.transaction, loc)
		if len(byKey[k]) == 0 {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], r.row)
	}
	out := [][]int{}
	for _, k := range order {
		if len(byKey[k]) > 1 {
			out = append(out, byKey[k])
		}
	}
	return out
}
func classify(records []record, existing []ledger.Transaction, loc *time.Location) ([]record, []record, []Ambiguous) {
	byKey := map[transactionKey][]int64{}
	for _, tr := range existing {
		if k, ok := key(tr, loc); ok {
			byKey[k] = append(byKey[k], tr.ID)
		}
	}
	newRows, oldRows, ambiguous := []record{}, []record{}, []Ambiguous{}
	for _, r := range records {
		k, _ := key(r.transaction, loc)
		ids := byKey[k]
		switch len(ids) {
		case 0:
			newRows = append(newRows, r)
		case 1:
			oldRows = append(oldRows, r)
		default:
			ambiguous = append(ambiguous, Ambiguous{Transaction: serialize(r), ExistingIDs: ids})
		}
	}
	return newRows, oldRows, ambiguous
}
