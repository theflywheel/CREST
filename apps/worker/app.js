// CREST worker wallet (apps/worker) — journey J7, "A worker, end to end"
// (screens w1_1–w1_24 of docs/reference/CREST — Actor Journeys_17Aug.html),
// rebuilt as a real product surface over live endpoints.
//
// Every screen is backed by an endpoint that exists, or says plainly that
// none does. Nothing here invents live-looking data.

import { api, ApiError, loginAs, setSession } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";

/* ————— helpers ————— */
const esc = x => String(x ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
const short = id => { const s = String(id || ""); return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-6) : s; };
const money = (minor, cur) => (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2 }) + " " + (cur || "");
const when = ts => ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short" }) : "—";
const whenFull = ts => ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "—";
const daysOld = ts => ts ? Math.max(0, Math.floor((Date.now() - new Date(ts).getTime()) / 86400000)) : 0;
const monthOf = ts => ts ? new Date(ts).toLocaleDateString(undefined, { month: "long", year: "numeric" }) : "Undated";

const app = document.getElementById("app");

// Session + per-render caches (route views stash lists here so detail
// screens can index into them without refetching).
const S = { me: null, err: null, flash: null, creds: [], instr: [], windows: [], face: null, rate: null };

function fail(e) {
  S.err = e instanceof ApiError ? `${e.status} ${e.code || ""} — ${e.message}` : String(e && e.message || e);
}

/* ————— shell pieces ————— */
const ICONS = {
  home: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10.5 12 3l9 7.5"/><path d="M5.5 9.5V20h13V9.5"/></svg>`,
  work: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="7" width="17" height="13" rx="2"/><path d="M8.5 7V5.5A1.5 1.5 0 0 1 10 4h4a1.5 1.5 0 0 1 1.5 1.5V7"/></svg>`,
  wallet: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="6" width="17" height="13" rx="2"/><path d="M15 12.5h5.5"/><circle cx="15.5" cy="12.5" r=".4"/></svg>`,
  profile: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8.5" r="3.5"/><path d="M5 20c.8-3.4 3.6-5 7-5s6.2 1.6 7 5"/></svg>`,
};

const TABS = [["home", "Home"], ["work", "Work"], ["wallet", "Wallet"], ["profile", "Profile"]];

function tabOf(route) {
  const r = route[0] || "home";
  if (r === "pay") return "wallet";
  return TABS.some(([k]) => k === r) ? r : "home";
}

function shell(route, title, backTo, content, headRight) {
  const active = tabOf(route);
  return `<div class="mobile-shell">
    <div class="statusbar"><span>9:41</span><span class="sig">▮▮▮ ▮▮ ▰</span></div>
    <div class="mhead">
      ${backTo ? `<button class="back" data-go="${esc(backTo)}">←</button>` : ""}
      <span class="t">${esc(title)}</span>
      <span class="right">${headRight || ""}</span>
    </div>
    <div class="mbody screen">
      ${S.err ? `<div class="errbar">${esc(S.err)}</div>` : ""}
      ${content}
    </div>
    <div class="bottomnav">
      ${TABS.map(([k, label]) => `<button class="${active === k ? "active" : ""}" data-go="#/${k}">${ICONS[k]}<span>${label}</span></button>`).join("")}
    </div>
  </div>`;
}

// The "What happens next" block — closes every terminal action (the
// journeys' rule: no action ends on a spinner or a bare toast).
function nextBlock({ happened, who, whenRow, told, ifnot }) {
  return `<div class="next">
    <span class="eyebrow">What happens next</span>
    <div class="nrow"><span class="k">What just happened</span><span class="v">${happened}</span></div>
    <div class="nrow"><span class="k">Who acts next</span><span class="v">${who}</span></div>
    <div class="nrow"><span class="k">When</span><span class="v">${whenRow}</span></div>
    <div class="nrow"><span class="k">How you will be told</span><span class="v">${told}</span></div>
    <div class="ifnot"><b>If nothing happens ·</b> ${ifnot}</div>
  </div>`;
}

const sideIco = `<svg class="ico" viewBox="0 0 15 15"><circle cx="7.5" cy="7.5" r="6.5"/><line x1="7.5" y1="7" x2="7.5" y2="11"/><line x1="7.5" y1="4.2" x2="7.5" y2="4.3"/></svg>`;
const sidecar = (txt, ok) => `<div class="sidecar${ok ? " ok" : ""}">${sideIco}<span class="txt">${txt}</span></div>`;

const tick = `<span class="tick"><svg viewBox="0 0 10 10" fill="none" stroke="#fff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M1.5 5.5 4 8l4.5-6"/></svg></span>`;
const disLi = (on, t, s) => `<div class="li${on ? "" : " off"}">${on ? tick : `<span class="tick"></span>`}<span><div class="t">${esc(t)}</div><div class="s">${esc(s)}</div></span></div>`;

const openNote = txt => `<div class="open-note">${txt}</div>`;
const ussd = `<p class="muted" style="text-align:center">Dial <b class="mono">*384*77#</b> to hear this on any phone — the channel-parity promise (#29): every screen here has a voice and USSD equivalent.</p>`;

/* ————— data loaders (each returns [] / null on failure, never throws) ————— */
const soft = p => p.catch(() => null);

async function loadCreds() {
  const out = await soft(api.get("verification", `/v1/parties/${encodeURIComponent(S.me)}/credentials`));
  S.creds = (out && out.credentials) || [];
  return S.creds;
}
async function loadWindows() {
  const out = await soft(api.get("confirmation", `/v1/windows?partyId=${encodeURIComponent(S.me)}`));
  S.windows = (out && out.windows) || [];
  return S.windows;
}
async function loadInstr() {
  const out = await soft(api.get("payments", `/v1/instructions?partyId=${encodeURIComponent(S.me)}`));
  S.instr = (out && out.instructions) || [];
  return S.instr;
}
async function loadFace() {
  S.face = await soft(api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/faces/worker`));
  const lr = await soft(api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/linked-records?type=payment-setup`));
  S.rate = (((lr && lr.linkedRecords) || [])[0] || {}).payload || null;
  return S.face;
}

// Tier, read defensively — trust strength is derived at query time, never
// stored; wherever the service surfaces it, we display, and where it does
// not, we say "tier not derivable here" rather than compute one clientside.
function tierOf(c) {
  const t = c.derivedTier || c.trustTier || (c.trust && c.trust.tier) || (c.proof && c.proof.trustTier) || null;
  const cm = (c.credentialSubject && c.credentialSubject.provenance && c.credentialSubject.provenance.captureMethod)
    || (c.provenance && c.provenance.captureMethod) || (c.evidence && c.evidence.captureMethod) || null;
  return { tier: t ? String(t).replace(/^tier[-_ ]?/i, "") : null, captureMethod: cm };
}
function tierChip(c) {
  const { tier, captureMethod } = tierOf(c);
  if (!tier) return `<span class="chip plain">Tier derived when checked</span>`;
  const cls = tier === "1" ? "tier1" : tier === "2" ? "tier2" : "tier3";
  return `<span class="chip ${cls}">Tier ${esc(tier)}${captureMethod ? " · " + esc(captureMethod.replace(/-/g, " ")) : ""}</span>`;
}

/* ————— screens ————— */

function loginView() {
  return `<div class="mobile-shell">
    <div class="statusbar"><span>9:41</span><span class="sig">▮▮▮ ▮▮ ▰</span></div>
    <div class="mbody screen" style="justify-content:center">
      <span class="eyebrow">CREST · Worker</span>
      <h1 class="scr-title">Your work, on the record.<br>Your money, explained.</h1>
      <p class="muted">This is the dev login. It stands in for eSignet: in a real deployment you would tap "Continue with eSignet" and prove who you are to the national identity system — CREST would only ever see a pairwise reference, never your ID number. Here, you just pick the person.</p>
      <div class="card hi">
        <div class="person-name">Grace</div>
        <p class="muted">Community health worker · bednet distribution, PRJ-118</p>
        <div style="height:10px"></div>
        <button class="btn" id="login-grace">Continue as Grace</button>
      </div>
      ${sidecar(`Signing in never uploads anything about you. It only proves to CREST that the person holding this phone is the person the record is about.`)}
      ${ussd}
    </div>
  </div>`;
}

/* w1_8 — home */
async function homeView() {
  const [creds, wins] = await Promise.all([loadCreds(), loadWindows(), loadFace()]);
  const open = wins.filter(w => !w.exitRoute);
  const f = S.face, rate = S.rate;
  const workCard = f ? `<div class="card hi">
      <span class="eyebrow">Available work</span>
      <div style="font:500 15px/1.4 Roboto;margin-top:4px">${esc((f.activity && f.activity.label) || f.activity || "Work")}</div>
      <p class="body-2">${esc((f.worker && f.worker.summary) || "")}</p>
      <div class="kv" style="margin-top:8px">
        <div class="row"><span class="k">Counted in</span><span class="v">${esc(f.outcomeUnit || "")}</span></div>
        <div class="row"><span class="k">One unit pays</span><span class="v">${rate ? esc(money(rate.ratePerOutcomeUnit.amountMinor, rate.ratePerOutcomeUnit.currency)) : "no rate attached — this work is recognised; recognition is a use of its own"}</span></div>
        <div class="row"><span class="k">Definition</span><span class="v mono">${esc(short(f.definitionId))} · v${esc(f.version)}</span></div>
      </div>
      <div style="height:10px"></div>
      <button class="btn" data-go="#/work">Open the campaign</button>
    </div>` : openNote(`The definitions service did not answer, so the campaign card cannot be drawn. Nothing is shown in its place.`);
  return `
    <span class="eyebrow">${esc(whenFull(Date.now()))}</span>
    <div class="person-name">Grace</div>
    <div class="stats">
      <div class="stat"><div class="n">${creds.length}</div><div class="l">credentials held</div></div>
      <div class="stat"><div class="n">${open.length}</div><div class="l">open confirmation ${open.length === 1 ? "window" : "windows"}</div></div>
    </div>
    ${open.length ? `<div class="card" style="border-color:var(--warning)">
      <span class="chip warn sm">Waiting for your say</span>
      <p class="body-2" style="margin-top:6px">${open.length === 1 ? "A record of your work" : open.length + " records of your work"} will count after you have had your say — or after seven days pass. You are paid either way.</p>
      <div style="height:8px"></div><button class="btn secondary" data-go="#/work">See what was recorded</button>
    </div>` : ""}
    ${workCard}
    <div class="spacer"></div>
    ${ussd}`;
}

/* w1_9 + w1_10 — work tab */
async function workView() {
  await Promise.all([loadFace(), loadWindows()]);
  const f = S.face, rate = S.rate;
  const open = S.windows.filter(w => !w.exitRoute);
  const closed = S.windows.filter(w => w.exitRoute);
  const wkr = (f && f.worker) || {};
  return `
    <h2 class="scr-title m">What counts as done</h2>
    ${f ? `<div class="card">
      <div style="display:flex;justify-content:space-between;gap:10px;align-items:flex-start">
        <div style="font:500 15px/1.4 Roboto">${esc((f.activity && f.activity.label) || f.activity || "")}</div>
        <span class="chip info sm">v${esc(f.version)} · ${esc(f.state || "")}</span>
      </div>
      <p class="body-2" style="margin-top:4px">${esc(wkr.summary || "")}</p>
      <div class="kv" style="margin-top:8px">
        <div class="row"><span class="k">Counted in</span><span class="v">${esc(f.outcomeUnit || "")}</span></div>
        <div class="row"><span class="k">One unit pays</span><span class="v">${rate ? esc(money(rate.ratePerOutcomeUnit.amountMinor, rate.ratePerOutcomeUnit.currency)) : "no rate attached"}</span></div>
      </div>
    </div>
    <div class="card">
      <span class="eyebrow">What stands as evidence</span>
      <div style="height:8px"></div>
      <div class="dis">${((wkr.evidenceInPlainLanguage || []).map(l => disLi(true, l, "counts as evidence")).join("")) || `<p class="muted">The definition carries no worker-language evidence list.</p>`}</div>
      <p class="muted" style="margin-top:10px">Nothing on this page asks you to enter work into CREST — that path does not exist, and will not be built. Evidence arrives from the programme's systems; your part is the say you get before it counts.</p>
    </div>` : openNote(`The definitions service did not answer; the definition cannot be shown.`)}

    <h2 class="scr-title m">Waiting for your say</h2>
    ${S.flash && S.flash.route === "work" ? S.flash.html : ""}
    ${open.length ? open.map(w => `<div class="card">
      <div style="display:flex;justify-content:space-between;gap:10px">
        <span style="font:500 14px/1.4 Roboto">A record of your work</span>
        <span class="chip warn sm">reply by ${esc(when(w.closesAt))}</span>
      </div>
      <div class="mono" style="margin:4px 0 2px;color:var(--text-2)">${esc(short(w.claimId))}</div>
      <p class="body-2">Confirm it, tell us what does not match, or let seven days pass. <b>You are paid on every one of those paths</b> — a dispute contests the record, never the money.</p>
      <div style="height:8px"></div>
      <div class="btn-row">
        <button class="btn dominant" data-confirm="${esc(w.claimId)}">It is right</button>
        <button class="btn secondary" data-go="#/work/dispute/${encodeURIComponent(w.claimId)}">Tell us what does not match</button>
      </div>
    </div>`).join("") : `<div class="card quiet"><p class="body-2">Nothing is waiting for you. When new work is recorded about you, it appears here before it counts — and a message reaches you on every channel you have.</p></div>`}

    ${closed.length ? `<div class="card">
      <span class="eyebrow">Settled</span>
      <div style="height:8px"></div>
      <div class="kv">${closed.map(w => `<div class="row">
        <span class="k mono">${esc(short(w.claimId))}</span>
        <span class="v"><span class="chip sm ${w.exitRoute === "dispute" ? "warn" : "ok"}">${esc(w.exitRoute)}</span>${w.paymentReleasedAt ? ` <span class="chip sm ok">paid</span>` : ""}</span>
      </div>`).join("")}</div>
      <p class="muted" style="margin-top:8px">Every exit — confirm, dispute, auto-confirm after seven days, or a supervisor confirming with you — releases the payment. All four.</p>
    </div>` : ""}
    <p class="muted"><a href="#/work/declined">Work you declined</a> — what saying no looks like here.</p>`;
}

/* w1_11 — dispute */
async function disputeView(claimId) {
  return `
    <h2 class="scr-title m">Tell us what does not match</h2>
    <p class="body-2">You are disputing the <i>record</i>, not your payment. <b>Your payment is released either way</b> — CREST holds that as a rule, not a courtesy.</p>
    <div class="kv"><div class="row"><span class="k">Record</span><span class="v mono">${esc(short(claimId))}</span></div></div>
    <form id="dispute-form" data-claim="${esc(claimId)}" style="display:flex;flex-direction:column;gap:10px">
      <textarea name="reason" rows="4" required placeholder="What is wrong? The count, the day, the place — say it in your own words." style="font:inherit;padding:11px 13px;border:1px solid var(--divider);border-radius:8px;resize:vertical"></textarea>
      <button class="btn" type="submit">Send the dispute</button>
      <button class="btn secondary" type="button" data-go="#/work">Go back</button>
    </form>
    ${sidecar(`A disputed record is never destroyed. Your dispute sits beside it, visible to anyone who checks, until the issuer answers.`)}`;
}

/* w1_14 — declined work (no backend) */
function declinedView() {
  return `
    <h2 class="scr-title m">Work you declined</h2>
    ${openNote(`<b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_14) show declined offers kept on your side only, never on your record. The services expose no offers or declines API today; when one lands it belongs to the definitions/notify surface. Nothing is drawn here because nothing real exists to draw.`)}
    <p class="body-2">The promise this screen will keep: declining work is not recorded about you. A verifier can never see what you said no to.</p>`;
}

/* w1_12 — wallet list */
async function walletView() {
  const creds = await loadCreds();
  return `
    <h2 class="scr-title m">My credentials</h2>
    <p class="muted">Each one is a signed document you hold. It is provable to a stranger in a minute, offline — it does not need CREST to be believed.</p>
    ${creds.length ? creds.map((c, i) => {
      const we = (c.credentialSubject || {}).workEvent || {};
      return `<button class="card" style="text-align:left;width:100%" data-go="#/wallet/${i}">
        <div style="display:flex;justify-content:space-between;gap:10px;align-items:flex-start">
          <span style="font:500 14.5px/1.4 Roboto">${esc(we.activity || "Work event")}</span>
          ${tierChip(c)}
        </div>
        <div class="muted">${esc(we.outcome ? we.outcome.value + " " + we.outcome.unit : "")} · ${esc(when((we.period || {}).start))}</div>
        <div class="mono" style="color:var(--text-2);margin-top:3px">${esc(short(c.id))}</div>
      </button>`;
    }).join("") : `<div class="card quiet"><p class="body-2">No credentials yet. One is issued each time a confirmation window closes — after you have had your say, or seven days have passed.</p></div>`}
    ${sidecar(`Long-term custody of these credentials belongs to your <b>Inji wallet</b> — the deployed browser wallet at <a href="https://crest-inji-web-production.up.railway.app" target="_blank" rel="noopener">crest-inji-web-production.up.railway.app</a>. This tab is the CREST-side view of the same credentials, not a second copy you must manage. One gap, named: the demo fleet does not yet issue through Certify/OpenID4VCI, so the import path into Inji is not wired (blueprint §5, docs/crest-inji-architecture.html).`)}
    <p class="muted"><a href="#/wallet/share">Share a link instead of showing the phone</a> · <a href="#/wallet/deferred">A skill still being assessed</a></p>`;
}

/* w1_18 — credential detail */
async function credView(idx) {
  if (!S.creds.length) await loadCreds();
  const c = S.creds[idx];
  if (!c) return `<div class="card quiet"><p class="body-2">That credential is not in your wallet. <a href="#/wallet">Back to the wallet.</a></p></div>`;
  const we = (c.credentialSubject || {}).workEvent || {};
  const issuer = typeof c.issuer === "object" ? (c.issuer.name || c.issuer.id) : c.issuer;
  const ceiling = c.tierCeiling || (c.trust && c.trust.tierCeiling) || null;
  return `
    <h2 class="scr-title m">${esc(we.activity || "Credential")}</h2>
    ${tierChip(c)}
    <div class="kv">
      <div class="row"><span class="k">Issued by</span><span class="v">${esc(issuer || "—")}</span></div>
      <div class="row"><span class="k">Under definition</span><span class="v mono">${esc(short(we.definitionId || c.definitionId || FIX.definition))}${we.definitionVersion ? " · v" + esc(we.definitionVersion) : ""}</span></div>
      <div class="row"><span class="k">Tier ceiling</span><span class="v">${ceiling ? esc(String(ceiling)) : "derived when a verifier checks — never stored"}</span></div>
      <div class="row"><span class="k">Skill code</span><span class="v mono">${esc(we.skillCode || "—")}</span></div>
      <div class="row"><span class="k">Outcome</span><span class="v">${esc(we.outcome ? we.outcome.value + " " + we.outcome.unit : "—")}</span></div>
      <div class="row"><span class="k">Credential id</span><span class="v mono">${esc(short(c.id))}</span></div>
    </div>
    ${sidecar(`This credential is yours. It resolves without trusting CREST — the signature, not the server, is what a verifier believes.`, true)}
    <div class="btn-row">
      <button class="btn secondary" data-go="#/pay">Payment</button>
      <button class="btn" data-go="#/wallet/${idx}/show">Show to someone</button>
    </div>`;
}

/* the "show to someone" face of a credential */
async function credShowView(idx) {
  if (!S.creds.length) await loadCreds();
  const c = S.creds[idx];
  if (!c) return `<div class="card quiet"><p class="body-2">That credential is not in your wallet. <a href="#/wallet">Back.</a></p></div>`;
  return `
    <h2 class="scr-title m">Show to someone</h2>
    <p class="body-2">Hand them the phone, or let them scan the printed card. This — and only this — is what a scan gives away:</p>
    <div class="card"><div class="dis">
      ${disLi(true, "That the work happened", "the activity, the outcome, the period")}
      ${disLi(true, "That it was confirmed", "how the window closed, and the trust tier they derive")}
      ${disLi(true, "The issuer's signature", "checkable offline, without asking CREST")}
      ${disLi(false, "Your name", "the credential names a pairwise reference, not you")}
      ${disLi(false, "Your ID number or biometrics", "CREST never held them, so a scan cannot leak them")}
      ${disLi(false, "Your other work, or your pay", "one credential proves one thing")}
    </div></div>
    <div class="card"><span class="eyebrow">The signed document itself</span>
      <div style="overflow-x:auto;margin-top:8px"><pre class="mono" style="font-size:10.5px;line-height:1.5">${esc(JSON.stringify(c, null, 2))}</pre></div>
    </div>
    ${sidecar(`Every scan leaves a line in "Who checked me", on your Profile — even a failed one, even inside a batch.`)}`;
}

/* w1_16/17 — deferred qualification (no backend) */
function deferredView() {
  return `
    <h2 class="scr-title m">A skill still being assessed</h2>
    ${openNote(`<b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_16–w1_17) show a qualification that arrives later than the work it rests on, with the wallet honest about the gap. No verification or definitions endpoint exposes deferred qualifications today; Blueprint §7 (definition faces) is where it will hang. Nothing live is drawn.`)}
    <p class="body-2">The promise this screen will keep: while an assessment is pending, the wallet says "being assessed" — it never shows a credential that does not exist, and never hides the work that does.</p>`;
}

/* w1_19/20 — share links (no backend) */
function shareView() {
  return `
    <h2 class="scr-title m">Share a link instead</h2>
    ${openNote(`<b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_19–w1_20) show a time-boxed share link — "anyone with this link, for 7 days, sees these two credentials and nothing else." The verification service has no share-link endpoint today. Nothing live is drawn.`)}
    <p class="body-2">The promise this screen will keep: a link you can revoke, scoped to exactly what you chose, with every open of it logged in "Who checked me".</p>`;
}

/* w1_13 / w1_23 / w1_24 — payments */
async function payView() {
  const list = await loadInstr();
  const held = list.filter(i => i.held);
  const flowing = list.filter(i => !i.held);
  const groups = {};
  for (const i of flowing) (groups[monthOf(i.releasedAt || i.createdAt)] ||= []).push(i);
  return `
    <h2 class="scr-title m">My money</h2>
    ${S.flash && S.flash.route === "pay" ? S.flash.html : ""}
    ${!list.length ? `<div class="card quiet"><p class="body-2">No payments yet. A payment instruction is created the moment your confirmation window closes — on any of its four exits.</p></div>` : ""}

    ${Object.entries(groups).map(([m, items]) => `
      <span class="eyebrow">${esc(m)}</span>
      ${items.map(i => {
        const idx = S.instr.indexOf(i);
        return `<button class="card" style="text-align:left;width:100%" data-go="#/pay/${idx}">
          <div style="display:flex;justify-content:space-between;align-items:center;gap:10px">
            <span style="font:500 15px/1.3 Roboto">${esc(money(i.amountMinor, i.currency))}</span>
            <span class="chip sm ${i.state === "RELEASED" ? "ok" : "info"}">${esc(i.state || "")}</span>
          </div>
          <div class="muted">${esc(i.releasedBy ? "via " + i.releasedBy : "")} · ${esc(when(i.releasedAt || i.createdAt))}</div>
        </button>`;
      }).join("")}`).join("")}

    ${held.length ? `
      <h2 class="scr-title m">What is waiting</h2>
      ${held.map(i => {
        const age = daysOld(i.heldAt || i.createdAt);
        return `<div class="held">
          <div class="top">
            <span class="amt">${esc(money(i.amountMinor, i.currency))}</span>
            <span class="chip sm ${age >= 7 ? "err" : "warn"}">${age} day${age === 1 ? "" : "s"}</span>
          </div>
          <div class="why">${esc(i.held.explanation || i.held.code || "Held — and the record carries why.")}</div>
          <div class="who">Waiting on: ${esc(short(i.held.ownerPartyId || "") || "a named owner")}</div>
        </div>`;
      }).join("")}
      ${sidecar(`You do not need to do anything about these — your project support agent can.`, true)}
      ${sidecar(`Nothing here is a mark against you. Being held is a problem between two offices, not a judgement about your work.`)}
    ` : ""}
    ${list.length ? sidecar(`Every held payment carries a reason and a named owner — the record itself refuses one without both.`) : ""}`;
}

/* payment detail — w1_13 */
async function payDetailView(idx) {
  if (!S.instr.length) await loadInstr();
  const i = S.instr[idx];
  if (!i) return `<div class="card quiet"><p class="body-2">That payment is not on your record. <a href="#/pay">Back to My money.</a></p></div>`;
  const released = i.state === "RELEASED";
  return `
    <span class="eyebrow">${esc(monthOf(i.releasedAt || i.createdAt))}</span>
    <div class="big-amount">${esc(money(i.amountMinor, i.currency))}</div>
    <span class="chip ${released ? "ok" : i.held ? "warn" : "info"}" style="align-self:flex-start">${esc(i.state || "")}</span>
    <div class="tline" style="margin-top:6px">
      <div class="step"><div class="rail"><div class="dot"></div><div class="conn"></div></div>
        <div><div class="lbl">Credential signed</div><div class="meta">Your confirmation window closed; the record became a signed credential.</div></div></div>
      <div class="step${released ? "" : " active"}"><div class="rail"><div class="dot"></div><div class="conn"></div></div>
        <div><div class="lbl">Amount calculated</div><div class="meta">${esc(money(i.amountMinor, i.currency))} — from the rate attached to the definition, versioned, never overwritten.</div></div></div>
      <div class="step${released ? "" : " todo"}"><div class="rail"><div class="dot"></div></div>
        <div><div class="lbl">Sent to rail</div><div class="meta">${released ? esc((i.releasedBy ? "Via " + i.releasedBy + " · " : "") + whenFull(i.releasedAt)) : i.held ? "Held — see the reason below." : "Not yet sent."}</div></div></div>
    </div>
    ${i.held ? `<div class="held">
      <div class="why">${esc(i.held.explanation || i.held.code || "")}</div>
      <div class="who">Waiting on: ${esc(short(i.held.ownerPartyId || "") || "a named owner")}</div>
    </div>` : ""}
    <div class="kv">
      <div class="row"><span class="k">Instruction</span><span class="v mono">${esc(short(i.id))}</span></div>
      <div class="row"><span class="k">For claim</span><span class="v mono">${esc(short(i.claimId || "—"))}</span></div>
    </div>
    ${sidecar(`CREST did not move this money — it told M-Pesa to. What you see here is the instruction and its trace, which is exactly what a delayed payment needs you to have.`)}`;
}

/* ————— profile tab ————— */
function profileView() {
  const rows = [
    ["#/profile/consents", "What I agreed to", "Consent, per programme — and withdrawing it"],
    ["#/profile/checks", "Who checked me", "Every look at your record leaves a line"],
    ["#/profile/messages", "Messages to me", "Everything the system ever sent you, kept"],
    ["#/profile/recovery", "If I lose this phone", "The people who can confirm it is you"],
  ];
  return `
    <h2 class="scr-title m">Profile</h2>
    <div class="person-name">Grace</div>
    <div class="mono" style="color:var(--text-2)">${esc(S.me)}</div>
    ${rows.map(([go, t, s]) => `<button class="card" style="text-align:left;width:100%" data-go="${go}">
      <div style="font:500 14px/1.4 Roboto">${esc(t)}</div><div class="muted">${esc(s)}</div>
    </button>`).join("")}
    <div class="spacer"></div>
    <button class="btn secondary" id="logout">Sign out</button>
    ${ussd}`;
}

async function consentsView() {
  const out = await soft(api.get("parties", `/v1/parties/${encodeURIComponent(S.me)}/consents`));
  const list = (out && out.consents) || [];
  const states = (out && out.enrolmentConsent) || {};
  return `
    <h2 class="scr-title m">What I agreed to</h2>
    <p class="body-2">Consent is per programme. Withdrawing it stops new evidence being collected about you — it never touches what you were already paid.</p>
    ${Object.keys(states).length ? `<div class="kv">${Object.entries(states).map(([ctx, st]) => `<div class="row">
      <span class="k mono">${esc(short(ctx))}</span>
      <span class="v"><span class="chip sm ${st === "GRANTED" ? "ok" : "warn"}">${esc(st)}</span></span>
    </div>`).join("")}</div>` : ""}
    ${list.length ? list.map(c => `<div class="card">
      <div style="display:flex;justify-content:space-between;gap:10px">
        <span style="font:500 13.5px/1.4 Roboto">${esc(c.moment || c.kind || "consent")}</span>
        <span class="chip sm ${c.state === "GRANTED" ? "ok" : "warn"}">${esc(c.state || "")}</span>
      </div>
      <div class="muted">${esc(c.captureMethod || "")} · ${esc(when(c.capturedAt || c.createdAt))}</div>
      ${c.state === "GRANTED" ? `<div style="height:8px"></div><button class="btn secondary" data-withdraw="${esc(c.id)}">Withdraw this consent</button>` : ""}
    </div>`).join("") : (!out ? openNote(`The parties service did not answer; your consents cannot be shown right now.`) : `<div class="card quiet"><p class="body-2">No consent record is stored about you. Nothing here means nothing was captured — ask your registering agent if that seems wrong.</p></div>`)}
    ${S.flash && S.flash.route === "consents" ? S.flash.html : ""}
    <div class="consent-quote">"CREST keeps a record of work you do, so you can prove it and be paid for it. It never stores your ID number. You can say stop at any time, and stopping never takes back money you were paid."<br><span class="muted">— the script read to you when you enrolled</span></div>`;
}

async function checksView() {
  const out = await soft(api.get("verification", `/v1/presentations?subjectRef=${encodeURIComponent(S.me)}`));
  const list = (out && out.presentations) || [];
  return `
    <h2 class="scr-title m">Who checked me</h2>
    <p class="body-2">Every look at your record leaves a line here — one per credential, even inside a batch, and even when the check failed.</p>
    ${list.length ? `<div class="kv">${list.map(p => `<div class="row">
      <span class="k">${esc(when(p.createdAt))}</span>
      <span class="v">${esc(short(p.requestedByPartyId) || "(bare scan)")} · ${esc(p.purpose || "no purpose given")} · <span class="chip sm ${p.outcome === "valid" ? "ok" : "plain"}">${esc(p.outcome || "")}</span></span>
    </div>`).join("")}</div>` : (!out ? openNote(`The verification service did not answer; the trail cannot be shown right now.`) : `<div class="card quiet"><p class="body-2">Nobody has checked your record. When someone does — a scan, a batch check, anything — the line appears here whether or not they say who they are.</p></div>`)}`;
}

async function messagesView() {
  const out = await soft(api.get("notify", `/v1/notifications?partyId=${encodeURIComponent(S.me)}`));
  const list = (out && out.notifications) || [];
  return `
    <h2 class="scr-title m">Messages to me</h2>
    <p class="body-2">Every message the system sent you, kept — so "you were told" is checkable, in both directions.</p>
    ${list.length ? list.map(n => `<div class="card">
      <div style="display:flex;justify-content:space-between;gap:10px">
        <span style="font:500 13.5px/1.4 Roboto">${esc(n.kind || "message")}</span>
        <span class="chip sm ${n.state === "SENT" ? "ok" : "info"}">${esc(n.state || "")}</span>
      </div>
      <p class="body-2" style="margin-top:4px">${esc(n.body || "")}</p>
      <div class="muted">via ${esc(n.channel || "")} to ${esc(n.destination || "")} · ${esc(when(n.createdAt))}</div>
    </div>`).join("") : (!out ? openNote(`The notify service did not answer; your messages cannot be shown right now.`) : `<div class="card quiet"><p class="body-2">No messages yet. When a record of your work opens its seven-day window, the message that tells you lands here too.</p></div>`)}`;
}

/* w1_7 — recovery contacts */
function recoveryView() {
  return `
    <h2 class="scr-title m">If I lose this phone</h2>
    <p class="body-2">You named people when you enrolled — people who can confirm it is you, so a lost phone never means a lost record.</p>
    ${openNote(`<b>The nomination endpoint is not yet public.</b> The parties service runs recoveries (a custodian opens one, a nominated contact confirms), but exposes no read API for a worker's own nominated contacts — so this screen will not pretend to know who yours are. When the endpoint lands, they appear here by name.`)}
    ${sidecar(`Losing the phone loses nothing. Your credentials are re-issued to you after recovery; your work record was never only on the phone.`, true)}`;
}

/* ————— router ————— */
const routes = {
  home: () => homeView(),
  work: (a) => a[0] === "dispute" ? disputeView(decodeURIComponent(a[1] || "")) : a[0] === "declined" ? declinedView() : workView(),
  wallet: (a) => a[0] === "share" ? shareView() : a[0] === "deferred" ? deferredView()
    : a.length === 2 && a[1] === "show" ? credShowView(Number(a[0]))
    : a.length === 1 ? credView(Number(a[0])) : walletView(),
  pay: (a) => a.length === 1 ? payDetailView(Number(a[0])) : payView(),
  profile: (a) => a[0] === "consents" ? consentsView() : a[0] === "checks" ? checksView()
    : a[0] === "messages" ? messagesView() : a[0] === "recovery" ? recoveryView() : profileView(),
};

const TITLES = {
  home: "CREST", work: "Work", wallet: "Wallet", pay: "My money", profile: "Profile",
};

function parseRoute() {
  const h = (location.hash || "#/home").replace(/^#\/?/, "");
  return h.split("/").filter(Boolean);
}

function backFor(route) {
  const [r, ...a] = route;
  if (!a.length) return null;
  if (r === "pay") return "#/pay";
  if (r === "wallet" && a[1] === "show") return "#/wallet/" + a[0];
  if (r === "wallet") return "#/wallet";
  if (r === "work") return "#/work";
  if (r === "profile") return "#/profile";
  return "#/home";
}

let renderSeq = 0;
async function render() {
  const seq = ++renderSeq;
  if (!S.me) { app.innerHTML = loginView(); bind(); return; }
  const route = parseRoute();
  const [r, ...args] = route;
  const fn = routes[r] || routes.home;
  let content;
  try { content = await fn(args); }
  catch (e) { fail(e); content = ""; }
  if (seq !== renderSeq) return; // a newer navigation superseded this render
  const back = backFor(route);
  app.innerHTML = shell(route, TITLES[r] || "CREST", back, content);
  S.err = null;
  bind();
}

/* ————— event wiring ————— */
function bind() {
  app.querySelectorAll("[data-go]").forEach(el => el.addEventListener("click", () => {
    location.hash = el.dataset.go;
  }));

  const lg = document.getElementById("login-grace");
  lg && lg.addEventListener("click", async () => {
    lg.disabled = true; lg.textContent = "Signing in…";
    try {
      await loginAs(FIX.workerA);
      S.me = FIX.workerA;
      location.hash = "#/home";
      render();
    } catch (e) { fail(e); render(); }
  });

  const lo = document.getElementById("logout");
  lo && lo.addEventListener("click", () => {
    setSession(null); S.me = null; S.flash = null; location.hash = "#/"; render();
  });

  app.querySelectorAll("[data-confirm]").forEach(el => el.addEventListener("click", async () => {
    el.disabled = true;
    try {
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(el.dataset.confirm)}/confirm`, { route: "self" });
      S.flash = { route: "work", html: nextBlock({
        happened: `You confirmed the record <b class="mono">${esc(short(el.dataset.confirm))}</b>. The window is closed; your payment is released and the signed credential is being issued.`,
        who: `Nobody — it is yours now. The credential lands in your Wallet on its own.`,
        whenRow: `The payment instruction exists already; the money moves as fast as the rail does — usually today.`,
        told: `An SMS to your phone, and a line under Messages on your Profile.`,
        ifnot: `If the money has not arrived in 3 days, open My money — the payment will be there with a reason and a named owner, and your project support agent can chase it.`,
      }) };
      render();
    } catch (e) { fail(e); render(); }
  }));

  const df = document.getElementById("dispute-form");
  df && df.addEventListener("submit", async ev => {
    ev.preventDefault();
    const claimId = df.dataset.claim, reason = df.reason.value.trim();
    if (!reason) return;
    try {
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/dispute`, { raisedByPartyId: S.me, reason });
      S.flash = { route: "work", html: nextBlock({
        happened: `Your dispute on <b class="mono">${esc(short(claimId))}</b> is on the record — and <b>your payment is released anyway</b>. A dispute contests the record, never the money.`,
        who: `The issuer of the record — the programme that submitted it — must answer your dispute.`,
        whenRow: `The contest is visible to any verifier from this moment until the issuer answers.`,
        told: `An SMS when the issuer responds, and a line under Messages on your Profile.`,
        ifnot: `If nobody has answered in 14 days, tell your project support agent — an unanswered dispute is the issuer's failure, never yours, and it stays visible until resolved.`,
      }) };
      location.hash = "#/work";
      render();
    } catch (e) { fail(e); render(); }
  });

  app.querySelectorAll("[data-withdraw]").forEach(el => el.addEventListener("click", async () => {
    el.disabled = true;
    try {
      await api.post("parties", `/v1/consents/${encodeURIComponent(el.dataset.withdraw)}/withdraw`, { reason: "withdrawn by the worker" });
      S.flash = { route: "consents", html: nextBlock({
        happened: `Your consent <b class="mono">${esc(short(el.dataset.withdraw))}</b> is withdrawn. No new evidence about you will be accepted under it.`,
        who: `Nobody — it is yours now. The programme is told; nothing is asked of you.`,
        whenRow: `Immediately. The withdrawal is on the record from this moment.`,
        told: `The state above changes now; a line lands under Messages on your Profile.`,
        ifnot: `If new work still appears on your record under this programme, that is a breach — tell your project support agent, and the record of this withdrawal is your proof.`,
      }) };
      render();
    } catch (e) { fail(e); render(); }
  }));
}

window.addEventListener("hashchange", () => { S.err = null; render(); });
render();
