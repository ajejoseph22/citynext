// Package httpapi exposes the single POST /appointments endpoint.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"citynext/internal/booking"
)

// maxBodyBytes caps a request body. A booking payload is tiny; anything larger
// is rejected before it can consume memory.
const maxBodyBytes = 16 << 10 // 16 KiB

// Booker books a visit. The handler depends on this narrow interface rather than
// the concrete service.
type Booker interface {
	Book(ctx context.Context, cmd booking.Command) (booking.Appointment, error)
}

// Handler serves the appointment endpoint.
type Handler struct {
	booker Booker
	log    *slog.Logger
}

// NewHandler returns a Handler backed by booker.
func NewHandler(booker Booker, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{booker: booker, log: log}
}

// Routes returns the mux for the service. There is exactly one route.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /appointments", h.createAppointment)
	return mux
}

// bookingRequest is the wire shape. Pointers distinguish an absent field from a
// zero value so a missing field yields ErrInvalidInput rather than a misleading
// downstream rejection.
type bookingRequest struct {
	FirstName *string       `json:"first_name"`
	LastName  *string       `json:"last_name"`
	VisitDate *booking.Date `json:"visit_date"`
}

// appointmentResponse is the success body.
type appointmentResponse struct {
	ID        string       `json:"id"`
	FirstName string       `json:"first_name"`
	LastName  string       `json:"last_name"`
	VisitDate booking.Date `json:"visit_date"`
}

func (h *Handler) createAppointment(w http.ResponseWriter, r *http.Request) {
	if err := requireJSON(r); err != nil {
		h.writeError(w, r, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req bookingRequest
	if err := dec.Decode(&req); err != nil {
		h.writeError(w, r, decodeError(err))
		return
	}
	// Reject anything after the first JSON value. Decoder.More is unreliable here
	// (it treats a trailing '}' or ']' as "no more"), so decode a second time and
	// require io.EOF — only a lone value followed by optional whitespace passes.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		h.writeError(w, r, wrapInvalid("body must contain a single JSON object"))
		return
	}

	cmd, err := req.toCommand()
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	appt, err := h.booker.Book(r.Context(), cmd)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	h.writeJSON(w, r, http.StatusCreated, appointmentResponse{
		ID:        appt.ID.String(),
		FirstName: appt.FirstName,
		LastName:  appt.LastName,
		VisitDate: appt.VisitDate,
	})
}

// toCommand validates presence of every field and builds a booking.Command.
func (r bookingRequest) toCommand() (booking.Command, error) {
	if r.FirstName == nil || r.LastName == nil || r.VisitDate == nil {
		return booking.Command{}, wrapInvalid("first_name, last_name and visit_date are required")
	}
	return booking.Command{
		FirstName: *r.FirstName,
		LastName:  *r.LastName,
		VisitDate: *r.VisitDate,
	}, nil
}

// requireJSON enforces a JSON content type.
func requireJSON(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return wrapInvalid("Content-Type must be application/json")
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return wrapInvalid("Content-Type must be application/json")
	}
	return nil
}

// decodeError converts a json decode failure into a domain error, preserving the
// ErrInvalidInput carried by booking.Date.UnmarshalJSON.
func decodeError(err error) error {
	if errors.Is(err, booking.ErrInvalidInput) {
		return err
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return wrapInvalid("request body too large")
	}
	return wrapInvalid("malformed JSON body")
}

func wrapInvalid(msg string) error {
	return fmt.Errorf("%w: %s", booking.ErrInvalidInput, msg)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := statusFor(err)
	if status >= http.StatusInternalServerError {
		h.log.ErrorContext(r.Context(), "request failed", "err", err, "status", status)
	}
	h.writeJSON(w, r, status, errorResponse{Error: errorBody{Code: code, Message: clientMessage(err, status)}})
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.log.ErrorContext(r.Context(), "encoding response", "err", err)
	}
}
