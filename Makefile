.PHONY: test test-integration build run up down logs fmt vet tidy

# Deterministic unit tests with the race detector; no external services.
test:
	go test -race ./...

# Integration tests against a real Postgres. Fails loudly if the database URL is
# unset, so persistence is never "green by omission".
test-integration:
ifndef TEST_DATABASE_URL
	$(error TEST_DATABASE_URL is required, e.g. postgres://citynext:citynext@localhost:5432/citynext?sslmode=disable)
endif
	go test -race -count=1 ./internal/postgres/... ./test/...

build:
	go build ./...

run:
	go run ./cmd/server

# Bring the whole stack up (app + Postgres) for a reviewer.
up:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy
