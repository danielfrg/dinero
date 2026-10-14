package maintenance

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/store"
	_ "modernc.org/sqlite"
)

type Service struct {
	store *store.Store
	cfg   config.Config
}

func New(s *store.Store, cfg config.Config) *Service { return &Service{store: s, cfg: cfg} }
func (s *Service) InitDB(ctx context.Context) error  { return s.store.InitSchema(ctx) }

type RuleStat struct {
	Description string `json:"description"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	Amount      int    `json:"amount"`
}

// PreviewRules counts non-null amount values, matching pandas count().
// A description with several qualifying groups receives the most frequent.
func (s *Service) PreviewRules(ctx context.Context) ([]RuleStat, error) {
	rows, err := s.store.RawTransactions(ctx)
	if err != nil {
		return nil, err
	}
	type group struct{ desc, cat, sub string }
	counts := map[group]int{}
	for _, r := range rows {
		if r.Description.Valid && r.Category.Valid && r.Subcategory.Valid && r.Amount.Valid {
			counts[group{r.Description.String, r.Category.String, r.Subcategory.String}]++
		}
	}
	rules := map[string]RuleStat{}
	best := map[string]int{}
	keys := make([]group, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].desc != keys[j].desc {
			return keys[i].desc < keys[j].desc
		}
		if keys[i].cat != keys[j].cat {
			return keys[i].cat < keys[j].cat
		}
		return keys[i].sub < keys[j].sub
	})
	for _, k := range keys {
		n := counts[k]
		if n >= 3 && n > best[k.desc] {
			best[k.desc] = n
			rules[k.desc] = RuleStat{Description: k.desc, Category: k.cat, Subcategory: k.sub, Amount: n}
		}
	}
	out := make([]RuleStat, 0, len(rules))
	for _, r := range rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Description < out[j].Description })
	return out, nil
}
func (s *Service) GenerateRules(ctx context.Context) (map[string][2]string, error) {
	stats, err := s.PreviewRules(ctx)
	if err != nil {
		return nil, err
	}
	rules := map[string][2]string{}
	for _, r := range stats {
		rules[r.Description] = [2]string{r.Category, r.Subcategory}
	}
	return rules, nil
}
func (s *Service) SaveRules(ctx context.Context, rules map[string][2]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(config.StateDir(), "category_rules.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
func (s *Service) BuildCache(ctx context.Context) error {
	rows, err := s.store.RawTransactions(ctx)
	if err != nil {
		return err
	}
	accounts, categories, subcategories := map[string]bool{}, map[string]bool{}, map[string]bool{}
	type pair struct {
		Category    string `json:"category"`
		Subcategory string `json:"subcategory"`
	}
	pairs := map[pair]bool{}
	for _, r := range rows {
		if r.Account.Valid && r.Account.String != "" {
			accounts[r.Account.String] = true
		}
		if r.Category.Valid && r.Category.String != "" {
			categories[r.Category.String] = true
		}
		if r.Subcategory.Valid && r.Subcategory.String != "" {
			subcategories[r.Subcategory.String] = true
		}
		if r.Category.Valid && r.Category.String != "" && r.Subcategory.Valid && r.Subcategory.String != "" {
			pairs[pair{r.Category.String, r.Subcategory.String}] = true
		}
	}
	sorted := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for v := range m {
			out = append(out, v)
		}
		sort.Strings(out)
		return out
	}
	pairList := make([]pair, 0, len(pairs))
	for p := range pairs {
		pairList = append(pairList, p)
	}
	sort.Slice(pairList, func(i, j int) bool {
		if pairList[i].Category != pairList[j].Category {
			return pairList[i].Category < pairList[j].Category
		}
		return pairList[i].Subcategory < pairList[j].Subcategory
	})
	dir := filepath.Join(config.StateDir(), "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "accounts.json"), sorted(accounts)); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "categories.json"), struct {
		Categories    []string `json:"categories"`
		Subcategories []string `json:"subcategories"`
		Pairs         []pair   `json:"category_subcategory_pairs"`
	}{sorted(categories), sorted(subcategories), pairList})
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func (s *Service) Export(ctx context.Context, directory string) error {
	rows, err := s.store.RawTransactions(ctx)
	if err != nil {
		return err
	}
	if directory == "" {
		return errors.New("export directory is required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".all.csv-*")
	if err != nil {
		return err
	}
	csvTempPath := file.Name()
	defer os.Remove(csvTempPath)
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"id", "date", "description", "category", "subcategory", "amount", "notes", "account"}); err != nil {
		return err
	}
	dbTempFile, err := os.CreateTemp(directory, ".transactions.db-*")
	if err != nil {
		return err
	}
	dbTempPath := dbTempFile.Name()
	if err := dbTempFile.Close(); err != nil {
		return err
	}
	defer func() {
		os.Remove(dbTempPath)
		os.Remove(dbTempPath + "-journal")
		os.Remove(dbTempPath + "-wal")
		os.Remove(dbTempPath + "-shm")
	}()
	db, err := sql.Open("sqlite", dbTempPath)
	if err != nil {
		return err
	}
	defer db.Close()
	dateType := "DATETIME"
	if s.store.Dialect() == "postgres" {
		dateType = "TIMESTAMP"
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE transactions (id BIGINT, date "+dateType+", description TEXT, category TEXT, subcategory TEXT, amount FLOAT, notes TEXT, account TEXT)"); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		date := r.Date.String
		if r.Date.Valid {
			date = strings.Replace(date, "T", " ", 1)
			date = strings.TrimSuffix(date, "Z")
			date = strings.TrimSuffix(date, "+00:00")
			if !strings.Contains(date, ".") {
				date += ".000000"
			} else {
				parts := strings.SplitN(date, ".", 2)
				if len(parts[1]) < 6 {
					date = parts[0] + "." + parts[1] + strings.Repeat("0", 6-len(parts[1]))
				}
			}
		}
		csvDate := strings.TrimSuffix(date, ".000000")
		if s.store.Dialect() == "postgres" && r.Date.Valid {
			csvDate += "+00:00"
		}
		csvRow := []string{strconv.FormatInt(r.ID, 10), nullText(r.Date, csvDate), nullText(r.Description, r.Description.String), nullText(r.Category, r.Category.String), nullText(r.Subcategory, r.Subcategory.String), nullAmount(r.Amount), nullText(r.Notes, r.Notes.String), nullText(r.Account, r.Account.String)}
		if err := writer.Write(csvRow); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO transactions VALUES (?,?,?,?,?,?,?,?)", r.ID, nullValue(r.Date, date), nullValue(r.Description, r.Description.String), nullValue(r.Category, r.Category.String), nullValue(r.Subcategory, r.Subcategory.String), nullFloat(r.Amount), nullValue(r.Notes, r.Notes.String), nullValue(r.Account, r.Account.String))
		if err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	csvPath := filepath.Join(directory, "all.csv")
	dbPath := filepath.Join(directory, "transactions.db")
	if err := replaceableExportTarget(csvPath); err != nil {
		return err
	}
	if err := replaceableExportTarget(dbPath); err != nil {
		return err
	}
	if err := os.Rename(dbTempPath, dbPath); err != nil {
		return err
	}
	return os.Rename(csvTempPath, csvPath)
}

func replaceableExportTarget(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("export target is not a regular file: %s", filepath.Base(path))
	}
	return nil
}
func nullText(v sql.NullString, text string) string {
	if v.Valid {
		return text
	}
	return ""
}
func nullValue(v sql.NullString, text string) any {
	if v.Valid {
		return text
	}
	return nil
}
func nullFloat(v sql.NullFloat64) any {
	if v.Valid {
		return v.Float64
	}
	return nil
}
func nullAmount(v sql.NullFloat64) string {
	if v.Valid {
		text := strconv.FormatFloat(v.Float64, 'f', -1, 64)
		if !strings.Contains(text, ".") {
			text += ".0"
		}
		return text
	}
	return ""
}
