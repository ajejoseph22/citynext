package booking

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// maxNameLen bounds a first or last name, in runes. The Postgres CHECK
// constraints mirror this so persistence cannot bypass the invariant.
const maxNameLen = 100

// Command carries the raw booking request into the domain. Names are untrimmed;
// the service trims and validates them.
type Command struct {
	FirstName string
	LastName  string
	VisitDate Date
}

// Service books visits to CityNext, enforcing every acceptance rule before an
// appointment is persisted.
type Service struct {
	repo     AppointmentRepository
	holidays HolidayProvider
	clock    Clock
	newID    IDGenerator
	loc      *time.Location
}

// NewService wires a Service. loc is the business timezone used to decide
// whether a visit date is in the past.
func NewService(repo AppointmentRepository, holidays HolidayProvider, clock Clock, newID IDGenerator, loc *time.Location) *Service {
	return &Service{repo: repo, holidays: holidays, clock: clock, newID: newID, loc: loc}
}

// Book validates cmd and, if every rule passes, persists and returns the
// appointment. Rules run cheap-to-expensive so a rejection never triggers a
// needless network call or database write:
//
//  1. input sanity (names, already-parsed date)
//  2. not in the past (business-timezone calendar day; today is valid)
//  3. not a weekend
//  4. not a public holiday (fails closed if the provider is unavailable)
//  5. not already booked (enforced atomically by the repository)
func (s *Service) Book(ctx context.Context, cmd Command) (Appointment, error) {
	firstName, err := cleanName("first_name", cmd.FirstName)
	if err != nil {
		return Appointment{}, err
	}
	lastName, err := cleanName("last_name", cmd.LastName)
	if err != nil {
		return Appointment{}, err
	}

	// Guard the invariant before any date rule: an unconstructed (zero) or
	// otherwise invalid Date must be rejected as bad input, not misclassified as
	// "in the past" by the comparison below.
	if !cmd.VisitDate.Valid() {
		return Appointment{}, fmt.Errorf("%w: visit_date is not a valid date", ErrInvalidInput)
	}

	now := s.clock()
	today := DateFromTime(now.In(s.loc))
	if cmd.VisitDate.Before(today) {
		return Appointment{}, ErrDateInPast
	}

	switch cmd.VisitDate.Weekday() {
	case time.Saturday, time.Sunday:
		return Appointment{}, ErrNotWorkingDay
	}

	isHoliday, err := s.holidays.IsPublicHoliday(ctx, cmd.VisitDate)
	if err != nil {
		return Appointment{}, err
	}
	if isHoliday {
		return Appointment{}, ErrDateIsHoliday
	}

	appt := Appointment{
		ID:        s.newID(),
		FirstName: firstName,
		LastName:  lastName,
		VisitDate: cmd.VisitDate,
		CreatedAt: now.UTC(),
	}
	if err := s.repo.Save(ctx, appt); err != nil {
		return Appointment{}, err
	}
	return appt, nil
}

// cleanName trims surrounding whitespace and validates the result is non-empty
// and within the length bound.
func cleanName(field, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: %s must not be blank", ErrInvalidInput, field)
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return "", fmt.Errorf("%w: %s must be at most %d characters", ErrInvalidInput, field, maxNameLen)
	}
	return name, nil
}
