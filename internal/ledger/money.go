package ledger

import (
	"errors"
	"github.com/shopspring/decimal"
	"math"
	"strconv"
	"strings"
)

func FormatMoney(value string) (string, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("invalid amount")
	}
	return d.RoundBank(2).StringFixed(2), nil
}
func ValidAmount(value *float64) bool {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return false
	}
	d, err := decimal.NewFromString(strconv.FormatFloat(*value, 'f', -1, 64))
	return err == nil && d.Equal(d.RoundBank(2))
}
