package booking

import "errors"

// Sentinel errors form the entire contract between the domain and the transport
// layer. The HTTP layer maps each to a status code and never inspects an
// adapter-specific error type.
var (
	// ErrInvalidInput signals malformed or missing request data.
	ErrInvalidInput = errors.New("invalid input")

	// ErrDateInPast signals a visit date earlier than today in the business
	// timezone. Today itself is valid.
	ErrDateInPast = errors.New("visit date is in the past")

	// ErrNotWorkingDay signals a visit date that falls on a weekend.
	ErrNotWorkingDay = errors.New("visit date is not a working day")

	// ErrDateIsHoliday signals a visit date that is a UK public holiday.
	ErrDateIsHoliday = errors.New("visit date is a public holiday")

	// ErrDateTaken signals that an appointment already exists for the date.
	ErrDateTaken = errors.New("an appointment already exists for this date")

	// ErrHolidayProviderUnavailable signals that holiday status could not be
	// established. Booking fails closed: nothing is persisted.
	ErrHolidayProviderUnavailable = errors.New("holiday provider unavailable")
)
