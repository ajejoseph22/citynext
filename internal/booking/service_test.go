package booking

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRepo records the last saved appointment and returns a configured error.
type fakeRepo struct {
	saved *Appointment
	err   error
	calls atomic.Int32
}

func (f *fakeRepo) Save(_ context.Context, appt Appointment) error {
	f.calls.Add(1)
	if f.err != nil {
		return f.err
	}
	saved := appt
	f.saved = &saved
	return nil
}

// fakeHolidays reports a fixed answer and counts calls.
type fakeHolidays struct {
	isHoliday bool
	err       error
	calls     atomic.Int32
}

func (f *fakeHolidays) IsPublicHoliday(_ context.Context, _ Date) (bool, error) {
	f.calls.Add(1)
	return f.isHoliday, f.err
}

func mustDate(t *testing.T, y int, m time.Month, d int) Date {
	t.Helper()
	got, err := NewDate(y, m, d)
	if err != nil {
		t.Fatalf("NewDate(%d-%d-%d): %v", y, m, d, err)
	}
	return got
}

// fixedClock returns a clock reporting a constant instant.
func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

var fixedID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func newTestService(t *testing.T, repo *fakeRepo, hol *fakeHolidays, now time.Time) *Service {
	t.Helper()
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return NewService(repo, hol, fixedClock(now), func() uuid.UUID { return fixedID }, loc)
}

func TestBookSuccess(t *testing.T) {
	repo := &fakeRepo{}
	hol := &fakeHolidays{}
	// Thursday 2075-08-15 (a working, non-holiday day).
	now := time.Date(2075, 8, 1, 9, 0, 0, 0, time.UTC)
	svc := newTestService(t, repo, hol, now)

	appt, err := svc.Book(context.Background(), Command{
		FirstName: "  Ada  ",
		LastName:  "Lovelace",
		VisitDate: mustDate(t, 2075, 8, 15),
	})
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if appt.FirstName != "Ada" || appt.LastName != "Lovelace" {
		t.Fatalf("names not trimmed: %+v", appt)
	}
	if appt.ID != fixedID {
		t.Fatalf("ID = %v, want injected %v", appt.ID, fixedID)
	}
	if !appt.CreatedAt.Equal(now.UTC()) {
		t.Fatalf("CreatedAt = %v, want %v", appt.CreatedAt, now.UTC())
	}
	if repo.saved == nil {
		t.Fatal("appointment not persisted")
	}
}

func TestBookRuleRejections(t *testing.T) {
	// now: Wednesday 2075-08-14 12:00 UTC.
	now := time.Date(2075, 8, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		cmd       Command
		holiday   bool
		wantErr   error
		wantSaved bool
	}{
		{
			name:    "blank first name",
			cmd:     Command{FirstName: "   ", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15)},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "over-long last name",
			cmd:     Command{FirstName: "Ada", LastName: strings.Repeat("x", 101), VisitDate: mustDate(t, 2075, 8, 15)},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "yesterday is past",
			cmd:     Command{FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 13)},
			wantErr: ErrDateInPast,
		},
		{
			name:    "saturday is not a working day",
			cmd:     Command{FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 17)},
			wantErr: ErrNotWorkingDay,
		},
		{
			name:    "sunday is not a working day",
			cmd:     Command{FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 18)},
			wantErr: ErrNotWorkingDay,
		},
		{
			name:    "public holiday rejected",
			cmd:     Command{FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15)},
			holiday: true,
			wantErr: ErrDateIsHoliday,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			hol := &fakeHolidays{isHoliday: tt.holiday}
			svc := newTestService(t, repo, hol, now)

			_, err := svc.Book(context.Background(), tt.cmd)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Book error = %v, want %v", err, tt.wantErr)
			}
			if repo.calls.Load() != 0 {
				t.Fatalf("repository was called on a rejected booking")
			}
		})
	}
}

// TestBookRejectsZeroDate ensures an unconstructed (zero) Date reaching Book is
// classified as invalid input, not misreported as "in the past".
func TestBookRejectsZeroDate(t *testing.T) {
	repo := &fakeRepo{}
	now := time.Date(2075, 8, 1, 9, 0, 0, 0, time.UTC)
	svc := newTestService(t, repo, &fakeHolidays{}, now)

	_, err := svc.Book(context.Background(), Command{
		FirstName: "Ada", LastName: "Lovelace", VisitDate: Date{}, // zero value
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Book error = %v, want ErrInvalidInput", err)
	}
	if repo.calls.Load() != 0 {
		t.Fatal("repository must not be called for an invalid date")
	}
}

func TestBookTodayIsValid(t *testing.T) {
	// 23:59 London on Thursday 2075-08-15; the visit is for the same day.
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := time.Date(2075, 8, 15, 23, 59, 0, 0, london)
	repo := &fakeRepo{}
	svc := newTestService(t, repo, &fakeHolidays{}, now)

	if _, err := svc.Book(context.Background(), Command{
		FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15),
	}); err != nil {
		t.Fatalf("today should be valid, got %v", err)
	}
}

func TestBookHolidayProviderFailureFailsClosed(t *testing.T) {
	repo := &fakeRepo{}
	hol := &fakeHolidays{err: ErrHolidayProviderUnavailable}
	now := time.Date(2075, 8, 1, 9, 0, 0, 0, time.UTC)
	svc := newTestService(t, repo, hol, now)

	_, err := svc.Book(context.Background(), Command{
		FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15),
	})
	if !errors.Is(err, ErrHolidayProviderUnavailable) {
		t.Fatalf("Book error = %v, want ErrHolidayProviderUnavailable", err)
	}
	if repo.calls.Load() != 0 {
		t.Fatal("nothing must be persisted when holiday status is unknown")
	}
}

func TestBookPropagatesDateTaken(t *testing.T) {
	repo := &fakeRepo{err: ErrDateTaken}
	now := time.Date(2075, 8, 1, 9, 0, 0, 0, time.UTC)
	svc := newTestService(t, repo, &fakeHolidays{}, now)

	_, err := svc.Book(context.Background(), Command{
		FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15),
	})
	if !errors.Is(err, ErrDateTaken) {
		t.Fatalf("Book error = %v, want ErrDateTaken", err)
	}
}

// TestBookUTCvsLondonMidnight verifies the past check uses the business
// timezone. Just after midnight London on 2075-08-15, the same instant is still
// 2075-08-14 in UTC — a booking for the 15th must be accepted, proving we do not
// compare against the UTC day.
func TestBookUTCvsLondonMidnight(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := time.Date(2075, 8, 15, 0, 30, 0, 0, london) // 2075-08-14 in UTC
	repo := &fakeRepo{}
	svc := newTestService(t, repo, &fakeHolidays{}, now)

	if _, err := svc.Book(context.Background(), Command{
		FirstName: "Ada", LastName: "Lovelace", VisitDate: mustDate(t, 2075, 8, 15),
	}); err != nil {
		t.Fatalf("booking for today (London) should pass, got %v", err)
	}
}
