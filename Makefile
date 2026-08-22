# CREST — the command surface every skill, hook and CI job builds against.
#
# These targets are the contract. Their implementations arrive with the services
# (P2, #22-#25); until then each one fails loudly with what it will do, rather
# than succeeding vacuously. A green target that ran nothing is worse than a red
# one, because it launders absence as proof.

.PHONY: help test test-all test-unit test-contract test-e2e test-invariants \
        harness-up harness-down harness-logs verify-deploy fmt lint todo

help:
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t28

todo:
	@echo "Not implemented yet — arrives with the services in P2."
	@echo "Tracked in the harness issue; see docs/TESTING.md for the contract it must meet."
	@exit 1

test: test-unit test-contract  ## Unit + contract (the fast pair, run constantly)

test-all: test test-e2e test-invariants  ## Everything CI runs on a PR

test-unit: todo          ## Pure functions: strength fn, schemas, state machines, money
test-contract: todo      ## Fixture-driven: adapters, OpenAPI shapes, credential shape
test-e2e: todo           ## Real services: CSV -> unit -> claim -> confirm -> issue -> verify
test-invariants: todo    ## W1-W10 as executable acceptance tests

harness-up: todo         ## Bring the stack up and leave it running for debugging
harness-down: todo       ## Tear the stack down
harness-logs: todo       ## Logs for one service: make harness-logs SERVICE=registry

verify-deploy: todo      ## Smoke a deployed env: make verify-deploy ENV=staging

fmt: todo                ## Format
lint: todo               ## Lint
