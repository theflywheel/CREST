---
title: Evidence adapters
---

# Evidence adapters

An adapter turns one source system's payload into canonical work-evidence records. One adapter per system class (a DHIS2 event bundle, a CommCare form response, an HCM export, a delimited file), configured per source. The source system does not adopt CREST fields or change its API.

## The interface

```go
type Adapter interface {
    // Ref identifies the adapter and its version, e.g. "csv-batch@1". It ends up
    // in every record's provenance, and the registry records which versions are
    // recognised, so a verifier can walk evidence back to a known translator.
    Ref() string

    // Parse reads one bounded payload. receivedAt is passed, not read, so a
    // harness running a week in milliseconds produces consistent timestamps.
    Parse(r io.Reader, src Source, receivedAt time.Time) ([]Row, []Rejection, error)
}
```

`Source` is the deployment's knowledge of the feed: the registered `adapterRef`, its `sourceClass`, `captureMethod`, `sourceExposure`, `systemRef`, and the per-source column mapping. A `Row` is a canonical record plus a reference that locates it in the original payload; a `Rejection` names the row and the reason.

## What an adapter must do

- Return a stable, versioned `name@version` reference.
- Account for every source row as an emitted row or a rejection, each with a reference a person can find in the file they sent.
- Emit records that satisfy `canonical-work-evidence-record.schema.json`.
- Take provenance from the supplied `Source` and `receivedAt`, never from trust claims in the payload.
- Keep unrecognised useful source fields in `Enrichment`.
- Be deterministic for the same payload and configuration.
- Leave worker matching, deduplication, definition validation and storage to the evidence service.

## Registration and selection

- **One registration point**: `adapters/builtin.Plugins()`. Both the evidence service and the definitions service (for the dry run) compose their registry from it. The registry validates the list at start-up; a duplicate, unversioned or mis-declared entry stops the service.
- **Selection is the source's**: a source is registered in a project with the exact `adapterRef` it will be parsed by (`POST /v1/sources`), and a batch submitted against that source's `systemRef` is parsed by that adapter. A batch request may name `adapterRef` only to confirm it; a mismatch is refused.
- **The definition decides which sources it admits** (`platform.sourceSystems`); a batch from a system the definition does not name is rejected at intake.
- **The catalogue** is public: `GET /v1/adapters` lists the registered versions and nothing else.

## Conformance

`adapters/contract.Run` is the suite every adapter runs in its own package test. It checks row accounting, schema validity, the whole provenance tuple (adapter ref matching both the adapter under test and the source's registration, class, capture, exposure, system reference, receipt time), actionable rejections, and determinism. The CSV adapter runs it over the clean and the malformed fixtures in `adapters/csv/contract_test.go`.

## Versioning

Bump the ref whenever a translation change could alter a canonical record. Old evidence keeps the ref it was parsed under; both versions may be registered during a migration, and a source is moved by re-registering it.

## Transport and secrets

An adapter translates a bounded payload; it does not schedule network calls or own credentials. A source-side job pulls an API, handles pagination and posts each resulting payload to `/v1/batches`; a source may push directly instead. Keeping transport outside the parser makes retries observable and lets one adapter serve push, scheduled pull and supervised upload. API keys never go in mappings, payloads or adapter metadata; the deployment's secret manager supplies them to the transport.

The worked example, with a complete adapter skeleton, is `adapters/README.md` (`adapters/README.md` in the repository).
