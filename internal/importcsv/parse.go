package importcsv

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/shopspring/decimal"
)

var moneyPattern = regexp.MustCompile(`^[+-]?(?:\d+|\d{1,3}(?:,\d{3})+)(?:\.\d+)?$`)

type record struct {
	transaction ledger.Transaction
	row         int
	amount      decimal.Decimal
}

func parse(input io.Reader, basename, account string, loc *time.Location) ([]record, error) {
	reader := csv.NewReader(input)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("Missing required columns: amount, date, description. Required columns: date, description, amount")
	}
	if err != nil {
		return nil, fmt.Errorf("invalid CSV header")
	}
	indexes := map[string]int{}
	for i, col := range header {
		indexes[col] = i
	}
	missing := []string{}
	for _, name := range []string{"amount", "date", "description"} {
		if _, ok := indexes[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Missing required columns: %s. Required columns: date, description, amount", strings.Join(missing, ", "))
	}
	rules, err := loadRules()
	if err != nil {
		return nil, err
	}
	cell := func(row []string, key string) string {
		i, ok := indexes[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	out := []record{}
	line := 2
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("Row %d: invalid CSV row", line)
		}
		dateStr, desc, amountStr := cell(row, "date"), cell(row, "description"), cell(row, "amount")
		if dateStr == "" && desc == "" && amountStr == "" {
			line++
			continue
		}
		if dateStr == "" {
			return nil, fmt.Errorf("Row %d: Missing date", line)
		}
		if desc == "" {
			return nil, fmt.Errorf("Row %d: Missing description", line)
		}
		if amountStr == "" {
			return nil, fmt.Errorf("Row %d: Missing amount", line)
		}
		date, e := ledger.ParseDay(dateStr, loc)
		if e != nil {
			return nil, fmt.Errorf("Row %d: Invalid date format: '%s'. Expected YYYY-MM-DD", line, dateStr)
		}
		amount, stored, e := parseAmount(amountStr)
		if e != nil {
			return nil, fmt.Errorf("Row %d: %w", line, e)
		}
		cat, sub := cell(row, "category"), cell(row, "subcategory")
		if cat == "" {
			if rule, ok := rules[desc]; ok && len(rule) >= 2 {
				cat, sub = rule[0], rule[1]
			} else {
				cat, sub = "", ""
			}
		}
		tr := ledger.Transaction{Date: &date, Description: desc, Amount: &stored, Category: cat, Subcategory: sub, Account: account, Notes: "csv-import:" + filepath.Base(basename)}
		out = append(out, record{transaction: tr, row: line, amount: amount})
		line++
	}
	return out, nil
}

func parseAmount(value string) (decimal.Decimal, float64, error) {
	invalid := func(reason string) (decimal.Decimal, float64, error) {
		return decimal.Decimal{}, 0, fmt.Errorf("Invalid amount: '%s'%s", value, reason)
	}
	if !moneyPattern.MatchString(value) {
		return invalid("")
	}
	d, err := decimal.NewFromString(strings.ReplaceAll(value, ",", ""))
	if err != nil {
		return invalid("")
	}
	if !d.Equal(d.Round(2)) {
		return invalid(". Expected at most two decimals")
	}
	stored, err := strconv.ParseFloat(d.String(), 64)
	if err != nil || math.IsInf(stored, 0) || math.IsNaN(stored) {
		return invalid(". Cannot preserve cents")
	}
	back, err := decimal.NewFromString(strconv.FormatFloat(stored, 'f', -1, 64))
	if err != nil || !back.Round(2).Equal(d) {
		return invalid(". Cannot preserve cents")
	}
	return d, stored, nil
}

func loadRules() (map[string][]string, error) {
	path := filepath.Join(config.StateDir(), "category_rules.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read category rules")
	}
	var rules map[string][]string
	if err = json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("invalid category rules")
	}
	return rules, nil
}
