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
	$(GO) test -race -count=1 -timeout=15m -tags=integration ./...

.PHONY: vet
vet: ## go vet, including the integration-tagged tests
	$(GO) vet $(PKG)
	# Behind a build tag, so `go vet ./...` never sees them and they rot in
	# silence: two of them had been referencing a renamed constant and an old
	# function signature for long enough that nobody could say when it started.
	# Vet compiles them without needing Docker or Postgres, which is the whole
	# cost of never letting that happen again.
	$(GO) vet -tags integration $(PKG)

# `go install` puts binaries in GOPATH/bin, which is not on PATH by default — so
# following the install line printed below leaves the next `make lint` still
# reporting the tool as missing. Look there as well as on PATH.
GOBIN := $(shell go env GOPATH)/bin
LINT := $(shell command -v golangci-lint 2>/dev/null || echo $(GOBIN)/golangci-lint)

.PHONY: lint
lint: ## golangci-lint, including the R-027 adapter import rule
	@test -x "$(LINT)" \
		|| { echo "golangci-lint v2 not installed:"; \
		     echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; exit 1; }
	$(LINT) run

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

.PHONY: console-check
console-check: ## Brand adherence and types for the console (needs npm)
	cd console && npm ci && npm run check

# ---------------------------------------------------------------------------
# Detection

.PHONY: detection-corpus
detection-corpus: ## Run detection against the corpus of real repositories
	$(GO) test -tags=integration -count=1 -timeout=20m -v ./test/corpus/

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
# Release
# ---------------------------------------------------------------------------

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml (needs goreleaser)
	goreleaser check

.PHONY: release-snapshot
release-snapshot: ## Build the release artifacts locally, publishing nothing
	goreleaser release --snapshot --clean

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage.out
