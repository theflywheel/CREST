// CREST — the Actor Journeys, as a web product (apps/web).
//
// One app, several faces, each face built from its journey in
// docs/reference/CREST — Actor Journeys_17Aug.html and backed only by
// endpoints that exist. Where the document promises a screen the backend
// cannot serve yet, the face says so and names the issue — a visible "not
// yet" rather than a silent gap.

import { api, ApiError, loginAs, actingFor, setSession } from "./api.js";

const FIX = {
  org:        "did:crest:party:01JCREST000000000000000RGN",
  supervisor: "did:crest:party:01JCREST00000000000000SPVR",
  custodian:  "did:crest:party:01JCREST00000000000000CSTD",
  workerA:    "did:crest:party:01JCREST00000000000000WRKA",
  project:    "crest:context:01JCREST00000000000000PRJC",
  definition: "crest:definition:01JCREST00000000000000DEFN",
};

const personas = [
  { id: FIX.workerA,    face: "worker",     who: "Grace (worker)",       what: "The wallet, not a dashboard — my record, my money, who checked me", initial: "G" },
  { id: FIX.supervisor, face: "supervisor", what: "Confirm what you saw; a spreadsheet arrived; who holds it next", who: "Naomi (supervisor)", initial: "N" },
  { id: FIX.custodian,  face: "custodian",  who: "Otieno (registry custodian)", what: "Coverage, duplicates, the queue and the rule for closing one", initial: "O" },
  { id: FIX.org,        face: "project",    who: "Ministry of Health (project)", what: "PRJ-118 — a funnel, not a set of totals", initial: "M" },
  { id: null,           face: "verifier",   who: "A verifier",           what: "Scan or enter the credential — yes, plus facts, and nothing that identifies anyone", initial: "V" },
  { id: null,           face: "agent",      who: "A registering agent",  what: "Registering a worker in the field, either way", initial: "A" },
];

const S = { me: null, face: "login", view: null, err: null };
const app = document.getElementById("app");
const esc = x => String(x ?? "").replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/"/g,"&quot;");
const short = id => { const s=String(id||""); return s.length>18 ? s.slice(0,10)+"…"+s.slice(-6) : s; };
const money = (minor, cur) => (minor/100).toLocaleString(undefined,{minimumFractionDigits:2}) + " " + (cur||"");
const when = ts => ts ? new Date(ts).toLocaleDateString(undefined,{day:"numeric",month:"short"}) : "—";

function fail(e){ S.err = e instanceof ApiError ? `${e.status} ${e.code||""} — ${e.message}` : String(e && e.message || e); render(); }
function clearErr(){ S.err = null; }

/* ————— login ————— */
async function choose(p){
  clearErr();
  try {
    if (p.id) { await loginAs(p.id); S.me = { partyId: p.id, label: p.who }; }
    else if (p.face === "agent") {
      // The agent is somebody who may act for the workers they enrol — in the
      // fixture world, the supervisor's context-scoped act-for-party grant.
      await loginAs(FIX.supervisor); S.me = { partyId: FIX.supervisor, label: "A registering agent" };
    }
    else { setSession(null); S.me = { partyId: null, label: p.who }; }
    S.face = p.face; S.view = null; render();
  } catch(e){ fail(e); }
}

function loginView(){
  return `<div class="login">
    <h2>CREST</h2>
    <p class="lede">Verifiable work history for informal workers. Choose who you are — in this dev build, signing in mints a token from the stack's own identity provider and binds it through the real first-login path.</p>
    ${S.err?`<div class="err">${esc(S.err)}</div>`:""}
    ${personas.map((p,i)=>`<button class="persona" data-p="${i}">
      <span class="avatar">${p.initial}</span>
      <span><div class="who">${esc(p.who)}</div><div class="what">${esc(p.what)}</div></span>
    </button>`).join("")}
    <p class="note">The stack: <code>make e2e-up</code>. This page is served by the <code>web</code> compose service on :59100, and the services admit it via <code>CREST_CORS_ORIGINS</code>.</p>
  </div>`;
}

/* ————— shared shell ————— */
const NAVS = {
  worker: [["record","My record"],["money","My money"],["wallet","My credentials"],["checks","Who checked me"],["consent","What I agreed to"]],
  supervisor: [["batch","A spreadsheet arrived"],["unreached","Who holds it next"],["unclear","Evidence that did not match"]],
  custodian: [["find","Find a worker"],["dupes","Duplicates — the queue"],["unclear","Whose work was this row"],["recover","Recoveries"],["review","Overdue for review"]],
  project: [["funnel","Where it stands"],["payments","Payments, and where the delay is"],["sources","Where evidence comes from"]],
  verifier: [["check","Scan or enter the credential"],["person","Resolve a person"],["batchcheck","Batch BAT — checking many"]],
  agent: [["register","Register a worker"],["consent","Read this to them"],["recoverstart","Who can confirm it is you?"]],
};

function shell(content){
  const nav = (NAVS[S.face]||[]).map(([k,label])=>
    `<button class="${S.view===k?"active":""}" data-nav="${k}">${esc(label)}</button>`).join("");
  return `<div class="top">
      <h1>CREST</h1><span class="sub">${esc({worker:"Worker",supervisor:"Supervisor (Attestor)",custodian:"Worker Registry Custodian",project:"Project console",verifier:"Verifier",agent:"Registering Agent"}[S.face]||"")}</span>
      <span class="me"><span class="chip">${esc(S.me?.label||"")}</span><button class="btn small" id="logout">Switch person</button></span>
    </div>
    <div class="wrap">
      <nav class="rail"><div class="cap">Journeys</div><div class="nav">${nav}</div></nav>
      <main class="main">${S.err?`<div class="err">${esc(S.err)}</div>`:""}${content}</main>
    </div>`;
}

/* ————— worker face ————— */
async function workerRecord(){
  const me = S.me.partyId;
  const [wins, claims] = await Promise.all([
    api.get("confirmation", `/v1/windows?partyId=${encodeURIComponent(me)}`),
    api.get("evidence", `/v1/claims?partyId=${encodeURIComponent(me)}`),
  ]);
  const open = (wins.windows||[]).filter(w=>!w.exitRoute);
  const closed = (wins.windows||[]).filter(w=>w.exitRoute);
  return `<h2>My record</h2>
    <p class="lede">Work recorded about you counts only after you have had your say. Confirm it, dispute it, or let seven days pass — <strong>you are paid either way</strong>; a dispute contests the record, never the money.</p>
    ${open.length?open.map(w=>`<div class="card">
      <div class="row"><h3>A record of your work</h3><span class="pill hold">reply by ${when(w.closesAt)}</span></div>
      <div class="kv"><b>claim</b><code>${short(w.claimId)}</code></div>
      <div class="row" style="margin-top:8px">
        <button class="btn primary" data-confirm="${esc(w.claimId)}">It is right</button>
        <button class="btn danger" data-dispute="${esc(w.claimId)}">Something does not match</button>
      </div></div>`).join("")
    : `<div class="empty">Nothing is waiting for you. When new work is recorded, it appears here before it counts.</div>`}
    ${closed.length?`<div class="card"><h3>Settled</h3><div class="tablewrap"><table>
      <tr><th>Claim</th><th>How it closed</th><th>Paid</th><th>Credential</th></tr>
      ${closed.map(w=>`<tr><td><code>${short(w.claimId)}</code></td>
        <td><span class="pill ${w.exitRoute==="dispute"?"risk":"ok"}">${esc(w.exitRoute)}</span></td>
        <td>${w.paymentReleasedAt?`<span class="pill ok">released</span>`:`<span class="pill mut">—</span>`}</td>
        <td>${w.credentialId?`<code>${short(w.credentialId)}</code>`:"—"}</td></tr>`).join("")}
    </table></div></div>`:""}
    <p class="note">${(claims.claims||[]).length} claim(s) on your record in total. A disputed claim never destroys the underlying work record.</p>`;
}

async function workerMoney(){
  const me = S.me.partyId;
  const out = await api.get("payments", `/v1/instructions?partyId=${encodeURIComponent(me)}`);
  const list = out.instructions||[];
  return `<h2>My money</h2>
    <p class="lede">Every payment, and — where one is held — why, and whose problem it is. A held payment with no reason and no owner cannot exist here; the record itself refuses it.</p>
    ${list.length?`<div class="tablewrap"><table><tr><th>Amount</th><th>State</th><th>Route</th><th>When</th><th>If held: why, and who owns it</th></tr>
      ${list.map(i=>`<tr><td class="money">${money(i.amountMinor,i.currency)}</td>
        <td><span class="pill ${i.state==="RELEASED"?"ok":"hold"}">${esc(i.state)}</span></td>
        <td>${esc(i.releasedBy||"")}</td><td>${when(i.releasedAt)}</td>
        <td>${i.heldReason?esc(i.heldReason)+" — <b>"+esc(i.heldOwner||"")+"</b>":"—"}</td></tr>`).join("")}
    </table></div>`:`<div class="empty">No payments yet.</div>`}`;
}

async function workerWallet(){
  const me = S.me.partyId;
  const out = await api.get("confirmation", `/v1/credentials?partyId=${encodeURIComponent(me)}`);
  const creds = out.credentials||[];
  return `<h2>My credentials</h2>
    <p class="lede">Each one is provable to a stranger in a minute, offline — the printed card carries the whole signed credential, not a link to one.</p>
    ${creds.length?creds.map(c=>{
      const we=(c.credentialSubject||{}).workEvent||{};
      return `<div class="card"><div class="row"><h3>${esc(we.activity||"Work event")}</h3>
        <span class="pill info">${esc(we.skillCode||"")}</span></div>
        <div class="kv"><b>outcome</b>${esc(we.outcome?we.outcome.value+" "+we.outcome.unit:"")}</div>
        <div class="kv"><b>period</b>${when((we.period||{}).start)}</div>
        <div class="kv"><b>id</b><code>${short(c.id)}</code></div>
        <div class="row" style="margin-top:6px"><button class="btn small" data-showdoc="${esc(c.id)}">Show the document</button>
        <a class="btn small" href="${esc(location.origin.replace(':59100',':59004'))}/v1/credentials/${encodeURIComponent(c.id)}/card" target="_blank" rel="noopener">Printed card (QR)</a></div>
        <pre class="doc" id="doc-${esc(c.id)}" hidden>${esc(JSON.stringify(c,null,2))}</pre></div>`;
    }).join(""):`<div class="empty">No credentials yet — they are issued when a confirmation window closes.</div>`}`;
}

async function workerChecks(){
  const me = S.me.partyId;
  const out = await api.get("verification", `/v1/presentations?subjectRef=${encodeURIComponent(me)}`);
  const list = out.presentations||[];
  return `<h2>Who checked me</h2>
    <p class="lede">Every look at your record leaves a line here — one per credential, even inside a batch, and even when the check failed.</p>
    ${list.length?`<div class="tablewrap"><table><tr><th>When</th><th>Who asked</th><th>Why</th><th>Outcome</th></tr>
      ${list.map(p=>`<tr><td>${when(p.createdAt)}</td><td><code>${short(p.requestedByPartyId)||"(bare scan)"}</code></td>
        <td>${esc(p.purpose||"—")}</td><td><span class="pill ${p.outcome==="valid"?"ok":"mut"}">${esc(p.outcome)}</span></td></tr>`).join("")}
    </table></div>`:`<div class="empty">Nobody has checked your record.</div>`}`;
}

async function workerConsent(){
  const me = S.me.partyId;
  const out = await api.get("parties", `/v1/parties/${encodeURIComponent(me)}/consents`);
  const list = out.consents||[];
  const states = out.enrolmentConsent||{};
  return `<h2>What I agreed to</h2>
    <p class="lede">Consent is per programme, and withdrawing it stops new evidence being collected about you — it never touches what you were already paid.</p>
    ${Object.keys(states).length?Object.entries(states).map(([ctx,st])=>`<div class="card">
      <div class="row"><h3>${esc(short(ctx))}</h3><span class="pill ${st==="GRANTED"?"ok":"risk"}">${esc(st)}</span></div></div>`).join(""):""}
    ${list.length?`<div class="tablewrap"><table><tr><th>Kind</th><th>Captured</th><th>State</th><th></th></tr>
      ${list.map(c=>`<tr><td>${esc(c.moment||c.kind||"")}</td><td>${esc(c.captureMethod||"")} · ${when(c.capturedAt||c.createdAt)}</td>
        <td><span class="pill ${c.state==="GRANTED"?"ok":"risk"}">${esc(c.state)}</span></td>
        <td>${c.state==="GRANTED"?`<button class="btn small danger" data-withdraw="${esc(c.id)}">Withdraw</button>`:""}</td></tr>`).join("")}
    </table></div>`:`<div class="empty">No consents recorded.</div>`}`;
}

/* ————— supervisor face ————— */
function supervisorBatch(){
  return Promise.resolve(`<h2>A spreadsheet arrived</h2>
    <p class="lede">The file is checked against the definition, row by row. A row that does not match is somebody named in the unclear queue — never a silent drop.</p>
    <div class="card"><form class="stack" id="batchform">
      <label>CSV (canonical or mapped columns)<textarea name="csv" rows="6">activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,household_id,source_record_ref
bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,HH-101,web-demo-1</textarea></label>
      <button class="btn primary">Submit the batch</button>
    </form><div id="batchout"></div></div>`);
}

async function supervisorUnreached(){
  const out = await api.get("confirmation", "/v1/unreached");
  const list = out.windows||out.unreached||[];
  return `<h2>Who holds it next</h2>
    <p class="lede">Workers whose window is open and who could not be told. For a worker with no phone, you are the route — an assisted confirmation is one of the four exits, recorded as itself with your name on it.</p>
    ${list.length?list.map(w=>`<div class="card"><div class="row">
      <h3><code>${short(w.claimId)}</code></h3><span class="pill hold">closes ${when(w.closesAt)}</span>
      <button class="btn primary small" data-assist="${esc(w.claimId)}">Confirm what you saw</button></div>
      <div class="kv"><b>worker</b><code>${short(w.partyId)}</code></div></div>`).join("")
    :`<div class="empty">Nobody is waiting on you.</div>`}`;
}

async function sharedUnclear(face){
  const out = await api.get("evidence", "/v1/unclear");
  const list = (out.unclear||[]).filter(u=>!u.resolvedAt);
  return `<h2>${face==="custodian"?"Whose work was this row":"Evidence that did not match"}</h2>
    <p class="lede">A mismatch is somebody named, not a status. ${face==="custodian"?"Attributing a row is a decision with your name on it, checked against your authorization.":"The custodian decides whose work an unattributed row was — the submitter deliberately cannot."}</p>
    ${list.length?`<div class="tablewrap"><table><tr><th>Row</th><th>Kind</th><th>Why it is here</th>${face==="custodian"?"<th></th>":""}</tr>
      ${list.map(u=>`<tr><td><code>${esc(u.rowRef||u.id)}</code></td><td>${esc(u.kind)}</td><td>${esc(u.reason)}</td>
      ${face==="custodian"?`<td><form class="row" data-unclear="${esc(u.id)}"><input name="party" placeholder="did:crest:party:…" size="24" required><button class="btn small primary">Attribute</button></form></td>`:""}</tr>`).join("")}
    </table></div>`:`<div class="empty">The queue is empty.</div>`}`;
}

/* ————— custodian face ————— */
function custodianFind(){
  return Promise.resolve(`<h2>Find a worker</h2>
    <p class="lede">By the identifier you have — strongest first. An ambiguous match holds; it never guesses, and it never merges.</p>
    <div class="card"><form class="stack" id="findform">
      <label>Kind<select name="kind"><option value="">any (precedence order)</option><option>national-id-hash</option><option>contact-route</option><option>roster-id</option></select></label>
      <label>Value<input name="value" placeholder="+15550100011 or roster id" required></label>
      <label>Context (for roster ids)<input name="ctx" value="${esc(FIX.project)}"></label>
      <button class="btn primary">Resolve</button>
    </form><div id="findout"></div></div>`);
}

async function custodianDupes(){
  const out = await api.get("parties", "/v1/holds");
  const metrics = await api.get("parties", "/v1/holds/metrics").catch(()=>null);
  const list = (out.holds||[]).filter(h=>!h.resolvedAt);
  return `<h2>Duplicates — the queue, and the rule for closing one</h2>
    <p class="lede">Two records collide on an identifier. The queue shows existence, never the identifier itself. A merge needs the worker's confirmation; <code>merges_without_confirmation</code> is a number you can read, and it must be zero.</p>
    ${metrics?`<div class="row" style="margin-bottom:12px"><span class="chip">merges without confirmation <b>${metrics.mergesWithoutConfirmation ?? metrics.merges_without_confirmation ?? 0}</b></span></div>`:""}
    ${list.length?list.map(h=>`<div class="card">
      <div class="row"><h3>Collision on <code>${esc(h.keyKind)}</code></h3><span class="pill hold">${esc(h.reason)}</span></div>
      <div class="kv"><b>candidates</b>${(h.candidates||[]).map(c=>`<code>${short(c)}</code>`).join(" · ")}</div>
      <form class="stack" data-hold="${esc(h.id)}" style="margin-top:8px">
        <label>Decision<select name="decision"><option value="distinct">distinct — two people share the identifier</option><option value="merge">merge — one person recorded twice</option></select></label>
        <label>The identifier belongs to<select name="party">${(h.candidates||[]).map(c=>`<option>${esc(c)}</option>`).join("")}</select></label>
        <label>Worker confirmation (merge only) — method<input name="method" placeholder="in-person"></label>
        <button class="btn primary">Close the hold</button>
      </form></div>`).join("")
    :`<div class="empty">No open holds.</div>`}`;
}

async function custodianRecoveries(){
  const out = await api.get("parties", "/v1/recoveries");
  const list = out.recoveries||[];
  return `<h2>Recoveries</h2>
    <p class="lede">A lost handset must not cost anyone their history. Two voices from different authorities decide it; the operator override can never be quiet, and never comes from the worker's own supervisor.</p>
    <div class="card"><form class="row" id="recopen">
      <input name="party" placeholder="worker party id" size="30" required>
      <input name="reason" placeholder="reason" size="26" required>
      <button class="btn primary small">Open a recovery</button></form></div>
    ${list.length?list.map(r=>`<div class="card">
      <div class="row"><h3><code>${short(r.partyId)}</code></h3>
        <span class="pill ${r.state==="COMPLETED"?"ok":r.state==="OVERRIDDEN"?"hold":"info"}">${esc(r.state)}</span></div>
      <div class="kv"><b>reason</b>${esc(r.reason)}</div>
      <div class="kv"><b>voices</b>${(r.confirmations||[]).length} (distinct authorities)</div>
      ${r.overrideByPartyId?`<div class="kv"><b>override</b>${esc(r.overrideReason||"")} — <code>${short(r.overrideByPartyId)}</code>, review by ${when(r.reviewBy)}</div>`:""}
      ${(r.state==="CONFIRMED"||r.state==="OVERRIDDEN")?`<form class="row" data-reccomplete="${esc(r.id)}"><input name="subject" placeholder="new subject ref" size="28" required><button class="btn small primary">Bind the new subject</button></form>`:""}
    </div>`).join(""):`<div class="empty">No recoveries.</div>`}`;
}

async function custodianReview(){
  const out = await api.get("parties", "/v1/authorizations/overdue");
  const over = await api.get("parties", "/v1/recoveries?overdue=true").catch(()=>({recoveries:[]}));
  return `<h2>Overdue for review</h2>
    <p class="lede">Passing a review date changes nothing by itself — the grant keeps working, the override keeps standing. What it must never be is unseen. This is where it is seen.</p>
    <div class="card"><h3>Authorizations past review-by</h3>
    ${(out.authorizations||[]).length?`<div class="tablewrap"><table><tr><th>Party</th><th>Functions</th><th>Review by</th></tr>
      ${out.authorizations.map(a=>`<tr><td><code>${short(a.partyId)}</code></td><td>${(a.functions||[]).join(", ")}</td><td>${when(a.reviewBy)}</td></tr>`).join("")}</table></div>`
    :`<div class="empty">Nothing overdue.</div>`}</div>
    <div class="card"><h3>Overrides past review-by</h3>
    ${(over.recoveries||[]).length?over.recoveries.map(r=>`<div class="kv"><b>${short(r.partyId)}</b>${esc(r.overrideReason||"")}</div>`).join(""):`<div class="empty">None.</div>`}</div>`;
}

/* ————— project face ————— */
async function projectFunnel(){
  const [unclear, unreleased, unreached, metrics] = await Promise.all([
    api.get("evidence","/v1/unclear").catch(()=>({unclear:[]})),
    api.get("confirmation","/v1/unreleased").catch(()=>({windows:[],unreleased:[]})),
    api.get("confirmation","/v1/unreached").catch(()=>({windows:[],unreached:[]})),
    api.get("parties","/v1/holds/metrics").catch(()=>null),
  ]);
  const n = x => (x.windows||x.unreleased||x.unreached||x.unclear||[]).filter(u=>!u.resolvedAt).length;
  return `<h2>PRJ-118 · where it stands</h2>
    <p class="lede">A funnel, not a set of totals: every stuck thing here has an owning role. The full metric contracts — straight-through rate, tier mix, payment-delay taxonomy — are <a href="https://github.com/theflywheel/CREST/issues/31">#31</a> and are not built yet; what is below is real.</p>
    <div class="grid2">
      <div class="card"><h3>Unclear evidence</h3><p class="money" style="font-size:28px">${n(unclear)}</p><p class="lede">rows waiting for the custodian</p></div>
      <div class="card"><h3>Unreleased exits</h3><p class="money" style="font-size:28px">${n(unreleased)}</p><p class="lede">windows closed, payment not yet confirmed released</p></div>
      <div class="card"><h3>Unreachable workers</h3><p class="money" style="font-size:28px">${n(unreached)}</p><p class="lede">open windows nobody could be told about</p></div>
      <div class="card"><h3>Merges without confirmation</h3><p class="money" style="font-size:28px">${metrics?(metrics.mergesWithoutConfirmation ?? 0):"—"}</p><p class="lede">the invariant, as a number (must be 0)</p></div>
    </div>`;
}

async function projectPayments(){
  const out = await api.get("payments","/v1/instructions");
  const rec = await api.get("payments","/v1/reconciliation").catch(()=>null);
  const list = out.instructions||[];
  const held = list.filter(i=>i.state==="HELD");
  return `<h2>Payments, and where the delay is</h2>
    <p class="lede">Money delayed and people waiting are different sentences. Every held payment carries a reason with an owner — the database refuses it otherwise.</p>
    <div class="row" style="margin-bottom:12px">
      <span class="chip">instructions <b>${list.length}</b></span>
      <span class="chip">held <b>${held.length}</b></span>
      ${rec?`<span class="chip">reconciliation gaps <b>${(rec.gaps||[]).length}</b></span>`:""}
    </div>
    <div class="tablewrap"><table><tr><th>Worker</th><th>Amount</th><th>State</th><th>Route</th><th>Held: reason — owner</th></tr>
    ${list.slice(0,50).map(i=>`<tr><td><code>${short(i.partyId)}</code></td><td class="money">${money(i.amountMinor,i.currency)}</td>
      <td><span class="pill ${i.state==="RELEASED"?"ok":"hold"}">${esc(i.state)}</span></td><td>${esc(i.releasedBy||"")}</td>
      <td>${i.heldReason?esc(i.heldReason)+" — "+esc(i.heldOwner||""):"—"}</td></tr>`).join("")}
    </table></div>`;
}

async function projectSources(){
  const out = await api.get("evidence","/v1/sources").catch(()=>({sources:[]}));
  const assess = await api.get("verification","/v1/source-assessments").catch(()=>({assessments:[]}));
  return `<h2>Where evidence comes from</h2>
    <p class="lede">Sources are registered with what the deployment knows about them — class, capture, field mapping — and can be re-assessed at any time: a downgrade moves every affected credential's tier instantly, with nothing reissued.</p>
    ${(out.sources||[]).length?`<div class="tablewrap"><table><tr><th>System</th><th>Class</th><th>Cadence</th></tr>
      ${out.sources.map(s=>`<tr><td><code>${esc(s.systemRef)}</code></td><td>${esc(s.class||s.sourceClass||"")}</td><td>${esc(s.cadence||"—")}</td></tr>`).join("")}</table></div>`:`<div class="empty">No sources registered — fixtures submit as canonical CSV.</div>`}
    <div class="card"><h3>Current assessments</h3>
    ${(assess.assessments||[]).length?assess.assessments.map(a=>`<div class="kv"><b>${esc(a.adapterRef)}</b>capped at tier ${a.maxTier} — ${esc(a.reason)}</div>`).join(""):`<div class="empty">No source is downgraded.</div>`}</div>`;
}

/* ————— verifier face ————— */
function verifierCheck(){
  return Promise.resolve(`<h2>Scan or enter the credential</h2>
    <p class="lede">A bare check needs no account and no consent beyond the showing itself. The verdict tells you what you can check without CREST, what you are trusting, and what a green result does <em>not</em> establish.</p>
    <div class="card"><form class="stack" id="verifyform" style="max-width:none">
      <label>The credential (JSON, as scanned)<textarea name="cred" rows="8" placeholder='{"@context": …}' required></textarea></label>
      <div class="row"><input name="who" placeholder="who is asking (party id, optional)" size="30"><input name="why" placeholder="why (optional — recorded for the worker)" size="30"></div>
      <button class="btn primary">Check it</button>
    </form><div id="verdict"></div></div>`);
}

function verifierPerson(){
  return Promise.resolve(`<h2>Resolve a person</h2>
    <p class="lede">Either of a merged person's ids returns their whole chain of credentials — and nothing about the chain itself. Each look writes into the worker's own trail.</p>
    <div class="card"><form class="row" id="personform">
      <input name="party" placeholder="did:crest:party:…" size="36" required>
      <input name="who" placeholder="who is asking" size="24">
      <input name="why" placeholder="why" size="24">
      <button class="btn primary small">Resolve</button></form><div id="personout"></div></div>`);
}

function verifierBatch(){
  return Promise.resolve(`<h2>Checking many at once</h2>
    <p class="lede">A batch declares who is asking and why — in words each worker will read in their own trail — and is size-capped by the deployment. Per-credential answers only; there are deliberately no aggregates.</p>
    <div class="card"><form class="stack" id="batchcheckform" style="max-width:none">
      <label>Credentials (JSON array)<textarea name="creds" rows="6" placeholder='[{…}, {…}]' required></textarea></label>
      <div class="row"><input name="who" placeholder="who is asking (required)" size="30" required><input name="why" placeholder="purpose, 10–200 chars (required)" size="34" required></div>
      <button class="btn primary">Check the batch</button>
    </form><div id="batchcheckout"></div></div>`);
}

function renderVerdict(v){
  return `<div class="card" style="margin-top:12px">
    <div class="row"><h3>${v.valid?"Valid":"Not valid"}</h3>
      ${v.valid?`<span class="pill ok">tier ${v.tier} — computed now, never stored</span>`:""}
      ${v.revoked?`<span class="pill risk">withdrawn</span>`:""}
      ${(v.contested||[]).length?`<span class="pill hold">contested — the record, not the money</span>`:""}</div>
    ${(v.reasons||[]).map(r=>`<div class="kv"><b>because</b>${esc(r)}</div>`).join("")}
    ${(v.trustChain||[]).map(l=>`<div class="link"><span class="lk ${l.checkable?"ok":"tr"}">${l.checkable?"checkable":"trusting"}</span><span>${esc(l.claim)}<span style="color:var(--muted)"> — ${esc(l.how||l.trusting||"")}</span></span></div>`).join("")}
    ${(v.notEstablished||[]).map(n=>`<div class="link"><span class="lk tr">not established</span><span style="color:var(--muted)">${esc(n)}</span></div>`).join("")}
  </div>`;
}

/* ————— agent face ————— */
function agentRegister(){
  return Promise.resolve(`<h2>Register a worker</h2>
    <p class="lede">Two pathways, neither a fallback: a phone and an OTP, or no document at all — a person with only a name still gets in, and identity binds later without losing anything already earned.</p>
    <div class="card"><form class="stack" id="regform">
      <label>Name<input name="name" required placeholder="Peter Njoroge"></label>
      <label>Contact route<select name="kind"><option value="phone">phone</option><option value="supervisor">their supervisor (no phone)</option></select></label>
      <label>Phone (if any)<input name="value" placeholder="+2547…"></label>
      <button class="btn primary">Register</button>
    </form><div id="regout"></div></div>
    <p class="note">The card is printed when the first credential is issued, not at registration — at enrolment there is no work history to print.</p>`);
}

function agentConsent(){
  return Promise.resolve(`<h2>Read this to them</h2>
    <p class="lede">Consent read aloud and recorded as voice — for a worker who cannot read the form, the recording is the only consent record that is actually theirs.</p>
    <div class="card"><form class="stack" id="agentconsent">
      <label>Worker party id<input name="party" required placeholder="did:crest:party:…"></label>
      <label>Programme<input name="ctx" value="${esc(FIX.project)}"></label>
      <label>The recording (dev stand-in: text)<textarea name="rec" rows="2">I agree that evidence of my work may be held and fetched.</textarea></label>
      <button class="btn primary">Record the consent</button>
    </form><div id="agentconsentout"></div></div>`);
}

function agentRecovery(){
  return Promise.resolve(`<h2>Who can confirm it is you?</h2>
    <p class="lede">Recovery is two voices from different authorities. Open one here; the voices confirm from their own logins; completion binds the worker's new subject.</p>
    <div class="card"><form class="stack" id="agentrec">
      <label>Worker party id<input name="party" required placeholder="did:crest:party:…"></label>
      <label>Why<input name="reason" required placeholder="handset lost"></label>
      <button class="btn primary">Open the recovery</button>
    </form><div id="agentrecout"></div></div>`);
}

/* ————— router ————— */
const VIEWS = {
  worker: { record: workerRecord, money: workerMoney, wallet: workerWallet, checks: workerChecks, consent: workerConsent },
  supervisor: { batch: supervisorBatch, unreached: supervisorUnreached, unclear: ()=>sharedUnclear("supervisor") },
  custodian: { find: custodianFind, dupes: custodianDupes, unclear: ()=>sharedUnclear("custodian"), recover: custodianRecoveries, review: custodianReview },
  project: { funnel: projectFunnel, payments: projectPayments, sources: projectSources },
  verifier: { check: verifierCheck, person: verifierPerson, batchcheck: verifierBatch },
  agent: { register: agentRegister, consent: agentConsent, recoverstart: agentRecovery },
};

async function render(){
  if (S.face === "login") { app.innerHTML = loginView(); bindLogin(); return; }
  const views = VIEWS[S.face];
  if (!S.view) S.view = Object.keys(views)[0];
  let content = "<div class='empty'>Loading…</div>";
  app.innerHTML = shell(content);
  try { content = await views[S.view](); } catch(e){
    content = `<div class="err">${esc(e instanceof ApiError ? `${e.status} ${e.code||""} — ${e.message}` : e.message||e)}</div>`;
  }
  app.innerHTML = shell(content);
  bindShell();
}

function bindLogin(){
  app.querySelectorAll("[data-p]").forEach(b=>b.addEventListener("click",()=>choose(personas[+b.dataset.p])));
}

function on(sel, ev, fn){ app.querySelectorAll(sel).forEach(el=>el.addEventListener(ev, fn)); }

function bindShell(){
  const lo = document.getElementById("logout");
  lo && lo.addEventListener("click", ()=>{ setSession(null); S.face="login"; S.me=null; S.err=null; render(); });
  on("[data-nav]","click",e=>{ S.view=e.currentTarget.dataset.nav; S.err=null; render(); });

  on("[data-confirm]","click",async e=>{ clearErr();
    try{ await api.post("confirmation", `/v1/claims/${encodeURIComponent(e.currentTarget.dataset.confirm)}/confirm`, {route:"self"}); render(); }catch(err){ fail(err); }});
  on("[data-dispute]","click",async e=>{ clearErr();
    const reason = prompt("Tell us what does not match. You will be paid either way.");
    if (reason===null) return;
    try{ await api.post("confirmation", `/v1/claims/${encodeURIComponent(e.currentTarget.dataset.dispute)}/dispute`, {raisedByPartyId:S.me.partyId, reason}); render(); }catch(err){ fail(err); }});
  on("[data-withdraw]","click",async e=>{ clearErr();
    try{ await api.post("parties", `/v1/consents/${encodeURIComponent(e.currentTarget.dataset.withdraw)}/withdraw`, {reason:"withdrawn by the worker"}); render(); }catch(err){ fail(err); }});
  on("[data-showdoc]","click",e=>{ const el=document.getElementById("doc-"+e.currentTarget.dataset.showdoc); if(el) el.hidden=!el.hidden; });
  on("[data-assist]","click",async e=>{ clearErr();
    try{
      const claim = e.currentTarget.dataset.assist;
      const win = await api.get("confirmation", `/v1/windows/${encodeURIComponent(claim)}`);
      actingFor(win.partyId);
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claim)}/confirm`, {route:"assisted", assistedByPartyId:S.me.partyId});
      actingFor(null); render();
    }catch(err){ actingFor(null); fail(err); }});

  const bf=document.getElementById("batchform");
  bf && bf.addEventListener("submit", async ev=>{ ev.preventDefault(); clearErr();
    const csv = bf.csv.value;
    try{
      const q = new URLSearchParams({contextId:FIX.project, definitionId:FIX.definition, submittedBy:S.me.partyId,
        sourceClass:"programme-system", captureMethod:"digital-capture", sourceExposure:"signed-batch", systemRef:"web-console"});
      const out = await api.postRaw("evidence", "/v1/batches?"+q, csv, "text/csv");
      document.getElementById("batchout").innerHTML =
        `<p class="note">${out.batch.rowsAccepted} accepted, ${out.batch.rowsUnclear} unclear of ${out.batch.rowsTotal}. ${(out.claimIds||[]).map(short).join(", ")}</p>`;
    }catch(err){ fail(err); }});

  const ff=document.getElementById("findform");
  ff && ff.addEventListener("submit", async ev=>{ ev.preventDefault(); clearErr();
    const q=new URLSearchParams(); if(ff.kind.value)q.set("kind",ff.kind.value); q.set("value",ff.value.value); q.set("contextId",ff.ctx.value);
    const out=document.getElementById("findout");
    try{ const m=await api.get("parties","/v1/resolve?"+q);
      out.innerHTML=`<p class="note">Resolved to <code>${esc(m.partyId)}</code> by <b>${esc(m.key)}</b> (confidence ${m.confidence}); enrolment consent: ${esc(m.enrolmentConsent||"")}</p>`;
    }catch(err){
      out.innerHTML = err.status===409?`<p class="note">Two records collide — a hold was raised for the custodian queue; nothing was guessed.</p>`
        : err.status===404?`<p class="note">Nobody matches.</p>`:`<div class="err">${esc(err.message)}</div>`;
    }});

  on("form[data-hold]","submit",async ev=>{ ev.preventDefault(); clearErr();
    const f=ev.currentTarget;
    const body={decision:f.decision.value, partyId:f.party.value, resolvedByPartyId:S.me.partyId};
    if(f.decision.value==="merge"){ body.confirmedByPartyId=f.party.value; body.confirmationMethod=f.method.value||"in-person"; }
    try{ await api.post("parties", `/v1/holds/${encodeURIComponent(f.dataset.hold)}/resolve`, body); render(); }catch(err){ fail(err); }});

  on("form[data-unclear]","submit",async ev=>{ ev.preventDefault(); clearErr();
    const f=ev.currentTarget;
    try{ await api.post("evidence", `/v1/unclear/${encodeURIComponent(f.dataset.unclear)}/resolve`,
      {partyId:f.party.value, resolvedByPartyId:S.me.partyId}); render(); }catch(err){ fail(err); }});

  const ro=document.getElementById("recopen");
  ro && ro.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{ await api.post("parties","/v1/recoveries",{partyId:ro.party.value, openedByPartyId:S.me.partyId, reason:ro.reason.value}); render(); }catch(err){ fail(err); }});
  on("form[data-reccomplete]","submit",async ev=>{ ev.preventDefault(); clearErr();
    const f=ev.currentTarget;
    try{ await api.post("parties",`/v1/recoveries/${encodeURIComponent(f.dataset.reccomplete)}/complete`,{subjectRef:f.subject.value}); render(); }catch(err){ fail(err); }});

  const vf=document.getElementById("verifyform");
  vf && vf.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{ const cred=JSON.parse(vf.cred.value);
      const v=await api.post("verification","/v1/verify",{credential:cred, requestedByPartyId:vf.who.value||undefined, purpose:vf.why.value||undefined});
      document.getElementById("verdict").innerHTML=renderVerdict(v);
    }catch(err){ fail(err); }});

  const pf=document.getElementById("personform");
  pf && pf.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{ const q=new URLSearchParams(); if(pf.who.value)q.set("requestedByPartyId",pf.who.value); if(pf.why.value)q.set("purpose",pf.why.value);
      const out=await api.get("verification",`/v1/parties/${encodeURIComponent(pf.party.value)}/credentials?`+q);
      document.getElementById("personout").innerHTML=
        `<p class="note">${out.count} credential(s) in this person's chain.</p>`+
        (out.credentials||[]).map(c=>`<pre class="doc">${esc(JSON.stringify(c,null,2).slice(0,1200))}…</pre>`).join("");
    }catch(err){ fail(err); }});

  const bcf=document.getElementById("batchcheckform");
  bcf && bcf.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{ const creds=JSON.parse(bcf.creds.value);
      const out=await api.post("verification","/v1/verify/batch",{credentials:creds, requestedByPartyId:bcf.who.value, purpose:bcf.why.value});
      document.getElementById("batchcheckout").innerHTML=(out.verdicts||[]).map(renderVerdict).join("");
    }catch(err){ fail(err); }});

  const rf=document.getElementById("regform");
  rf && rf.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{
      const routes=[rf.kind.value==="phone"?{kind:"phone",value:rf.value.value}:{kind:"supervisor",value:FIX.supervisor}];
      const out=await api.post("parties","/v1/enrolments",{party:{kind:"person",displayName:rf.name.value,contactRoutes:routes},
        enrolledBy:S.me.partyId, contextId:FIX.project, method:"field-visit"});
      document.getElementById("regout").innerHTML=`<p class="note">${esc(rf.name.value)} is registered: <code>${esc(out.party.id)}</code>. Assurance starts where the evidence is — being vouched for is not an identity check.</p>`;
    }catch(err){ fail(err); }});

  const ac=document.getElementById("agentconsent");
  ac && ac.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{
      actingFor(ac.party.value);
      const q=new URLSearchParams({moment:"enrolment",captureMethod:"voice",purpose:"hold and fetch evidence of my work",capturedBy:S.me.partyId,contextId:ac.ctx.value});
      const out=await api.postRaw("parties",`/v1/parties/${encodeURIComponent(ac.party.value)}/consents?`+q, ac.rec.value, "audio/ogg");
      actingFor(null);
      document.getElementById("agentconsentout").innerHTML=`<p class="note">Recorded: <code>${esc(out.id)}</code>, state ${esc(out.state)}. The worker can hear it back, and withdrawing deletes the recording and keeps the record.</p>`;
    }catch(err){ actingFor(null); fail(err); }});

  const ar=document.getElementById("agentrec");
  ar && ar.addEventListener("submit",async ev=>{ ev.preventDefault(); clearErr();
    try{ const out=await api.post("parties","/v1/recoveries",{partyId:ar.party.value, openedByPartyId:S.me.partyId, reason:ar.reason.value});
      document.getElementById("agentrecout").innerHTML=`<p class="note">Recovery <code>${short(out.id)}</code> is open. Confirmers act from their own logins; the custodian completes it.</p>`;
    }catch(err){ fail(err); }});
}

render();
