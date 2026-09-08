.PHONY: build check fmt-check run test vet race integration reliability

build:
	go build -o bin/ ./cmd/...

check: fmt-check build test vet

fmt-check:
	test -z "$$(gofmt -l cmd internal)"

run:
	go run ./cmd/api

test:
	go test -shuffle=on -timeout=60s ./...

vet:
	go vet ./...

race:
	go test -race -shuffle=on -timeout=120s ./...

integration:
	test -n "$$TEST_DATABASE_URL"
	go test -race -tags=integration -shuffle=on -count=1 -timeout=180s ./internal/postgres

reliability:
	test -n "$$TEST_DATABASE_URL"
	go test -race -tags=integration -shuffle=on -count=3 -timeout=300s -v -run 'TestConcurrent|TestProcess(Crash|Stale|API)' ./internal/postgres
