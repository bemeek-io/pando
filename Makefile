.DEFAULT_GOAL := help
SHELL := /bin/bash

GO      ?= go
BIN     ?= bin/pando
PKG     := ./...

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Build and test
# ---------------------------------------------------------------------------

.PHONY: build
build: ## Build the pando binary
	$(GO) build -o $(BIN) ./cmd/pando

.PHONY: test
test: ## Run unit tests
	$(GO) test -race -count=1 $(PKG)

.PHONY: test-integration
test-integration: ## Run integration tests (real Postgres + Docker, via testcontainers)
	$(GO) test -race -count=1 -tags=integration ./test/...

.PHONY: vet
vet: ## go vet
	$(GO) vet $(PKG)

.PHONY: lint
lint: ## golangci-lint, including the R-027 adapter import rule
	@command -v golangci-lint >/dev/null 2>&1 \
		|| { echo "golangci-lint v2 not installed:"; \
		     echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; exit 1; }
	golangci-lint run

.PHONY: check
check: vet lint test ## Everything CI runs on a pull request

# ---------------------------------------------------------------------------
# Code generation
# ---------------------------------------------------------------------------

.PHONY: sqlc
sqlc: ## Regenerate typed queries from SQL
	sqlc generate

.PHONY: console
console: ## Build the console into assets embedded by internal/console
	cd console && npm ci && npm run build

# ---------------------------------------------------------------------------
# Requirement traceability (design 00 §4)
# ---------------------------------------------------------------------------

.PHONY: requirements-index
requirements-index: ## Regenerate docs/traceability/requirements-index.md
	python3 scripts/gen-requirements-index.py

.PHONY: requirements-coverage
requirements-coverage: ## Report which R-IDs have a named acceptance test
	python3 scripts/gen-requirements-index.py --coverage

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage.out
