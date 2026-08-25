// Project view — the live heart of the console. Every number on these screens
// is read from a running service; where a number the design promises has no
// backend yet, the screen says so and names the issue instead of inventing it.

import { api } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";
import {
  esc, short, money, when, agoDays, monoShort, mono, chip, stat, kvRows, title,
  lede, empty, table, card, cardTitled, openNote, sidecar, tstep, tierChip,
} from "./ui.js";

const unresolved = arr => (arr || []).filter(x => !x.resolvedAt);
const oldestAge = (arr, field) => {
  const days = (arr || []).map(x => agoDays(x[field] || x.createdAt)).filter(d => d !== null);
  return days.length ? Math.max(...days) + "d oldest" : "";
};

async function funnelCounts() {
  const [claims, unclear, unreleased, unreached, instr, metrics] = await Promise.all([
    api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
    api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] })),
    api.get("confirmation", "/v1/unreleased").catch(() => ({ windows: [], unreleased: [] })),
    api.get("confirmation", "/v1/unreached").catch(() => ({ windows: [], unreached: [] })),
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    api.get("parties", "/v1/holds/metrics").catch(() => null),
  ]);
  return {
    claims: claims.claims || [],
    unclear: unresolved(unclear.unclear),
    unreleased: unreleased.windows || unreleased.unreleased || [],
    unreached: unreached.windows || unreached.unreached || [],
    instructions: instr.instructions || [],
    metrics,
  };
}

/* ————— 1 · Status ————— */
export async function projectStatus() {
  const f = await funnelCounts();
  const byState = f.claims.reduce((m, c) => { const k = c.state || "?"; m[k] = (m[k] || 0) + 1; return m; }, {});
  const held = f.instructions.filter(i => i.state === "HELD");
  const settled = f.instructions.filter(i => i.state === "RELEASED" || i.state === "SETTLED");
  return `${title("PRJ-118 · a funnel, not totals")}
    ${lede("Riverside bednet campaign 2026. Each stage below is a real queue read from a service, and every stuck thing in it has an owning role — a count without an owner is a number nobody has to act on.")}
    <div class="stats">
      ${stat(f.claims.length, "claims on record — " + Object.entries(byState).map(([k, n]) => `${n} ${k}`).join(", "), "evidence service")}
      ${stat(f.unclear.length, `unclear rows waiting ${oldestAge(f.unclear, "createdAt")}`, "owner: registry custodian")}
      ${stat(f.unreached.length, `open windows nobody could be told about ${oldestAge(f.unreached, "opensAt")}`, "owner: supervisor (assisted route)")}
    </div>
    <div class="stats">
      ${stat(f.unreleased.length, "windows exited, payment not yet confirmed released", "owner: payments service — the relay owes this")}
      ${stat(f.instructions.length + " / " + held.length + " / " + settled.length, "instructions raised / held / released-or-settled", "held ones each name an owning office below in Payments")}
      ${stat(f.metrics ? (f.metrics.mergesWithoutConfirmation ?? f.metrics.merges_without_confirmation ?? 0) : "—", "merges without confirmation — the invariant as a number, must be 0", "owner: registry custodian")}
    </div>
    ${openNote(`The full metric contracts — straight-through rate, tier mix, time-to-say — are <a href="https://github.com/theflywheel/CREST/issues/31">#31</a> and are not built. Presentation counts exist only per worker (<span class="mono">/v1/presentations?subjectRef=</span>); a project-wide presentations total would need #31's contracts, so none is shown.`)}`;
}

/* ————— 2 · Payments ————— */
export async function projectPayments() {
  const [out, rec] = await Promise.all([
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    api.get("payments", "/v1/reconciliation").catch(() => null),
  ]);
  const list = out.instructions || [];
  const held = list.filter(i => i.state === "HELD");
  const groups = held.reduce((m, i) => {
    const k = (i.held && i.held.code) || i.heldReason || "held";
    (m[k] = m[k] || []).push(i); return m;
  }, {});
  return `${title("Payments, and where the delay is")}
    ${lede("Money delayed and people waiting are different sentences. Every held payment carries a reason with an owner — the record itself refuses one without.")}
    <div class="stats">
      ${stat(list.length, "instructions")}
      ${stat(held.length, "held — each with a named owner")}
      ${stat(rec ? (rec.gaps || []).length : "—", rec ? "reconciliation gaps" : "reconciliation not answering")}
    </div>
    ${held.length ? Object.entries(groups).map(([code, items]) => `<div class="held">
      <div class="top"><span class="amt">${esc(code)} · ${items.length} instruction(s) · ${money(items.reduce((s, i) => s + (i.amountMinor || 0), 0), items[0].currency)}</span>
        ${chip("warn", "held")}</div>
      <div class="why">${esc((items[0].held && items[0].held.explanation) || "No explanation string — the code is the reason.")}</div>
      <div class="who">Owner: ${esc(short((items[0].held && items[0].held.ownerPartyId) || "")) || "(unnamed — that is itself a defect to raise)"}</div>
    </div>`).join("") : ""}
    ${cardTitled("The delay, by reason", table(
      ["Reason", "Count", "Amount held", "Owner"],
      Object.entries(groups).map(([code, items]) => [
        esc(code), String(items.length),
        money(items.reduce((s, i) => s + (i.amountMinor || 0), 0), items[0].currency),
        monoShort((items[0].held && items[0].held.ownerPartyId) || ""),
      ]),
      "No payment is held. Delay taxonomy rows appear the moment one is."))}
    ${table(
      ["Worker", "Amount", "State", "Released by", "When", "If held: why — owner", ""],
      list.slice(0, 100).map(i => [
        monoShort(i.partyId),
        money(i.amountMinor, i.currency),
        chip(i.state === "RELEASED" || i.state === "SETTLED" ? "ok" : "warn", i.state),
        esc(i.releasedBy || "—"),
        esc(when(i.releasedAt)),
        i.held ? esc(i.held.explanation || i.held.code) + " — " + monoShort(i.held.ownerPartyId || "") : "—",
        `<button class="btn secondary" style="padding:7px 12px;width:auto" data-trace="${esc(i.claimId)}">Trace</button>`,
      ]),
      "No payment has been instructed yet. Instructions appear the moment a confirmation window exits — all four exits release one, a dispute included: a dispute contests the record, never the money.")}`;
}

/* ————— 3 · Trace ————— */
export async function projectTrace(state) {
  const claimId = state.traceClaim || "";
  let body = "";
  if (claimId) {
    const [win, instr, claim] = await Promise.all([
      api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("payments", `/v1/instructions/by-claim/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("evidence", `/v1/claims/${encodeURIComponent(claimId)}`).catch(() => null),
    ]);
    const i = instr && (instr.instruction || instr);
    const c = claim && (claim.claim || claim);
    const st = (ok, active) => ok ? "" : active ? "active" : "todo";
    body = cardTitled("Claim " + short(claimId), `<div class="tline">
      ${tstep(st(!!c, true), "Work recorded and attributed", c ? "unit " + monoShort(c.unitId) + " · state " + esc(c.state || "") : "the evidence service has no such claim — the trail stops before it starts")}
      ${tstep(st(!!(win && win.exitRoute), !!win), "The worker had their say", win ? (win.exitRoute ? "exit: " + esc(win.exitRoute) + " · " + esc(when(win.exitedAt || win.closedAt)) : "window still open — closes " + esc(when(win.closesAt))) : "no confirmation window found — the confirmation service owes this step")}
      ${tstep(st(!!(win && win.credentialId), !!(win && win.exitRoute)), "Credential signed", win && win.credentialId ? monoShort(win.credentialId) : "issued when the window exits")}
      ${tstep(st(!!i, !!(win && win.exitRoute)), "Payment instruction raised", i ? money(i.amountMinor, i.currency) + " · " + esc(i.state) : "none — if the window exited, this gap is the payments service's to explain", true)}
    </div>
    ${i && i.held ? kvRows([["held", esc(i.held.explanation || i.held.code) + " — owner " + monoShort(i.held.ownerPartyId || "")]]) : ""}
    <p class="muted" style="margin-top:8px">A trace that ends early is an answer, not a failure: it names the step that owes the next fact.</p>`);
  }
  return `${title("Trace a claim — one search, one checkable trail")}
    ${lede("From “the money did not arrive” to the step that owes an answer, without guessing. Each fact comes from a different service, so a gap names the service responsible for it.")}
    ${card(`<form id="traceform" style="display:flex;gap:10px">
      <input name="claim" placeholder="crest:claim:…" value="${esc(claimId)}" required
        style="flex:1;font:400 13px/1.4 var(--mono);padding:10px 12px;border:1px solid var(--divider);border-radius:6px">
      <button class="btn" style="width:auto;padding:10px 22px">Trace</button>
    </form>`)}
    ${body}`;
}

/* ————— 4 · Definition ————— */
export async function projectDefinition() {
  const [d, v, lr] = await Promise.all([
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/faces/verifier`).catch(() => null),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/linked-records`).catch(() => ({ linkedRecords: [] })),
  ]);
  const tm = (v && v.tierMap) || d.tierMap || [];
  const pays = (lr.linkedRecords || []).find(x => x.type === "payment-setup");
  return `${title("The work, defined", chip(d.state === "ACTIVE" ? "ok" : "info", "v" + d.version + " · " + d.state))}
    ${lede("A definition is versioned and immutable once active; author and ratifier are two people by construction. What it pays lives in a separate record, referenced by id — the rate can change without touching what the work <em>is</em>.")}
    ${cardTitled((d.activity || {}).label || String(d.activity || ""), kvRows([
      ["id", mono(d.id)],
      ["counted in", esc(d.outcomeUnit || "")],
      ["skill code (the part that travels)", mono(d.skillCode || "")],
      ["authored by", monoShort(d.authoredByPartyId || "")],
      d.ratifiedByPartyId && ["ratified by", monoShort(d.ratifiedByPartyId) + " — not its author, by construction"],
      ["activated", esc(when(d.activatedAt))],
    ]))}
    ${cardTitled("How evidence becomes a tier — first matching rule wins", kvRows(tm.map(r => [
      "Tier " + r.tier,
      tierChip(r.tier) + " source in " + esc((r.sourceClassIn || []).join(" / ")) +
        "; captured by " + esc((r.captureMethodIn || []).join(" / ")) +
        "; identity ≥ " + esc(r.minIdentityAssurance || "any") +
        ((r.requiresFields || []).length ? "; needs " + esc(r.requiresFields.join(", ")) : ""),
    ])) + `<p class="muted" style="margin-top:8px">The map is public to verifiers on purpose — a verifier who cannot see it can only be told a tier, never check one. The tier itself is computed at query time and stored nowhere.</p>`)}
    ${(lr.linkedRecords || []).length ? cardTitled("Linked records", kvRows((lr.linkedRecords || []).map(x => [
      x.type,
      "v" + x.version + " · " + esc(x.state) + (x.type === "payment-setup" && x.payload
        ? " — " + money(x.payload.ratePerOutcomeUnit.amountMinor, x.payload.ratePerOutcomeUnit.currency) + " per unit, effective " + esc(when(x.payload.effectiveFrom))
        : ""),
    ]))) : ""}
    ${sidecar(`The 28-screen authoring wizard is the <strong>Define work</strong> journey in the sidebar — it walks this same signed definition step by step, as the screens would have captured it.${pays ? "" : " No payment-setup record is attached; the work is recognised, and recognition is a use of its own."}`, true)}`;
}

/* ————— 5 · Sources ————— */
export async function projectSources() {
  const [out, assess] = await Promise.all([
    api.get("evidence", "/v1/sources").catch(() => ({ sources: [], silent: 0 })),
    api.get("verification", "/v1/source-assessments").catch(() => ({ assessments: [] })),
  ]);
  const sources = out.sources || [];
  const neverSeen = sources.filter(s => s.state === "NEVER_SEEN");
  const stateChip = s => chip(s.state === "HEALTHY" ? "ok" : s.state === "SILENT" ? "err" : "warn", s.state);
  return `${title("Where evidence comes from")}
    ${lede("A source going quiet is the only failure that produces nothing a worker can see or report — their record simply stops growing. So every feed is registered with a cadence and an owner, and the system notices silence unaided.")}
    ${table(
      ["System", "Adapter", "Cadence", "Last seen", "State", "Owner"],
      sources.map(s => [
        mono(s.systemRef || ""), mono(s.adapterRef), esc(s.expectedEvery || ""),
        esc(when(s.lastSeenAt)), stateChip(s), monoShort(s.ownerPartyId || ""),
      ]),
      "No sources registered — fixtures submit as canonical CSV.")}
    ${neverSeen.length ? openNote(`${neverSeen.length} source(s) read NEVER_SEEN even though this project's evidence arrived through them. Known bug, design finding <a href="https://github.com/theflywheel/CREST/issues/117">#117</a>: the heartbeat joins the batch's <span class="mono">systemRef</span> against the source's <span class="mono">adapter_ref</span>, so a feed named differently in the two places never registers a beat. Reported, deliberately not patched around here.`) : ""}
    ${cardTitled("Current assessments", (assess.assessments || []).length
      ? kvRows(assess.assessments.map(a => [a.adapterRef, "capped at tier " + a.maxTier + " — " + esc(a.reason)]))
      : `<div class="muted">No source is downgraded. A downgrade moves every affected credential's tier instantly, with nothing reissued — the tier is derived, never stored.</div>`)}`;
}

/* ————— 6 · Reports ————— */
export async function projectReports() {
  const f = await funnelCounts();
  const released = f.instructions.filter(i => i.state === "RELEASED" || i.state === "SETTLED");
  const paidMinor = released.reduce((s, i) => s + (i.amountMinor || 0), 0);
  const cur = (released[0] || f.instructions[0] || {}).currency || "";
  return `${title("Reports — what a funder is told")}
    ${lede("Rendered from the same live queues as Status — a report that could disagree with the console it came from would be two versions of the truth.")}
    <div class="stats">
      ${stat(f.claims.length, "claims on record", "evidence service")}
      ${stat(released.length, "payments released — all four window exits release, a dispute included", "payments service")}
      ${stat(money(paidMinor, cur), "released to workers, total", "payments service")}
    </div>
    <div class="stats">
      ${stat(f.unclear.length, "rows still unattributed", "owner: registry custodian")}
      ${stat(f.instructions.filter(i => i.state === "HELD").length, "payments held — each names an owner in Payments", "see Payments for the taxonomy")}
      ${stat(f.metrics ? (f.metrics.mergesWithoutConfirmation ?? 0) : "—", "merges without confirmation (must be 0)", "owner: registry custodian")}
    </div>
    ${openNote(`Pre-built funder report formats — straight-through rate, tier mix, cost per verified outcome, exportable periods — are the metric contracts of <a href="https://github.com/theflywheel/CREST/issues/31">#31</a> and are not built. The counts above are live; nothing else is pretended.`)}`;
}
