package booking

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AppointmentRepository persists appointments.
type AppointmentRepository interface {
	// Save inserts appt. If the visit_date uniqueness constraint is violated the
	// insert is rejected atomically — nothing is written — and Save returns
	// ErrDateTaken. Any other error (including a different unique violation) is
	// returned as-is.
	Save(ctx context.Context, appt Appointment) error
}

// HolidayProvider reports UK public holidays.
type HolidayProvider interface {
	// IsPublicHoliday reports whether d is a GB public holiday. It returns
	// ErrHolidayProviderUnavailable (wrapping the cause) when holiday status
	// cannot be established.
	IsPublicHoliday(ctx context.Context, d Date) (bool, error)
}

// Clock returns the current time. Production wires time.Now; tests provide a
// fixed function so the booking rules are deterministic.
type Clock func() time.Time

// IDGenerator returns a new appointment identifier. Keeping entropy behind this
// function keeps the booking rules deterministic under test.
type IDGenerator func() uuid.UUID
