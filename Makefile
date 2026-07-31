SHELL := /bin/sh

.PHONY: help install api migrate tui fmt lint test check clean-data

help:
	@printf '%s\n' \
		'install     Install Bun dependencies and download Go modules' \
		'api         Run the local Orkoda daemon' \
		'migrate     Initialize or update the local SQLite database' \
		'tui         Run the OpenTUI client' \
		'fmt         Format Go and TypeScript sources' \
		'lint        Run Go vet and Biome checks' \
		'test        Run Go and TypeScript tests' \
		'check       Run all quality gates' \
		'clean-data  Remove local .orkoda runtime data'

install:
	bun install
	go mod download

api:
	go run ./cmd/api

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

clean-data:
	rm -rf .orkoda
