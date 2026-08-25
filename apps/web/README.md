# apps/web — the Actor Journeys, as a web product

One static app, six faces, each built from its journey in
`docs/reference/CREST — Actor Journeys_17Aug.html` and backed only by
endpoints that exist. Where the document promises a screen the backend cannot
serve yet, the face says so and names the issue.

```sh
make web-up          # stack + web app + fixture world
open http://localhost:59100
```

No build step: plain ES modules, served by nginx from the `web` compose
service. The services admit the app's origin through `CREST_CORS_ORIGINS`
(never a wildcard), and login mints a token from the dev stack's own mock
OIDC issuer, then binds it through the real first-login path — self-proof by
token possession (#102). A real deployment replaces `loginAs` in `api.js`
with an eSignet redirect; everything after the token is identical.

| Face | Journey | Backed by |
|---|---|---|
| Worker | W-1 "The wallet, not a dashboard" | windows, claims, credentials + card, instructions with held reasons, presentations trail, consents + withdraw |
| Supervisor (Attestor) | W-4, P-2 | CSV batches, unreached workers + assisted confirmation, unclear queue |
| Registry Custodian | W-2, custodian console | five-key-ish resolve, duplicate holds (hold-never-merge), unclear attribution, recoveries, overdue-review queues |
| Project console | P-2 (thin) | funnel counts over real queues, payments with reasons-and-owners, sources + assessments. The metric contracts are #31 and the page says so |
| Verifier | V-1/V-2 | verify with the checkable/trusting chain, resolve-a-person (#104), bounded batch checks (#107) |
| Registering Agent | W-2 | field registration (either pathway), voice consent, opening a recovery |
