# OpenAPI — the service boundaries

One file per service, describing the HTTP surface each one actually exposes:

| File | Service | Endpoints (excluding health) |
|---|---|---|
| `registry.yaml` | registry | 10 |
| `definitions.yaml` | definitions | 7 |
| `evidence.yaml` | evidence | 7 |
| `confirmation.yaml` | confirmation | 10 |
| `payments.yaml` | payments | 4 |
| `verification.yaml` | verification | 5 |
| `notify.yaml` | notify | 2 |

Every service also carries `GET /healthz` and `GET /readyz`, added by
`pkg/httpx.Server` rather than by each service's `routes.go`. They are described
once per file as a shared path item.

`/internal/clock` is described too, and it is the one path that is not always
there: it exists only when `CLOCK_DRIVEABLE` is set, which `pkg/service` refuses
in production. It is documented rather than hidden because a reader who finds it
in a running stack should be able to learn what it is and why it must not be in
a live deployment.

## They describe the boundary, not the data

Request and response bodies that are CREST primitives are referenced by the `$id`
of their JSON Schema — `urn:crest:schema:primitives:claim:1` and so on — and never
restated inline. `schemas/` is the source of truth; a second hand-maintained copy
of a primitive is precisely what `schemas/README.md` exists to prevent, and it
would agree with the original right up until it didn't. Each file says so in its
`info.description`.

What is written out inline is only what has no schema of its own: the shared error
body from `pkg/httpx` (`{code, message, problems[]}`), query parameters, and the
ad-hoc envelopes handlers build with `map[string]any` — verdicts, holds, windows,
sweep results. Those exist in Go and nowhere else, so there is nothing to point at.

## Status codes are the content

The point of these files is not the path list; `grep 'mux.HandleFunc'` gives that
in a second. It is the codes that carry meaning and the reason each one is the
answer it is:

- `GET /v1/resolve` — 200 one match, 404 no match (the row goes to the unclear
  queue), **409 more than one candidate**: a hold is recorded and nothing is
  merged. The 409 is a designed outcome, not an error (W7).
- `POST /v1/definitions/{id}/versions/{v}/ratify` — 409 when the ratifier is the
  author. Separation of duties is an L1 rule (§7).
- `POST /v1/instructions` — **201 for a held payment too**, carrying the reason
  and its owner. A hold is not an error; returning one would leave the hold
  unrecorded and the caller retrying forever (W10).
- `POST /v1/claims/{id}/dispute` — releases payment like every other exit (W4).
- `POST /v1/verify` — an invalid credential is a 200 with `valid: false`. The
  question was answered; the answer was no.

## Hand-written, and that is the gap

These were written by reading the seven `routes.go` files. Nothing generates them
and nothing checks them against the handlers, so they drift the moment somebody
adds a route without touching this directory.

**The direction worth generating is spec-from-code**: derive the OpenAPI from the
Go handlers rather than the handlers from the OpenAPI. Two reasons. The handlers
already exist and are the thing that decides what a caller gets — a generated
server would mean rewriting working services to satisfy a document. And the parts
of these files that matter — that a 409 on resolve is a recorded hold, that a
held payment is a 201 — live in the handler's control flow and its comments,
which is where a generator can see them; a spec-first flow would put the reasoning
in a document the handler is free to ignore.

The cheaper intermediate step, and probably the next one: a test that walks each
service's `http.ServeMux` and fails when a registered pattern has no matching
`paths` entry here, and vice versa. That catches drift in the path list without
committing to generating the descriptions, which are the half a generator would
do worst.

## Validating

Valid YAML, which is the check that runs today:

```
python3 -c "import yaml,glob; [yaml.safe_load(open(f)) for f in glob.glob('schemas/openapi/*.yaml')]"
```

No OpenAPI validator is installed in this repo or on the build image, so nothing
currently checks these against the 3.1 metaschema. If one is added, the
conventional choices are `redocly lint schemas/openapi/*.yaml` or
`spectral lint schemas/openapi/*.yaml`; both should be wired to a `make` target
alongside the YAML check rather than run by hand.

Note that a strict validator will want to resolve the `urn:crest:schema:…` refs.
They are resolvable only against the schemas in `schemas/`, which are keyed by
`$id` and not by file path, so any linting setup needs those registered — the
same registry `pkg/schema` builds at start-up.
