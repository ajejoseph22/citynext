// Package e2e wires the real HTTP mux, booking service, Nager client (pointed at
// a stub), and a real Postgres database, exercising the full happy path plus a
// duplicate rejection. It is skipped unless TEST_DATABASE_URL is set.
package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"citynext/internal/booking"
	"citynext/internal/httpapi"
	"citynext/internal/nager"
	"citynext/internal/postgres"
	"citynext/internal/postgres/pgtest"
)

func TestBookingEndToEnd(t *testing.T) {
	ctx := context.Background()

	// Isolated schema so this package never clobbers the postgres package's rows
	// when go test runs them in parallel.
	pool := pgtest.Setup(t)
	repo := postgres.NewRepository(pool)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Stub Nager: no holidays.
	holidayStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer holidayStub.Close()

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("tz: %v", err)
	}
	// Freeze "now" well before the visit date so it is neither past nor today.
	clock := func() time.Time { return time.Date(2075, 1, 1, 9, 0, 0, 0, time.UTC) }
	svc := booking.NewService(repo, nager.New(holidayStub.URL), clock, uuid.New, loc)
	mux := httpapi.NewHandler(svc, nil).Routes()

	// A Thursday, working day, non-holiday.
	body := `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`

	rec := post(t, mux, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first booking status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	rec = post(t, mux, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate booking status = %d, want 409; body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "date_taken") {
		t.Fatalf("duplicate body = %s, want date_taken code", rec.Body)
	}
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
