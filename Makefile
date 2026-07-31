SHELL := /bin/sh

.PHONY: help install dev-up dev-down api worker migrate tui fmt lint test check

help:
	@printf '%s\n' \
		'install   Install Bun dependencies and download Go modules' \
		'dev-up    Start PostgreSQL, Redis, RabbitMQ, and MinIO' \
		'dev-down  Stop local infrastructure' \
		'api       Run the local API daemon' \
		'worker    Run the worker process' \
		'migrate   Run database migrations' \
		'tui       Run the OpenTUI client' \
		'fmt       Format Go and TypeScript sources' \
		'lint      Run Go vet and Biome checks' \
		'test      Run Go and TypeScript tests' \
		'check     Run all quality gates'

install:
	bun install
	go mod download

dev-up:
	docker compose up -d --wait

dev-down:
	docker compose down

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

migrate:
	go run ./cmd/migrate

tui:
	bun run dev:tui

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)
	bun run format:ts

lint:
	go vet ./...
	bun run lint:ts

test:
	go test ./...
	bun run test:ts

check: lint test
	bun run typecheck
