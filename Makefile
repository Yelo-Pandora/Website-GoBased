APP_NAME := purchase-api
APP_ENTRY := ./cmd/api
APP_BIN := bin/$(APP_NAME)

.PHONY: build run test tidy fmt

build:
	@go build -o $(APP_BIN) $(APP_ENTRY)

run:
	@go run $(APP_ENTRY)

test:
	@go test -v ./...

tidy:
	@go mod tidy

fmt:
	@gofmt -w ./cmd ./internal
