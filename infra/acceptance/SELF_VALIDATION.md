# Validate the development lifecycle

Use the running `crest-acceptance` deployment. The older `crest` deployment uses
different ports and contains a different history. These instructions create no
business data automatically and do not invoke the repository's seed tools.

Watch the [recorded local demonstration](../../docs/assets/crest-development-validation/crest-development-validation.mp4)
for encrypted-wallet recovery, offline signature verification and the real
eSignet/Inji Web guest download. The downloaded PDF's QR was independently checked
with the Inji verification API and returned `SUCCESS`. The recording does not show
new CSV intake, payment settlement, or acceptance of the subsequent review fixes.

## Open the applications

| Application | Address |
| --- | --- |
| Console | http://localhost:59510/console/ |
| CSV upload | http://localhost:59510/console/#/intake/file |
| Worker wallet | http://localhost:59510/worker/#/wallet |
| Worker review | Follow the claim's notification link |
| CREST verifier | http://localhost:59510/verify/ |
| Development notification inbox | http://localhost:59511/inbox |
| Inji Web | http://localhost:58093 |

Use separate browser profiles for operator, author, reviewer and worker. Sign in
through eSignet with the local development identities in the ignored files
`infra/acceptance/private/development-{operator,author,reviewer,worker}.json`.
Read those files locally; do not paste their PINs or session tokens into an issue.
The notification inbox requires the configured notification-provider token.

The existing journey's project, definition and source identifiers are recorded
locally in `infra/acceptance/private/csv-journey.json`. Select that project and its
active definition in the console before uploading. Alternatively, create a new
project through the normal authorized setup flow and obtain separate definition
approval. First-run instance setup is already complete and must reject replay.

## Exercise the boundaries

1. Inspect the published definition, its schema, source restrictions and required
   fields. Confirm a separate reviewer approved it. A worker without the relevant
   grant must not be able to register a source or publish a definition.
2. Upload a CSV through the console using a registered source. Match its activity,
   outcome unit and required fields to the approved definition. Use a new source
   record reference for new work. Check the returned claim IDs and quarantine
   reasons; uploading the identical file again must create no duplicate claims.
3. Open the actual notification. Viewing the inbox is not acknowledgement.
   Acknowledge while signed in as the intended worker; a different actor must be
   refused. Follow the configured review process. The current development
   automatic window is two minutes and uses elapsed wall time.
4. Check the resulting credential online. Its evidence-field names should include
   the canonical fields present at intake, including required fields used to
   determine strength. Identifier values must not be copied into that field list.
5. Save/export the encrypted worker wallet before completing custody transfer.
   Reload offline, unlock it with its passphrase and verify the saved credential.
   An offline signature check must disclose that withdrawal status was not checked.
   Test an encrypted backup import in a separate offline browser profile too.
6. Inspect payment status. Missing configuration must produce an explicit hold.
   An authorized rate publication and retry may move the instruction to pending;
   only explicit simulator settlement moves it to confirmed. Check reconciliation
   and repeat settlement to verify that no second compensation appears.
7. In Inji Web, choose the guest flow, CREST, Work Event and eSignet PIN login.
   Download the PDF and verify its embedded QR using Inji Verify. Changing signed
   work-event content must invalidate the signature.

## Payment API operations

These controls currently require API calls; the console exposes rate publication
and payment reads. Send an actual logged-in payment operator's bearer token in
`Authorization` and use the identifiers and amounts from the selected instruction.
The payment service is `http://localhost:59406`.

Retry after correcting a missing rate:

```http
POST /v1/instructions/{instructionId}/retry?contextId={projectId}
Authorization: Bearer <current-session-token>
```

Settle an existing pending development simulator instruction:

```http
POST /v1/providers/simulator/settle
Authorization: Bearer <current-session-token>
Content-Type: application/json

{
  "contextId": "<projectId>",
  "idempotencyKey": "<instructionId>",
  "reference": "<stable-settlement-reference>",
  "amountMinor": 100,
  "currency": "KES"
}
```

The amount and currency above are an example, not a rate to assume for other
instructions. Repeat the identical request to check replay behavior. A wrong
amount, currency or project must be refused. This simulator moves no real money.

## What this does not establish

The development stack uses official eSignet with its mock identity provider,
CSV evidence, HTTP notifications and simulated payments. Production provider
acceptance is separate. Core revocation propagation to Certify, historical and
post-custody companion retrieval, companion recovery, and offline status freshness
remain separate implementation/acceptance gates. The guest PDF flow does not
establish mobile or account-based cloud-wallet acceptance.

Existing credentials are signed immutable records. Fixing issuance does not
retroactively change their evidence-field lists: validate that regression using
newly submitted work after the rebuilt services are running.
