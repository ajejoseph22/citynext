package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"citynext/internal/booking"
)

// fakeBooker returns a configured appointment/error and records whether it ran.
type fakeBooker struct {
	appt   booking.Appointment
	err    error
	called bool
}

func (f *fakeBooker) Book(_ context.Context, cmd booking.Command) (booking.Appointment, error) {
	f.called = true
	if f.err != nil {
		return booking.Appointment{}, f.err
	}
	f.appt.FirstName = cmd.FirstName
	f.appt.LastName = cmd.LastName
	f.appt.VisitDate = cmd.VisitDate
	return f.appt, nil
}

func do(t *testing.T, h http.Handler, body string, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, opt := range opts {
		opt(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(b Booker) http.Handler {
	return NewHandler(b, nil).Routes()
}

func TestCreateAppointmentSuccess(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	b := &fakeBooker{appt: booking.Appointment{ID: id, CreatedAt: time.Now()}}
	rec := do(t, newHandler(b), `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	var got appointmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != id.String() || got.FirstName != "Ada" || got.VisitDate.String() != "2075-08-15" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		bookErr    error
		wantStatus int
		wantCode   string
	}{
		{"in past", booking.ErrDateInPast, http.StatusUnprocessableEntity, "date_in_past"},
		{"weekend", booking.ErrNotWorkingDay, http.StatusUnprocessableEntity, "not_working_day"},
		{"holiday", booking.ErrDateIsHoliday, http.StatusUnprocessableEntity, "date_is_holiday"},
		{"taken", booking.ErrDateTaken, http.StatusConflict, "date_taken"},
		{"provider down", booking.ErrHolidayProviderUnavailable, http.StatusBadGateway, "holiday_provider_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBooker{err: tt.bookErr}
			rec := do(t, newHandler(b), `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var env errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if env.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", env.Error.Code, tt.wantCode)
			}
		})
	}
}

// TestHolidayProviderErrorMasksDetail ensures a 502 response returns only the
// stable sentinel message, never the wrapped upstream URL or network detail.
func TestHolidayProviderErrorMasksDetail(t *testing.T) {
	wrapped := fmt.Errorf("%w: Get %q: dial tcp 10.0.0.9:9999: connection refused",
		booking.ErrHolidayProviderUnavailable, "http://secret-internal/api/v3/PublicHolidays/2075/GB")
	b := &fakeBooker{err: wrapped}
	rec := do(t, newHandler(b), `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-internal") || strings.Contains(body, "dial tcp") {
		t.Fatalf("upstream detail leaked: %s", body)
	}
	var env errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Message != booking.ErrHolidayProviderUnavailable.Error() {
		t.Fatalf("message = %q, want sentinel %q", env.Error.Message, booking.ErrHolidayProviderUnavailable.Error())
	}
}

func TestInternalErrorIsMasked(t *testing.T) {
	b := &fakeBooker{err: errorString("secret db detail")}
	rec := do(t, newHandler(b), `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("internal detail leaked: %s", rec.Body)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestInputValidationCases(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string // "" means default application/json
		wantStatus  int
		wantCalled  bool
	}{
		{"missing visit_date", `{"first_name":"Ada","last_name":"Lovelace"}`, "", http.StatusBadRequest, false},
		{"bad date", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-02-30"}`, "", http.StatusBadRequest, false},
		{"unknown field", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15","x":1}`, "", http.StatusBadRequest, false},
		{"trailing object", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}{}`, "", http.StatusBadRequest, false},
		{"trailing close brace", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}}`, "", http.StatusBadRequest, false},
		{"trailing close bracket", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}]`, "", http.StatusBadRequest, false},
		{"trailing garbage", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"} x`, "", http.StatusBadRequest, false},
		{"trailing whitespace ok", "{\"first_name\":\"Ada\",\"last_name\":\"Lovelace\",\"visit_date\":\"2075-08-15\"}\n  ", "", http.StatusCreated, true},
		{"malformed json", `{`, "", http.StatusBadRequest, false},
		{"wrong content type", `{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}`, "text/plain", http.StatusBadRequest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBooker{appt: booking.Appointment{ID: uuid.New()}}
			rec := do(t, newHandler(b), tt.body, func(r *http.Request) {
				if tt.contentType != "" {
					r.Header.Set("Content-Type", tt.contentType)
				}
			})
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body)
			}
			if b.called != tt.wantCalled {
				t.Fatalf("booker called = %v, want %v (must not run on invalid input)", b.called, tt.wantCalled)
			}
		})
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	huge := `{"first_name":"` + strings.Repeat("a", 20000) + `","last_name":"L","visit_date":"2075-08-15"}`
	b := &fakeBooker{}
	rec := do(t, newHandler(b), huge)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for oversized body", rec.Code)
	}
	if b.called {
		t.Fatal("booker must not run for an oversized body")
	}
}
