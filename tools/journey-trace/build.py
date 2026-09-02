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
    "g1_1": m("missing", note="Instance stand-up wizard: no deployment-creation surface; a stack is stood up by compose/Railway, not in the console"),
    "g1_2": m("compressed", "console", "#/instance", "Read-only: GET /v1/instance shows the instance identity/issuer; naming and identity binding are deploy-time configuration, not console actions"),
    "g1_3": m("illustrative", "console", "#/instance", "Consent floor is displayed; consent-policy editing has no endpoint (Admin.tsx Instance view labels it)"),
    "g1_5": m("missing", note="Invitation naming a person: no invitation service exists"),
    "g1_6": m("compressed", "console", "#/instance", "Service health sweep reads real /healthz of each member; the services-not-roles framing is shown read-only"),
    "g4_1": m("illustrative", "console", "#/instance", "Admission queue rendered from the registration states; the review queue itself is labelled illustrative in Admin.tsx"),
    "g4_2": m("illustrative", "console", "#/instance", "What-can-be-checked review detail: display only"),
    "g4_3": m("compressed", "console", "#/onboard/status", "The approval decision exists as POST /v1/organisations/{id}/decision (unit-tested), but the console has no reviewer surface for it; the deployed model is REGISTRY_ORG_APPROVAL=on-terms-acceptance"),
    # ---- G-2 Onboarding Authorising Signatory (console, public onboarding) ----
    "g2_1": m("implemented", "console", "#/onboard", "The reference's six-field identity form (legal name, country, work email, contact person, kind, sector) as a desktop console frame — Registration · 1 of 4 with the Register/Terms/Certificates/Done rail; registration documents deliberately not asked, per the reference's own callout → POST /v1/organisations. Since #168 the whole form persists: country/kind/sector/contact person ride the Party's generic attributes map and are read back from GET /v1/organisations/{id}/registration — a registry round-trip, not browser state"),
    "g2_4": m("implemented", "console", "#/onboard/status", "GET /v1/organisations/{id}/registration: state, exact terms version, decider. Under REGISTRY_ORG_APPROVAL=manual (the local default) the terminal state waits on the operator's decision; on-terms-acceptance approves in the acceptance's transaction. 'On nobody's project' is true — no project membership exists"),
    "g2_5": m("missing", note="Invitation-before-or-after ordering: no invitation service"),
    "g2_6": m("missing", note="Asking for wider terms later: only one terms listing exists (GET /v1/terms), no term-upgrade request"),
    "g2_7": m("missing", note="Qualification documents: no document upload on onboarding"),
    "g2_8": m("compressed", "console", "#/onboard/status", "Terminal screen says what was decided and by whom; 'what to watch for' guidance partially present"),
    "g2_9": m("missing", note="Receiving an invitation/enablement: no invitation service"),
    "g2_10": m("missing", note="'Live, and what to do first' post-enablement onboarding: not built"),
    "g2_11": m("implemented", "console", "#/onboard/terms", "GET /v1/terms lists published versions; acceptance names an exact version via POST /v1/organisations/{id}/terms-acceptance — 'never edited underneath you' is the backend's versioning rule"),
    "g2_12": m("missing", note="Certificate checks: no certificate verification exists"),
    "g2_13": m("implemented", "console", "#/onboard/status", "Approval recorded with decider (policy or person); publication to the registry log happens in the approving transaction"),
    # ---- G-4 Worker Registry Custodian (console, persona: custodian) ----
    "g4_4": m("missing", note="Coverage-by-place headline: no geography endpoint"),
    "g4_5": m("missing", note="Record-by-record quality worklist: not built (unclear rows are evidence-side, not registry quality)"),
    "g4_6": m("implemented", "console", "#/dupes", "GET /v1/holds + POST /v1/holds/{id}/resolve; merges_without_confirmation metric from GET /v1/holds/metrics; probable matches hold and never auto-merge"),
    "g4_7": m("missing", note="Registry reuse / return-on-shared-registry metric: no endpoint"),
    # ---- P-1 Org Admin (console, persona: orgadmin) ----
    "p1_1": m("compressed", "console", "#/org", "Organisation view reads the party, terms and authorizations (GET /v1/parties/{id}, /v1/terms, /v1/authorizations); standing-configuration framing only"),
    "p1_2": m("missing", note="Role assignment ('a role is held, not just recorded'): no role-grant write surface"),
    "p1_3": m("missing", note="Project creation: no POST for projects; the fixture context is seeded"),
    # ---- P-2 Project Configurator (console, persona: configurator) ----
    "p2_1": m("missing", note="Five composition choices: no project-composition surface"),
    "p2_2": m("compressed", "field", "#/register", "Worker registration exists in the field door (assisted) and worker door (self); the configurator's registration-vs-import choice screen does not"),
    "p2_3": m("missing", note="Definition origin choice (three origins, one ratification): not built"),
    "p2_19": m("compressed", "field", "#/roster", "Evidence intake: roster CSV close posts real batches (POST /v1/evidence/batches); the two-ways-in configuration screen itself is absent"),
    "p2_20": m("implemented", "field", "#/roster", "Row-by-row check against the definition: per-row accepted/unclear verdicts from the real evidence service"),
    "p2_21": m("implemented", "console", "#/unclear", "GET /v1/unclear; a mismatch is a named row in the custodian's queue, resolvable by attribution — never a silent drop"),
    "p2_4": m("compressed", "console", "#/unclear", "What happens when evidence does not clear: the unclear queue shows it; the validation-posture configuration does not exist"),
    "p2_5": m("missing", note="Payment posture configuration: not built"),
    "p2_6": m("missing", note="Owner assignment: not built"),
    "p2_17": m("missing", note="Partner directory browse: no directory view"),
    "p2_18": m("missing", note="Partner grants (narrower than terms, time-bound): no grant surface"),
    "p2_7": m("missing", note="Activation gate: no activation state on projects"),
    "p2_8": m("missing", note="Finance-code linking: not built"),
    "p2_9": m("compressed", "console", "#/sources", "Source registration is real (the story registers riverside-dhis2, heartbeat state shows); the connect-wizard framing is absent"),
    "p2_10": m("missing", note="Support ownership configuration: not built"),
    "p2_11": m("implemented", "console", "#/status", "The funnel reads real queues: windows, exits, credentials, instructions"),
    "p2_12": m("illustrative", "console", "#/status", "STP headline with ranked causes: metric contract unbuilt (Project.tsx names it)"),
    "p2_13": m("illustrative", "console", "#/status", "Tier mix (capped-by-source vs fell-short): metric contract unbuilt"),
    "p2_14": m("implemented", "console", "#/payments", "GET /v1/instructions grouped by state and held reason; money-delayed vs people-waiting distinction shown with owners"),
    "p2_15": m("implemented", "console", "#/trace", "One claim id → windows, exits, credential, instruction — each hop a real GET"),
    "p2_16": m("illustrative", "console", "#/reports", "Pre-built funder reports: labelled not backed; no report endpoint"),
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
        rows.append({**s, "roleName": ROLE_NAMES.get(s["role"], s["role"]), **MAPPING[s["id"]]})
    counts = Counter(r["status"] for r in rows)
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
    }
    with open(OUT_JSON, "w", encoding="utf-8") as f:
        json.dump(doc, f, indent=1, ensure_ascii=False)
        f.write("\n")

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
    for k in ["implemented", "compressed", "semantically-different", "illustrative", "missing"]:
        n = counts.get(k, 0)
        lines.append(f"| {k} | {n} | {n * 100 // total}% |")
    lines += [f"| **total** | **{total}** | |", ""]
    role = None
    for r in rows:
        if r["role"] != role:
            role = r["role"]
            n = sum(1 for x in rows if x["role"] == role)
            lines += [f"## {role} — {r['roleName']} ({n} screens)", "",
                      "| Screen | Stage | Reference title | Status | Surface | Evidence / gap |",
                      "|---|---|---|---|---|---|"]
        surface = f"{r['app']} `{r['route']}`" if r["route"] else "—"
        lines.append(
            f"| `{r['id']}` | {r['stage']} | {r['title']} | **{r['status']}** | {surface} | {r['note']} |")
    with open(OUT_MD, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    print(f"{total} screens; coverage: {dict(sorted(counts.items()))}")


if __name__ == "__main__":
    main()
