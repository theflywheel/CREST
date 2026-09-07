# The clean-slate run — production, from an empty registry, eSignet only

Recorded 2026-09-05 against the Railway demo fleet after the wipe in
`docs/DEPLOYMENT.md` ("Standing up from nothing"). Nothing was seeded. Every
actor below arrived through a door with their own eSignet login (the
production mock identity system; PIN `112233` for all), and every record the
later journeys read was written by the earlier ones on screen. Files come as
`.webm` (as recorded), `.mp4`, and `-8x.mp4` (eight times speed).

| # | Recording (prefixed with its journey) | Journey | Who (UIN) | What happens |
|---|---|---|---|---|
| 1 | `J1-g1-operator-claims-and-stands-up` | G-1 | CREST demo operator (3312345670) | Opens the claim link `tools/bootstrap-operator` printed, signs in with eSignet, becomes the Instance Operator; walks the instance screens. |
| 2 | `J1-g1-operator-publishes-terms` | G-1 | operator | Publishes "Standard delivery" — the terms an organisation can be admitted on. Nothing can be admitted before this. |
| 3 | `J1-g2-organisation-registers-and-claims` | G-2 | Peter Otieno (3312345674) | Registers Riverside Community Health at the open door, accepts terms, submits checks, and claims the organisation with his own login: Org Admin. |
| 4 | `J1-g4-operator-admits-the-organisation` | G-4 | operator | The queue and the decision (approved on terms acceptance under this deployment's model). |
| 5 | `J3-j3-org-admin-sets-up-the-project-and-invites` | J3 | Peter | Creates the project naming Alice its configurator; invites Amina, Ndegwa, Naomi, Joseph, Nadia, Daniel — a record, a role where one is a grant, a one-time link each. |
| 6 | `J3-claim-*` (7) | — | each invitee | Each claims their record with their own eSignet login; the console derives the role from the grant: Configurator, Author, Approver, Custodian; Naomi, Nadia and Daniel arrive as Org Admin until their records exist. |
| 7 | `J4-p3-author-writes-and-submits-the-definition` | P-3 | Amina Yusuf (3312345676) | The whole wizard, proven dry, submitted; pricing handed to Nadia. |
| 8 | `J4-p3-approver-ratifies-with-the-gaps-named` | P-3 | Prof. Ndegwa (3312345677) | Signs and publishes with a pending field named. |
| 9 | `J5-f1-org-assigns-the-rate-owner` | F-1 | Peter | Assigns Nadia as rate owner. |
| 10 | `J5-f1-rate-owner-publishes-the-rate` | F-1 | Nadia Okoth (3312345672) | Signs in — now Rate Owner — and publishes the rate as terms. |
| 11 | `J5-f2-org-stands-up-the-mechanism-under-its-owner` | F-2 | Peter | Chooses the posture and rails; creates the mechanism naming Daniel its owner. |
| 12 | `J5-f2-mechanism-owner-connects-tests-and-activates` | F-2 | Daniel Mwangi (3312345673) | Signs in — now Payment Mechanism Owner — connects, sends the test payment, agrees reconciliation and statement, records batching, activates. |
| 13 | `J6-j6-agent-registers-a-worker` | J6 | Naomi Achieng (3312345678) | Enrolment door, eSignet: registers Halima Noor and records her consent. |
| 14 | `J7-j7-worker-registers-herself` | J7 | Grace Wanjiru (3312345671) | Worker door, from the project's `#/join/<context>` link: consent first, then her own record. |
| 15 | `J6-j6-agent-closes-the-roster` | J6 | Naomi | Evidence intake against the ratified definition: three rows, two for Grace by phone, one for Halima by roster id. |
| 16 | `J7-j7-worker-confirms-her-record` | J7 | Grace | Sees the records waiting for her say; confirms one; the window closes and payment is released. |
| 17 | `J8-j8-assisted-confirmation-and-handoff` | J8 | Naomi | The confirmer's worklist and the handoff chain. |
| 18 | `J8-j8-verifier-panels` | P-10 | nobody | The verifier door's panels, no account. |
| 19 | `J9-j9-project-dashboards` | J9 | Dr. Alice Mutua (3312345675) | Work status, straight-through, quality, payments, proof, reports — over the records above. |
| 20 | `J11-j11-registry-dashboards` | J11 | Joseph Kariuki (3312345679) | Coverage, quality, duplicates, reuse, unclear rows. |

Not recorded: J10, the funding oversight viewer (Elena Marsh, 3312345680).
No record derives that persona — there is no funder-viewer grant or
ownership fact — so on a clean deployment she would arrive as Org Admin. That
is a gap to design, not to paper over.

The scripts that drove these are in the session's scratch directory
(`tmp/prod/*.mjs`, `run-all.sh`, `final-run.sh`); they sign in through the
real eSignet PIN form with Playwright and record with its video recorder.
