// CREST Console — one console, role-based views (Actor Journeys: "One console
// · instance view / organisation view / project view / define work / payment
// set up"). Desktop web: app bar + sidebar. Real data everywhere a service
// answers; the reference's own honesty labels everywhere one does not.

import { api, ApiError, loginAs, setSession } from "../shared/api.js";
import { FIX } from "../shared/fixtures.js";
import { esc, errbar, chip, openNote } from "./ui.js";
import {
  projectStatus, projectPayments, projectTrace, projectDefinition,
  projectSources, projectReports,
} from "./views-project.js";
import {
  defineWork, paymentSetup, orgView, instanceView, funderPortfolio,
} from "./views-admin.js";
import {
  custodianFind, custodianDupes, custodianUnclear, custodianRecoveries,
  custodianReview, supportCases, supportTraceSidecar,
} from "./views-custodian.js";

// Personas. The instance administrator reuses FIX.org: the fixture world
// seeds the instance-scoped grants (attest-work at instance scope, the
// specifier chain) on the organisation party, because in this deployment the
// programme organisation is also the deployment operator — a real deployment
// would separate them.
const personas = [
  { key: "org", id: FIX.org, who: "Ministry of Health", role: "org admin · project",
    what: "PRJ-118 — the funnel, the payments, the definition, the money set up" },
  { key: "custodian", id: FIX.custodian, who: "Otieno", role: "registry custodian",
    what: "Duplicates, unattributed rows, recoveries, the review queue — and support" },
  { key: "instance", id: FIX.org, who: "Instance administrator", role: "instance view",
    what: "The deployment itself: services, issuer, consent floor, admission" },
];

// Sidebar groups per persona; every item is a hash route.
const NAV = {
  org: [
    ["Project view", [
      ["status", "Status"], ["payments", "Payments"], ["trace", "Trace"],
      ["definition", "Definition"], ["sources", "Sources"], ["reports", "Reports"],
    ]],
    ["Define work", [["definework", "The wizard"]]],
    ["Payment set up", [["paysetup", "Rate & rail"]]],
    ["Organisation view", [["org", "Organisation"]]],
    ["Funder", [["portfolio", "Portfolio"]]],
  ],
  custodian: [
    ["Registry", [
      ["find", "Find a worker"], ["dupes", "Duplicates"], ["unclear", "Unclear rows"],
      ["recover", "Recoveries"], ["review", "Overdue reviews"],
    ]],
    ["Support", [
      ["cases", "Open cases"], ["supportfind", "Find a worker"], ["supporttrace", "Payment trace"],
    ]],
  ],
  instance: [
    ["Instance view", [["instance", "The deployment"]]],
    ["Project view", [
      ["status", "Status"], ["payments", "Payments"], ["trace", "Trace"],
      ["definition", "Definition"], ["sources", "Sources"],
    ]],
  ],
};

const VIEWS = {
  status: projectStatus,
  payments: projectPayments,
  trace: s => projectTrace(s),
  definition: projectDefinition,
  sources: projectSources,
  reports: projectReports,
  definework: s => defineWork(s),
  paysetup: paymentSetup,
  org: orgView,
  instance: instanceView,
  portfolio: funderPortfolio,
  find: s => custodianFind(s, false),
  dupes: custodianDupes,
  unclear: custodianUnclear,
  recover: s => custodianRecoveries(s, S.me && S.me.partyId),
  review: custodianReview,
  cases: supportCases,
  supportfind: s => custodianFind(s, true),
  supporttrace: async s => supportTraceSidecar() + await projectTrace(s),
};

const S = { me: null, persona: null, view: null, err: null, traceClaim: "", wizStep: 0 };
const app = document.getElementById("app");

function fail(e) {
  S.err = e instanceof ApiError ? `${e.status} ${e.code || ""} — ${e.message}` : String(e && e.message || e);
  render();
}

/* ————— login ————— */
function loginPage() {
  app.innerHTML = `<div class="panel-shell screen">
    <div style="display:flex;align-items:center;gap:10px">
      <div style="width:24px;height:24px;border-radius:5px;background:var(--p1)"></div>
      <div style="font:500 16px/1 Roboto">CREST Console</div>
    </div>
    <p class="muted">One console, role-based views. In this dev build, signing in mints a token from the stack's own identity provider and binds it through the real first-login path.</p>
    ${S.err ? errbar({ message: S.err }) : ""}
    ${personas.map((p, i) => `<button class="card" data-p="${i}" style="text-align:left;cursor:pointer">
      <div style="font:500 14px/1.4 Roboto">${esc(p.who)} <span class="muted">· ${esc(p.role)}</span></div>
      <div class="muted">${esc(p.what)}</div>
    </button>`).join("")}
  </div>`;
  app.querySelectorAll("[data-p]").forEach(b => b.addEventListener("click", async () => {
    const p = personas[+b.dataset.p];
    S.err = null;
    try {
      await loginAs(p.id);
      S.me = { partyId: p.id, who: p.who, role: p.role };
      S.persona = p.key;
      S.view = NAV[p.key][0][1][0][0];
      location.hash = "#/" + S.view;
      render();
    } catch (e) { fail(e); }
  }));
}

/* ————— shell ————— */
function shell(content) {
  const groups = NAV[S.persona] || [];
  return `<div class="console-shell">
    <div class="appbar">
      <div class="mark"></div>
      <div class="t">CREST Console</div>
      <div class="who">${esc(S.me.who)} · ${esc(S.me.role)}
        <button id="logout" style="background:none;border:1px solid #ffffff44;color:#CFE0E8;border-radius:5px;padding:4px 10px;margin-left:12px;font:400 11.5px Roboto">Switch person</button>
      </div>
    </div>
    <div class="console-body">
      <nav class="sidebar">
        ${groups.map(([cap, items]) => `<div class="cap">${esc(cap)}</div>` +
          items.map(([k, label]) => `<button class="${S.view === k ? "active" : ""}" data-nav="${k}">${esc(label)}</button>`).join("")).join("")}
      </nav>
      <main class="pane"><div class="screen" style="display:flex;flex-direction:column;gap:15px">
        ${S.err ? errbar({ message: S.err }) : ""}${content}
      </div></main>
    </div>
  </div>`;
}

async function render() {
  if (!S.me) { loginPage(); return; }
  const fn = VIEWS[S.view] || VIEWS[NAV[S.persona][0][1][0][0]];
  app.innerHTML = shell(`<div class="muted">Loading…</div>`);
  bindChrome();
  let content;
  try { content = await fn(S); }
  catch (e) {
    content = errbar(e) + openNote("The service behind this screen did not answer. In dev, bring the stack up with <span class=\"mono\">make e2e-up</span>; on a deployment, the health sweep in the Instance view names which service is down.");
  }
  app.innerHTML = shell(content);
  bindChrome();
  bindForms();
}

function go(view) {
  S.view = view; S.err = null;
  location.hash = "#/" + view;
  render();
}

function bindChrome() {
  const lo = document.getElementById("logout");
  lo && lo.addEventListener("click", () => {
    setSession(null); S.me = null; S.persona = null; S.err = null;
    location.hash = ""; render();
  });
  app.querySelectorAll("[data-nav]").forEach(b =>
    b.addEventListener("click", () => go(b.dataset.nav)));
}

function bindForms() {
  const on = (sel, ev, fn) => app.querySelectorAll(sel).forEach(el => el.addEventListener(ev, fn));

  on("[data-trace]", "click", e => {
    S.traceClaim = e.currentTarget.dataset.trace;
    go(S.persona === "custodian" ? "supporttrace" : "trace");
  });
  on("[data-go]", "click", e => go(e.currentTarget.dataset.go));
  on("[data-wiz]", "click", e => { S.wizStep = +e.currentTarget.dataset.wiz; render(); });

  const tf = document.getElementById("traceform");
  tf && tf.addEventListener("submit", ev => {
    ev.preventDefault(); S.traceClaim = tf.claim.value.trim(); render();
  });

  const ff = document.getElementById("findform");
  ff && ff.addEventListener("submit", async ev => {
    ev.preventDefault(); S.err = null;
    const q = new URLSearchParams();
    if (ff.kind.value) q.set("kind", ff.kind.value);
    q.set("value", ff.value.value);
    q.set("contextId", ff.ctx.value);
    const out = document.getElementById("findout");
    try {
      const m = await api.get("parties", "/v1/resolve?" + q);
      out.innerHTML = `<div class="sidecar ok"><div class="txt">Resolved to <span class="mono">${esc(m.partyId)}</span> by <strong>${esc(m.key)}</strong> (confidence ${esc(String(m.confidence))}); enrolment consent: ${esc(m.enrolmentConsent || "—")}</div></div>`;
    } catch (err) {
      out.innerHTML = err.status === 409
        ? `<div class="open-note">Two records collide — a hold was raised for the duplicates queue; nothing was guessed.</div>`
        : err.status === 404
          ? `<div class="card" style="color:var(--text-2)">Nobody matches.</div>`
          : errbar(err);
    }
  });

  on("form[data-hold]", "submit", async ev => {
    ev.preventDefault(); S.err = null;
    const f = ev.currentTarget;
    const body = { decision: f.decision.value, partyId: f.party.value, resolvedByPartyId: S.me.partyId };
    if (f.decision.value === "merge") {
      body.confirmedByPartyId = f.party.value;
      body.confirmationMethod = f.method.value || "in-person";
    }
    try { await api.post("parties", `/v1/holds/${encodeURIComponent(f.dataset.hold)}/resolve`, body); render(); }
    catch (err) { fail(err); }
  });

  on("form[data-unclear]", "submit", async ev => {
    ev.preventDefault(); S.err = null;
    const f = ev.currentTarget;
    try {
      await api.post("evidence", `/v1/unclear/${encodeURIComponent(f.dataset.unclear)}/resolve`,
        { partyId: f.party.value, resolvedByPartyId: S.me.partyId });
      render();
    } catch (err) { fail(err); }
  });

  const ro = document.getElementById("recopen");
  ro && ro.addEventListener("submit", async ev => {
    ev.preventDefault(); S.err = null;
    try {
      await api.post("parties", "/v1/recoveries",
        { partyId: ro.party.value, openedByPartyId: S.me.partyId, reason: ro.reason.value });
      render();
    } catch (err) { fail(err); }
  });

  on("form[data-reccomplete]", "submit", async ev => {
    ev.preventDefault(); S.err = null;
    const f = ev.currentTarget;
    try {
      await api.post("parties", `/v1/recoveries/${encodeURIComponent(f.dataset.reccomplete)}/complete`,
        { subjectRef: f.subject.value });
      render();
    } catch (err) { fail(err); }
  });
}

window.addEventListener("hashchange", () => {
  if (!S.me) return;
  const v = location.hash.replace(/^#\//, "");
  if (v && VIEWS[v] && v !== S.view) { S.view = v; S.err = null; render(); }
});

render();
