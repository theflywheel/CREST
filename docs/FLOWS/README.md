---
title: Flows
---

# Flows

Every flow the system carries, in the order a deployment meets them. Each document has the sequence diagram and links to the recording that shows it running.

The recordings are the clean-slate run of 5 September 2026 against the production fleet: one deployment stood up from nothing and walked to a paid worker, every person signing in through eSignet. Most are under 40 seconds; the long wizards are cut to eight-times speed. The journey numbers are the blueprint's twelve (§15); the persona codes are the Actor Journeys' (G-1, P-3, W-1 …).

| # | Flow | Journeys | Document |
|---|---|---|---|
| 1 | Standing the instance up | J2 · G-1 | [INSTANCE_SETUP.md](INSTANCE_SETUP.md) |
| 2 | An organisation joins | J1 · G-2, G-4 | [ORGANISATION_ONBOARDING.md](ORGANISATION_ONBOARDING.md) |
| 3 | A project is set up and its people invited | J3 · P-1 | [PROJECT_SETUP.md](PROJECT_SETUP.md) |
| 4 | A definition is written, proven dry, and ratified | J4 · P-3 | [DEFINITION_LIFECYCLE.md](DEFINITION_LIFECYCLE.md) |
| 5 | Money is set up: a rate as terms, a mechanism under its owner | J5 · F-1, F-2 | [PAYMENT_SETUP.md](PAYMENT_SETUP.md) |
| 6 | A worker is enrolled, by an agent or by herself | J6 · W-2, J7 · W-1 | [ENROLMENT.md](ENROLMENT.md) |
| 7 | Evidence arrives and becomes claims | J6 · W-4 | [EVIDENCE_INGESTION.md](EVIDENCE_INGESTION.md) |
| 8 | The confirmation window and its four exits | J7 · W-1, J8 · W-4 | [CONFIRMATION_WINDOW.md](CONFIRMATION_WINDOW.md) |
| 9 | The credential, the wallet, and verification | J9 · V-1, V-2 | [CREDENTIALS_AND_VERIFICATION.md](CREDENTIALS_AND_VERIFICATION.md) |
| 10 | Consent per share | W-1, V-2 | [CONSENT_PER_SHARE.md](CONSENT_PER_SHARE.md) |
| 11 | Duplicates hold, and recovery of a lost login | G-4, W-5 | [DUPLICATES_AND_RECOVERY.md](DUPLICATES_AND_RECOVERY.md) |
| 12 | Reading it all back | J9, J10, J11 | [DASHBOARDS.md](DASHBOARDS.md) |

## The recordings

| # | Recording | Journey | Who | Length |
|---|---|---|---|---|
| 1 | [operator claims and stands up](../assets/clean-slate-watch/J1-g1-operator-claims-and-stands-up.mp4) | G-1 | the operator | 39 s |
| 2 | [operator publishes terms](../assets/clean-slate-watch/J1-g1-operator-publishes-terms.mp4) | G-1 | the operator | 22 s |
| 3 | [organisation registers and claims](../assets/clean-slate-watch/J1-g2-organisation-registers-and-claims.mp4) | G-2 | Peter Otieno | 41 s |
| 4 | [operator admits the organisation](../assets/clean-slate-watch/J1-g4-operator-admits-the-organisation.mp4) | G-4 | the operator | 16 s |
| 5 | [org admin sets up the project and invites](../assets/clean-slate-watch/J3-j3-org-admin-sets-up-the-project-and-invites-8x.mp4) | J3 | Peter | 11 s at 8× |
| 6–12 | claims by [Alice](../assets/clean-slate-watch/J3-claim-alice.mp4), [Amina](../assets/clean-slate-watch/J3-claim-amina.mp4), [Ndegwa](../assets/clean-slate-watch/J3-claim-ndegwa.mp4), [Joseph](../assets/clean-slate-watch/J3-claim-joseph.mp4), [Naomi](../assets/clean-slate-watch/J3-claim-naomi.mp4), [Nadia](../assets/clean-slate-watch/J3-claim-nadia.mp4), [Daniel](../assets/clean-slate-watch/J3-claim-daniel.mp4) | J3 | each invitee | 16–33 s |
| 13 | [author writes and submits the definition](../assets/clean-slate-watch/J4-p3-author-writes-and-submits-the-definition-8x.mp4) | P-3 | Amina Yusuf | 14 s at 8× |
| 14 | [approver ratifies with the gaps named](../assets/clean-slate-watch/J4-p3-approver-ratifies-with-the-gaps-named.mp4) | P-3 | Prof. Ndegwa | 22 s |
| 15 | [org assigns the rate owner](../assets/clean-slate-watch/J5-f1-org-assigns-the-rate-owner.mp4) | F-1 | Peter | 23 s |
| 16 | [rate owner publishes the rate](../assets/clean-slate-watch/J5-f1-rate-owner-publishes-the-rate.mp4) | F-1 | Nadia Okoth | 29 s |
| 17 | [org stands up the mechanism under its owner](../assets/clean-slate-watch/J5-f2-org-stands-up-the-mechanism-under-its-owner.mp4) | F-2 | Peter | 22 s |
| 18 | [mechanism owner connects, tests and activates](../assets/clean-slate-watch/J5-f2-mechanism-owner-connects-tests-and-activates.mp4) | F-2 | Daniel Mwangi | 77 s |
| 19 | [agent registers a worker](../assets/clean-slate-watch/J6-j6-agent-registers-a-worker.mp4) | J6 | Naomi Achieng | 24 s |
| 20 | [worker registers herself](../assets/clean-slate-watch/J7-j7-worker-registers-herself.mp4) | J7 | Grace Wanjiru | 34 s |
| 21 | [agent closes the roster](../assets/clean-slate-watch/J6-j6-agent-closes-the-roster.mp4) | J6 | Naomi | 26 s |
| 22 | [worker confirms her record](../assets/clean-slate-watch/J7-j7-worker-confirms-her-record.mp4) | J7 | Grace | 39 s |
| 23 | [assisted confirmation and handoff](../assets/clean-slate-watch/J8-j8-assisted-confirmation-and-handoff.mp4) | J8 | Naomi | 17 s |
| 24 | [verifier panels](../assets/clean-slate-watch/J8-j8-verifier-panels.mp4) | P-10 | nobody | 24 s |
| 25 | [project dashboards](../assets/clean-slate-watch/J9-j9-project-dashboards.mp4) | J9 | Dr. Alice Mutua | 34 s |
| 26 | [registry dashboards](../assets/clean-slate-watch/J11-j11-registry-dashboards.mp4) | J11 | Joseph Kariuki | 31 s |
| 27 | [the wallet: encrypted restore, offline verification, Inji guest download](../assets/crest-development-validation/crest-development-validation.mp4) | W-1, V-1 | Grace | 2 m 17 s |

Not recorded: J10, the funding oversight viewer, because no record derives that persona on a clean deployment. How the run was made is in the recordings' own [README](../assets/clean-slate-watch/README.md).

## How these are proven

Every flow is driven end to end in CI: the spine scenarios in `harness/scenarios` (`make test-e2e`), the journey walk in `tests/e2e-apps` (`make e2e-apps`), and the fidelity gate against the design reference (`make fidelity`). [../TESTING.md](../TESTING.md) explains the layers; [../test-manifest.md](../test-manifest.md) names the check behind each feature.
