// CREST Console — one console, role-derived sessions. Each persona maps to
// one reference role flow (docs/journey-traceability.md) and its navigation
// shows only that flow's views; a route outside the persona's flow redirects
// home rather than rendering. Every view keeps its old hash (#/status …) so
// bookmarks keep working.
import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup } from "@crest/ui";
import { personas, useConsole, type PersonaKey } from "./state";
import { Status, Payments, Trace, Definition, Sources, Reports } from "./views/Project";
import { DefineWork, PaySetup, Org, Instance, Portfolio } from "./views/Admin";
import { Ratify } from "./views/Ratify";
import { Find, Dupes, Unclear, Recoveries, Review, Cases, SupportTraceNote } from "./views/Custodian";
import { OnboardApply, OnboardTerms, OnboardStatus } from "./views/Onboard";

const NAV: Record<PersonaKey, NavGroup[]> = {
  // P-1: standing configuration, not project work.
  orgadmin: [
    {
      caption: "Org admin · P-1",
      items: [
        { to: "/org", label: "Organisation" },
        { to: "/status", label: "Project status" },
        { to: "/reports", label: "Reports" },
      ],
    },
  ],
  // P-2: the operational project — monitoring subset (composition screens are
  // missing; the traceability manifest says so, this nav does not pretend).
  configurator: [
    {
      caption: "Project configurator · P-2",
      items: [
        { to: "/status", label: "Status" },
        { to: "/payments", label: "Payments" },
        { to: "/trace", label: "Trace" },
        { to: "/definition", label: "Definition" },
        { to: "/sources", label: "Sources" },
        { to: "/reports", label: "Reports" },
      ],
    },
  ],
  // P-3, the author's half: the wizard belongs to the author alone.
  author: [
    {
      caption: "Definition author · P-3",
      items: [
        { to: "/definework", label: "The wizard" },
        { to: "/definition", label: "As published" },
      ],
    },
  ],
  // P-3, the approver's half: ratifies, never drafts — no /definework here,
  // and the route guard refuses it even typed by hand.
  approver: [
    {
      caption: "Definition approver · P-3",
      items: [
        { to: "/ratify", label: "Ratify" },
        { to: "/definition", label: "As published" },
      ],
    },
  ],
  rateowner: [
    { caption: "Rate owner · F-1", items: [{ to: "/paysetup", label: "The rate" }] },
  ],
  payowner: [
    { caption: "Payment mechanism · F-2", items: [{ to: "/paysetup", label: "Rail & mechanism" }] },
  ],
  instance: [
    { caption: "Instance admin · G-1", items: [{ to: "/instance", label: "The deployment" }] },
    {
      caption: "Project view",
      items: [
        { to: "/status", label: "Status" },
        { to: "/payments", label: "Payments" },
        { to: "/trace", label: "Trace" },
        { to: "/definition", label: "Definition" },
        { to: "/sources", label: "Sources" },
      ],
    },
  ],
  custodian: [
    {
      caption: "Registry custodian · G-4",
      items: [
        { to: "/find", label: "Find a worker" },
        { to: "/dupes", label: "Duplicates" },
        { to: "/unclear", label: "Unclear rows" },
        { to: "/recover", label: "Recoveries" },
        { to: "/review", label: "Overdue reviews" },
      ],
    },
  ],
  support: [
    {
      caption: "Support agent · W-3",
      items: [
        { to: "/cases", label: "Open cases" },
        { to: "/supportfind", label: "Find a worker" },
        { to: "/supporttrace", label: "Payment trace" },
      ],
    },
  ],
  funder: [
    {
      caption: "Funding oversight · V-4",
      items: [
        { to: "/portfolio", label: "Portfolio" },
        { to: "/status", label: "Project status" },
      ],
    },
  ],
};

const homeOf = (key: PersonaKey) => NAV[key][0].items[0].to;
const allowed = (key: PersonaKey, path: string) =>
  NAV[key].some((g) => g.items.some((i) => i.to === path));

function LoginPage() {
  const s = useConsole();
  const nav = useNavigate();
  const pick = async (i: number) => {
    s.clearErr();
    try {
      const key = await s.login(i);
      nav(homeOf(key));
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST Console</span>
        <span className="who">
          <span className="who-label">Not signed in</span>
        </span>
      </div>
      <div className="console-body">
        <main className="pane">
          <div className="screen pane-wide">
            <div className="pagehead">
              <h2 className="scr-title">Who is at the console?</h2>
            </div>
            <p className="muted" style={{ maxWidth: 700 }}>
              One console, role-derived sessions: each card is one reference role flow, and its navigation shows only
              that flow's views. In this dev build, signing in mints a token from the stack's own identity provider
              and binds it through the real first-login path.
            </p>
            {s.err ? <ErrBar>{s.err}</ErrBar> : null}
            <button
              className="card hi"
              id="onboard-org"
              style={{ textAlign: "left", cursor: "pointer", maxWidth: 700 }}
              onClick={() => nav("/onboard")}
            >
              <div style={{ font: "500 14px/1.4 Roboto" }}>Onboard your organisation</div>
              <div className="muted">Apply, accept the published terms, get approved — the real flow, no seeded party.</div>
            </button>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))",
                gap: 12,
              }}
            >
              {personas.map((p, i) => (
                <button
                  className="card"
                  data-p={i}
                  data-persona={p.key}
                  key={p.key}
                  style={{ textAlign: "left", cursor: "pointer" }}
                  onClick={() => pick(i)}
                >
                  <div style={{ font: "500 14px/1.4 Roboto" }}>
                    {p.who}{" "}
                    <span className="muted">
                      · {p.role} · {p.ref}
                    </span>
                  </div>
                  <div className="muted">{p.what}</div>
                </button>
              ))}
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}

function Shell() {
  const s = useConsole();
  const loc = useLocation();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!s.me || !s.persona) return <LoginPage />;
  // The role boundary: a view outside this persona's flow is not rendered —
  // an approver who types #/definework lands back on their own home.
  if (!allowed(s.persona, loc.pathname)) return <Navigate to={homeOf(s.persona)} replace />;
  return (
    <ConsoleShell
      appName="CREST Console"
      who={
        <>
          <span className="who-label">
            {s.me.who} · {s.me.role}
          </span>
          <button id="logout" onClick={s.logout}>
            Switch person
          </button>
        </>
      }
      nav={NAV[s.persona]}
    >
      <div className="screen" key={loc.pathname} style={{ display: "flex", flexDirection: "column", gap: 15 }}>
        {s.err ? <ErrBar>{s.err}</ErrBar> : null}
        <Outlet />
      </div>
    </ConsoleShell>
  );
}

export function App() {
  const s = useConsole();
  return (
    <Routes>
      {/* Programme onboarding (#155 phase D): reachable with no persona —
          applying is the open bootstrap by design (#20). */}
      <Route path="/onboard" element={<OnboardApply />} />
      <Route path="/onboard/terms" element={<OnboardTerms />} />
      <Route path="/onboard/status" element={<OnboardStatus />} />
      <Route element={<Shell />}>
        <Route path="/status" element={<Status />} />
        <Route path="/payments" element={<Payments />} />
        <Route path="/trace" element={<Trace />} />
        <Route path="/definition" element={<Definition />} />
        <Route path="/sources" element={<Sources />} />
        <Route path="/reports" element={<Reports />} />
        <Route path="/definework" element={<DefineWork />} />
        <Route path="/ratify" element={<Ratify />} />
        <Route path="/paysetup" element={<PaySetup />} />
        <Route path="/org" element={<Org />} />
        <Route path="/instance" element={<Instance />} />
        <Route path="/portfolio" element={<Portfolio />} />
        <Route path="/find" element={<Find />} />
        <Route path="/dupes" element={<Dupes />} />
        <Route path="/unclear" element={<Unclear />} />
        <Route path="/recover" element={<Recoveries />} />
        <Route path="/review" element={<Review />} />
        <Route path="/cases" element={<Cases />} />
        <Route path="/supportfind" element={<Find support />} />
        <Route
          path="/supporttrace"
          element={
            <>
              <SupportTraceNote />
              <Trace />
            </>
          }
        />
        <Route path="*" element={<Navigate to={s.persona ? homeOf(s.persona) : "/status"} replace />} />
      </Route>
    </Routes>
  );
}
