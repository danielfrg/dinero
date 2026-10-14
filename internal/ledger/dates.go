package ledger

import (
	"fmt"
	"regexp"
	"time"
)

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func ParseDay(value string, loc *time.Location) (time.Time, error) {
	if !isoDate.MatchString(value) {
		return time.Time{}, &ErrInvalidInput{Field: "date", Reason: "expected YYYY-MM-DD"}
	}
	day, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil || day.Format("2006-01-02") != value {
		return time.Time{}, &ErrInvalidInput{Field: "date", Reason: fmt.Sprintf("invalid date %q", value)}
	}
	return day, nil
}
func NextDay(value string, loc *time.Location) (time.Time, error) {
	day, err := ParseDay(value, loc)
	if err != nil {
		return time.Time{}, err
	}
	return day.AddDate(0, 0, 1), nil
}
