.PHONY: up down build test smoke lint

up:
	docker compose up --build -d

down:
	docker compose down -v

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -v -race

smoke:
	@echo "==> Checking healthz..."
	@curl -sf http://localhost:8080/healthz | jq .
	@echo "==> Checking readyz..."
	@curl -sf http://localhost:8080/readyz | jq .
	@echo "==> Checking sales mock..."
	@curl -sf http://localhost:9001/__admin/mappings | jq '.total'
	@echo "==> Checking service mock..."
	@curl -sf http://localhost:9002/__admin/mappings | jq '.total'

lint:
	golangci-lint run ./...
