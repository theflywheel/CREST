// Registry custodian (G-4) and support (J10). The custodian holds the two
// decisions the system refuses to make for itself: which candidate a held
// match is, and whose work an unattributed row was. Support can escalate;
// it can never retry or release money.

import { api } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";
import {
  esc, short, money, when, agoDays, mono, monoShort, chip, stat, kvRows,
  title, lede, empty, table, card, cardTitled, openNote, sidecar,
} from "./ui.js";

/* ————— Find a worker (custodian find; support w5_2 shares it) ————— */
export function custodianFind(state, support) {
  return Promise.resolve(`${title("Find a worker")}
    ${lede("By the identifier you have — strongest first. An ambiguous match holds; it never guesses, and it never merges.")}
    ${support ? cardTitled("Which method to try, in order", kvRows([
      ["1 · printed card", "Most reliable · works offline — the card carries the whole signed credential, not a link to one"],
      ["2 · phone number", "works when they registered with one"],
      ["3 · roster id", "the programme's own id, scoped to a project"],
      ["4 · national-id hash", "never the raw id — a salted hash is all the registry holds"],
      ["5 · name + supervisor", "weakest; resolves through the people who know them"],
    ])) : ""}
    ${card(`<form id="findform" style="display:flex;flex-direction:column;gap:10px;max-width:520px">
      <label class="body-2">Kind<br><select name="kind" style="width:100%;padding:9px;border:1px solid var(--divider);border-radius:6px;font:inherit">
        <option value="">any (precedence order)</option>
        <option>national-id-hash</option><option>contact-route</option><option>roster-id</option>
      </select></label>
      <label class="body-2">Value<br><input name="value" placeholder="+15550100011 or roster id" required style="width:100%;padding:9px;border:1px solid var(--divider);border-radius:6px;font:400 13px var(--mono)"></label>
      <label class="body-2">Context (for roster ids)<br><input name="ctx" value="${esc(FIX.project)}" style="width:100%;padding:9px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)"></label>
      <button class="btn" style="width:auto;align-self:flex-start;padding:11px 24px">Resolve</button>
    </form><div id="findout" style="margin-top:10px"></div>`)}`);
}

/* ————— Duplicates queue ————— */
export async function custodianDupes() {
  const [out, metrics] = await Promise.all([
    api.get("parties", "/v1/holds"),
    api.get("parties", "/v1/holds/metrics").catch(() => null),
  ]);
  const list = (out.holds || []).filter(h => !h.resolvedAt);
  return `${title("Duplicates — the queue, and the rule for closing one")}
    ${lede("Two records collide on an identifier. The queue shows existence, never the identifier itself. Probable matches hold; they never auto-merge — a merge needs the worker's own confirmation.")}
    <div class="stats" style="max-width:440px">
      ${stat(list.length, "open holds", "owner: registry custodian")}
      ${stat(metrics ? (metrics.mergesWithoutConfirmation ?? metrics.merges_without_confirmation ?? 0) : "—", "merges_without_confirmation — a monitored metric, not an aspiration; must be 0")}
    </div>
    ${list.length ? list.map(h => cardTitled("Collision on " + (h.keyKind || "identifier"), `
      ${kvRows([
        ["why it is here", esc(h.reason || "")],
        ["candidates", (h.candidates || []).map(monoShort).join(" · ")],
        ["opened", esc(when(h.createdAt))],
      ])}
      <form data-hold="${esc(h.id)}" style="display:flex;flex-direction:column;gap:8px;margin-top:10px;max-width:520px">
        <label class="body-2">Decision<br><select name="decision" style="width:100%;padding:8px;border:1px solid var(--divider);border-radius:6px;font:inherit">
          <option value="distinct">distinct — two people share the identifier</option>
          <option value="merge">merge — one person recorded twice</option>
        </select></label>
        <label class="body-2">The identifier belongs to<br><select name="party" style="width:100%;padding:8px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)">
          ${(h.candidates || []).map(c => `<option>${esc(c)}</option>`).join("")}
        </select></label>
        <label class="body-2">Worker confirmation (merge only) — method<br><input name="method" placeholder="in-person" style="width:100%;padding:8px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
        <button class="btn" style="width:auto;align-self:flex-start;padding:10px 20px">Close the hold</button>
      </form>`)).join("") : empty("No open holds. When two records collide on an identifier, the hold appears here and waits for you — nothing is guessed in the meantime.")}`;
}

/* ————— Unclear attribution ————— */
export async function custodianUnclear() {
  const out = await api.get("evidence", "/v1/unclear");
  const list = (out.unclear || []).filter(u => !u.resolvedAt);
  return `${title("Whose work was this row")}
    ${lede("A mismatch is somebody named, not a status. Attributing a row is a decision with your name on it, checked against your authorization — the submitter deliberately cannot make it.")}
    ${list.length ? table(
      ["Row", "Kind", "Why it is here", "Waiting", "Attribute to"],
      list.map(u => [
        mono(u.rowRef || u.id),
        esc(u.kind || ""),
        esc(u.reason || ""),
        esc(String(agoDays(u.createdAt) ?? "—")) + "d",
        `<form data-unclear="${esc(u.id)}" style="display:flex;gap:6px">
          <input name="party" placeholder="did:crest:party:…" required style="width:230px;padding:7px;border:1px solid var(--divider);border-radius:6px;font:400 11.5px var(--mono)">
          <button class="btn" style="width:auto;padding:7px 14px">Attribute</button></form>`,
      ])) : empty("The queue is empty. A row that fails to match never disappears — it waits here for a named decision.")}`;
}

/* ————— Recoveries ————— */
export async function custodianRecoveries(state, me) {
  const out = await api.get("parties", "/v1/recoveries");
  const list = out.recoveries || [];
  return `${title("Recoveries")}
    ${lede("A lost handset must not cost anyone their history. Two voices from different authorities decide it; the operator override can never be quiet, and never comes from the worker's own supervisor.")}
    ${card(`<form id="recopen" style="display:flex;gap:8px;flex-wrap:wrap">
      <input name="party" placeholder="worker party id" required style="flex:2;min-width:240px;padding:9px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)">
      <input name="reason" placeholder="reason" required style="flex:1;min-width:160px;padding:9px;border:1px solid var(--divider);border-radius:6px;font:inherit">
      <button class="btn" style="width:auto;padding:9px 18px">Open a recovery</button></form>`)}
    ${list.length ? list.map(r => cardTitled("Worker " + short(r.partyId), `
      <div style="margin-bottom:8px">${chip(r.state === "COMPLETED" ? "ok" : r.state === "OVERRIDDEN" ? "warn" : "info", r.state)}</div>
      ${kvRows([
        ["reason", esc(r.reason || "")],
        ["voices", String((r.confirmations || []).length) + " (distinct authorities)"],
        r.overrideByPartyId && ["override", esc(r.overrideReason || "") + " — " + monoShort(r.overrideByPartyId) + ", review by " + esc(when(r.reviewBy))],
      ])}
      ${(r.state === "CONFIRMED" || r.state === "OVERRIDDEN")
        ? `<form data-reccomplete="${esc(r.id)}" style="display:flex;gap:8px;margin-top:10px">
            <input name="subject" placeholder="new subject ref" required style="flex:1;max-width:320px;padding:8px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)">
            <button class="btn" style="width:auto;padding:8px 16px">Bind the new subject</button></form>` : ""}`)).join("")
      : empty("No recoveries.")}`;
}

/* ————— Overdue reviews ————— */
export async function custodianReview() {
  const [out, over] = await Promise.all([
    api.get("parties", "/v1/authorizations/overdue"),
    api.get("parties", "/v1/recoveries?overdue=true").catch(() => ({ recoveries: [] })),
  ]);
  return `${title("Overdue for review")}
    ${lede("Passing a review date changes nothing by itself — the grant keeps working, the override keeps standing. What it must never be is unseen. This is where it is seen.")}
    ${cardTitled("Authorizations past review-by", table(
      ["Party", "Functions", "Scope", "Review by"],
      (out.authorizations || []).map(a => [
        monoShort(a.partyId),
        (a.functions || []).join(", "),
        a.scope ? esc(a.scope.kind) + (a.scope.contextId ? " · " + short(a.scope.contextId) : "") : "—",
        esc(when(a.reviewBy || (a.period || {}).reviewBy)),
      ]),
      "Nothing overdue."))}
    ${cardTitled("Overrides past review-by", (over.recoveries || []).length
      ? kvRows(over.recoveries.map(r => [short(r.partyId), esc(r.overrideReason || "") + " — review by " + esc(when(r.reviewBy))]))
      : `<div class="muted">None.</div>`)}`;
}

/* ————— Support: open cases (w5_1) ————— */
// There is no case-management service; a "case" here is synthesized from the
// real stalled things — a held instruction, an unattributed row, an open
// recovery — each of which already names its owner.
export async function supportCases() {
  const [instr, unclear, rec] = await Promise.all([
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] })),
    api.get("parties", "/v1/recoveries").catch(() => ({ recoveries: [] })),
  ]);
  const rows = [];
  for (const i of (instr.instructions || []).filter(x => x.state === "HELD")) {
    rows.push([chip("warn", "held payment"), monoShort(i.claimId),
      esc((i.held && (i.held.explanation || i.held.code)) || "held"),
      monoShort((i.held && i.held.ownerPartyId) || "") || "(unowned — a defect)",
      `<button class="btn secondary" style="width:auto;padding:6px 12px" data-trace="${esc(i.claimId)}">Trace</button>`]);
  }
  for (const u of (unclear.unclear || []).filter(x => !x.resolvedAt)) {
    rows.push([chip("info", "unattributed row"), mono(u.rowRef || u.id), esc(u.reason || ""), "registry custodian", ""]);
  }
  for (const r of (rec.recoveries || []).filter(x => x.state !== "COMPLETED")) {
    rows.push([chip("brand", "open recovery"), monoShort(r.partyId), esc(r.reason || ""), "registry custodian + confirming authorities", ""]);
  }
  return `${title("Open cases")}
    ${lede("There is no ticket system behind this — and there does not need to be one for a first answer: everything stalled in the real queues is a case, and each already names an office. A worker must never see a missing payment with no explanation attached.")}
    ${table(["What", "About", "Why it is stalled", "Owning office", ""], rows,
      "Nothing is stalled. Every payment instruction is moving, every row is attributed, no recovery is open.")}`;
}

/* ————— Support framing around the shared trace ————— */
export function supportTraceSidecar() {
  return sidecar("Support can escalate, never retry or release money. A trace that ends early names the step — and the service — that owes the next fact; that name is the escalation.", false);
}
