// CREST Console — one console, role-based views, ported 1:1 from
// apps/console. The persona chosen at login decides the sidebar; every view
// keeps its old hash (#/status …) so bookmarks and the Playwright walk both
// keep working.
import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup } from "@crest/ui";
import { personas, useConsole, type PersonaKey } from "./state";
import { Status, Payments, Trace, Definition, Sources, Reports } from "./views/Project";
import { DefineWork, PaySetup, Org, Instance, Portfolio } from "./views/Admin";
import { Find, Dupes, Unclear, Recoveries, Review, Cases, SupportTraceNote } from "./views/Custodian";
import { OnboardApply, OnboardTerms, OnboardStatus } from "./views/Onboard";

const NAV: Record<PersonaKey, NavGroup[]> = {
  org: [
    {
      caption: "Project view",
      items: [
        { to: "/status", label: "Status" },
        { to: "/payments", label: "Payments" },
        { to: "/trace", label: "Trace" },
        { to: "/definition", label: "Definition" },
        { to: "/sources", label: "Sources" },
        { to: "/reports", label: "Reports" },
      ],
    },
    { caption: "Define work", items: [{ to: "/definework", label: "The wizard" }] },
    { caption: "Payment set up", items: [{ to: "/paysetup", label: "Rate & rail" }] },
    { caption: "Organisation view", items: [{ to: "/org", label: "Organisation" }] },
    { caption: "Funder", items: [{ to: "/portfolio", label: "Portfolio" }] },
  ],
  custodian: [
    {
      caption: "Registry",
      items: [
        { to: "/find", label: "Find a worker" },
        { to: "/dupes", label: "Duplicates" },
        { to: "/unclear", label: "Unclear rows" },
        { to: "/recover", label: "Recoveries" },
        { to: "/review", label: "Overdue reviews" },
      ],
    },
    {
      caption: "Support",
      items: [
        { to: "/cases", label: "Open cases" },
        { to: "/supportfind", label: "Find a worker" },
        { to: "/supporttrace", label: "Payment trace" },
      ],
    },
  ],
  instance: [
    { caption: "Instance view", items: [{ to: "/instance", label: "The deployment" }] },
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
};

function LoginPage() {
  const s = useConsole();
  const nav = useNavigate();
  const pick = async (i: number) => {
    s.clearErr();
    try {
      const key = await s.login(i);
      nav(NAV[key][0].items[0].to);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <div className="panel-shell screen">
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <div style={{ width: 24, height: 24, borderRadius: 5, background: "var(--p1)" }} />
        <div style={{ font: "500 16px/1 Roboto" }}>CREST Console</div>
      </div>
      <p className="muted">
        One console, role-based views. In this dev build, signing in mints a token from the stack's own identity
        provider and binds it through the real first-login path.
      </p>
      {s.err ? <ErrBar>{s.err}</ErrBar> : null}
      <button
        className="card hi"
        id="onboard-org"
        style={{ textAlign: "left", cursor: "pointer" }}
        onClick={() => nav("/onboard")}
      >
        <div style={{ font: "500 14px/1.4 Roboto" }}>Onboard your organisation</div>
        <div className="muted">Apply, accept the published terms, get approved — the real flow, no seeded party.</div>
      </button>
      {personas.map((p, i) => (
        <button className="card" data-p={i} key={i} style={{ textAlign: "left", cursor: "pointer" }} onClick={() => pick(i)}>
          <div style={{ font: "500 14px/1.4 Roboto" }}>
            {p.who} <span className="muted">· {p.role}</span>
          </div>
          <div className="muted">{p.what}</div>
        </button>
      ))}
    </div>
  );
}

function Shell() {
  const s = useConsole();
  const loc = useLocation();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!s.me || !s.persona) return <LoginPage />;
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
        <Route path="*" element={<Navigate to={s.persona ? NAV[s.persona][0].items[0].to : "/status"} replace />} />
      </Route>
    </Routes>
  );
}
