package booking

import (
	"fmt"
	"time"
)

// dateLayout is the canonical wire format for a Date: ISO-8601 calendar day.
const dateLayout = "2006-01-02"

// minYear and maxYear bound a valid Date to a four-digit year, matching the
// canonical wire format ("YYYY-MM-DD").
const (
	minYear = 1
	maxYear = 9999
)

// Date is a timezone-independent calendar day. The zero value is not valid;
// construct a Date through NewDate, DateFromTime, or ParseDate. NewDate and
// ParseDate reject anything that is not a real day in the year range
// [minYear, maxYear], so a constructed Date is always canonical. Because Go
// permits a zero value of any struct, callers that receive a Date from an
// untrusted boundary should still guard with Valid. The unexported fields keep
// every Date canonical and prevent the instant-oriented operations of time.Time
// that are meaningless for a calendar day.
type Date struct {
	year  int
	month time.Month
	day   int
}

// Valid reports whether d is a real calendar day within [minYear, maxYear].
// The zero Date is not valid.
func (d Date) Valid() bool {
	if d.year < minYear || d.year > maxYear {
		return false
	}
	if d.month < time.January || d.month > time.December {
		return false
	}
	// time.Date normalises out-of-range components; if reconstruction changed
	// the value, it was not a real day (e.g. 2075-02-30).
	t := time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
	return t.Year() == d.year && t.Month() == d.month && t.Day() == d.day
}

// NewDate returns the Date for the given year, month, and day. It returns
// ErrInvalidInput if the components do not form a real calendar day within the
// four-digit year range (e.g. year 0 or 2075-02-30).
func NewDate(year int, month time.Month, day int) (Date, error) {
	d := Date{year: year, month: month, day: day}
	if !d.Valid() {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d is not a valid date", ErrInvalidInput, year, month, day)
	}
	return d, nil
}

// DateFromTime returns the calendar day of t in t's own location. Callers that
// need a specific timezone must convert t (for example t.In(loc)) beforehand.
func DateFromTime(t time.Time) Date {
	return Date{year: t.Year(), month: t.Month(), day: t.Day()}
}

// ParseDate parses a string in the canonical "2006-01-02" layout. Any other
// layout, or a syntactically valid but out-of-range/non-existent day, yields
// ErrInvalidInput.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("%w: %q is not a valid date (want YYYY-MM-DD)", ErrInvalidInput, s)
	}
	// Route through NewDate so the year-range invariant is enforced uniformly
	// (time.Parse alone accepts years like 0000).
	return NewDate(t.Year(), t.Month(), t.Day())
}

// Before reports whether d falls on an earlier calendar day than o.
func (d Date) Before(o Date) bool {
	if d.year != o.year {
		return d.year < o.year
	}
	if d.month != o.month {
		return d.month < o.month
	}
	return d.day < o.day
}

// Weekday returns the day of the week for d.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC).Weekday()
}

// Year returns the year component of d.
func (d Date) Year() int { return d.year }

// Month returns the month component of d.
func (d Date) Month() time.Month { return d.month }

// Day returns the day-of-month component of d.
func (d Date) Day() int { return d.day }

// String returns d in the canonical "2006-01-02" layout.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// MarshalJSON encodes d as a JSON string in the canonical layout.
func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON decodes a JSON string in the canonical layout into d.
func (d *Date) UnmarshalJSON(data []byte) error {
	var s string
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("%w: visit_date must be a JSON string", ErrInvalidInput)
	}
	s = string(data[1 : len(data)-1])
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
