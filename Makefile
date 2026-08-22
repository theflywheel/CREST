# CREST — the command surface every skill, hook and CI job builds against.
#
# Targets that are implemented, run. Targets whose subject does not exist yet
# fail loudly saying what they will do. A green target that ran nothing is worse
# than a red one, because it launders absence as proof.

SHELL := bash
COMPOSE := docker compose -f infra/compose/docker-compose.yml
SERVICES := registry definitions evidence confirmation verification payments notify
GO ?= go

.PHONY: help build test test-all test-unit test-contract test-e2e test-invariants \
        lint fmt structure substrate-up substrate-down harness-up harness-down \
        harness-logs verify-deploy clean todo

help: ## Show available targets
	@grep -E '^[a-z][a-z-]*:.*?## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t26

todo:
	@echo "Not implemented yet — arrives with the issue that owns it."
	@echo "See docs/COMPONENTS.md for what this will do."
	@exit 1

# ── Build and check ─────────────────────────────────────────────────────────

build: ## Build every Go binary
	$(GO) build ./...

fmt: ## Format Go sources
	$(GO) fmt ./...
	@command -v golangci-lint >/dev/null && golangci-lint fmt ./... || true

lint: structure ## Lint, including the structure rules
	golangci-lint run ./...

structure: ## Enforce repository layout (fails on a misplaced file)
	@$(GO) run ./tools/lint/structure

# ── Tests ───────────────────────────────────────────────────────────────────

test: test-unit test-contract ## Unit + contract (the fast pair, run constantly)

test-all: test test-e2e test-invariants ## Everything CI runs on a PR

test-unit: ## Pure functions: strength fn, schemas, state machines, money
	$(GO) test ./pkg/... ./services/... ./adapters/... ./tools/...

test-contract: ## Fixture-driven: adapters, OpenAPI shapes, credential shape
	@if [ -d tests/contract ]; then $(GO) test ./tests/contract/...; \
	else echo "no contract tests yet — they arrive with #41"; fi

test-e2e: ## Real services: CSV -> unit -> claim -> confirm -> issue -> verify
	@if [ -d harness/scenarios ] && [ -n "$$(find harness/scenarios -name '*_test.go' 2>/dev/null)" ]; then \
		$(GO) test -tags=e2e ./harness/...; \
	else echo "harness not built yet — #40"; exit 1; fi

test-invariants: ## W1-W10 as executable acceptance tests
	@if [ -d harness/invariants ]; then $(GO) test -tags=invariants ./harness/invariants/...; \
	else echo "invariant suite not built yet — #43"; exit 1; fi

# ── Local stack ─────────────────────────────────────────────────────────────

substrate-up: ## Substrate only (Postgres, object store, DeDi, Inji, eSignet) — what P0 needs
	$(COMPOSE) --profile substrate up -d postgres objectstore dedi inji-certify inji-verify esignet esignet-mock-identity
	@echo "substrate starting; check with: $(COMPOSE) ps"

substrate-down: ## Stop the substrate
	$(COMPOSE) --profile substrate down

harness-up: ## Bring the whole stack up and leave it running for debugging
	$(COMPOSE) up -d --build
	@echo "stack up. services on 9001-9007, mocks on 9101-9102"

harness-down: ## Tear the stack down, including volumes
	$(COMPOSE) down -v

harness-logs: ## Logs for one service: make harness-logs SERVICE=registry
	@test -n "$(SERVICE)" || { echo "usage: make harness-logs SERVICE=<name>"; exit 1; }
	$(COMPOSE) logs -f $(SERVICE)

ps: ## What is running
	$(COMPOSE) ps

# ── Deploy ──────────────────────────────────────────────────────────────────

verify-deploy: todo ## Smoke a deployed env: make verify-deploy ENV=staging

clean: ## Remove build output
	$(GO) clean -cache -testcache
	rm -rf dist/
