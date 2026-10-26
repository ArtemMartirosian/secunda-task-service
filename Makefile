.PHONY: up down test test-integration

up:
	docker compose up --build

down:
	docker compose down

test:
	docker run --rm -v "$$(pwd):/src" -w /src golang:1.23-alpine sh -c "go test ./..."

test-integration:
	docker compose up -d mysql redis
	docker run --rm --network host -v "$$(pwd):/src" -w /src -e TEST_MYSQL_DSN='tasks:tasks@tcp(127.0.0.1:3306)/tasks?parseTime=true&multiStatements=true' golang:1.23-alpine sh -c "go test -tags=integration ./internal/store -run TestTeamStatsIntegration -v"
