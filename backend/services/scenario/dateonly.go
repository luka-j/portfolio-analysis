package scenario

import (
	"fmt"
	"strings"
	"time"
)

// DateOnly is a time.Time that JSON-marshals as "YYYY-MM-DD" so the frontend
// can send plain date strings without a time component.
type DateOnly struct {
	t time.Time
}

// NewDateOnly wraps t, normalising to UTC noon so comparisons are stable.
func NewDateOnly(t time.Time) DateOnly {
	return DateOnly{t: time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)}
}

// Time returns the underlying time.Time (UTC noon on the given date).
func (d DateOnly) Time() time.Time { return d.t }

func (d DateOnly) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.t.Format("2006-01-02") + `"`), nil
}

func (d *DateOnly) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fmt.Errorf("date field: cannot parse %q as YYYY-MM-DD: %w", s, err)
	}
	d.t = time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
	return nil
}
