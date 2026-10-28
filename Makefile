.PHONY: run up down test test-race test-pg lint fmt generate

TEST_PG_CONTAINER := posts-test-pg
TEST_PG_PORT      := 55432
TEST_PG_URL       := postgres://posts:posts@localhost:$(TEST_PG_PORT)/posts?sslmode=disable

## run: start the server with in-memory storage
run:
	go run ./cmd/server -storage=memory

## up: start the server with PostgreSQL in docker compose
up:
	docker compose up --build

## down: stop docker compose and remove its data
down:
	docker compose down -v

## test: run unit tests (PostgreSQL tests are skipped)
test:
	go test ./...

## test-race: run unit tests with the race detector (requires cgo)
test-race:
	go test -race ./...

## test-pg: run the storage contract tests against a throwaway PostgreSQL
test-pg:
	docker run -d --rm --name $(TEST_PG_CONTAINER) -p $(TEST_PG_PORT):5432 \
		-e POSTGRES_USER=posts -e POSTGRES_PASSWORD=posts -e POSTGRES_DB=posts postgres:17-alpine
	until docker exec $(TEST_PG_CONTAINER) pg_isready -U posts -d posts -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done
	TEST_DATABASE_URL="$(TEST_PG_URL)" go test -count=1 ./internal/storage/postgres; \
		status=$$?; docker stop $(TEST_PG_CONTAINER) >/dev/null; exit $$status

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## fmt: format the code
fmt:
	golangci-lint fmt ./...

## generate: regenerate GraphQL code from the schema
generate:
	go tool gqlgen generate
