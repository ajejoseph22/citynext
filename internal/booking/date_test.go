package booking

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", "2075-08-14", false},
		{"leap day valid", "2076-02-29", false},
		{"non-leap 29 Feb", "2075-02-29", true},
		{"day 31 in short month", "2075-04-31", true},
		{"wrong layout slashes", "2075/08/14", true},
		{"with time component", "2075-08-14T00:00:00Z", true},
		{"empty", "", true},
		{"garbage", "not-a-date", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDate(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("ParseDate(%q) error = %v, want ErrInvalidInput", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) unexpected error: %v", tt.in, err)
			}
		})
	}
}

func TestNewDateRejectsInvalid(t *testing.T) {
	bad := []struct {
		name      string
		year, day int
		month     time.Month
	}{
		{"impossible day", 2075, 30, 2},
		{"year zero", 0, 1, 1},
		{"negative year", -1, 1, 1},
		{"five-digit year", 12345, 1, 1},
		{"month zero", 2075, 1, 0},
		{"month thirteen", 2075, 1, 13},
	}
	for _, tt := range bad {
		if _, err := NewDate(tt.year, tt.month, tt.day); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("NewDate(%s) error = %v, want ErrInvalidInput", tt.name, err)
		}
	}
	if _, err := NewDate(2075, 8, 14); err != nil {
		t.Fatalf("NewDate(2075-08-14) unexpected error: %v", err)
	}
}

func TestValid(t *testing.T) {
	var zero Date
	if zero.Valid() {
		t.Fatal("zero Date must not be Valid")
	}
	good, err := NewDate(2075, 8, 14)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	if !good.Valid() {
		t.Fatal("constructed Date must be Valid")
	}
}

func TestParseDateRejectsOutOfRangeYear(t *testing.T) {
	if _, err := ParseDate("0000-01-01"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ParseDate(0000-01-01) error = %v, want ErrInvalidInput", err)
	}
}

func TestDateBefore(t *testing.T) {
	mustDate := func(y int, m time.Month, d int) Date {
		got, err := NewDate(y, m, d)
		if err != nil {
			t.Fatalf("NewDate: %v", err)
		}
		return got
	}
	tests := []struct {
		name string
		a, b Date
		want bool
	}{
		{"earlier year", mustDate(2074, 12, 31), mustDate(2075, 1, 1), true},
		{"earlier month", mustDate(2075, 7, 31), mustDate(2075, 8, 1), true},
		{"earlier day", mustDate(2075, 8, 13), mustDate(2075, 8, 14), true},
		{"equal", mustDate(2075, 8, 14), mustDate(2075, 8, 14), false},
		{"later day", mustDate(2075, 8, 15), mustDate(2075, 8, 14), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Before(tt.b); got != tt.want {
				t.Fatalf("%s.Before(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	d, err := NewDate(2075, 8, 14)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(b), `"2075-08-14"`; got != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
	var back Date
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back != d {
		t.Fatalf("round trip = %v, want %v", back, d)
	}
}

func TestDateUnmarshalRejectsBad(t *testing.T) {
	for _, in := range []string{`"2075-02-30"`, `"2075/08/14"`, `12345`, `"not-a-date"`} {
		var d Date
		if err := json.Unmarshal([]byte(in), &d); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Unmarshal(%s) error = %v, want ErrInvalidInput", in, err)
		}
	}
}

func TestDateFromTimeUsesTimeLocation(t *testing.T) {
	// 2075-08-14 23:30 in London is still the 14th; the same instant in UTC
	// (22:30) is also the 14th. Convert first, then take the day.
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	instant := time.Date(2075, 8, 14, 23, 30, 0, 0, london)
	if got := DateFromTime(instant); got.String() != "2075-08-14" {
		t.Fatalf("DateFromTime = %s, want 2075-08-14", got)
	}
}
