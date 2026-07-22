package httpapi

import (
	"errors"
	"net/http"

	"citynext/internal/booking"
)

// errorResponse is the uniform JSON error envelope.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// statusFor maps a domain error to an HTTP status and a stable machine code.
// Unknown errors collapse to 500 with a generic message so internal detail is
// never leaked to clients.
func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, booking.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_input"
	case errors.Is(err, booking.ErrDateInPast):
		return http.StatusUnprocessableEntity, "date_in_past"
	case errors.Is(err, booking.ErrNotWorkingDay):
		return http.StatusUnprocessableEntity, "not_working_day"
	case errors.Is(err, booking.ErrDateIsHoliday):
		return http.StatusUnprocessableEntity, "date_is_holiday"
	case errors.Is(err, booking.ErrDateTaken):
		return http.StatusConflict, "date_taken"
	case errors.Is(err, booking.ErrHolidayProviderUnavailable):
		return http.StatusBadGateway, "holiday_provider_unavailable"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

// clientMessage returns a message safe to show a client. Domain errors carry
// user-appropriate text; internal and upstream detail is masked. The full
// wrapped error still reaches the logs (see Handler.writeError).
func clientMessage(err error, status int) string {
	switch {
	case status == http.StatusInternalServerError:
		return "an unexpected error occurred"
	case errors.Is(err, booking.ErrHolidayProviderUnavailable):
		// Do not leak wrapped upstream URLs or network detail; return the stable
		// sentinel text only.
		return booking.ErrHolidayProviderUnavailable.Error()
	default:
		return err.Error()
	}
}
