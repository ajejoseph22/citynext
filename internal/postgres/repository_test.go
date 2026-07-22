package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"citynext/internal/booking"
	"citynext/internal/postgres/pgtest"
)

// newTestRepo returns a migrated Repository backed by a fresh, isolated schema
// (see pgtest.Setup). Each call gets its own schema, so tests never share rows —
// within a package or across concurrently-run packages. Skipped when
// TEST_DATABASE_URL is unset; make test-integration sets it.
func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	pool := pgtest.Setup(t)
	repo := NewRepository(pool)
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repo
}

func mustDate(t *testing.T, y int, m time.Month, d int) booking.Date {
	t.Helper()
	got, err := booking.NewDate(y, m, d)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	return got
}

func sampleAppt(t *testing.T, d booking.Date) booking.Appointment {
	return booking.Appointment{
		ID:        uuid.New(),
		FirstName: "Ada",
		LastName:  "Lovelace",
		VisitDate: d,
		CreatedAt: time.Date(2075, 8, 1, 9, 0, 0, 0, time.UTC),
	}
}

func TestSaveAndReadBackNoDateShift(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	appt := sampleAppt(t, mustDate(t, 2075, 8, 15))
	if err := repo.Save(ctx, appt); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var visit time.Time
	var first string
	var created time.Time
	err := repo.pool.QueryRow(ctx,
		"SELECT visit_date, first_name, created_at FROM appointments WHERE id=$1", appt.ID,
	).Scan(&visit, &first, &created)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got := booking.DateFromTime(visit); got != appt.VisitDate {
		t.Fatalf("visit_date round-trip = %s, want %s (day shifted)", got, appt.VisitDate)
	}
	if first != "Ada" {
		t.Fatalf("first_name = %q, want Ada", first)
	}
	if !created.Equal(appt.CreatedAt) {
		t.Fatalf("created_at = %v, want application-supplied %v", created, appt.CreatedAt)
	}
}

func TestSaveDuplicateDateReturnsErrDateTaken(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	d := mustDate(t, 2075, 8, 15)
	if err := repo.Save(ctx, sampleAppt(t, d)); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := repo.Save(ctx, sampleAppt(t, d)); !errors.Is(err, booking.ErrDateTaken) {
		t.Fatalf("second Save error = %v, want ErrDateTaken", err)
	}
}

// TestConcurrentDuplicateInsert proves the UNIQUE constraint yields exactly one
// winner under concurrency, with no TOCTOU pre-check.
func TestConcurrentDuplicateInsert(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	d := mustDate(t, 2075, 8, 15)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	var success, taken atomic.Int32
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			err := repo.Save(ctx, sampleAppt(t, d))
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, booking.ErrDateTaken):
				taken.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || taken.Load() != n-1 {
		t.Fatalf("success=%d taken=%d, want 1 and %d", success.Load(), taken.Load(), n-1)
	}
}

// TestOtherUniqueViolationNotMappedToDateTaken inserts a duplicate primary key
// (same id, different date). That is a different constraint and must NOT be
// reported as ErrDateTaken.
func TestOtherUniqueViolationNotMappedToDateTaken(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	id := uuid.New()

	first := sampleAppt(t, mustDate(t, 2075, 8, 15))
	first.ID = id
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second := sampleAppt(t, mustDate(t, 2075, 8, 16))
	second.ID = id // PK collision, different date
	err := repo.Save(ctx, second)
	if err == nil {
		t.Fatal("expected a primary-key violation")
	}
	if errors.Is(err, booking.ErrDateTaken) {
		t.Fatalf("PK violation wrongly mapped to ErrDateTaken: %v", err)
	}
}

// TestCheckConstraintsRejectUntrimmedName covers every ASCII whitespace the
// domain trims. The CHECK must reject each, so tab- or newline-surrounded values
// cannot bypass the mirrored invariant. Each case uses a distinct visit_date to
// avoid the unique constraint.
func TestCheckConstraintsRejectUntrimmedName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	cases := []struct {
		name  string
		value string
	}{
		{"space surrounded", " untrimmed "},
		{"tab surrounded", "\tAda\t"},
		{"newline surrounded", "\nAda\n"},
		{"carriage return", "\rAda\r"},
	}
	for i, tc := range cases {
		appt := sampleAppt(t, mustDate(t, 2075, 8, 10+i))
		appt.FirstName = tc.value
		err := repo.Save(ctx, appt)
		if err == nil || errors.Is(err, booking.ErrDateTaken) {
			t.Fatalf("%s: expected a CHECK violation, got %v", tc.name, err)
		}
	}
}

// TestCheckConstraintsAcceptNamesWithV guards against a whitespace set that
// accidentally contains the literal letter "v" (a \v escape that PostgreSQL does
// not support). Legitimate names that start or end with "v" must be accepted.
func TestCheckConstraintsAcceptNamesWithV(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	names := []string{"Liv", "Vera", "Yaroslav"}
	for i, name := range names {
		appt := sampleAppt(t, mustDate(t, 2075, 9, 1+i))
		appt.FirstName = name
		appt.LastName = name
		if err := repo.Save(ctx, appt); err != nil {
			t.Fatalf("Save(%q) should succeed, got %v", name, err)
		}
	}
}
