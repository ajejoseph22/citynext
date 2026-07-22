// Package postgres implements booking.AppointmentRepository on PostgreSQL.
package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"citynext/internal/booking"
)

//go:embed schema.sql
var schema string

// visitDateConstraint is the name of the UNIQUE(visit_date) constraint. Only a
// violation of this specific constraint means the date is already taken.
const visitDateConstraint = "appointments_visit_date_unique"

// uniqueViolation is the SQLSTATE code Postgres reports for a UNIQUE breach.
const uniqueViolation = "23505"

// Repository persists appointments in PostgreSQL.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a Repository backed by pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Migrate applies the schema bootstrap. It is idempotent.
func (r *Repository) Migrate(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("applying schema: %w", err)
	}
	return nil
}

// Save inserts appt. A violation of the visit_date uniqueness constraint is
// rejected atomically by Postgres — nothing is written — and mapped to
// booking.ErrDateTaken. Any other error, including a different unique violation,
// is returned unwrapped.
func (r *Repository) Save(ctx context.Context, appt booking.Appointment) error {
	const query = `
		INSERT INTO appointments (id, first_name, last_name, visit_date, created_at)
		VALUES ($1, $2, $3, $4, $5)`

	visitDate := pgtype.Date{
		Time:  time.Date(appt.VisitDate.Year(), appt.VisitDate.Month(), appt.VisitDate.Day(), 0, 0, 0, 0, time.UTC),
		Valid: true,
	}

	_, err := r.pool.Exec(ctx, query, appt.ID, appt.FirstName, appt.LastName, visitDate, appt.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == visitDateConstraint {
			return booking.ErrDateTaken
		}
		return fmt.Errorf("inserting appointment: %w", err)
	}
	return nil
}
