// CREST — the field app (apps/enrolment).
//
// Two journeys from the Actor Journeys reference, as one Android-shaped
// mobile-web surface over the live services:
//   J6  W-2  Registering a worker who cannot self-register  (w2_1–w2_5)
//   J8  W-4  From attestation to credential — the supervisor (w3_1–w3_5)
//
// Naomi is both the registering agent and the attestor in the demo world,
// exactly as apps/web's "agent" face borrows the supervisor's grants.
// Every endpoint below is the one the PoC (apps/web/app.js) already calls;
// where the reference promises a screen no L1 endpoint serves yet, the
// screen says so in an .open-note rather than faking it.

import { api, ApiError, loginAs, actingFor, setSession } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";

const app = document.getElementById("app");
const esc = x => String(x ?? "").replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/"/g,"&quot;");
const short = id => { const s=String(id||""); return s.length>18 ? s.slice(0,10)+"…"+s.slice(-6) : s; };
const when = ts => ts ? new Date(ts).toLocaleDateString(undefined,{day:"numeric",month:"short"}) : "—";

/* ————— state ————— */
const S = {
  me: null,          // {partyId, label} once Naomi signs in
  err: null,
  reg: null,         // the registration in flight: {name, phone, rosterId, partyId?}
  batch: null,       // last roster-close result, for w3_5
};

const QKEY = "crest.enrolment.queue";
function queue(){ try { return JSON.parse(localStorage.getItem(QKEY)||"[]"); } catch { return []; } }
function setQueue(q){ try { localStorage.setItem(QKEY, JSON.stringify(q)); } catch {} }

const online = () => navigator.onLine;
window.addEventListener("online", render);
window.addEventListener("offline", render);
window.addEventListener("hashchange", render);

function fail(e){ S.err = e instanceof ApiError ? `${e.status} ${e.code||""} — ${e.message}` : String(e && e.message || e); render(); }
function go(route){ S.err = null; if (location.hash === "#"+route) render(); else location.hash = "#"+route; }

/* ————— chrome ————— */
function statusbar(){
  return `<div class="statusbar"><span>${new Date().toLocaleTimeString(undefined,{hour:"2-digit",minute:"2-digit"})}</span><span class="sig">${online()?"▲▲▲ 4G":"✕ NO SIGNAL"}</span></div>`;
}
function offlineBanner(){
  return online() ? "" :
    `<div class="offline-banner">You are offline. Registrations are held on this device and sync when you have signal.</div>`;
}
function head(title, backRoute){
  return `<div class="mhead">
    ${backRoute!==undefined?`<button class="back" data-go="${esc(backRoute)}">‹</button>`:""}
    <span class="t">${esc(title)}</span>
    <span class="right">${esc(S.me?S.me.label:"")}</span>
  </div>`;
}
function frame(title, backRoute, body){
  return statusbar() + offlineBanner() + head(title, backRoute) +
    `<div class="mbody screen">${S.err?`<div class="errbar">${esc(S.err)}</div>`:""}${body}</div>`;
}
function nextBlock(rows, ifnot){
  return `<div class="next"><span class="eyebrow">What happens next</span>
    ${rows.map(([k,v])=>`<div class="nrow"><span class="k">${esc(k)}</span><span class="v">${esc(v)}</span></div>`).join("")}
    ${ifnot?`<div class="ifnot">${esc(ifnot)}</div>`:""}</div>`;
}
function sidecar(txt, ok){
  return `<div class="sidecar${ok?" ok":""}"><svg class="ico" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6.5"/><line x1="8" y1="7" x2="8" y2="11.4"/><line x1="8" y1="4.6" x2="8" y2="5"/></svg><span class="txt">${esc(txt)}</span></div>`;
}
const openNote = txt => `<div class="open-note">${esc(txt)}</div>`;

/* ————— screens ————— */

function loginScreen(){
  return statusbar() + offlineBanner() + head("CREST field") + `<div class="mbody screen">
    <div class="scr-title m">Who is carrying this device?</div>
    <p class="muted">In this dev build, signing in mints a token from the stack's own identity provider and binds it through the real first-login path. Naomi carries both hats here: registering agent (J6) and attestor (J8).</p>
    <button class="btn" data-login="1">Naomi (supervisor)</button>
    ${sidecar("Registrations made from this device carry Naomi's agent identity; every assisted action is recorded with her name on it.")}
  </div>`;
}

function homeScreen(){
  const q = queue();
  const item = (route, title, sub, badge) => `<button class="card" style="text-align:left;display:flex;flex-direction:column;gap:3px" data-go="${esc(route)}">
    <span style="display:flex;justify-content:space-between;align-items:center;width:100%"><span style="font:500 14.5px/1.3 Roboto">${esc(title)}</span>${badge||""}</span>
    <span class="muted">${esc(sub)}</span></button>`;
  return frame("CREST field", undefined, `
    <span class="eyebrow">Registering agent · J6</span>
    ${item("/registrations","Registrations","The day's list — who came in through you", q.length?`<span class="chip warn sm">${q.length} held on this device</span>`:"")}
    ${item("/register","New worker","Phone or roster id — neither is a fallback")}
    <span class="eyebrow" style="margin-top:6px">Supervisor · attestor · J8</span>
    ${item("/toconfirm","To confirm","Workers whose window is open and who could not be told")}
    ${item("/roster","Close the roster","The month's tally, checked row by row")}
    ${item("/handoff","Who holds it next","After the close — where every unclear row went")}
  `);
}

/* w2_1 — the day's list. There is no parties listing endpoint for enrolments,
   so the day's list is what this device did: registrations completed from
   here this session, plus any held offline. Honest, and labelled. */
function registrationsScreen(){
  const q = queue();
  const done = doneToday();
  return frame("Registrations", "/", `
    <div class="scr-title m">Today, through you</div>
    ${q.length?`<span class="eyebrow">Held on this device</span>`+q.map((r,i)=>`<div class="card quiet">
      <div style="display:flex;justify-content:space-between;align-items:center"><span style="font:500 13.5px/1.3 Roboto">${esc(r.name)}</span><span class="chip warn sm">held on this device</span></div>
      <span class="muted">${esc(r.phone?("phone "+r.phone):("roster id "+(r.rosterId||"—")))} · will sync when you have signal</span>
      ${online()?`<div class="btn-row" style="margin-top:8px"><button class="btn secondary" data-sync="${i}">Sync now</button></div>`:""}
    </div>`).join(""):""}
    ${done.length?`<span class="eyebrow">Registered</span>`+done.map(r=>`<div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center"><span style="font:500 13.5px/1.3 Roboto">${esc(r.name)}</span><span class="chip ok sm">registered</span></div>
      <span class="mono">${esc(r.partyId)}</span>
    </div>`).join(""):""}
    ${!q.length&&!done.length?`<div class="card quiet"><span class="muted">Nothing yet today. The registry keeps the durable list; this screen shows what came through this device — there is no “everyone Naomi ever registered” endpoint at L1, and the day's work is a device-local view on purpose.</span></div>`:""}
    <div class="spacer"></div>
    <button class="btn" data-go="/register">New worker</button>
  `);
}
const DONEKEY = "crest.enrolment.done";
function doneToday(){ try { return JSON.parse(sessionStorage.getItem(DONEKEY)||"[]"); } catch { return []; } }
function pushDone(r){ try { sessionStorage.setItem(DONEKEY, JSON.stringify([r, ...doneToday()])); } catch {} }

/* w2_2 — the form. Two pathways, neither a fallback. No national-ID pathway:
   the record is marked with how identity was established, never refused. */
function registerScreen(){
  const r = S.reg || {};
  return frame("New worker", "/registrations", `
    <div class="scr-title m">Register a worker who cannot self-register</div>
    <div class="card"><form id="regform" style="display:flex;flex-direction:column;gap:10px">
      <label class="body-2">Name<input name="name" required placeholder="Peter Njoroge" value="${esc(r.name||"")}" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
      <span class="eyebrow">One of these — neither is a fallback</span>
      <label class="body-2">Phone<input name="phone" placeholder="+2547…" value="${esc(r.phone||"")}" inputmode="tel" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
      <label class="body-2">Roster id (this programme's roster)<input name="rosterId" placeholder="CHW-2026-0114" value="${esc(r.rosterId||"")}" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
      <label class="body-2">Assertion reference (optional)<input name="assertionRef" placeholder="a reference to an identity assertion — never the ID number itself" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
      <button class="btn" type="submit">Register</button>
    </form></div>
    ${sidecar("No national ID? We can still register them — the record is marked with how identity was established rather than being refused. Nothing on this form takes a raw ID number: CREST holds a pairwise reference and a salted hash, nothing else.")}
    ${openNote("The assertion-reference field is illustrative — the L1 enrolment endpoint takes contact routes and a roster id today; identity assertions bind later through identity-bindings without losing anything already earned.")}
  `);
}

/* w2_3 — consent is voice, not a checkbox. */
function consentScreen(){
  const r = S.reg || {};
  const name = r.name || "the worker";
  const first = String(name).split(" ")[0];
  return frame("Read this to " + first, "/register", `
    <div style="display:flex;gap:8px"><span class="chip lang-pill">Kiswahili</span><span class="chip readaloud-pill">Read aloud</span></div>
    <div class="consent-quote">“${esc(first)}, Crest itaweka rekodi ya kazi yako — kazi unayofanya, na malipo yake. Utaambiwa kila mara kazi yako inaporekodiwa, na una siku saba za kusema kama ni sahihi. Unaweza kuondoa idhini hii wakati wowote. Je, unakubali?”</div>
    <div class="body-2" style="color:var(--text-2)">“${esc(first)}, Crest will keep a record of your work — the work you do, and what it pays. You will be told each time your work is recorded, and you have seven days to say whether it is right. You can withdraw this consent at any time. Do you agree?”</div>
    <div class="spacer"></div>
    ${sidecar("Recording captures the worker's answer, your agent ID and the time. This is the consent record.")}
    <button class="btn" id="recordbtn">● Record</button>
  `);
}

/* w2_4 — a probable duplicate holds; the agent cannot decide. Existence,
   never content. */
function holdScreen(){
  const r = S.reg || {};
  return frame("Possible duplicate", "/registrations", `
    <div class="scr-title m">This may already be a Crest worker</div>
    <div class="card hi">
      <p class="body-2">The registry found more than one possible match for ${esc(r.name||"this worker")}. <strong>You cannot decide this — it goes to the registry custodian.</strong></p>
      <p class="muted" style="margin-top:6px">What you see here is that a hold exists — not whose records collided, and not on what. Probable matches hold; they never merge without the worker's own confirmation.</p>
    </div>
    ${nextBlock([
      ["What just happened","A duplicate hold was raised in the registry. Nothing was merged and nothing was guessed."],
      ["Who acts next","The registry custodian, from the duplicates queue."],
      ["When","At the next queue review — typically within days."],
      ["How you will be told","The registration completes or is joined once the custodian closes the hold; the worker keeps everything either way."],
    ],"If the custodian finds two distinct people, both records stand — sharing an identifier is not being the same person.")}
    <div class="spacer"></div>
    <button class="btn secondary" data-go="/registrations">Back to the day's list</button>
  `);
}

/* w2_5 — registered. */
function registeredScreen(){
  const r = S.reg || {};
  return frame("Registered", "/registrations", `
    <div class="card hi" style="text-align:center;display:flex;flex-direction:column;gap:8px;padding:22px 14px">
      <span class="person-name">${esc(r.name||"")}</span>
      <span class="chip ok" style="align-self:center">registered</span>
      <span class="eyebrow">Crest ID</span>
      <span class="mono" style="word-break:break-all">${esc(r.partyId||"")}</span>
    </div>
    <div class="card quiet"><span class="body-2">A card is printed on the spot when the first credential is issued — at enrolment there is no work history to print.</span> <span class="chip plain sm">Illustrative — no printer on this device</span></div>
    ${nextBlock([
      ["What just happened","The worker exists in the registry, with voice consent on record, enrolled by you."],
      ["Who acts next","The programme — evidence of their work arrives from its systems; nobody types work into Crest."],
      ["When","From the next roster close onward."],
      ["How you will be told","Each recorded claim opens a seven-day window; a worker with no phone is told through you."],
    ],"If no evidence ever arrives, the registration still stands — existing in Crest does not depend on a document, a phone, or work already logged.")}
    <div class="spacer"></div>
    <div class="btn-row"><button class="btn secondary" data-go="/registrations">Day's list</button><button class="btn" data-go="/register">Next worker</button></div>
  `);
}

/* w3_1 — the worklist. */
async function toConfirmScreen(){
  const out = await api.get("confirmation", "/v1/unreached");
  const list = out.windows || out.unreached || [];
  return frame("To confirm", "/", `
    <div class="scr-title m">Workers waiting on you</div>
    <span class="chip info sm">DIGIT HCM · the worklist belongs to the delivery platform</span>
    <p class="muted">Open windows whose worker could not be told — no phone, or no signal. You are their route; an assisted exit is one of the four, recorded as itself with your name on it. Every exit releases payment.</p>
    ${list.length?list.map(w=>`<div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;gap:8px">
        <span class="mono">${esc(short(w.claimId))}</span>
        <span class="chip warn sm">closes ${esc(when(w.closesAt))}</span></div>
      <span class="muted">worker <span class="mono">${esc(short(w.partyId))}</span></span>
      <div class="btn-row" style="margin-top:10px">
        <button class="btn dominant" data-assistview="${esc(w.claimId)}">Confirm what you saw</button>
        <button class="btn secondary" data-differview="${esc(w.claimId)}">Different figure</button>
      </div></div>`).join("")
    :`<div class="card quiet"><span class="muted">Nobody is waiting on you. When a window opens for a worker who cannot be reached, they appear here.</span></div>`}
  `);
}

/* w3_2 — assisted confirm. */
async function confirmSawScreen(claimId){
  const win = await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(()=>null);
  return frame("Confirm what you saw", "/toconfirm", `
    <div class="card">
      <span class="eyebrow">Claim</span>
      <div class="mono" style="word-break:break-all">${esc(claimId)}</div>
      ${win?`<div class="kv" style="margin-top:10px">
        <div class="row"><span class="k">worker</span><span class="v mono">${esc(short(win.partyId))}</span></div>
        <div class="row"><span class="k">window closes</span><span class="v">${esc(when(win.closesAt))}</span></div>
        ${win.exitRoute?`<div class="row"><span class="k">already exited</span><span class="v">${esc(win.exitRoute)}</span></div>`:""}
      </div>`:""}
    </div>
    ${sidecar("You are confirming on the worker's behalf what you saw done. The exit is recorded as assisted — never as if the worker pressed the button themselves.")}
    <div class="spacer"></div>
    ${win && !win.exitRoute ? `<button class="btn" data-assist="${esc(claimId)}" data-party="${esc(win.partyId||"")}">Confirm — it is right</button>` : `<div class="card quiet"><span class="muted">This window has already exited${win&&win.exitRoute?` (${esc(win.exitRoute)})`:""}; payment was released by that exit.</span></div>`}
  `);
}
function assistedDoneScreen(claimId){
  return frame("Confirmed", "/toconfirm", `
    <div class="card hi"><span class="chip ok">assisted confirmation recorded</span>
      <p class="body-2" style="margin-top:8px">Claim <span class="mono">${esc(short(claimId))}</span> exited by the assisted route.</p></div>
    ${nextBlock([
      ["What just happened","The window closed by assisted confirmation — recorded as assisted, with your name on it, never a quieter route."],
      ["Who acts next","The system: the credential is issued and the payment instruction raised now."],
      ["When","Immediately — every one of the four exits releases payment."],
      ["How you will be told","The worker's record shows the credential; a worker with no phone hears through you on your next visit."],
    ])}
    <div class="spacer"></div>
    <button class="btn secondary" data-go="/toconfirm">Back to the worklist</button>
  `);
}

/* w3_3 — a different figure, on the worker's behalf. */
async function differScreen(claimId){
  const win = await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(()=>null);
  return frame("Record a different figure", "/toconfirm", `
    <div class="card">
      <span class="eyebrow">Claim</span>
      <div class="mono" style="word-break:break-all">${esc(claimId)}</div>
      <form id="differform" style="display:flex;flex-direction:column;gap:10px;margin-top:10px" data-claim="${esc(claimId)}" data-party="${esc(win&&win.partyId||"")}">
        <label class="body-2">What the worker says the figure was<input name="figure" required placeholder="e.g. 9, not 12" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></label>
        <label class="body-2">In the worker's words, why<textarea name="reason" rows="3" required style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:inherit"></textarea></label>
        <button class="btn" type="submit">Record the dispute</button>
      </form>
    </div>
    ${sidecar("A dispute contests the record, never the money — the worker is paid either way, and the underlying record of the work is never destroyed.")}
    ${openNote("Honest gap: the L1 dispute endpoint carries the worker's dispute but has no assistedBy field the way confirmation does — this screen records it on the worker's behalf and names you inside the reason text. Whether dispute needs a first-class assisted marker is a design question for the confirmation service.")}
  `);
}
function differDoneScreen(claimId){
  return frame("Dispute recorded", "/toconfirm", `
    <div class="card hi"><span class="chip warn">dispute on the record</span>
      <p class="body-2" style="margin-top:8px">Claim <span class="mono">${esc(short(claimId))}</span> exited by dispute — <strong>and the payment is released anyway</strong>.</p></div>
    ${nextBlock([
      ["What just happened","The worker's dispute closed the window. The record is contested; the money is not withheld."],
      ["Who acts next","The issuer answers the contest; it stays visible to any verifier until they do."],
      ["When","The contest has no expiry — it stands until answered."],
      ["How you will be told","The claim shows as contested in the worker's record and in any verification of the credential."],
    ])}
    <div class="spacer"></div>
    <button class="btn secondary" data-go="/toconfirm">Back to the worklist</button>
  `);
}

/* w3_4 — close the roster: the month's CSV, checked row by row. */
function rosterScreen(){
  const b = S.batch;
  return frame("Close the roster", "/", `
    <div class="scr-title m">The month's tally, row by row</div>
    <p class="muted">The file is checked against the definition. A row that does not match becomes somebody named in the unclear queue — never a silent drop.</p>
    <div class="card"><form id="rosterform" style="display:flex;flex-direction:column;gap:10px">
      <label class="body-2">CSV file<input type="file" name="file" accept=".csv,text/csv" style="font:inherit"></label>
      <label class="body-2">…or paste it<textarea name="csv" rows="6" class="mono" style="width:100%;padding:11px;border:1px solid var(--divider);border-radius:6px;font:500 11px/1.6 var(--mono)">activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,period_end,geography,source_record_ref,household_id,beneficiary_count,supervisor_present
bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,2026-03-02,ward-7,roster-close-1,HH-101,4,true</textarea></label>
      <button class="btn" type="submit">Close the roster</button>
    </form></div>
    ${b?`<div class="card">
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        <span class="chip ok">${b.rowsAccepted} accepted</span>
        ${b.rowsUnclear?`<span class="chip warn">${b.rowsUnclear} unclear</span>`:""}
        <span class="chip plain">${b.rowsTotal} rows</span></div>
      ${(b.rows||[]).map(r=>`<div style="display:flex;justify-content:space-between;gap:8px;border-top:1px solid var(--generic-bg);padding:8px 0 2px;margin-top:8px">
        <span class="body-2">${esc(r.label)}</span>
        <span class="chip sm ${r.ok?"ok":"warn"}">${r.ok?"accepted":"unclear"}</span></div>`).join("")}
      <div class="btn-row" style="margin-top:12px"><button class="btn" data-go="/handoff">Who holds it next</button></div>
    </div>`:""}
  `);
}

/* w3_5 — after the close: where everything went. */
async function handoffScreen(){
  const [unclear, unreached] = await Promise.all([
    api.get("evidence","/v1/unclear").catch(()=>({unclear:[]})),
    api.get("confirmation","/v1/unreached").catch(()=>({windows:[],unreached:[]})),
  ]);
  const uc = (unclear.unclear||[]).filter(u=>!u.resolvedAt);
  const ur = unreached.windows || unreached.unreached || [];
  const b = S.batch;
  return frame("Who holds it next", "/", `
    ${b?`<div class="card quiet"><span class="body-2">Your last close: <b>${b.rowsAccepted}</b> accepted, <b>${b.rowsUnclear}</b> unclear of ${b.rowsTotal}.</span></div>`:""}
    <div class="stats">
      <div class="stat"><div class="n">${uc.length}</div><div class="l">unclear rows — the custodian's queue</div></div>
      <div class="stat"><div class="n">${ur.length}</div><div class="l">open windows waiting on you</div></div>
    </div>
    ${uc.length?`<span class="eyebrow">In the custodian's queue</span>`+uc.slice(0,8).map(u=>`<div class="card quiet">
      <div style="display:flex;justify-content:space-between;gap:8px"><span class="mono">${esc(u.rowRef||u.id)}</span><span class="chip warn sm">${esc(u.kind)}</span></div>
      <span class="muted">${esc(u.reason)}</span></div>`).join(""):""}
    ${nextBlock([
      ["What just happened","Accepted rows became claims; each opens the worker's seven-day window. Unclear rows went to the custodian — a mismatch is somebody named, not a status."],
      ["Who acts next","The registry custodian on the unclear rows; the workers (or you, assisted) on the open windows."],
      ["When","Windows close in seven days at the latest; every exit releases payment."],
      ["How you will be told","Workers you are the route for reappear under To confirm; the custodian's attributions land as claims like any other."],
    ],"If a row stays unclear, no one is silently unpaid by it — the row waits, named, until the custodian decides whose work it was.")}
  `);
}

/* ————— actions ————— */

async function doLogin(){
  S.err = null;
  try {
    await loginAs(FIX.supervisor);
    S.me = { partyId: FIX.supervisor, label: "Naomi" };
    go("/");
  } catch(e){ fail(e); }
}

// The same registration the PoC's agent face posts (apps/web/app.js regform):
// POST parties /v1/enrolments; a worker with no phone gets the supervisor
// contact route — the party schema's route kinds are phone/email/ussd/
// supervisor, and the roster id is its own registry key, added right after.
async function submitRegistration(reg){
  const routes = [ reg.phone ? {kind:"phone", value:reg.phone} : {kind:"supervisor", value:FIX.supervisor} ];
  const out = await api.post("parties","/v1/enrolments",{
    party:{kind:"person", displayName:reg.name, contactRoutes:routes},
    enrolledBy:S.me.partyId, contextId:FIX.project, method:"field-visit"});
  const partyId = out.party.id;
  if (reg.rosterId) {
    try {
      actingFor(partyId);
      await api.post("parties",`/v1/parties/${encodeURIComponent(partyId)}/roster-ids`,
        {rosterId:reg.rosterId, contextId:FIX.project});
    } finally { actingFor(null); }
  }
  return partyId;
}

async function onRegister(f){
  const reg = { name:f.name.value.trim(), phone:f.phone.value.trim(), rosterId:f.rosterId.value.trim(), at:Date.now() };
  if (!reg.phone && !reg.rosterId) { fail(new Error("give the pathway you have — a phone or a roster id; neither is a fallback for the other")); return; }
  S.reg = reg;
  if (!online()) { setQueue([reg, ...queue()]); go("/registrations"); return; }
  try {
    reg.partyId = await submitRegistration(reg);
    pushDone(reg);
    // The duplicate check: resolve by the contact route. 200 = this route now
    // resolves to one person; 409 = a collision — a hold exists, and the
    // custodian owns it. The agent never sees the other record.
    if (reg.phone) {
      try {
        await api.get("parties", `/v1/resolve?kind=contact-route&value=${encodeURIComponent(reg.phone)}`);
      } catch(e){
        if (e instanceof ApiError && e.status === 409) { go("/hold"); return; }
        // 404 or anything else: not a collision — carry on to consent.
      }
    }
    go("/consent");
  } catch(e){
    if (e instanceof ApiError && e.status === 409) { go("/hold"); return; }
    fail(e);
  }
}

async function syncQueued(i){
  const q = queue();
  const reg = q[i];
  if (!reg) return;
  S.err = null;
  try {
    reg.partyId = await submitRegistration(reg);
    q.splice(i,1); setQueue(q); pushDone(reg);
    S.reg = reg;
    go("/consent");
  } catch(e){
    if (e instanceof ApiError && e.status === 409) { q.splice(i,1); setQueue(q); S.reg = reg; go("/hold"); return; }
    fail(e);
  }
}

// The consent the PoC posts (apps/web agentconsent): supervisor-assisted,
// voice channel — the recording body travels as the artefact, and the query
// names moment, capture method, purpose, capturer and context. The dev
// stand-in for the recording is text; a device build attaches audio.
async function onRecordConsent(){
  const reg = S.reg || {};
  if (!reg.partyId) { fail(new Error("no registration in flight — register the worker first")); return; }
  S.err = null;
  try {
    actingFor(reg.partyId);
    const q = new URLSearchParams({moment:"enrolment", captureMethod:"voice",
      purpose:"hold and fetch evidence of my work", capturedBy:S.me.partyId, contextId:FIX.project});
    await api.postRaw("parties",`/v1/parties/${encodeURIComponent(reg.partyId)}/consents?`+q,
      `${reg.name} answered yes to the enrolment consent script, read aloud in Kiswahili.`, "audio/ogg");
    actingFor(null);
    go("/registered");
  } catch(e){ actingFor(null); fail(e); }
}

// The assisted exit, exactly as apps/web's assist handler does it: fetch the
// window for the worker, act for them, post the confirm with route=assisted
// and your name in assistedByPartyId.
async function onAssist(claimId, partyId){
  S.err = null;
  try {
    const pid = partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
    actingFor(pid);
    await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/confirm`,
      {route:"assisted", assistedByPartyId:S.me.partyId});
    actingFor(null);
    go("/confirmed/"+encodeURIComponent(claimId));
  } catch(e){ actingFor(null); fail(e); }
}

async function onDiffer(f){
  const claimId = f.dataset.claim, partyId = f.dataset.party;
  S.err = null;
  try {
    const pid = partyId || (await api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)).partyId;
    actingFor(pid);
    await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/dispute`,
      {raisedByPartyId:pid,
       reason:`The worker says the figure was ${f.figure.value.trim()}. In their words: ${f.reason.value.trim()} — recorded on their behalf by ${S.me.partyId} (supervisor-assisted).`});
    actingFor(null);
    go("/differed/"+encodeURIComponent(claimId));
  } catch(e){ actingFor(null); fail(e); }
}

// The batch the PoC's supervisor face posts: canonical CSV to the evidence
// service, provenance in the query string. Per-row verdicts are derived
// honestly: the service returns counts and claim ids, and the unclear queue
// names each mismatched row; we join those to the rows we sent.
async function onRoster(f){
  S.err = null;
  let csv = f.csv.value;
  const file = f.file.files && f.file.files[0];
  if (file) csv = await file.text();
  try {
    const q = new URLSearchParams({contextId:FIX.project, definitionId:FIX.definition, submittedBy:S.me.partyId,
      sourceClass:"programme-system", captureMethod:"digital-capture", sourceExposure:"signed-batch", systemRef:"field-app-roster"});
    const out = await api.postRaw("evidence", "/v1/batches?"+q, csv, "text/csv");
    const b = out.batch;
    // Join the unclear queue back to this batch's rows for per-row results.
    const unclear = await api.get("evidence","/v1/unclear").catch(()=>({unclear:[]}));
    const unclearRefs = new Set((unclear.unclear||[]).filter(u=>!u.resolvedAt).map(u=>String(u.rowRef||"")));
    const lines = csv.trim().split(/\r?\n/);
    const header = (lines[0]||"").split(",").map(s=>s.trim());
    const wi = header.indexOf("worker_id"), ai = header.indexOf("activity"), sri = header.indexOf("source_record_ref");
    const rows = lines.slice(1).filter(l=>l.trim()).map((l,idx)=>{
      const c = l.split(",");
      const ref = sri>=0 ? (c[sri]||"").trim() : "";
      const unclearHit = [...unclearRefs].some(r=>r && (r===ref || r.includes(ref) || ref.includes(r)));
      return { label:`${ai>=0?(c[ai]||"").trim():"row "+(idx+1)} · ${wi>=0?(c[wi]||"").trim():""}`, ok:!unclearHit || !b.rowsUnclear };
    });
    S.batch = { rowsTotal:b.rowsTotal, rowsAccepted:b.rowsAccepted, rowsUnclear:b.rowsUnclear, rows };
    render();
  } catch(e){ fail(e); }
}

/* ————— router ————— */
async function render(){
  const route = (location.hash || "#/").slice(1);
  if (!S.me) { app.innerHTML = loginScreen(); bind(); return; }
  let html;
  try {
    if (route === "/" || route === "") html = homeScreen();
    else if (route === "/registrations") html = registrationsScreen();
    else if (route === "/register") html = registerScreen();
    else if (route === "/consent") html = consentScreen();
    else if (route === "/hold") html = holdScreen();
    else if (route === "/registered") html = registeredScreen();
    else if (route === "/toconfirm") html = await toConfirmScreen();
    else if (route.startsWith("/confirmsee/")) html = await confirmSawScreen(decodeURIComponent(route.slice(12)));
    else if (route.startsWith("/confirmed/")) html = assistedDoneScreen(decodeURIComponent(route.slice(11)));
    else if (route.startsWith("/differ/")) html = await differScreen(decodeURIComponent(route.slice(8)));
    else if (route.startsWith("/differed/")) html = differDoneScreen(decodeURIComponent(route.slice(10)));
    else if (route === "/roster") html = rosterScreen();
    else if (route === "/handoff") html = await handoffScreen();
    else html = homeScreen();
  } catch(e){
    html = frame("CREST field", "/", `<div class="errbar">${esc(e instanceof ApiError ? `${e.status} ${e.code||""} — ${e.message}` : e.message||e)}</div>
      <p class="muted">The services could not answer. Nothing was guessed in their place.</p>`);
  }
  app.innerHTML = html;
  bind();
}

function on(sel, ev, fn){ app.querySelectorAll(sel).forEach(el=>el.addEventListener(ev, fn)); }

function bind(){
  on("[data-login]","click",doLogin);
  on("[data-go]","click",e=>go(e.currentTarget.dataset.go));
  on("[data-sync]","click",e=>syncQueued(+e.currentTarget.dataset.sync));
  on("[data-assistview]","click",e=>go("/confirmsee/"+encodeURIComponent(e.currentTarget.dataset.assistview)));
  on("[data-differview]","click",e=>go("/differ/"+encodeURIComponent(e.currentTarget.dataset.differview)));
  on("[data-assist]","click",e=>onAssist(e.currentTarget.dataset.assist, e.currentTarget.dataset.party));
  const rf = document.getElementById("regform");
  rf && rf.addEventListener("submit",ev=>{ ev.preventDefault(); onRegister(rf); });
  const rb = document.getElementById("recordbtn");
  rb && rb.addEventListener("click",onRecordConsent);
  const df = document.getElementById("differform");
  df && df.addEventListener("submit",ev=>{ ev.preventDefault(); onDiffer(df); });
  const rof = document.getElementById("rosterform");
  rof && rof.addEventListener("submit",ev=>{ ev.preventDefault(); onRoster(rof); });
}

render();
