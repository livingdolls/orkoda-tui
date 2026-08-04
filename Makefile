SHELL := /bin/sh

.PHONY: help install api migrate tui protocol sandbox-image release fmt lint security test check clean-data

help:
	@printf '%s\n' \
		'install     Install Bun dependencies and download Go modules' \
		'api         Run the local Orkoda daemon' \
		'migrate     Initialize or update the local SQLite database' \
		'tui         Run the OpenTUI client' \
		'protocol    Regenerate TypeScript protocol types from JSON Schema' \
		'sandbox-image Build the isolated check runner image' \
		'release     Build the local daemon and TUI distribution artifacts' \
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

protocol:
	bun run scripts/generate-protocol.ts

sandbox-image:
	docker build --file Dockerfile.sandbox --tag orkoda-sandbox:local .

release:
	mkdir -p dist
	go build -trimpath -o dist/orkoda-api ./cmd/api
	bun build apps/tui/src/index.tsx --target bun --outdir dist/tui --external '@opentui/core-*'
	mkdir -p dist/protocol-schemas
	cp packages/protocol/schemas/*.json dist/protocol-schemas/

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)
	bun run format:ts

lint:
	go vet ./...
	bun run lint:ts

security:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	gitleaks detect --redact --no-banner --config .gitleaks.toml

test:
	go test ./...
	bun run test:ts

check: lint security test
	bun run typecheck

clean-data:
	rm -rf .orkoda
