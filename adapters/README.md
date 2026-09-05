# Building a CREST connector

A connector translates one source-system payload into CREST's canonical work-evidence records. The source system does not need to adopt CREST fields or change its API.

Use the built-in CSV connector and field mapping when a system can export a file. Write a connector when the system has a stable format that needs its own parser, such as a DHIS2 event bundle or a CommCare form response.

## The contract

Implement `adapters.Adapter`:

```go
package example

import (
    "encoding/json"
    "io"
    "time"

    "github.com/theflywheel/crest/adapters"
)

const Version = "example-api@1"

type Adapter struct{}

func (Adapter) Ref() string { return Version }

func (Adapter) Parse(
    input io.Reader,
    source adapters.Source,
    receivedAt time.Time,
) ([]adapters.Row, []adapters.Rejection, error) {
    var payload exampleResponse
    if err := json.NewDecoder(input).Decode(&payload); err != nil {
        return nil, nil, err // the whole payload could not be read
    }

    rows := make([]adapters.Row, 0, len(payload.Events))
    rejected := make([]adapters.Rejection, 0)
    for _, event := range payload.Events {
        row, err := translate(event, source, receivedAt)
        if err != nil {
            rejected = append(rejected, adapters.Rejection{
                Ref: event.ID, Reason: err.Error(),
            })
            continue
        }
        rows = append(rows, adapters.Row{Ref: event.ID, Record: row})
    }
    return rows, rejected, nil
}
```

The complete interface and source configuration types are in [`adapter.go`](adapter.go). A connector must:

- return a stable, versioned `name@version` reference;
- account for every source row as either an emitted row or a rejection;
- give every row and rejection an actionable source reference;
- emit records satisfying `canonical-work-evidence-record.schema.json`;
- set provenance from the supplied `adapters.Source` and `receivedAt`, never from trust claims in the source payload;
- keep unrecognised useful source fields in `Enrichment`;
- behave deterministically for the same payload and configuration;
- leave worker matching, deduplication, definition validation and storage to the evidence service.

Increment the connector version whenever a translation change could alter a canonical record. Existing evidence retains the old reference, so old and new translators may be registered together during migration.

## Register it

Add the package and one entry to [`builtin/registry.go`](builtin/registry.go):

```go
adapters.Connector{
    Adapter: example.Adapter{},
    Class: "example-api",
    Name: "Example source API",
    ContentTypes: []string{"application/json"},
    Description: "Example event API responses",
},
```

Registration makes the connector selectable by the evidence service and visible in `GET /v1/adaptors`. Duplicate, unversioned or incomplete registrations stop the service during startup.

Register a source using the same exact `adapterRef`, then name it when submitting evidence:

```text
POST /v1/batches?adapterRef=example-api%401&contextId=...&definitionId=...&submittedBy=...&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=push-api&systemRef=example-production
Content-Type: application/json
```

Requests that omit `adapterRef` continue to use `csv-batch@1` for compatibility. New integrations should always send it explicitly.

## Prove conformance

Every connector package should invoke the reusable test suite with representative fixtures:

```go
func TestConnectorContract(t *testing.T) {
    contract.Run(t, contract.Suite{
        Adapter: example.Adapter{},
        Source: configuredSource,
        ReceivedAt: fixedTime,
        Cases: []contract.Case{
            {
                Name: "one valid and one invalid event",
                Payload: fixture,
                WantRows: 1,
                WantRejections: 1,
            },
        },
    })
}
```

The suite checks schema validity, provenance ownership, deterministic output, row accounting and rejection quality. Add connector-specific tests for mappings, pagination, source quirks and attempts by a payload to assert its own trust level.

Run:

```sh
go test ./adapters/...
go test ./services/core/evidence/...
```

## Transport and secrets

An adapter translates a bounded payload; it does not schedule network calls or own credentials. A source-side job may pull an API, handle pagination and post each resulting payload to `/v1/batches`. A source system may instead push directly. Keeping transport outside the parser makes retries observable and lets the same connector handle push, scheduled pull and supervised upload.

Do not place API keys in source mappings, evidence payloads or connector metadata. Supply them to the source-side transport through the deployment's secret manager. The evidence endpoint still authenticates the submitter and checks their permission in the target context.
