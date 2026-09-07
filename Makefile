.PHONY: build check fmt-check run test vet

build:
	go build ./cmd/api

check: fmt-check test vet

fmt-check:
	test -z "$$(gofmt -l $$(find . -type f -name '*.go' -not -path './work/*'))"

run:
	go run ./cmd/api

test:
	go test ./...

vet:
	go vet ./...
