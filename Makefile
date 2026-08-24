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
        harness-logs verify-deploy clean todo dedi-image dedi-keys spike-dedi certify-bind certify-issue printed-card offline-verify-sealed \
        spike-dedi-deployed spike-esignet verify-deployed verify-registry hooks generate generate-check \
        e2e-up e2e-run

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

# schemas/ is the source of truth; Go types are one projection of it. Two
# hand-maintained definitions of a primitive is two systems (docs/COMPONENTS.md).
generate: ## Regenerate Go types from schemas/
	@$(GO) run ./tools/codegen

generate-check: ## Fail if the generated types are behind schemas/ (CI)
	@$(GO) run ./tools/codegen -check

# ── Tests ───────────────────────────────────────────────────────────────────

test: test-unit test-contract ## Unit + contract (the fast pair, run constantly)

test-all: test test-e2e test-invariants ## Everything CI runs on a PR

test-unit: ## Pure functions: strength fn, schemas, state machines, money
	$(GO) test ./pkg/... ./services/... ./adapters/... ./tools/...

test-contract: ## Fixture-driven: adapters, OpenAPI shapes, credential shape
	$(GO) test ./tests/contract/...

# One command, from a clean checkout, no manual step (#40). It brings the stack
# up, waits on readiness rather than sleeping, runs the spine, and tears down —
# including volumes, so the next run starts from nothing and cannot pass on
# yesterday's rows.
test-e2e: ## Real services: CSV -> unit -> claim -> confirm -> issue -> verify
	@# Torn down at BOTH ends, and the leading one is not redundant. The trailing
	@# teardown only runs if the previous run reached it: an interrupted run, or
	@# one killed mid-suite, leaves its volumes behind and the next run inherits
	@# them. That inheritance is invisible until the two runs were configured
	@# differently — a stack pointed at a real DeDi node leaves publication rows
	@# saying `transparent: true`, and the next run on the Postgres fallback then
	@# fails an assertion that is entirely correct about a database that is
	@# entirely stale. It cost two "is this flaky?" investigations to find.
	@$(COMPOSE) down -v --remove-orphans >/dev/null 2>&1 || true
	$(COMPOSE) up -d --build --wait postgres objectstore mock-sms mock-rail $(SERVICES)
	@$(GO) test -tags=e2e -count=1 -timeout=10m ./harness/... ; \
		status=$$? ; \
		if [ $$status -ne 0 ]; then \
			echo "── logs from the failing run ──" ; \
			$(COMPOSE) logs --tail=80 $(SERVICES) objectstore ; \
		fi ; \
		$(COMPOSE) down -v --remove-orphans >/dev/null 2>&1 ; \
		exit $$status

e2e-up: ## Bring up just what the spine needs, and leave it running
	$(COMPOSE) up -d --build --wait postgres objectstore mock-sms mock-rail $(SERVICES)

e2e-run: ## Run the spine against an already-running stack (fast iteration)
	$(GO) test -tags=e2e -count=1 -timeout=10m ./harness/...

test-invariants: ## W1-W10 as executable acceptance tests
	@if [ -d harness/invariants ]; then $(GO) test -tags=invariants ./harness/invariants/...; \
	else echo "invariant suite not built yet — #43"; exit 1; fi

# ── Local stack ─────────────────────────────────────────────────────────────

substrate-up: ## Substrate only (Postgres, object store, DeDi, Inji, eSignet) — what P0 needs
	$(COMPOSE) --profile substrate up -d postgres objectstore dedi-postgres dedi \
		inji-certify inji-verify-service inji-verify-ui esignet esignet-mock-identity
	@echo "substrate starting; check with: $(COMPOSE) ps"

# DeDi-node publishes no image (P0 finding, #2), so the node is built from a
# checkout. DEDI_SRC points at it.
DEDI_SRC ?= ../DeDi-node

dedi-image: ## Build the DeDi node image from a checkout (DEDI_SRC=/path/to/DeDi-node)
	@test -f "$(DEDI_SRC)/Dockerfile" || { \
		echo "no Dockerfile at $(DEDI_SRC) — clone theflywheel/DeDi-node and pass DEDI_SRC=<path>"; exit 1; }
	docker build -t crest/dedi-node:local "$(DEDI_SRC)"

# The node's signing key and the publisher key are secrets. They are generated
# into infra/compose/dedi-keys/, which is gitignored — a signing key in the
# repository is a signing key on someone's laptop forever.
dedi-keys: ## Generate the DeDi node key and a CREST publisher key
	@test -d "$(DEDI_SRC)" || { echo "set DEDI_SRC=/path/to/DeDi-node"; exit 1; }
	@mkdir -p infra/compose/dedi-keys
	@cd $(DEDI_SRC) && go build -o "$(CURDIR)/infra/compose/dedi-keys/dedid" ./cmd/dedid
	@cd infra/compose/dedi-keys && ./dedid keygen -out dedid.key && \
		./dedid pubkeygen -out publisher.key -kid crest -namespace crest.local
	@echo
	@echo "Put the printed DEDI_PUBLISHER_KEYS line and the verifier key into infra/compose/.env"

# ── Deployed environment ────────────────────────────────────────────────────
# The Railway production stack lives in the DeDi project so it can reach the
# shared Postgres over private networking. See docs/DEPLOYMENT.md.
CREST_DEDI_URL ?= https://crest-dedi-production.up.railway.app
CREST_REGISTRY_URL ?= https://crest-registry-production.up.railway.app
# The UI is eSignet's public hostname; the API service alone serves none of the
# URLs its own discovery document advertises. See p0-findings C12.
CREST_ESIGNET_URL ?= https://crest-esignet-ui-production.up.railway.app
CREST_MOCK_IDENTITY_URL ?= https://crest-mock-identity-production.up.railway.app

verify-deployed: ## Check the deployed stack answers, and verify its log independently
	@echo "── crest-registry"
	@curl -fsS $(CREST_REGISTRY_URL)/healthz && echo
	@echo "── crest-dedi"
	@curl -fsS $(CREST_DEDI_URL)/healthz && echo
	@echo "── work definition WD-4471, inclusion proof checked by our own verifier"
	@curl -fsS "$(CREST_DEDI_URL)/dedi/lookup/crest/work-definitions/WD-4471?proof=inclusion" \
		| go run ./tools/spikes/dediproof \
			-key "$$(./tools/spikes/dedi-verifier-key.py $(CREST_DEDI_URL))"
	@echo "── crest-esignet: discovery answers, and its issuer is where we fetched it"
	@curl -fsS $(CREST_ESIGNET_URL)/v1/esignet/oidc/.well-known/openid-configuration \
		| python3 -c "import json,sys; d=json.load(sys.stdin); \
		  iss=d['issuer']; want='$(CREST_ESIGNET_URL)'; \
		  sys.exit(0) if iss==want else sys.exit('issuer is %s, expected %s' % (iss, want))"
	@echo "issuer ok"

# One record, fetched from the deployed node and checked by our own verifier.
# The point is not that DeDi answered — it is that a second implementation,
# written from the wire format, agrees the record is in the log. Asking DeDi to
# check its own proof would only establish that DeDi agrees with itself.
verify-registry: ## Resolve a published CREST fact on the deployed node and validate its proof (#20, #21)
	@test -n "$(RECORD)" || { \
		echo "usage: make verify-registry REGISTRY=work-definitions RECORD=<id>"; \
		echo "  the record id is the 'record' field of GET /v1/definitions/<id>/publication"; \
		echo "  or of GET /v1/publications/organisation/<id> on the registry service"; exit 1; }
	@curl -fsS "$(CREST_DEDI_URL)/dedi/lookup/$(DEDI_NAMESPACE)/$(REGISTRY)/$(RECORD)?proof=inclusion" \
		| tee /dev/stderr \
		| go run ./tools/spikes/dediproof \
			-key "$$(./tools/spikes/dedi-verifier-key.py $(CREST_DEDI_URL))"

DEDI_NAMESPACE ?= crest
REGISTRY ?= work-definitions

spike-esignet: ## Run the identity spike: is the subject pairwise and stable? (#3)
	ESIGNET=$(CREST_ESIGNET_URL) MOCK_IDENTITY=$(CREST_MOCK_IDENTITY_URL) \
		python3 ./tools/spikes/esignet-pairwise.py

spike-dedi-deployed: ## Run the registry spike against the DEPLOYED node (#2)
	DEDI_URL=$(CREST_DEDI_URL) \
	DEDI_CLI=$(CURDIR)/infra/compose/dedi-keys/dedid \
	DEDI_KEY_FILE=$(CURDIR)/infra/compose/dedi-keys/crest-publisher.key \
	DEDI_KID=crest DEDI_NAMESPACE=crest \
	./tools/spikes/dedi-spike.sh

hooks: ## Install the git hooks (lefthook)
	pnpm install
	npx lefthook install

spike-dedi: ## Run the P0 registry spike against a running node (#2)
	DEDI_CLI=$(CURDIR)/infra/compose/dedi-keys/dedid \
	DEDI_KEY_FILE=$(CURDIR)/infra/compose/dedi-keys/publisher.key \
	DEDI_KID=crest \
	./tools/spikes/dedi-spike.sh

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

# ── Inji Certify (#1) ───────────────────────────────────────────────────────
# The work-event fixture is keyed by the pairwise subject eSignet mints for this
# deployment, so it cannot be baked into the image. This resolves the subjects
# and sets the file as a Railway variable, which redeploys Certify — and the
# redeploy is not incidental: the CSV plugin reads the file once at startup, so
# a bind without a restart looks like it did nothing.
certify-bind: ## Key the work-event fixture by this deployment's pairwise subjects (#1)
	@ESIGNET=$(CERTIFY_ESIGNET) MOCK_IDENTITY=$(CERTIFY_MOCK_IDENTITY) \
		python3 tools/certify/bind-subject.py | base64 | tr -d '\n' > $(CURDIR)/.work_events.b64
	@railway variables --service crest-certify \
		--set "CERTIFY_WORK_EVENTS_B64=$$(cat $(CURDIR)/.work_events.b64)" >/dev/null
	@rm -f $(CURDIR)/.work_events.b64
	@echo "bound; setting the variable redeploys Certify, which is the restart the CSV needs"

printed-card: ## Issue, print a PixelPass card, and verify it with no network (#66)
	@mkdir -p tools/spikes/card
	@CREDENTIAL_OUT=tools/spikes/card/credential.json CERTIFY=$(CERTIFY_URL) \
		ESIGNET=$(CERTIFY_ESIGNET) MOCK_IDENTITY=$(CERTIFY_MOCK_IDENTITY) \
		python3 tools/spikes/certify-issue.py >/dev/null
	@curl -fsS $(CERTIFY_URL)/v1/certify/.well-known/did.json -o tools/spikes/card/issuer-did.json
	@cd tools/spikes/printedcard && npm install --silent
	@node tools/spikes/printedcard/card.mjs tools/spikes/card/credential.json tools/spikes/card
	@$(GO) run ./tools/spikes/offlineverify tools/spikes/card/decoded.json tools/spikes/card/issuer-did.json
	@echo "card: tools/spikes/card/card.html — open and print it, then scan it with the radios off"

offline-verify-sealed: ## The above's last step in a container with no network at all (#66)
	@CGO_ENABLED=0 GOOS=linux $(GO) build -o tools/spikes/card/offlineverify ./tools/spikes/offlineverify
	docker run --rm --network none -v "$(PWD)/tools/spikes/card":/c -w /c alpine:3.20 \
		./offlineverify decoded.json issuer-did.json

certify-issue: ## Issue a WorkEventCredential over OpenID4VCI and verify it (#1)
	@CERTIFY=$(CERTIFY_URL) ESIGNET=$(CERTIFY_ESIGNET) MOCK_IDENTITY=$(CERTIFY_MOCK_IDENTITY) \
		python3 tools/spikes/certify-issue.py

# Deployed by default. #1 is about whether the substrate works where it is
# actually going to run, and a laptop-only proof of that is not one.
CERTIFY_URL ?= https://crest-certify-production.up.railway.app
# eSignet's public hostname is its UI, which fronts the API. The API service's
# own domain serves none of the URLs eSignet advertises. See p0-findings C12.
CERTIFY_ESIGNET ?= https://crest-esignet-ui-production.up.railway.app
CERTIFY_MOCK_IDENTITY ?= https://crest-mock-identity-production.up.railway.app
