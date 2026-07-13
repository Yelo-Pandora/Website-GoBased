.PHONY: build compose-build compose-down compose-up fmt test tidy verify

build:
	go build ./services/platform-api/cmd/platform-api
	go build ./services/orchestrator/cmd/orchestrator
	go build ./services/lab-app/cmd/lab-app

compose-build:
	docker compose build

compose-up:
	docker compose up -d --wait

compose-down:
	docker compose down

fmt:
	gofmt -w ./internal ./services

test:
	go test ./...

tidy:
	go mod tidy

verify: fmt test build
	docker compose config --quiet
