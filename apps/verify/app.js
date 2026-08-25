// CREST — the verification surfaces (apps/verify).
//
// Journey J9 "Checking a credential": V-1, the pass-only verifier (v1_1–v1_3),
// V-2, the institutional verifier (v2_1–v2_3) plus resolving a person, and
// P-10, the external-institution panel (w6_1–w6_3). Verification is
// deliberately account-free: trust rides the credential's signature, not a
// login, so most of this app works logged out against the open verification
// endpoints. Where the design promises a screen the backend cannot serve yet,
// the screen says so with an .open-note and names the gap — a visible "not
// yet", never fake data.

import { api, ApiError, loginAs, setSession } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";

const esc = x => String(x ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
const short = id => { const s = String(id || ""); return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-7) : s; };
const when = ts => ts ? new Date(ts).toLocaleString(undefined, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }) : "—";
const day = ts => ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "—";

const app = document.getElementById("app");

// State the router carries between screens: the last verdict (v1_3 and v2_2
// render it), the credential it was computed for, and whether we hold the
// institutional session.
const S = {
  err: null,
  verdict: null,          // last /v1/verify answer
  credential: null,       // the credential that verdict was computed for
  verifiedAt: null,
  verifiedAs: null,       // null = bare check; FIX.org = institutional
  orgSession: false,
  orgParty: null,         // GET /v1/parties/{org} if readable
  orgPartyErr: null,
};

function fail(e) {
  S.err = e instanceof ApiError ? `${e.status} ${e.code || ""} — ${e.message}` : String(e && e.message || e);
}
const errbar = () => S.err ? `<div class="errbar">${esc(S.err)}</div>` : "";

const infoIco = `<svg class="ico" viewBox="0 0 15 15" aria-hidden="true"><circle cx="7.5" cy="7.5" r="6.5"/><line x1="7.5" y1="7" x2="7.5" y2="11"/><line x1="7.5" y1="4.4" x2="7.5" y2="4.5"/></svg>`;
const tickSvg = `<svg viewBox="0 0 10 10" aria-hidden="true"><path d="M1.5 5.5 L4 8 L8.5 2.5" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;

/* ————— navigation ————— */

const NAV = [
  ["V-1 · A pass-only verifier", [
    ["v1_1", "Get a pass"],
    ["v1_2", "Scan or enter"],
    ["v1_3", "The answer"],
  ]],
  ["V-2 · An onboarded institution", [
    ["v2_1", "Checking as an institution"],
    ["v2_2", "Verified, refusals shown"],
    ["v2_3", "Batch — checking many"],
    ["person", "Resolve a person"],
  ]],
  ["P-10 · External institution", [
    ["w6_1", "A request for one attestation"],
    ["w6_2", "Three tiers, four kinds of evidence"],
  ]],
];

const PANEL_ROUTES = new Set(["w6_1", "w6_2"]);
const ORG_ROUTES = new Set(["v2_1", "v2_2", "v2_3", "person"]);

function route() {
  const h = (location.hash || "#/v1_1").replace(/^#\//, "");
  return NAV.some(([, items]) => items.some(([k]) => k === h)) ? h : "v1_1";
}

function sidebar(active) {
  return `<nav class="sidebar">${NAV.map(([cap, items]) =>
    `<div class="cap">${esc(cap)}</div>` + items.map(([k, label]) =>
      `<button class="${k === active ? "active" : ""}" data-go="${k}">${esc(label)}</button>`).join("")
  ).join("")}</nav>`;
}

function consoleShell(active, content) {
  const who = S.orgSession && ORG_ROUTES.has(active)
    ? "Ministry of Health — onboarded verifier"
    : "Not signed in — verification does not need an account";
  return `<div class="console-shell">
    <header class="appbar"><span class="mark"></span><span class="t">CREST · Checking a credential</span><span class="who">${esc(who)}</span></header>
    <div class="console-body">
      ${sidebar(active)}
      <main class="pane"><div class="pane-narrow screen">${errbar()}${content}</div></main>
    </div>
  </div>`;
}

function panelShell(active, content) {
  return `<div class="panel-shell screen">
    ${errbar()}${content}
    <div class="btn-row">
      <button class="btn secondary" data-go="${active === "w6_1" ? "w6_2" : "w6_1"}">${active === "w6_1" ? "How tiers are decided" : "Back to the request"}</button>
      <button class="btn secondary" data-go="v1_2">The verifier itself</button>
    </div>
  </div>`;
}

/* ————— the verify call, shared by v1 and v2 ————— */

async function loadSampleCredential(partyId) {
  // The same open chain-read a verifier gets; borrowing the newest credential
  // is exactly what scanning the worker's printed card gives.
  const out = await api.get("verification", `/v1/parties/${encodeURIComponent(partyId || FIX.workerA)}/credentials`);
  const c = (out.credentials || [])[0];
  if (!c) throw new Error("that person's chain holds no credentials yet");
  return c;
}

async function runVerify(credential, requestedByPartyId, purpose) {
  const v = await api.post("verification", "/v1/verify", {
    credential,
    requestedByPartyId: requestedByPartyId || undefined,
    purpose: purpose || undefined,
  });
  S.verdict = v; S.credential = credential; S.verifiedAt = new Date().toISOString();
  S.verifiedAs = requestedByPartyId || null;
  return v;
}

function verdictChip(v) {
  return v.valid
    ? `<span class="chip ok">Valid — the signature checks out</span>`
    : `<span class="chip err">Not valid</span>`;
}

function chainList(v) {
  // The checkable/trusting chain, field handling copied from the PoC's
  // verifier result: each link says whether you could check it without CREST.
  return (v.trustChain || []).map(l =>
    `<div class="dis"><div class="li ${l.checkable ? "" : "off"}">
      <span class="tick">${l.checkable ? tickSvg : ""}</span>
      <span><div class="t">${esc(l.claim)}</div><div class="s">${l.checkable ? "checkable — " : "trusting — "}${esc(l.how || l.trusting || "")}</div></span>
    </div></div>`).join("")
    + (v.notEstablished || []).map(n =>
      `<div class="dis"><div class="li off"><span class="tick"></span>
        <span><div class="t">Not established</div><div class="s">${esc(n)}</div></span></div></div>`).join("");
}

/* ————— V-1: pass-only verifier ————— */

function v1_1() {
  return `<div class="eyebrow">V-1 · Screen 1 of 3</div>
    <h2 class="scr-title">Get a pass to check credentials</h2>
    <p class="body-2">A verifier pass identifies you without onboarding you. It puts a name on your checks — the worker sees <em>who</em> looked, in their own "who checked me" trail — but it grants nothing: no accreditation ceiling, no batch rights, no vetting. Identified, not onboarded.</p>
    <div class="kv">
      <div class="row"><span class="k">A pass adds</span><span class="v">your name on every check the worker sees</span></div>
      <div class="row"><span class="k">A pass does not add</span><span class="v">any trust — the answer rides the signature either way</span></div>
      <div class="row"><span class="k">Onboarding (V-2) adds</span><span class="v">an accreditation ceiling and batch checking</span></div>
    </div>
    <div class="open-note"><b>Not backed yet.</b> Pass issuance has no endpoint — the services expose verification (<span class="mono">/v1/verify</span>, chain reads) but no <span class="mono">/v1/verifier-passes</span> or equivalent, and the PoC's verifier face never issues one. This screen is the design's shape only. In this demo the check itself works without a pass: continue to the next screen and check a credential logged out.</div>
    <div class="btn-row"><button class="btn" data-go="v1_2">Check a credential without a pass</button></div>`;
}

function v1_2() {
  return `<div class="eyebrow">V-1 · Screen 2 of 3</div>
    <h2 class="scr-title">Scan or enter the credential</h2>
    <p class="body-2">Paste the credential exactly as scanned from the worker's printed card or wallet. A bare check needs no account and no consent beyond the showing itself.</p>
    <div class="card">
      <form id="verifyform" style="display:flex;flex-direction:column;gap:10px">
        <div style="display:flex;gap:8px;align-items:center">
          <input name="sampleparty" placeholder="…or borrow one: worker party id" style="flex:1;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
          <button type="button" class="btn secondary" id="loadsample" style="width:auto;padding:10px 14px">Load their newest credential</button>
        </div>
        <label class="body-2">The credential (JSON, as scanned)
          <textarea name="cred" rows="9" required placeholder='{"@context": …}' style="width:100%;margin-top:4px;padding:10px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)"></textarea>
        </label>
        <div style="display:flex;gap:8px">
          <input name="who" placeholder="who is asking (party id, optional)" style="flex:1;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
          <input name="why" placeholder="why (optional — recorded for the worker)" style="flex:1;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
        </div>
        <button class="btn">Check it</button>
      </form>
    </div>
    <div class="sidecar">${infoIco}<span class="txt">Every check — with a purpose or without one — leaves a line in the worker's own trail. That is by design: the record of who looked belongs to the person looked at.</span></div>`;
}

function v1_3() {
  const v = S.verdict;
  if (!v) return `<div class="eyebrow">V-1 · Screen 3 of 3</div>
    <h2 class="scr-title">The answer</h2>
    <p class="body-2">No credential has been checked in this session yet. The answer screen renders the verdict of the last check.</p>
    <div class="btn-row"><button class="btn" data-go="v1_2">Check one now</button></div>`;
  const we = ((S.credential || {}).credentialSubject || {}).workEvent || {};
  const defRef = (we.definition || {}).id || we.definitionRef || we.activity || "";
  const defVersion = (we.definition || {}).version || "";
  return `<div class="eyebrow">V-1 · Screen 3 of 3</div>
    <h2 class="scr-title">${v.valid ? "Verified" : "Not verified"}</h2>
    <div style="display:flex;gap:8px;flex-wrap:wrap">
      ${verdictChip(v)}
      ${v.valid ? `<span class="chip tier${v.tier || 3}">Tier ${esc(v.tier ?? "—")} — computed now, never stored</span>` : ""}
      ${v.revoked ? `<span class="chip err">Withdrawn</span>` : ""}
      ${(v.contested || []).length ? `<span class="chip warn">Contested — the record, not the money</span>` : ""}
    </div>
    ${(v.reasons || []).length ? `<div class="card quiet">${(v.reasons || []).map(r => `<div class="body-2">${esc(r)}</div>`).join("")}</div>` : ""}
    <div class="eyebrow">Yes, plus facts — and nothing identifying</div>
    <div class="kv">
      <div class="row"><span class="k">What work</span><span class="v">${esc(we.activity || "—")}${we.outcome ? " · " + esc(we.outcome.value + " " + we.outcome.unit) : ""}</span></div>
      <div class="row"><span class="k">Under which definition</span><span class="v"><span class="mono">${esc(short(defRef)) || "—"}</span>${defVersion ? " · v" + esc(defVersion) : ""}</span></div>
      <div class="row"><span class="k">At what tier</span><span class="v">Tier ${esc(v.tier ?? "—")}, derived from provenance at this moment</span></div>
      <div class="row"><span class="k">When</span><span class="v">${esc(day((we.period || {}).start))}${(we.period || {}).end ? " – " + esc(day(we.period.end)) : ""}</span></div>
    </div>
    <div class="eyebrow">What you can check, and what you are trusting</div>
    ${chainList(v)}
    <div class="sidecar ok">${infoIco}<span class="txt">This answer rides the signature, not CREST's word. The same credential shown to you offline, against the published key, verifies the same way — no account, no vetting, and nothing here identifies the worker to you.</span></div>
    <div class="next">
      <div class="eyebrow">What happens next</div>
      <div class="nrow"><span class="k">What just happened</span><span class="v">The credential was checked and the check was recorded, one line, even for a bare scan.</span></div>
      <div class="nrow"><span class="k">Who acts next</span><span class="v">Nobody has to. The worker can see this check in their own "who checked me" trail.</span></div>
      <div class="nrow"><span class="k">When</span><span class="v">The trail line exists already — it was written with the verdict.</span></div>
      <div class="nrow"><span class="k">How you will be told</span><span class="v">You will not be — the answer above is the whole of what a pass-only verifier gets.</span></div>
      <div class="ifnot">If the result was "not valid": that is an answer too, and it was recorded the same way. A failed check never quietly disappears.</div>
    </div>
    <div class="btn-row"><button class="btn secondary" data-go="v1_2">Check another</button></div>`;
}

/* ————— V-2: institutional verifier ————— */

async function ensureOrg() {
  if (S.orgSession) return;
  await loginAs(FIX.org);
  S.orgSession = true;
  try {
    S.orgParty = await api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`);
    S.orgPartyErr = null;
  } catch (e) {
    S.orgParty = null;
    S.orgPartyErr = e instanceof ApiError ? `${e.status} ${e.code || ""}` : String(e && e.message || e);
  }
}

function v2_1() {
  const p = S.orgParty;
  const facts = p ? `<div class="kv">
      <div class="row"><span class="k">Signed in as</span><span class="v">${esc(p.displayName || p.name || "Ministry of Health")}</span></div>
      <div class="row"><span class="k">Party</span><span class="v mono">${esc(short(p.id || FIX.org))}</span></div>
      <div class="row"><span class="k">Kind</span><span class="v">${esc(p.kind || "organisation")}</span></div>
    </div>`
    : `<div class="open-note"><b>Authorization facts not readable here.</b> The org's party record could not be read (${esc(S.orgPartyErr || "unknown")}). The session is real — checks below run under the Ministry of Health login — but what onboarding granted this verifier is shown as a label, not as read facts.</div>`;
  return `<div class="eyebrow">V-2 · Screen 1 of 3</div>
    <h2 class="scr-title">Checking as an onboarded institution</h2>
    <p class="body-2">Onboarding does not make the answer more true — the signature already settled that. What it adds is standing: an accreditation ceiling (what tier of claim this institution is trusted to attest, not to read), batch checking, and a name the worker's trail can hold accountable.</p>
    ${facts}
    <div class="kv">
      <div class="row"><span class="k">Onboarding adds</span><span class="v">accreditation ceiling · batch checks · an accountable name</span></div>
      <div class="row"><span class="k">Onboarding never adds</span><span class="v">access to anything identifying, or a stronger verdict</span></div>
    </div>
    <div class="open-note"><b>The accreditation ceiling itself has no readable endpoint.</b> The parties service records authorizations, but no route exposes a verifier's accreditation ceiling as a fact this screen could render. Shown here as the design's label only.</div>
    <div class="btn-row"><button class="btn" data-go="v2_2">Check a credential as this institution</button></div>`;
}

// The fields a work-event credential can carry, in the order the disclosure
// list shows them. Included fields get the filled tick; anything the worker
// withheld — or that was never captured — gets the hollow ring and an explicit
// refused state, never a silent omission.
// Optional evidence fields appear in the credential as names in
// we.evidenceFields (values may live in the underlying record, not the
// credential); the core fields carry their values directly.
const evHas = (we, name) => (we.evidenceFields || []).includes(name);
const DISCLOSABLE = [
  ["activity", "What the work was", we => we.activity],
  ["outcome", "The counted outcome", we => we.outcome ? we.outcome.value + " " + we.outcome.unit : undefined],
  ["period", "When it was done", we => (we.period || {}).start ? day(we.period.start) + ((we.period || {}).end ? " – " + day(we.period.end) : "") : undefined],
  ["geography", "Where, coarsely", we => we.geography || (evHas(we, "geography") ? "included in the credential's evidence" : undefined)],
  ["household_id", "Household reference", we => we.householdId || (evHas(we, "household_id") ? "included in the credential's evidence" : undefined)],
  ["beneficiary_count", "How many people it reached", we => we.beneficiaryCount ?? (evHas(we, "beneficiary_count") ? "included in the credential's evidence" : undefined)],
  ["supervisor_present", "Whether a supervisor was present", we => we.supervisorPresent ?? (evHas(we, "supervisor_present") ? "included in the credential's evidence" : undefined)],
  ["source_record_ref", "The source system's own record reference", we => we.sourceRecordRef || (evHas(we, "source_record_ref") ? "included in the credential's evidence" : undefined)],
];

function v2_2() {
  const v = S.verdict;
  const isOrgCheck = v && S.verifiedAs === FIX.org;
  const form = `<div class="card">
    <form id="orgverifyform" style="display:flex;flex-direction:column;gap:10px">
      <div style="display:flex;gap:8px;align-items:center">
        <input name="sampleparty" placeholder="worker party id (blank: the fixture worker)" style="flex:1;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
        <button type="button" class="btn secondary" id="orgloadsample" style="width:auto;padding:10px 14px">Load newest</button>
      </div>
      <label class="body-2">The credential (JSON)
        <textarea name="cred" rows="7" required style="width:100%;margin-top:4px;padding:10px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)"></textarea>
      </label>
      <input name="why" placeholder="purpose (recorded for the worker to read)" style="padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
      <button class="btn">Check as Ministry of Health</button>
    </form>
  </div>`;
  let result = "";
  if (isOrgCheck) {
    const we = ((S.credential || {}).credentialSubject || {}).workEvent || {};
    const rows = DISCLOSABLE.map(([, label, get]) => {
      const val = get(we);
      const present = val !== undefined && val !== null && val !== "";
      return { label, val, present };
    });
    const refused = rows.filter(r => !r.present);
    result = `
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        ${verdictChip(v)}
        ${v.valid ? `<span class="chip tier${v.tier || 3}">Tier ${esc(v.tier ?? "—")}</span>` : ""}
        ${refused.length ? `<span class="chip warn">${refused.length} field${refused.length === 1 ? "" : "s"} refused or absent</span>` : `<span class="chip ok">Everything disclosable was shown</span>`}
      </div>
      <div class="eyebrow">What was shown — and what was refused</div>
      <div class="dis">
        ${rows.map(r => r.present
          ? `<div class="li"><span class="tick">${tickSvg}</span><span><div class="t">${esc(r.label)}</div><div class="s">${esc(String(r.val))}</div></span></div>`
          : `<div class="li off"><span class="tick"></span><span><div class="t">${esc(r.label)}</div><div class="s">Refused by the worker — shown as refused, never as a blank</div></span></div>`
        ).join("")}
      </div>
      <div class="sidecar">${infoIco}<span class="txt">If withheld fields simply vanished, a verifier could not tell a worker who refused from a worker who has nothing. So refusals render as refusals — the fact of withholding is disclosed even though the field is not.</span></div>
      <div class="open-note"><b>Two honest caveats.</b> First, whether showing refusals at all is acceptable is an open question in the design — a visible refusal is itself information about the worker. Second, the backend has no selective-disclosure yet: a hollow ring above means the field is absent from the credential, and this demo cannot distinguish "the worker refused" from "never captured". The refused-state rendering is the design's stance; the refusal <em>fact</em> awaits selective disclosure in the credential itself.</div>
      ${chainList(v)}`;
  } else if (v) {
    result = `<div class="open-note">The last check in this session was a bare (pass-only) check, not an institutional one — run one above to see the disclosure list rendered under this institution's name.</div>`;
  }
  return `<div class="eyebrow">V-2 · Screen 2 of 3</div>
    <h2 class="scr-title">Verified, with refusals shown</h2>
    <p class="body-2">The same verify call as V-1 — same endpoint, same signature, same verdict. What changes is the rendering: an institution sees the disclosure list, field by field, with every withheld field shown as an explicit refusal.</p>
    ${form}${result}`;
}

function v2_3() {
  return `<div class="eyebrow">V-2 · Screen 3 of 3</div>
    <h2 class="scr-title">Batch — checking many</h2>
    <p class="body-2">A batch declares who is asking and why — in words each worker will read in their own trail — and is size-capped by the deployment. Per-credential answers only: there are deliberately no aggregate answers, because "83% of this cohort verified" is a judgement about people none of them agreed to.</p>
    <div class="card">
      <form id="batchform" style="display:flex;flex-direction:column;gap:10px">
        <div style="display:flex;gap:8px;align-items:center">
          <input name="sampleparty" placeholder="worker party id (blank: the fixture worker)" style="flex:1;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
          <button type="button" class="btn secondary" id="batchloadsample" style="width:auto;padding:10px 14px">Load their whole chain</button>
        </div>
        <label class="body-2">Credentials (JSON array)
          <textarea name="creds" rows="7" required placeholder='[{…}, {…}]' style="width:100%;margin-top:4px;padding:10px;border:1px solid var(--divider);border-radius:6px;font:400 12px var(--mono)"></textarea>
        </label>
        <label class="body-2">Purpose — free text, 10–200 characters, worker-visible
          <input name="why" required minlength="10" maxlength="200" placeholder="e.g. Annual CHW programme audit, District North, 2026" style="width:100%;margin-top:4px;padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
        </label>
        <div class="muted" id="purposecount">0 / 200 — at least 10</div>
        <button class="btn">Check the batch</button>
      </form>
      <div id="batchout" style="margin-top:12px"></div>
    </div>
    <div class="sidecar">${infoIco}<span class="txt">The size cap is a deployment (L2) setting over an L1 default of 100 — configurable, but never removable (#107). A batch over the cap is refused whole, not truncated: a truncated batch would silently check different people than the verifier declared.</span></div>`;
}

function renderBatchVerdicts(out) {
  const verdicts = out.verdicts || [];
  if (!verdicts.length) return `<div class="muted">The batch returned no verdicts.</div>`;
  return `<div class="tblwrap"><table class="tbl">
    <tr><th>#</th><th>Credential</th><th>Verdict</th><th>Tier</th><th>Flags</th></tr>
    ${verdicts.map((v, i) => `<tr>
      <td>${i + 1}</td>
      <td class="mono">${esc(short((v.credentialId || (v.credential || {}).id || "—")))}</td>
      <td>${v.valid ? `<span class="chip sm ok">valid</span>` : `<span class="chip sm err">not valid</span>`}</td>
      <td>${v.valid ? `<span class="chip sm tier${v.tier || 3}">tier ${esc(v.tier ?? "—")}</span>` : "—"}</td>
      <td>${v.revoked ? `<span class="chip sm err">withdrawn</span>` : ""}${(v.contested || []).length ? `<span class="chip sm warn">contested</span>` : ""}</td>
    </tr>`).join("")}
  </table></div>
  <p class="muted" style="margin-top:8px">${verdicts.length} per-credential answer${verdicts.length === 1 ? "" : "s"}. No totals, no rate, no aggregate — deliberately.</p>`;
}

function personView() {
  return `<div class="eyebrow">V-2 · Resolve a person</div>
    <h2 class="scr-title">Resolve a person</h2>
    <p class="body-2">Either of a merged person's ids returns their whole chain of credentials — and nothing about the chain itself. A verifier is never told a merge happened (#104): the join is invisible by design, because "these two identities were once separate" is itself a fact about the worker.</p>
    <div class="card">
      <form id="personform" style="display:flex;flex-direction:column;gap:10px">
        <input name="party" required placeholder="did:crest:party:… (blank field loads nothing; try the fixture worker)" value="${esc(FIX.workerA)}" style="padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px var(--mono)">
        <input name="why" placeholder="why (recorded for the worker)" style="padding:9px 11px;border:1px solid var(--divider);border-radius:6px;font:400 13px Roboto">
        <button class="btn">Resolve the chain</button>
      </form>
      <div id="personout" style="margin-top:12px"></div>
    </div>`;
}

function renderChain(out) {
  const creds = out.credentials || [];
  if (!creds.length) return `<div class="muted">This person's chain holds no credentials yet — an honest empty chain, not an error.</div>`;
  return `<p class="body-2">${esc(String(out.count ?? creds.length))} credential(s) in this person's chain. Each read of this chain wrote a line into the worker's own trail.</p>
    <div class="tblwrap"><table class="tbl">
      <tr><th>Credential</th><th>Activity</th><th>Outcome</th><th>Period</th></tr>
      ${creds.map(c => {
        const we = (c.credentialSubject || {}).workEvent || {};
        return `<tr><td class="mono">${esc(short(c.id))}</td><td>${esc(we.activity || "—")}</td>
          <td>${we.outcome ? esc(we.outcome.value + " " + we.outcome.unit) : "—"}</td>
          <td>${esc(day((we.period || {}).start))}</td></tr>`;
      }).join("")}
    </table></div>`;
}

/* ————— P-10: the external-institution panel ————— */

const CSV_COLUMNS = [
  ["activity", "bednet-distribution"],
  ["outcome_value", "12"],
  ["outcome_unit", "bednets-distributed"],
  ["worker_id_kind", "phone"],
  ["worker_id", "+15550100011"],
  ["period_start", "2026-03-02"],
  ["period_end", "2026-03-02"],
  ["geography", "district-north"],
  ["source_record_ref", "HMIS-2026-03-8841"],
  ["household_id", "HH-101"],
  ["beneficiary_count", "5"],
  ["supervisor_present", "true"],
];

function w6_1() {
  return `<div class="eyebrow">P-10 · External institution · w6_1</div>
    <h2 class="scr-title m">A request for one attestation</h2>
    <p class="body-2">You received an emailed, scoped link: CREST is asking your institution to attest one thing it already knows. The shape is a template out, a file back — the definition's own CSV columns, one row, from your records. You need no CREST account; the link is the scope.</p>
    <div class="eyebrow">The row you would return</div>
    <div class="tblwrap"><table class="tbl">
      <tr><th>Column</th><th>Example</th></tr>
      ${CSV_COLUMNS.map(([c, ex]) => `<tr><td class="mono">${esc(c)}</td><td class="mono">${esc(ex)}</td></tr>`).join("")}
    </table></div>
    <div class="sidecar">${infoIco}<span class="txt">The worker id here is whatever identifier your system holds — never a national ID number. CREST resolves it and keeps only a pairwise reference and a salted hash.</span></div>
    <div class="open-note"><b>Not backed yet.</b> The scoped-link request object — the thing the emailed link would resolve to, carrying which attestation is being asked for and from whom — has no endpoint. The evidence service accepts CSV batches from authenticated submitters (<span class="mono">POST /v1/batches</span>), but nothing issues or honours an external scoped link. This panel is the design's shape only; no file can actually be returned from here.</div>`;
}

function w6_2() {
  return `<div class="eyebrow">P-10 · External institution · w6_2–w6_3</div>
    <h2 class="scr-title m">Three tiers, four kinds of evidence</h2>
    <p class="body-2">What your attestation is worth is not decided by you, and not stored anywhere — it is computed from provenance every time someone checks.</p>
    <div class="card">
      <div style="display:flex;gap:8px;align-items:flex-start"><span class="chip tier1">Tier 1</span>
        <span class="body-2"><b>Outcome-linked.</b> The claim rides a record in a system that exists for its own reasons — a health information system, a stock ledger. The evidence would exist whether or not anyone was paid for it.</span></div>
    </div>
    <div class="card">
      <div style="display:flex;gap:8px;align-items:flex-start"><span class="chip tier2">Tier 2</span>
        <span class="body-2"><b>Supervisor-entered.</b> A named person with standing attested it. Checkable against that person's authorization, contestable to their name.</span></div>
    </div>
    <div class="card">
      <div style="display:flex;gap:8px;align-items:flex-start"><span class="chip tier3">Tier 3</span>
        <span class="body-2"><b>Worker-asserted.</b> The worker's own account, held as exactly that. Real, kept, and labelled — never dressed up as more.</span></div>
    </div>
    <div class="eyebrow">Where your institution sits</div>
    <div class="card hi">
      <p class="body-2"><b>External institutions are capped at Tier 2</b> — the §16 ruling. An institution's account of its own capture is one nobody here can check: your system may well be outcome-linked from where you stand, but from CREST's side that linkage is your assertion about yourself. Tier 1 needs provenance a verifier can trace past the teller — a registered, assessed source. An external attestation arrives as the word of a named institution, and the word of a named institution is what Tier 2 means.</p>
    </div>
    <p class="muted">The tier is derived at query time from provenance facts on the credential (<span class="mono">sourceClass</span>, <span class="mono">captureMethod</span>, <span class="mono">adapterRef</span>) against the definition's public tier map. If your source is later registered and assessed, existing credentials rise with it — nothing is reissued, because nothing was stored.</p>`;
}

/* ————— router / render ————— */

const SCREENS = { v1_1, v1_2, v1_3, v2_1, v2_2, v2_3, person: personView, w6_1, w6_2 };

async function render() {
  const r = route();
  if (ORG_ROUTES.has(r)) {
    if (!S.orgSession) {
      app.innerHTML = consoleShell(r, `<div class="muted">Signing in as the onboarded institution…</div>`);
      try { await ensureOrg(); } catch (e) { fail(e); }
    }
  } else if (S.orgSession && !PANEL_ROUTES.has(r)) {
    // V-1 is deliberately logged out: drop the org session when leaving V-2.
    setSession(null); S.orgSession = false;
  }
  let content;
  try { content = SCREENS[r](); } catch (e) { fail(e); content = ""; }
  app.innerHTML = PANEL_ROUTES.has(r) ? panelShell(r, content) : consoleShell(r, content);
  S.err = null;
  bind(r);
}

function on(sel, ev, fn) { app.querySelectorAll(sel).forEach(el => el.addEventListener(ev, fn)); }

function bind(r) {
  on("[data-go]", "click", e => { location.hash = "#/" + e.currentTarget.dataset.go; });

  // v1_2 — the bare check
  const ls = document.getElementById("loadsample");
  ls && ls.addEventListener("click", async () => {
    const f = document.getElementById("verifyform");
    try { f.cred.value = JSON.stringify(await loadSampleCredential(f.sampleparty.value.trim()), null, 2); }
    catch (e) { fail(e); render(); }
  });
  const vf = document.getElementById("verifyform");
  vf && vf.addEventListener("submit", async ev => {
    ev.preventDefault();
    try {
      const cred = JSON.parse(vf.cred.value);
      await runVerify(cred, vf.who.value.trim(), vf.why.value.trim());
      location.hash = "#/v1_3";
      if (route() === "v1_3") render();
    } catch (e) { fail(e); render(); }
  });

  // v2_2 — institutional check with disclosure rendering
  const ols = document.getElementById("orgloadsample");
  ols && ols.addEventListener("click", async () => {
    const f = document.getElementById("orgverifyform");
    try { f.cred.value = JSON.stringify(await loadSampleCredential(f.sampleparty.value.trim()), null, 2); }
    catch (e) { fail(e); render(); }
  });
  const ovf = document.getElementById("orgverifyform");
  ovf && ovf.addEventListener("submit", async ev => {
    ev.preventDefault();
    try {
      const cred = JSON.parse(ovf.cred.value);
      await runVerify(cred, FIX.org, ovf.why.value.trim());
      render();
    } catch (e) { fail(e); render(); }
  });

  // v2_3 — the bounded batch
  const bls = document.getElementById("batchloadsample");
  bls && bls.addEventListener("click", async () => {
    const f = document.getElementById("batchform");
    try {
      const pid = f.sampleparty.value.trim() || FIX.workerA;
      const out = await api.get("verification", `/v1/parties/${encodeURIComponent(pid)}/credentials`);
      const creds = out.credentials || [];
      if (!creds.length) throw new Error("that person's chain holds no credentials yet");
      f.creds.value = JSON.stringify(creds, null, 2);
    } catch (e) { fail(e); render(); }
  });
  const bf = document.getElementById("batchform");
  if (bf) {
    const count = document.getElementById("purposecount");
    bf.why.addEventListener("input", () => {
      const n = bf.why.value.length;
      count.textContent = `${n} / 200 — ${n < 10 ? "at least 10" : "long enough"}`;
    });
    bf.addEventListener("submit", async ev => {
      ev.preventDefault();
      const purpose = bf.why.value.trim();
      if (purpose.length < 10 || purpose.length > 200) {
        document.getElementById("batchout").innerHTML =
          `<div class="errbar">The purpose must be 10–200 characters — each worker reads it in their own trail, so it has to say something.</div>`;
        return;
      }
      try {
        const creds = JSON.parse(bf.creds.value);
        if (!Array.isArray(creds)) throw new Error("the batch must be a JSON array of credentials");
        if (creds.length > 100) {
          document.getElementById("batchout").innerHTML =
            `<div class="errbar">${creds.length} credentials exceeds the deployment's batch cap (L1 default 100, #107). The batch is refused whole — nothing was checked.</div>`;
          return;
        }
        const out = await api.post("verification", "/v1/verify/batch",
          { credentials: creds, requestedByPartyId: FIX.org, purpose });
        document.getElementById("batchout").innerHTML = renderBatchVerdicts(out);
      } catch (e) { fail(e); render(); }
    });
  }

  // person — the chain read
  const pf = document.getElementById("personform");
  pf && pf.addEventListener("submit", async ev => {
    ev.preventDefault();
    try {
      const q = new URLSearchParams({ requestedByPartyId: FIX.org });
      if (pf.why.value.trim()) q.set("purpose", pf.why.value.trim());
      const out = await api.get("verification", `/v1/parties/${encodeURIComponent(pf.party.value.trim())}/credentials?` + q);
      document.getElementById("personout").innerHTML = renderChain(out);
    } catch (e) {
      document.getElementById("personout").innerHTML = e instanceof ApiError && e.status === 404
        ? `<div class="muted">Nobody resolves to that id — and whether it once merged into another is deliberately not said.</div>`
        : `<div class="errbar">${esc(e.message || e)}</div>`;
    }
  });
}

window.addEventListener("hashchange", render);
render();
