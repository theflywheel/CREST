// Define work (J4), Payment set up (J5), Organisation view (J1), Instance
// view (J2) and the Funder portfolio (J11). The rule throughout: real data
// where a service answers, the reference's own honesty labels where none does.

import { api, services } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";
import {
  esc, short, money, when, mono, monoShort, chip, stat, kvRows, title, lede,
  empty, table, card, cardTitled, openNote, sidecar, next, tierChip,
  ILLUSTRATIVE, SIMULATED,
} from "./ui.js";

async function loadDefinition() {
  const [d, worker, lr] = await Promise.all([
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/faces/worker`).catch(() => null),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/linked-records`).catch(() => ({ linkedRecords: [] })),
  ]);
  return { d, worker, lr: lr.linkedRecords || [] };
}

/* ————— Define work (J4, p3_1–p3_28): the wizard, read-first ————— */
// The registry has no GET /v1/definitions list (POST only, checked against
// schemas/openapi/definitions.yaml), so the registry screen shows the one
// definition the fixture world seeds, read for real.
export async function defineWork(state) {
  const { d, worker, lr } = await loadDefinition();
  const wkr = (worker && worker.worker) || (d.faces && d.faces.worker) || {};
  const plat = (d.faces && d.faces.platform) || {};
  const pays = lr.find(x => x.type === "payment-setup");
  const authNote = openNote(`Authoring writes are not built; this shows the signed definition v${d.version} as the wizard would have captured it.`);
  const steps = [
    ["Scope", `${kvRows([
      ["activity", esc((d.activity || {}).label || "") + " " + mono((d.activity || {}).code || "")],
      ["skill code", mono(d.skillCode || "")],
      ["project", mono(FIX.project)],
    ])}<p class="muted" style="margin-top:8px">The skill code is the part of the record that travels; the activity code is this deployment's own word for it.</p>`],
    ["Counting basis", kvRows([["one unit is", esc(d.outcomeUnit || "")]]) +
      `<p class="muted" style="margin-top:8px">Everything downstream — the rate, the credential, the funnel — counts in this unit.</p>`],
    ["What is counted", kvRows([
      ["required fields", (plat.requiredFields || []).map(mono).join(" · ") || "—"],
      ["record schema", mono(plat.schemaRef || "")],
    ]) + `<p class="muted" style="margin-top:8px">A record providing only the mandatory core is still valid Tier-1-capable evidence; these fields only decide how strong.</p>`],
    ["Parties", kvRows([
      ["authored by", monoShort(d.authoredByPartyId || "")],
      ["ratified by", monoShort(d.ratifiedByPartyId || "") + " — two people by construction"],
      ["who may attest", (d.authorisedAttesterFunctions || []).map(f => chip("info", f)).join(" ") || "—"],
    ])],
    ["Evidence tiers", kvRows((d.tierMap || []).map(r => ["Tier " + r.tier,
      tierChip(r.tier) + " source in " + esc((r.sourceClassIn || []).join(" / ")) +
      "; captured by " + esc((r.captureMethodIn || []).join(" / ")) +
      "; identity ≥ " + esc(r.minIdentityAssurance || "any") +
      ((r.requiresFields || []).length ? "; needs " + esc(r.requiresFields.join(", ")) : ""),
    ])) + `<p class="muted" style="margin-top:8px">Read top to bottom; the first rule whose conditions all hold wins. The floor has no requirements at all — that is what keeps the weakest worker payable.</p>`],
    ["Source", kvRows([
      ["source systems", (plat.sourceSystems || []).map(mono).join(" · ") || "—"],
      ["worker tier ceiling", wkr.tierCeiling != null ? String(wkr.tierCeiling) : "—"],
    ]) + `<div style="margin-top:10px">${openNote(`Adaptor mapping (p3_24–p3_28) and extension fields (p3_14) have no write side. Authoring writes are not built; this shows the signed definition v${d.version} as the wizard would have captured it.`)}</div>`],
    ["Template — the worker's own words", `<div class="consent-quote">${esc(wkr.summary || "")}</div>
      <div style="margin-top:10px">${(wkr.evidenceInPlainLanguage || []).map(l => `<div style="display:flex;gap:8px;margin-bottom:6px">${chip("ok", "counts")}<span class="body-2">${esc(l)}</span></div>`).join("") || `<div class="muted">The definition carries no worker-language evidence list.</div>`}</div>`],
    ["Validation", kvRows([
      ["a row missing a required field", "lands in the unclear queue with a reason — never a silent drop"],
      ["who decides whose work it was", "the custodian, holding resolve-unclear-evidence — never the submitter"],
    ])],
    ["Payment", pays
      ? kvRows([
          ["rate per " + esc(d.outcomeUnit || "unit"), money(pays.payload.ratePerOutcomeUnit.amountMinor, pays.payload.ratePerOutcomeUnit.currency)],
          ["payer", monoShort(pays.payload.payerPartyId)],
          ["effective from", esc(when(pays.payload.effectiveFrom))],
          ["record", mono(pays.id) + " · v" + pays.version + " · " + esc(pays.state)],
        ]) + `<p class="muted" style="margin-top:8px">Attached by reference — the definition is complete without it. See Payment set up for the full journey.</p>`
      : empty("No payment-setup record is attached. The work is recognised, and recognition is a use of its own.")],
    ["Ratify", kvRows([
      ["the fact", "two linked signed records: the definition, and its ratification — author and ratifier are different parties or the service refuses (409)"],
      ["authored by", monoShort(d.authoredByPartyId || "")],
      ["ratified by", monoShort(d.ratifiedByPartyId || "")],
      ["activated", esc(when(d.activatedAt))],
    ])],
  ];
  const idx = Math.min(Math.max(state.wizStep || 0, 0), steps.length - 1);
  return `${title("Define work — the wizard, read against the real definition", chip(d.state === "ACTIVE" ? "ok" : "info", "v" + d.version + " · " + d.state))}
    ${lede("The design draws a 28-screen authoring journey. This walkthrough shows each step with the values the seeded definition actually carries — real data, authoring labeled.")}
    ${cardTitled("Definition registry", table(["Definition", "Activity", "Version", "State"], [[
      mono(d.id), esc((d.activity || {}).label || ""), "v" + d.version, chip(d.state === "ACTIVE" ? "ok" : "info", d.state),
    ]]) + `<p class="muted" style="margin-top:8px">The definitions service has no list endpoint (POST-only registry, checked against its OpenAPI); this is the one definition the fixture world seeds, read for real.</p>`)}
    ${authNote}
    <div style="display:flex;gap:6px;flex-wrap:wrap">
      ${steps.map(([name], i) => `<button class="btn ${i === idx ? "" : "secondary"}" style="width:auto;padding:9px 14px;font-size:12.5px" data-wiz="${i}">${i + 1} · ${esc(name)}</button>`).join("")}
    </div>
    ${cardTitled("Step " + (idx + 1) + " of " + steps.length + " — " + steps[idx][0], steps[idx][1])}`;
}

/* ————— Payment set up (J5, f1/f2) ————— */
export async function paymentSetup() {
  const { d, worker, lr } = await loadDefinition();
  const wkr = (worker && worker.worker) || {};
  const pays = lr.find(x => x.type === "payment-setup");
  return `${title("Payment set up")}
    ${lede("The rate is a versioned linked record keyed to the definition — it can change without touching what the work is, and an old version is never overwritten.")}
    ${pays ? cardTitled("The rate (f1_3 / f1_4)", kvRows([
      ["rate per " + esc(d.outcomeUnit || "unit"), `<span class="big-stat">${money(pays.payload.ratePerOutcomeUnit.amountMinor, pays.payload.ratePerOutcomeUnit.currency)}</span>`],
      ["payer", monoShort(pays.payload.payerPartyId)],
      ["effective from", esc(when(pays.payload.effectiveFrom))],
      ["record", mono(pays.id) + " · v" + pays.version + " · " + esc(pays.state)],
    ])) : empty("No payment-setup record exists for this definition.")}
    ${cardTitled("What the worker will see", `<div class="consent-quote">${esc(wkr.summary || "")}${pays ? `<br><br><strong>One ${esc(d.outcomeUnit || "unit")} pays ${esc(money(pays.payload.ratePerOutcomeUnit.amountMinor, pays.payload.ratePerOutcomeUnit.currency))}</strong> — set separately from the definition, versioned, never overwritten.` : "<br><br>No rate is attached — this work is recognised, and recognition is a use of its own."}</div>
      <p class="muted" style="margin-top:8px">Rendered from the definition's worker face — the same read the worker's own app makes.</p>`)}
    ${cardTitled("Payment rail (f2)", `<div style="display:flex;gap:8px;margin-bottom:10px">${ILLUSTRATIVE}${SIMULATED}</div>
      ${kvRows([
        ["rail", "mobile-money · MockPay sandbox"],
        ["connection test", chip("ok", "Simulated: 200 OK · 412ms")],
        ["settlement report", "daily, by instruction id"],
      ])}
      <p class="muted" style="margin-top:8px">A rail today is a URL in deployment configuration — the payments service raises instructions and records their release; carrying them to a real rail is deployment wiring, not an API this console can call. These screens are drawn with the reference's own labels.</p>`)}
    ${next([
      ["What just happened", "Nothing — this page reads the rate record and the worker face; it changes nothing."],
      ["Who acts next", "The payments service, per instruction, the moment a confirmation window exits."],
      ["When", "Immediately at exit — all four exits release, a dispute included."],
      ["How you will be told", "The instruction appears in Payments with its state, and a held one names its owner."],
      ["ifnot", "If an instruction does not appear after an exit, the gap is the payments service's to explain — trace the claim."],
    ])}`;
}

/* ————— Organisation view (J1 / P-1) ————— */
// There is no GET list of authorizations by party (parties OpenAPI has POST,
// /permits, /overdue only), so grants are shown as live permits() answers for
// the functions this deployment's terms name, plus the real overdue queue.
export async function orgView() {
  const [org, overdue] = await Promise.all([
    api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`),
    api.get("parties", "/v1/authorizations/overdue").catch(() => ({ authorizations: [] })),
  ]);
  const checks = [
    ["attest-work", null, "may this organisation attest work at all (instance-wide)"],
    ["attest-work", FIX.project, "…and specifically on PRJ-118"],
    ["submit-work-evidence", FIX.project, "may it submit evidence on PRJ-118"],
  ];
  const permits = await Promise.all(checks.map(([fn, ctx]) => {
    const q = new URLSearchParams({ partyId: FIX.org, function: fn });
    if (ctx) q.set("contextId", ctx);
    return api.get("parties", "/v1/authorizations/permits?" + q).catch(() => null);
  }));
  const p = org.party || org;
  return `${title("Organisation — " + (p.displayName || "…"))}
    ${cardTitled("Profile", kvRows([
      ["party", mono(p.id || FIX.org)],
      ["kind", esc(p.kind || "")],
      ["contact routes", (p.contactRoutes || []).map(r => esc(r.kind) + " " + mono(r.value || "")).join(" · ") || "—"],
      ["registered", esc(when(p.createdAt))],
    ]))}
    ${cardTitled("Authorizations held — live permits() answers", table(
      ["Question", "Scope", "Answer", "Overdue for review?"],
      checks.map(([fn, ctx, label], i) => [
        esc(label) + " (" + esc(fn) + ")",
        ctx ? monoShort(ctx) : "instance",
        permits[i] ? chip(permits[i].permitted ? "ok" : "err", permits[i].permitted ? "permitted" : "not permitted") : chip("plain", "no answer"),
        permits[i] && permits[i].overdue ? chip("warn", "past review-by — still working, by design") : "—",
      ])) + `<p class="muted" style="margin-top:8px">The parties service deliberately has no list-my-grants endpoint — only <span class="mono">permits()</span> and the overdue queue — so this table asks the real question the services ask. Overdue never changes the answer: flag overdue, keep working.</p>`)}
    ${cardTitled("Grants past their review-by date", table(
      ["Held by", "Functions", "Scope", "Review by", ""],
      (overdue.authorizations || []).map(a => [
        monoShort(a.partyId),
        (a.functions || []).join(", "),
        a.scope ? esc(a.scope.kind) + (a.scope.contextId ? " · " + short(a.scope.contextId) : "") : "—",
        esc(when(a.reviewBy || (a.period || {}).reviewBy)),
        chip("warn", "overdue"),
      ]),
      "Nothing is overdue. Passing a review date changes nothing by itself — what it must never be is unseen."))}
    ${cardTitled("Invitations and terms (g2_6–g2_13)", `<div style="margin-bottom:10px">${ILLUSTRATIVE}</div>
      ${kvRows([
        ["invite an organisation", "an email carrying the terms version to agree to"],
        ["terms on file", mono("crest:terms:01JCREST00000000000000TERM") + " v1 — real, seeded via POST /v1/terms"],
        ["agreement flow", "the invitee's decision recorded against that exact version"],
      ])}
      ${openNote("There is no terms-catalogue or invitation service; terms exist as versioned records the parties service stores, and the invitation flow is drawn here with the reference's own label rather than pretended.")}`)}`;
}

/* ————— Instance view (J2 / G-1) ————— */
export async function instanceView() {
  const issuer = await api.get("verification", "/v1/issuer").catch(() => null);
  const instAnswer = await api.get("parties", "/v1/instance").catch(() => null);
  const inst = (instAnswer || {}).instance || null;
  const reg = (inst || {}).registry || {};
  const names = ["parties", "definitions", "evidence", "confirmation", "verification", "payments"]; // all but payments answer from crest-core (#150)
  const health = await Promise.all(names.map(n =>
    api.get(n, "/healthz").then(h => ({ n, ok: true, h })).catch(e => ({ n, ok: false, e }))));
  const instRows = inst ? kvRows([
    ["instance", mono(inst.instanceId || "—")],
    ["name", inst.name || "—"],
    ["operator", inst.operatorPartyId ? mono(inst.operatorPartyId) : "not configured"],
    ["issuer (per the instance)", inst.issuerId ? mono(inst.issuerId) : "not configured"],
    ["registry", reg.url ? mono(reg.url) + (reg.namespace ? " · " + mono(reg.namespace) : "") : "Postgres fallback — no external registry"],
    ["transparency log", reg.transparent ? "yes — an append-only log a reader can watch" : "no — answers rest on this deployment's word"],
  ]) : kvRows([
    ["instance", "the parties service did not answer /v1/instance — a deployment that has not been told who it is answers 503 here"],
  ]);
  return `${title("Instance — the deployment itself")}
    ${cardTitled("Instance facts", instRows + kvRows([
      ["issuer (per the verification service)", issuer ? mono(issuer.id || issuer.issuer || JSON.stringify(issuer).slice(0, 60)) : "the verification service did not answer /v1/issuer"],
      ["services", String(names.length) + " CREST services behind this console"],
    ]) + `<p class="muted" style="margin-top:8px">GET /v1/instance is the deployment's public self-description (#70) — every field is configuration or derived from it, read live rather than stored, which is exactly where the layering test puts it.</p>`)}
    ${cardTitled("The services behind all of it — live health sweep", `<div class="stats" style="flex-wrap:wrap">
      ${health.map(x => `<div class="stat" style="min-width:150px"><div class="n" style="font-size:18px">${x.ok ? chip("ok", "healthy") : chip("err", "unreachable")}</div><div class="l">${esc(x.n)}<br><span class="mono">/healthz</span></div></div>`).join("")}
    </div>`)}
    ${cardTitled("Consent floor", kvRows([
      ["the floor", "enrolment consent is captured per programme; withdrawing stops new evidence collection and never touches what was already paid"],
      ["message templates", "deployment configuration, per the #59 decision — two deployments wording the ask differently are both CREST"],
    ]) + `<div style="margin-top:10px">${openNote("Editing consent scripts and message templates from this screen is not built; templates are deployment config (#59). What is real: every captured consent is a record with an artefact the worker can hear back.")}</div>`)}
    ${cardTitled("Admission queue (g4_1–g4_3)", `<div style="margin-bottom:10px">${ILLUSTRATIVE}</div>
      ${table(["Organisation", "Requested", "Decision"], [
        ["Riverside Community Health NGO", "example row", chip("plain", "no queue service exists")],
      ])}
      ${openNote("Organisation admission (POST /v1/organisations/{id}/decision) exists as a decision record, but there is no pending-admissions queue endpoint to read — this screen is illustrative until one exists.")}`)}`;
}

/* ————— Funder portfolio (V-4, J11) ————— */
export async function funderPortfolio() {
  const [claims, instr] = await Promise.all([
    api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
  ]);
  const list = instr.instructions || [];
  const released = list.filter(i => i.state === "RELEASED" || i.state === "SETTLED");
  const paid = released.reduce((s, i) => s + (i.amountMinor || 0), 0);
  const cur = (released[0] || list[0] || {}).currency || "";
  return `${title("Portfolio")}
    ${lede("One project exists in this deployment; one row. Every count is live; the allocation column is not, and says so.")}
    ${table(
      ["Project", "Claims", "Instructions", "Paid to workers", "Held", "Allocated vs paid", ""],
      [[
        mono("PRJ-118") + " · Riverside bednet campaign 2026",
        String((claims.claims || []).length),
        String(list.length),
        money(paid, cur),
        String(list.filter(i => i.state === "HELD").length),
        `${ILLUSTRATIVE}`,
        `<button class="btn secondary" style="width:auto;padding:7px 12px" data-go="status">Open</button>`,
      ]])}
    ${openNote("Allocated-vs-paid needs a funding ledger no service holds; the paid column is real (summed released instructions), the allocation is not shown rather than invented.")}`;
}
