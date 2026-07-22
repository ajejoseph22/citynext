# Design

This document describes the architecture and the design decisions of the
CityNext appointment service. For the API and the commands to run the service,
see [README.md](README.md).

## Architecture

The service uses the hexagonal architecture (ports and adapters). The domain
code does not depend on HTTP, SQL, or the holiday API. The domain code defines
the interfaces. The adapter code obeys these interfaces. All dependencies point
inward to the domain.

```
httpapi ──▶ booking ◀── postgres (AppointmentRepository)
                    ◀── nager    (HolidayProvider)
```

The packages are:

- `internal/booking` — the domain. It contains the model, the calendar-day
  `Date` type, the rules, the sentinel errors, and the port interfaces.
- `internal/httpapi` — the HTTP adapter. It reads the request, checks the input,
  calls the domain, and maps each error to an HTTP status.
- `internal/postgres` — the database adapter. It obeys the repository interface.
- `internal/nager` — the holiday adapter. It obeys the holiday provider
  interface.
- `internal/config` — it reads the configuration from environment variables.
- `cmd/server` — the composition root. It connects the parts and starts the
  server.

## Rules and decisions

1. **Weekends.** The service always rejects Saturday and Sunday. There is no
   option to disable this rule.
2. **Subdivisions.** The service rejects a date if the GB holiday endpoint
   returns any record for that date. The service ignores the `global` and
   `counties` fields.
3. **Timezone.** The service compares the visit date to the current day in the
   `Europe/London` timezone. The service accepts the current day.
4. **Database.** The service uses PostgreSQL. Docker Compose starts the database.
   You need only Docker.

## How the service prevents duplicates

The `appointments` table has a `UNIQUE` constraint on `visit_date`. The
repository runs one `INSERT`. If the date is already present, PostgreSQL rejects
the insert and writes nothing. The repository maps this error to `date_taken`.
The service does not read the table before the insert. Therefore two requests
for the same date cannot both succeed.
