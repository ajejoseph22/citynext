package booking

import (
	"time"

	"github.com/google/uuid"
)

// Appointment is a booked visit to CityNext. Exactly one Appointment may exist
// per VisitDate; that invariant is enforced by the repository.
type Appointment struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
	VisitDate Date
	CreatedAt time.Time
}
