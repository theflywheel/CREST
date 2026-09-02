#!/usr/bin/env python3
"""Build docs/journey-traceability.json and .md from the Actor Journeys reference.

The reference (docs/reference/"CREST — Actor Journeys_17Aug.html") is the
design of record for the four React apps. Its 18 detailed role flows carry 143
`.role-step` screens; this script extracts every one (id, role, stage, title)
and merges the authored MAPPING below — which React route/component answers
each screen, with what fidelity, and on what API evidence.

Statuses (from docs/JOURNEY_GAP_ASSESSMENT.md):
  implemented            the screen's decisions and state transitions work in the UI
  compressed             the substance exists but is folded into a broader screen
  illustrative           a labelled mock renders; no real operation behind it
  semantically-different a real operation exists, but not the reference one
  missing                no corresponding user flow

Re-run after editing MAPPING or when the reference changes:
    python3 tools/journey-trace/build.py
The script fails if the reference's screen set and MAPPING diverge, so a
reference edit cannot silently orphan a row.
"""
import html
import json
import os
import re
import sys
from collections import Counter

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
REF = os.path.join(ROOT, "docs", "reference", "CREST — Actor Journeys_17Aug.html")
OUT_JSON = os.path.join(ROOT, "docs", "journey-traceability.json")
OUT_MD = os.path.join(ROOT, "docs", "journey-traceability.md")

ROLE_NAMES = {
    "G-1": "Instance Administrator",
    "G-2": "Onboarding Authorising Signatory",
    "G-4": "Worker Registry Custodian",
    "P-1": "Org Admin",
    "P-2": "Project Configurator",
    "P-3": "Work Definition Author",
    "P-10": "External Evidence Contact",
    "F-1": "Rate Owner",
    "F-2": "Payment Mechanism Owner",
    "W-1": "Worker",
    "W-2": "Registering Agent",
    "W-3": "Support Agent",
    "W-4": "Supervisor (Attestor)",
    "W-5": "Recovery Confirmer",
    "V-1": "Verifier",
    "V-2": "Institutional Verifier",
    "V-4": "Funding Oversight Viewer",
    "EXT": "External systems",
}

# Authored map: reference screen id -> (status, app, route, evidence/notes).
# route "" means no surface; evidence names the real API where status is
# implemented/compressed/semantically-different, and the honest gap otherwise.


def m(status, app="", route="", note=""):
    return {"status": status, "app": app, "route": route, "note": note}


MAPPING = {
    # ---- G-1 Instance Administrator (console, persona: instance) ----
    "g1_1": m("implemented", "console", "#/instance/setup", "Honest stand-up front door: deployment read live from GET /v1/instance; stand-up is deploy-time (compose/Railway); no wizard write faked"),
    "g1_2": m("implemented", "console", "#/instance/covers", "Read-only by design: published self-description live; the four unpublished reference fields named as deploy-time config, not invented"),
    "g1_3": m("implemented", "console", "#/instance/consent", "Consent floor stated as enforced infrastructure facts; scripts/templates are deployment config (#59); officer appointment recordless, said so"),
    "g1_5": m("compressed", "console", "#/instance/invite", "Reference's five fields, no Send: instance-level invitation has no primitive (#182 is project→org; entry decision open per g2_5) — design finding #185"),
    "g1_6": m("implemented", "console", "#/instance/services", "Real six-service /healthz sweep in the reference frame; 'Done — awaiting the organisation' walks to the queue"),
    "g4_1": m("implemented", "console", "#/admissions", "Real queue: GET /v1/registrations (new) + GET /v1/terms-requests; both reference callouts verbatim"),
    "g4_2": m("implemented", "console", "#/admissions/:pid", "Registration read with declared name/attributes; 'what this does not prove' callout verbatim"),
    "g4_3": m("implemented", "console", "#/admissions/:pid", "POST /v1/organisations/{id}/decision through the screen; decider is the authenticated caller (#89); reasonless rejection refused; 'issue the key' named unbuilt"),
    # ---- G-2 Onboarding Authorising Signatory (console, public onboarding) ----
    "g2_1": m("implemented", "console", "#/onboard", "The reference's six-field identity form (legal name, country, work email, contact person, kind, sector) as a desktop console frame — Registration · 1 of 4 with the Register/Terms/Certificates/Done rail; registration documents deliberately not asked, per the reference's own callout → POST /v1/organisations. Since #168 the whole form persists: country/kind/sector/contact person ride the Party's generic attributes map and are read back from GET /v1/organisations/{id}/registration — a registry round-trip, not browser state"),
    "g2_4": m("implemented", "console", "#/onboard/status", "GET /v1/organisations/{id}/registration: state, exact terms version, decider. Under REGISTRY_ORG_APPROVAL=manual (the local default) the terminal state waits on the operator's decision; on-terms-acceptance approves in the acceptance's transaction. 'On nobody's project' is true — no project membership exists"),
    "g2_5": m("implemented", "console", "#/onboard/standalone", "The org's real invitation inbox via GET /v1/organisations/{id}/invitations; an empty inbox is the true 'stands alone' answer, and either ordering works — offers are sendable before or after registration (#182)"),
    "g2_6": m("implemented", "console", "#/onboard/wider", "Held permissions vs other published terms sets, read live; POST /v1/organisations/{id}/terms-requests opens the request (#182)"),
    "g2_7": m("implemented", "console", "#/onboard/documents", "Declared {kind, ref, hash} references only via PUT .../documents then POST .../submit — no file input exists and the walk asserts its absence; document custody is a stated gap (#182)"),
    "g2_8": m("implemented", "console", "#/onboard/review", "The submitted request read whole — state, documents, checks; Withdraw works via POST .../withdraw and the walk proves withdraw-then-resubmit"),
    "g2_9": m("implemented", "console", "#/onboard/invited", "Decline requires a reason (422 without), Ask a question appends to the trail, Accept before APPROVED renders the 409 as 'not yet, not no' without expiring the offer"),
    "g2_10": m("implemented", "console", "#/onboard/project", "The accepted invitation and the partner grant its acceptance minted, read back — grantId, functions, real end date; 'Assign your people' carries the honest later-journey note"),
    "g2_11": m("implemented", "console", "#/onboard/terms", "GET /v1/terms lists published versions; acceptance names an exact version via POST /v1/organisations/{id}/terms-acceptance — 'never edited underneath you' is the backend's versioning rule"),
    "g2_12": m("implemented", "console", "#/onboard/checks", "Checks are recorded PASS/FAIL verdicts with a named owner (party or policy) from GET .../terms-requests/{id} — no fake automation, per the honest-checks call in #182"),
    "g2_13": m("implemented", "console", "#/onboard/status", "Approval recorded with decider (policy or person); publication to the registry log happens in the approving transaction"),
    # ---- G-4 Worker Registry Custodian (console, persona: custodian) ----
    "g4_4": m("missing", note="Coverage-by-place headline: no geography endpoint"),
    "g4_5": m("missing", note="Record-by-record quality worklist: not built (unclear rows are evidence-side, not registry quality)"),
    "g4_6": m("implemented", "console", "#/dupes", "GET /v1/holds + POST /v1/holds/{id}/resolve; merges_without_confirmation metric from GET /v1/holds/metrics; probable matches hold and never auto-merge"),
    "g4_7": m("missing", note="Registry reuse / return-on-shared-registry metric: no endpoint"),
    # ---- P-1 Org Admin (console, persona: orgadmin) ----
    "p1_1": m("compressed", "console", "#/org", "The reference's own frame — 'Welcome to <organisation>', the terms held with the version and decider, the custodian-sits-here card, the two actions — over live registry reads (GET /v1/parties/{id}, GET /v1/organisations/{id}/registration). Compressed, not implemented, for one measured reason stated on the frame itself: the projects count reads '—' because no project list is readable to this console door yet"),
    "p1_2": m("compressed", "console", "#/people", "The reference's frame at the rail's People & roles entry, over GET /v1/organisations/{id}/roles — the question GET /v1/authorizations cannot answer, because it keys on who GAVE the grant. Every holder lists with its functions, its grantor, its grant date and its state, revoked and expired included. Compressed rather than implemented: the invitation to a work email and the invited/active distinction that goes with it need a notification service, and the screen says so instead of drawing states it cannot read"),
    "p1_3": m("implemented", "console", "#/projects/new", "POST /v1/projects with the reference's three fields: the project is created DRAFT, coverage rides the opaque configuration object, and naming a Configurator leaves ownership PENDING because naming is a proposal (finding F2). Proven end to end by tests/e2e-apps/apps.spec.js 'the J3 handover is real': create, decline with a reason, re-hand, accept"),
    # ---- P-2 Project Configurator (console, persona: configurator) ----
    "p2_1": m("compressed", "console", "#/compose", "PUT /v1/projects/{id}/composition/{choice} and GET .../composition: choices are answered separately, each carrying its decider and date, and answering one never overwrites another. Compressed because the reference's five NAMED choices are L2 vocabulary with no enum in CREST — the screen renders whatever the deployment declares on the project's configuration and takes a typed answer where it declares nothing, rather than inventing a taxonomy"),
    "p2_2": m("compressed", "console", "#/workers", "The configurator's own frame now exists at the rail's Workers entry: the three reference options (register in CREST / import existing records / import only) with the reference's copy verbatim, the source-registry field and the 'importing does not create identities' callout. Compressed: the choice has no composition record to be written to, and the registration it configures lives in the field and worker doors"),
    "p2_3": m("compressed", "console", "#/compose", "Definition origin is one composition choice, recorded with its decider through the same PUT as the rest. Compressed: the three named origins and the ratification step that follows them are L2 vocabulary and an unbuilt authoring flow respectively"),
    "p2_19": m("compressed", "console", "#/intake", "The two-ways-in frame now exists at #/intake with the reference's three options — including 'somebody entering work into CREST' shown as not available, which is the point of the screen — and the pull list read from GET /v1/sources, so an empty list means no feed exists rather than one being hidden. Compressed: which ways-in a project allows has no store. The upload half is real in the field door's roster close (POST /v1/batches)"),
    "p2_20": m("implemented", "field", "#/roster", "Row-by-row check against the definition: per-row accepted/unclear verdicts from the real evidence service"),
    "p2_21": m("implemented", "console", "#/unclear", "The reference's frame over GET /v1/unclear: held-now, share-of-everything-received (claims plus held rows) and oldest-unresolved, every row naming what did not match and who it sits with, and the 'who is not told about this' callout verbatim. A mismatch is somebody named, not a status; attribution is checked against the actor's authorization and the submitter deliberately cannot make it"),
    "p2_4": m("compressed", "console", "#/validation", "The reference's routing frame — the two postures, 'a returned work unit goes to', and the Evidence Validator vacancy with its consequence — in the reference's own words. Compressed: the posture cannot be stored, so the screen chooses nothing on the service's behalf; the queue it warns about is the real unclear queue this console works at #/unclear"),
    "p2_5": m("compressed", "console", "#/compose", "Payment posture is one composition choice, recorded with its decider through the same PUT as the rest — a posture, not a feature flag, and never a rate. Compressed: the posture vocabulary is L2 and this deployment declares none"),
    "p2_6": m("implemented", "console", "#/owners", "GET|POST /v1/projects/{id}/roles: every holder with its functions, its grantor, its grant date and its state — revoked and expired listed rather than filtered, because a console showing only live grants cannot say who used to be able to do this. Granting is the owning organisation's act and is refused to the named configurator. Proven by apps.spec.js 'the J3 handover is real'"),
    "p2_17": m("implemented", "console", "#/partners", "GET /v1/partners: the approved organisations, filterable by sector, country and what their accepted terms actually allow, so nobody appears who could not do the work. Nothing here re-examines an organisation and nothing here can change one — the reference's own callout, carried verbatim. Proven by the e2e walk, which finds the approved programme organisation in it"),
    "p2_18": m("compressed", "console", "#/partners", "POST|GET /v1/projects/{id}/partner-grants on the same screen: the grant must carry an end date (the service answers 422 grant_must_end without one — proven in the e2e walk), rides the terms version the partner actually accepted, and cannot be passed on. Compressed: the reference also scopes a grant to named work definitions and a geography, which the grant record does not carry, and its next step is an invitation — there is no invitation service, and the screen says so rather than approximating one"),
    "p2_7": m("implemented", "console", "#/activate", "GET|POST /v1/projects/{id}/activation with every condition shown, satisfied ones included: a refused activation names what is missing rather than being a dead end, and the e2e walk proves the refusal, the gate satisfaction at the service's clock, and the ACTIVE state that follows. Gate names are the deployment's and nothing is hardcoded"),
    "p2_8": m("compressed", "console", "#/finance", "PUT|GET /v1/projects/{id}/finance-link: the system and the code, stored verbatim with who linked it and when — CREST never generates, formats or validates a code. Compressed: the reference's frame also lists a fixture chart of accounts to pick from and labels its buttons with fixture codes, and no chart-of-accounts pull exists to populate one"),
    "p2_9": m("illustrative", "console", "#/finance", "The reference's finance-connection frame with its adaptor table, endpoint/credential fields and 'one direction only' callout verbatim — and an on-screen note that the adaptor library shown is the REFERENCE's, not this deployment's: CREST has no finance adaptor, so nothing here can be tested or saved. The evidence-side twin of this integration pattern is real and lives at #/sources"),
    "p2_10": m("implemented", "console", "#/support", "PUT|GET /v1/projects/{id}/support-owner: a named party a worker's question reaches, with a contact route, and the service refuses an owner who is not a Party here — the e2e walk proves the refusal and then the record. Escalation's absence is rendered as unarranged rather than unnecessary, and the reference's 'what moved, and why' callout is carried verbatim"),
    "p2_11": m("implemented", "console", "#/status", "The reference's funnel frame: work units received, cleared-not-yet-paid and stuck-with-nobody-holding read from the real queues (claims, unreleased windows, held instructions), a needs-somebody-today list where every row names its owning office, and the reference's own rail — Work status · Quality · Payments · Proof · Reports. The straight-through tile reads '—': its metric contract (#31) does not exist, and a fabricated rate on the screen that says whether a project is healthy is the one thing this frame must not do"),
    "p2_12": m("illustrative", "console", "#/stp", "The reference's frame at its own route, with the ranked-cause table real — every unclear row and every held payment, ranked by count, each naming the party who can fix it — and the headline percentage reading '—' because the straight-through contract (#31) does not exist. Illustrative for the headline, live for the causes underneath it, and the screen says which is which"),
    "p2_13": m("illustrative", "console", "#/quality", "The reference's tier frame at its own route: the capped-by-source / fell-short split stated, each registered source shown with the ceiling GET /v1/source-assessments really puts on it, and the definition's own tier map. The tier-share tiles read '—': credentials are listable per worker only, by design, so a project-wide tier mix needs #31's contract"),
    "p2_14": m("implemented", "console", "#/payments", "The reference's frame: confirmed-paid, instructed-not-confirmed, failed-at-the-rail and oldest-unpaid tiles from GET /v1/instructions, then the why-it-is-delayed table with a worker count, an amount, a median age and the owning office per reason — and a row that could not name an owner would itself be the defect to raise. On the reference's own rail"),
    "p2_15": m("implemented", "console", "#/trace", "One claim id → the worker it belongs to, then windows, exits, credential and instruction, each hop a real GET, with the consent posture stated on the frame. On the reference's own rail. What the reference also draws and this cannot: a whole per-worker project history and a portable proof bundle — neither has an endpoint, so neither is drawn"),
    "p2_16": m("illustrative", "console", "#/reports", "The reference's funder frame: the stage-by-stage reconciliation with instructed and confirmed-paid live from GET /v1/instructions and the difference explained, the proof-of-work table, and the 'why tier is in a funder report' callout verbatim. Illustrative because the allocation row has no funding ledger, the tier column has no project-wide read (#31), and there is no report endpoint to save or schedule one"),
    # ---- P-3 Work Definition Author (console: author reads, cannot write) ----
    "p3_1": m("compressed", "console", "#/definework", "A registry of one: the seeded definition read via GET /v1/definitions/{id}; no list/registry endpoint"),
    "p3_15": m("missing", note="Ratify with pending fields: no ratification endpoint; the approver persona surfaces this gap honestly at #/ratify"),
    "p3_16": m("missing", note="Two records, one signature: no signing flow"),
    "p3_pay": m("missing", note="Payment handoff from author to rate owner: no handoff object"),
    "p3_19": m("compressed", "console", "#/definework", "The schema-under-the-form section renders the real stored definition JSON"),
    "p3_18": m("compressed", "console", "#/definework", "'What is still undecided' — the wizard's honest gaps section"),
    # ---- P-10 External Evidence Contact (verify panel) ----
    "w6_1": m("illustrative", "verify", "#/w6_1", "Scoped request + template out: panel says no scoped-link endpoint exists"),
    "w6_2": m("illustrative", "verify", "#/w6_2", "File-back answer: display only; no upload endpoint"),
    "w6_3": m("missing", note="Receipt and where the returned evidence sits: not built"),
    # ---- F-1 Rate Owner (console, persona: rateowner) ----
    "f1_1": m("compressed", "console", "#/paysetup", "Reads the existing linked rate (real GET); the cold-start arrive screen is absent"),
    "f1_2": m("missing", note="Rate-owner assignment: no assignment flow"),
    "f1_3": m("missing", note="Rate authoring: no rate write endpoint in the console"),
    "f1_4": m("missing", note="Rate publication as terms: not built"),
    "f1_5": m("missing", note="Half-done handover state: not built"),
    # ---- F-2 Payment Mechanism Owner (console, persona: payowner) ----
    "f2_1": m("compressed", "console", "#/paysetup", "The where-CREST-stops boundary is stated on the paysetup view"),
    "f2_2": m("illustrative", "console", "#/paysetup", "Rails: labelled illustrative/simulated in Admin.tsx; cannot connect a rail"),
    "f2_3": m("illustrative", "console", "#/paysetup", "Connection: display only"),
    "f2_4": m("missing", note="Real payment test: not built"),
    "f2_5": m("missing", note="Reconciliation file contract: not built"),
    "f2_6": m("missing", note="Advisory statement: not built"),
    "f2_7": m("missing", note="Batching timing choice: not built"),
    "f2_8": m("missing", note="Mechanism activation gate: not built"),
    "f2_9": m("missing", note="Qualification gate before disbursement: not built"),
    "f2_10": m("missing", note="The last gate and what it opened: not built"),
    # ---- W-1 Worker ----
    "w1_1": m("implemented", "worker", "#/", "The entry screen presents the two enrollment pathways as equals: self-enroll (identity via eSignet) and assisted enrollment (the field door), neither a fallback"),
    "w1_2": m("semantically-different", "worker", "#/auth", "The reference's phone+OTP anchor is replaced by the eSignet OIDC leg — a national-identity anchor, not a phone anchor; phone is collected at signup as a contact route, unverified"),
    "w1_3": m("compressed", "worker", "#/", "National ID is the eSignet path itself; CREST sees only the pairwise subject (never the ID number). The optional photograph-the-card route does not exist"),
    "w1_4": m("missing", note="No-document confidence-check route: self path requires eSignet; the no-document worker's route is assisted enrollment (named on the entry screen)"),
    "w1_5": m("implemented", "worker", "#/auth", "Enrollment consent is its own step BEFORE the record is created; acceptance is recorded via POST /v1/parties/{id}/consents (moment=enrolment, captureMethod=screen) immediately after party creation — the party post never fires without it"),
    "w1_6": m("compressed", "worker", "#/auth", "The Crest ID (party did) is shown on completion; the physical card is the field door's (labelled illustrative there)"),
    "w1_7": m("missing", note="Recovery-contact nomination at enrolment: recoveries exist server-side (POST /v1/recoveries) but nomination has no worker-facing write"),
    "w1_8": m("implemented", "worker", "#/home", "The wallet-not-dashboard home: real credential and payment counts"),
    "w1_9": m("implemented", "worker", "#/work", "Definition worker-face: what will and will not count, from GET /v1/definitions/{id}"),
    "w1_10": m("implemented", "worker", "#/work", "Open windows with closes-at; confirm exits the window (POST confirm) — a draft credential and seven days to check it"),
    "w1_11": m("implemented", "worker", "#/work/dispute/:claimId", "POST dispute; the dispute exit releases payment like every exit"),
    "w1_12": m("implemented", "worker", "#/wallet", "Credential list; tier derived at read, never stored"),
    "w1_13": m("implemented", "worker", "#/pay", "Instructions with state; every held payment names its reason and owner"),
    "w1_23": m("compressed", "worker", "#/pay", "Month view folded into the payment list"),
    "w1_24": m("implemented", "worker", "#/pay/:idx", "Why money has not arrived and whose problem it is: held reason + owner"),
    "w1_14": m("illustrative", "worker", "#/work/declined", "Declining work: labelled — no decline endpoint"),
    "w1_15": m("missing", note="Per-share consent: no share/presentation flow exists"),
    "w1_16": m("illustrative", "worker", "#/wallet/deferred", "Deferred qualification: labelled — no endpoint"),
    "w1_17": m("missing", note="Qualification arrival changing earned state: not built"),
    "w1_18": m("implemented", "worker", "#/wallet/:idx/show", "Offline presentation: the credential rendered as a QR (scannable without CREST), the what-a-scan-gives-away disclosure list, and the signed JSON behind a toggle"),
    "w1_19": m("missing", note="Who-is-asking pre-consent: no presentation-request flow"),
    "w1_20": m("missing", note="Worker sees the verifier's list: no presentation flow"),
    # ---- W-2 Registering Agent (field) ----
    "w2_1": m("compressed", "field", "#/registrations", "The day's list: real registrations plus the on-device offline queue"),
    "w2_2": m("compressed", "field", "#/register", "Phone-or-roster enrollment via POST /v1/enrolments/assisted; ID scan does not exist and no raw ID is ever persisted (identity assertion labelled illustrative in Enrol.tsx)"),
    "w2_3": m("semantically-different", "field", "#/consent", "Consent script is read and recorded via POST consents, but the 'voice recording' posts a text sentence with audio/ogg content type — not captured audio (state.tsx:111-130)"),
    "w2_4": m("implemented", "field", "#/hold", "Duplicate hold: probable match holds for the custodian, never auto-merges"),
    "w2_5": m("illustrative", "field", "#/registered", "Card printing labelled illustrative; deferred until a first credential, unlike the reference's on-the-spot card"),
    # ---- W-3 Support Agent (console, persona: support) ----
    "w5_1": m("compressed", "console", "#/cases", "Synthesized queue from real stalled states; no case-management service (Custodian.tsx names it)"),
    "w5_2": m("implemented", "console", "#/supportfind", "Worker lookup via GET /v1/resolve"),
    "w5_3": m("implemented", "console", "#/supporttrace", "Payment trace over the real chain"),
    # ---- W-4 Supervisor / Attestor ----
    "w3_1": m("semantically-different", "field", "#/toconfirm", "The reference worklist belongs to the delivery platform (source attestation). The field door's surface is assisted confirmation of CREST windows (GET /v1/unreached) — now labelled as exactly that, not as J8 source attestation"),
    "w3_2": m("semantically-different", "field", "#/confirmsee/:claimId", "Assisted window exit (route:'assisted') — a valid W1/W4 exit that releases payment, but not confirmation inside the source system"),
    "w3_3": m("semantically-different", "field", "#/differ/:claimId", "Assisted dispute on the worker's behalf — not a correction of the source record"),
    "w3_4": m("semantically-different", "field", "#/roster", "The roster is closed in CREST's evidence intake (POST /v1/evidence/batches), not in the delivery platform; the boundary is stated on the screen"),
    "w3_5": m("compressed", "field", "#/handoff", "The ingestion handoff: accepted rows became claims, unclear rows went to the custodian — real queues, though the provenance-preserving source handoff is CREST-side only"),
    # ---- W-5 Recovery Confirmer ----
    "w4_1": m("missing", note="Confirmer SMS request: no confirmer-facing channel (recoveries administered from the custodian console only)"),
    "w4_2": m("missing", note="Two-of-three quorum progress: POST /v1/recoveries/{id}/confirmations exists server-side; no confirmer UI"),
    "w4_3": m("missing", note="Refusal path: not built"),
    # ---- V-1 Verifier ----
    "v1_1": m("illustrative", "verify", "#/v1_1", "Verifier pass: no pass-issuance endpoint (V1.tsx names it)"),
    "v1_2": m("semantically-different", "verify", "#/v1_2", "The check is real (POST /v1/verify) but 'scan' is a JSON textarea or fixture lookup, not camera/QR/offline scanning"),
    "v1_3": m("implemented", "verify", "#/v1_2", "Yes-plus-four-facts result from the real verification chain; nothing identifying is shown"),
    # ---- V-2 Institutional Verifier ----
    "v2_1": m("compressed", "verify", "#/v2_1", "Wider window with an org session held only inside V-2; the accreditation ceiling cannot be read (V2.tsx names it)"),
    "v2_2": m("semantically-different", "verify", "#/v2_2", "Shows presence/absence of fields; selective disclosure does not exist, so worker refusal cannot be represented"),
    "v2_3": m("implemented", "verify", "#/v2_3", "Bounded batch verification against the real endpoint"),
    # ---- V-4 Funding Oversight Viewer (console, persona: funder) ----
    "v4_1": m("illustrative", "console", "#/portfolio", "Allocated-vs-paid: no funding ledger (Admin.tsx names it); paid side reads real instructions"),
    "v4_2": m("compressed", "console", "#/portfolio", "Trail-down reduced to opening the generic project status view; no place/service/day trace"),
    # ---- EXT ----
    "ext_1": m("missing", note="External by design: identity comes from eSignet/MOSIP — the integration exists (the login IS it) but no walkthrough screen"),
    "ext_2": m("missing", note="External by design: source evidence platforms; the roster intake is the CREST side of the boundary"),
    "ext_3": m("missing", note="External by design: payment rail beyond the instruction boundary"),
    "ext_4": m("missing", note="External by design: next-employer verification — the verify door is the CREST side"),
}

# P-3 authoring screens not singled out above: the entire write/ratification
# workflow is absent; the read-only wizard section covering the topic is the
# nearest surface.
P3_READER = {
    "p3_2": "Scope/sector", "p3_3": "Counting-basis fork", "p3_4": "Category picker",
    "p3_5": "Unit of work", "p3_21": "Training cascade", "p3_6": "Time-based path",
    "p3_7": "Outcome path", "p3_8": "Parties", "p3_9": "Evidence tiers",
    "p3_22": "Source class choice", "p3_23": "Template", "p3_24": "Adaptor library",
    "p3_25": "Adaptor mapping", "p3_26": "Connection", "p3_27": "Dry run",
    "p3_28": "Version registration", "p3_10": "Validation posture", "p3_11": "Payment split",
    "p3_20": "Project roles", "p3_12": "Stacked pay/tranches", "p3_13": "Preconditions/deductions",
    "p3_14": "Extensions",
}
for sid, topic in P3_READER.items():
    MAPPING[sid] = m(
        "missing", "console", "#/definework",
        f"{topic}: authoring does not exist — the wizard is a read-only section over the one seeded definition (Admin.tsx names adaptor mapping, extensions and authoring writes as unbuilt)")


# The J3 connective-tissue screens n1–n5 are OUR design, not the reference's
# (docs/design/j3-connective-tissue/README.md). They belong in the ledger —
# a screen the fidelity gate asserts and the ledger does not carry is a screen
# nobody counts — but they are never mixed into the reference's 143, and their
# source is recorded on every row.
DESIGN_SCREENS = [
    {"id": "n1", "role": "P-1", "stage": "Sign in",
     "title": "Sign in to CREST Console",
     **m("implemented", "console", "#/", "The console door: eSignet as the primary way in (the same server-side flow the worker door uses), the instance-configured demo personas below a divider, and NO role selector anywhere — a role is granted in the registry and read back, never self-declared. The persona block probes the mock issuer's discovery document and is absent where no mock provider answers. Carries one waived facet: the gate's desktop idiom expects a navigation rail, and this screen deliberately has none because nothing is scoped until somebody has signed in")},
    {"id": "n2", "role": "P-1", "stage": "Scope",
     "title": "Where do you want to work?",
     **m("implemented", "console", "#/where", "GET /v1/projects?ownerPartyId= and ?configuratorPartyId=: every context comes from a grant the registry reports, so an empty list is a true answer and names somebody who can grant a role. A project handed over but unanswered lands on n4 rather than on its setup. One deviation from our own design, recorded rather than hidden: the screen renders inside the console shell, so the rail is visible before a scope is chosen")},
    {"id": "n3", "role": "P-1", "stage": "Navigate",
     "title": "One rail, two actors",
     **m("implemented", "console", "#/projects", "The rail contract, and the rail itself: both J3 actors get the same five entries per section, with no entry removed by role. Carries the correction to finding F1 — the reference uses THREE rails across the 24 J3 frames (setup, dashboard, finance/support), so 'identical for both actors' holds per section rather than across all of J3")},
    {"id": "n4", "role": "P-2", "stage": "Handover",
     "title": "Ministry of Health handed you a project",
     **m("compressed", "console", "#/handover", "The receiving side, on the real record: what arrived read from GET /v1/projects/{id}, the whole append-only trail from GET .../ownership, and a decline that requires a reason and returns the project to the Org Admin's queue (POST .../ownership-decision). Proven end to end in apps.spec.js. Compressed rather than implemented for one measured reason: the fixture world seeds no pending handover, so on a fresh stack this screen has nothing to accept and correctly says so — the accept/decline actions appear only for a project somebody has actually been handed")},
    {"id": "n5", "role": "P-2", "stage": "Guard",
     "title": "People & roles is not yours to change",
     **m("implemented", "console", "#/people", "The guard renders at the same rail entry the Org Admin uses — the entry is never hidden. GET /v1/projects/{id}/roles supplies grantableBy, which names the organisation that can grant, and every holder with its grantor and grant date, read only. No HTTP status code reaches the user. The one thing still in copy rather than derived: the role name a Configurator would need, because role-to-function mapping is L2 and unbuilt")},
]

FIDELITY_MAP = os.path.join(ROOT, "tests", "e2e-apps", "fidelity-map.json")
FIDELITY_QUARANTINE = os.path.join(ROOT, "tests", "e2e-apps", "fidelity-quarantine.json")


def gate_verdicts(rows):
    """What the fidelity gate does with each screen, as the gate's own scope
    and the ledger's own statuses decide it.

    Deterministic and stack-free: this says which screens the gate ASSERTS
    and which it skips-with-reason. Whether an asserted screen actually holds
    is the gate's verdict at run time — `make fidelity` fails on a screen
    statused implemented whose assertions fail, which is the check that stops
    this ledger telling a lie."""
    fmap = json.load(open(FIDELITY_MAP, encoding="utf-8"))
    quar = json.load(open(FIDELITY_QUARANTINE, encoding="utf-8"))["screens"]
    scope = fmap["screens"]
    out = {}
    for r in rows:
        sid = r["id"]
        if sid not in scope:
            out[sid] = "—"
        elif sid in quar:
            out[sid] = f"quarantined ({quar[sid]['issue']})"
        elif r["status"] == "implemented":
            out[sid] = "**asserted**"
        else:
            out[sid] = f"skipped ({r['status']})"
    return out


def extract():
    doc = open(REF, encoding="utf-8").read()
    parts = re.split(r'(?=<div class="role-step" data-step=")', doc)
    out = []
    for p in parts[1:]:
        mm = re.match(r'<div class="role-step" data-step="([a-z0-9_]+)" data-role="([A-Z]+-?\d*)"', p)
        if not mm:
            continue
        sid, role = mm.groups()
        st = re.search(r'data-stage="([^"]+)"', p)
        t = re.search(r'<h2 class="narr-h">(.*?)</h2>', p, re.S)
        title = html.unescape(re.sub(r"<[^>]+>", "", t.group(1))).strip() if t else ""
        out.append({"id": sid, "role": role, "stage": st.group(1) if st else "", "title": title})
    return out


def main():
    screens = extract()
    ids = {s["id"] for s in screens}
    mapped = set(MAPPING)
    if ids != mapped:
        raise SystemExit(f"reference/mapping diverged: unmapped={sorted(ids - mapped)} orphaned={sorted(mapped - ids)}")

    rows = []
    for s in screens:
        rows.append({**s, "source": "reference",
                     "roleName": ROLE_NAMES.get(s["role"], s["role"]), **MAPPING[s["id"]]})
    design = [{**d, "source": "crest-design",
               "roleName": ROLE_NAMES.get(d["role"], d["role"])} for d in DESIGN_SCREENS]
    counts = Counter(r["status"] for r in rows)
    gate = gate_verdicts(rows + design)
    doc = {
        "reference": "docs/reference/CREST — Actor Journeys_17Aug.html",
        "assessment": "docs/JOURNEY_GAP_ASSESSMENT.md",
        "generatedBy": "tools/journey-trace/build.py",
        "screens": len(rows),
        "coverage": dict(sorted(counts.items())),
        "statuses": {
            "implemented": "the screen's decisions and state transitions work in the UI, on real endpoints",
            "compressed": "the substance exists but is folded into a broader screen",
            "illustrative": "a labelled mock renders; no real operation behind it",
            "semantically-different": "a real operation exists, but not the reference one at that lifecycle point",
            "missing": "no corresponding user flow",
        },
        "rows": rows,
        "designRows": design,
        "fidelityGate": {
            "suite": "tests/e2e-apps/fidelity.spec.js",
            "scope": "tests/e2e-apps/fidelity-map.json",
            "verdicts": gate,
            "note": "asserted = the gate drives the real stack to this screen and holds it to its journey-spec entry; skipped = the status is not implemented, reported with the reason; quarantined = claimed implemented but unjudgeable today, with an issue.",
        },
    }
    for r in rows + design:
        r["gate"] = gate[r["id"]]
    out_json = json.dumps(doc, indent=1, ensure_ascii=False) + "\n"

    total = len(rows)
    lines = [
        "# Journey traceability — reference screens to React surfaces",
        "",
        "Generated by `tools/journey-trace/build.py` from the 143 `.role-step` screens in",
        '`docs/reference/CREST — Actor Journeys_17Aug.html`. Do not edit by hand; edit the',
        "script's MAPPING and re-run. Assessment context: `docs/JOURNEY_GAP_ASSESSMENT.md`.",
        "",
        "## Measured coverage",
        "",
        "| Status | Screens | Share |",
        "|---|---:|---:|",
    ]
    asserted = sum(1 for v in gate.values() if v == "**asserted**")
    quarantined = sum(1 for v in gate.values() if v.startswith("quarantined"))
    in_scope = sum(1 for v in gate.values() if v != "—")
    for k in ["implemented", "compressed", "semantically-different", "illustrative", "missing"]:
        n = counts.get(k, 0)
        lines.append(f"| {k} | {n} | {n * 100 // total}% |")
    lines += [f"| **total** | **{total}** | |", ""]

    lines += [
        "## The fidelity gate",
        "",
        "The **Gate** column below is generated from the gate's own scope",
        "(`tests/e2e-apps/fidelity-map.json`) and this ledger's statuses, so the",
        "two cannot drift apart. `make fidelity` drives the real stack to every",
        "*asserted* screen and holds it to its `docs/journey-spec.json` entry;",
        "a screen statused **implemented** whose assertions fail turns the gate",
        "red, which is the check that stops this table claiming coverage the",
        "screens do not have. Nothing here is evidence that an asserted screen",
        "passed — only the gate run is that.",
        "",
        f"In scope today (J3 — `p1_*`, `p2_*` — and G-2 — `g2_*` — plus the design screens `n1`–`n5`): "
        f"**{in_scope}** screens — **{asserted}** asserted, "
        f"**{quarantined}** quarantined, "
        f"**{in_scope - asserted - quarantined}** skipped with a reason.",
        "",
    ]

    role = None
    for r in rows:
        if r["role"] != role:
            role = r["role"]
            n = sum(1 for x in rows if x["role"] == role)
            lines += [f"## {role} — {r['roleName']} ({n} screens)", "",
                      "| Screen | Stage | Reference title | Status | Surface | Gate | Evidence / gap |",
                      "|---|---|---|---|---|---|---|"]
        surface = f"{r['app']} `{r['route']}`" if r["route"] else "—"
        lines.append(
            f"| `{r['id']}` | {r['stage']} | {r['title']} | **{r['status']}** | {surface} | {r['gate']} | {r['note']} |")

    lines += [
        "",
        "## CREST design — J3 connective tissue (5 screens)",
        "",
        "Not the reference's screens: the 17 Aug walkthrough does not draw how",
        "anyone signs in, how a person holding roles in two places chooses",
        "where to work, what the shared rail means, or the receiving side of the",
        "`p1_3` → `p2_1` handover. These five are **our** design",
        "(`docs/design/j3-connective-tissue/README.md`), carried in",
        "`docs/journey-spec.json` with `source: \"crest-design\"` and asserted by",
        "the same gate. They are counted separately and never folded into the",
        "reference's 143.",
        "",
        "| Screen | Stage | Design title | Status | Gate | Evidence / gap |",
        "|---|---|---|---|---|---|",
    ]
    for r in design:
        lines.append(
            f"| `{r['id']}` | {r['stage']} | {r['title']} | **{r['status']}** | {r['gate']} | {r['note']} |")
    out_md = "\n".join(lines) + "\n"

    # Same generate-and-diff contract as `make generate-check` and
    # `make journey-spec-check`: a ledger behind its own inputs — the
    # reference, the MAPPING, or the fidelity gate's scope — is a second,
    # wrong answer to "what is covered?".
    if "--check" in sys.argv[1:]:
        stale = [
            name for name, path, want in (
                ("journey-traceability.json", OUT_JSON, out_json),
                ("journey-traceability.md", OUT_MD, out_md),
            )
            if not os.path.exists(path)
            or open(path, encoding="utf-8").read() != want
        ]
        if stale:
            print("STALE: " + ", ".join(stale)
                  + " — run `python3 tools/journey-trace/build.py` and commit the result",
                  file=sys.stderr)
            return 1
        print(f"journey traceability: current ({total} reference screens, "
              f"{len(design)} design screens)")
        return 0

    with open(OUT_JSON, "w", encoding="utf-8") as f:
        f.write(out_json)
    with open(OUT_MD, "w", encoding="utf-8") as f:
        f.write(out_md)
    print(f"{total} screens; coverage: {dict(sorted(counts.items()))}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
