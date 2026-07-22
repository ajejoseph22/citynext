# CityNext Appointment Service

A REST service, consisting of a single endpoint that books a visit to the CityNext office for a particular date.

The office obeys these rules:

- The office accepts only one visit for each date.
- The office is open only on a working day.
- The office is closed on a public holiday in the United Kingdom.
- The office does not accept a date in the past.

## The endpoint

Send a POST request to `localhost:8080/appointments`. Set the `Content-Type` header to
`application/json`. Add a JSON object in the request body having these 3
fields:

- `first_name`
- `last_name`
- `visit_date` (format: `YYYY-MM-DD`)

Sample request:

```bash
curl -sS -i -X POST localhost:8080/appointments \
  -H 'Content-Type: application/json' \
  -d '{"first_name":"Ada","last_name":"Lovelace","visit_date":"2075-08-15"}'
```

If the service accepts the request, it returns HTTP status 201. The response
body contains the new appointment:

```json
{ "id": "…", "first_name": "Ada", "last_name": "Lovelace", "visit_date": "2075-08-15" }
```

The service rejects the request in the conditions that follow. The table lists
each condition, the HTTP status, and the error code.

| Condition | HTTP status | Code |
|-----------|-------------|------|
| The input is malformed or incomplete. The date is not valid. The body is too large. The content type is wrong. | `400` | `invalid_input` |
| The visit date is in the past. | `422` | `date_in_past` |
| The visit date is a weekend. | `422` | `not_working_day` |
| The visit date is a public holiday. | `422` | `date_is_holiday` |
| The date is already booked. | `409` | `date_taken` |
| The holiday provider does not answer. | `502` | `holiday_provider_unavailable` |

Each error response has the same shape:

```json
{ "error": { "code": "date_taken", "message": "an appointment already exists for this date" } }
```

## Running the service

To run the service, do the steps that follow:

1. Copy the example environment file:
   ```bash
   cp .env.example .env
   ```
2. Start the service and the database:
   ```bash
   docker compose up --build
   ```
3. Send a request (see section [The endpoint](README.md#the-endpoint)).

To test the duplicate rule, send the same request a second time. The service
returns HTTP status `409`.

## Design

For the architecture, the design decisions, and how the service prevents
duplicate bookings, see [DESIGN.md](DESIGN.md).

## Tests

To run the unit tests, use this command:

```bash
make test
```

This command runs all unit tests with the race detector. It needs no external
service.

To run the integration tests, set `TEST_DATABASE_URL` and use this command:

```bash
make test-integration
```

This command needs a real PostgreSQL database. It fails if `TEST_DATABASE_URL`
is not set. Each integration test uses its own database schema. Therefore the
tests do not share rows, and they can run in parallel.

To start a local database and run the integration tests, run the steps below:

```bash
docker compose up -d db
export TEST_DATABASE_URL=postgres://citynext:citynext@localhost:5432/citynext?sslmode=disable
make test-integration
```

## Configuration

The service reads the configuration from environment variables. The file
`.env.example` lists them: `PORT`, `DATABASE_URL`, `DB_MAX_CONNS`,
`NAGER_BASE_URL`, `HTTP_TIMEOUT`, `REQUEST_TIMEOUT`, `TIMEZONE`,
`DB_CONNECT_RETRIES`, and `DB_CONNECT_BACKOFF`.

`DATABASE_URL` is mandatory. The service also rejects a numeric value or a
duration value that is zero or negative. This rule prevents a configuration that
disables a timeout or stops the service.

The service creates the table at startup with `CREATE TABLE IF NOT EXISTS`. This
is a startup bootstrap, not a migration tool. It is sufficient for this single
table.
