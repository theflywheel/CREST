// CREST Console — one console, role-derived sessions. Each persona maps to
// one reference role flow (docs/journey-traceability.md); every view keeps its
// old hash (#/status …) so bookmarks and tests keep working.
//
// The J3 actors (P-1 Org Admin, P-2 Project Configurator) navigate by the
// reference's own rails rather than a per-persona list, because the reference
// draws THREE of them across the 24 J3 frames:
//
//   setup      Projects · People & roles · Work definitions · Payment set up · Workers   (p1_*, p2_1–p2_7, p2_17–p2_21)
//   dashboard  Work status · Quality · Payments · Proof · Reports                        (p2_11–p2_16)
//   finance    Project · Work definitions · Finance · Support · Dashboard                (p2_8–p2_10)
//
// Within a section the rail is IDENTICAL for both J3 actors and no entry is
// ever removed by role — that is finding F1's rule, and screen n3 states it on
// its own face. What differs is what an entry does, and an entry you cannot
// act on says who can (n5). The "one rail across all of J3" reading of F1 is
// corrected in docs/design/j3-connective-tissue/README.md: it holds per
// section, not across all 24 frames.
import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup, type NavItem } from "@crest/ui";
import { useConsole, type PersonaKey } from "./state";
import { Definition, Sources } from "./views/Project";
import { Status, Stp, Quality, Payments, Trace, Reports } from "./views/Dashboard";
import { DefineWork, PaySetup, Instance, Portfolio } from "./views/Admin";
import { Ratify } from "./views/Ratify";
import { Find, Dupes, Unclear, Recoveries, Review, Cases, SupportTraceNote } from "./views/Custodian";
import { OnboardApply, OnboardTerms, OnboardStatus, OnboardChecks } from "./views/Onboard";
import {
  OnboardStandalone, OnboardWider, OnboardDocuments, OnboardReview, OnboardInvited, OnboardProject,
} from "./views/OnboardOrg";
import { SignIn } from "./views/SignIn";
import {
  Projects as OrgHome, NewProject, People, Workers, Validation, Intake, Finance as FinanceConnect, Navigation,
} from "./views/Setup";
import {
  Where, Handover, Compose, Owners, Activate, FinanceCode, SupportOwner, Partners,
} from "./views/J3";

// ── The reference's three J3 rails ─────────────────────────────────────────
const SETUP_RAIL: NavItem[] = [
  { to: "/org", label: "Projects" },
  { to: "/people", label: "People & roles" },
  { to: "/definition", label: "Work definitions" },
  { to: "/paysetup", label: "Payment set up" },
  { to: "/workers", label: "Workers" },
];
const DASHBOARD_RAIL: NavItem[] = [
  { to: "/status", label: "Work status" },
  { to: "/quality", label: "Quality" },
  { to: "/payments", label: "Payments" },
  { to: "/trace", label: "Proof" },
  { to: "/reports", label: "Reports" },
];
const FINANCE_RAIL: NavItem[] = [
  { to: "/org", label: "Project" },
  { to: "/definition", label: "Work definitions" },
  { to: "/finance", label: "Finance" },
  { to: "/support", label: "Support" },
  { to: "/status", label: "Dashboard" },
];

const DASHBOARD_ROUTES = ["/status", "/quality", "/stp", "/payments", "/trace", "/reports"];
const FINANCE_ROUTES = ["/finance", "/finance/connect", "/support"];

// Every route a J3 actor may open. The rail shows five entries per section;
// this is the whole surface behind them, including the screens reached by a
// frame's own buttons rather than by the rail.
const J3_ROUTES = [
  "/org", "/people", "/projects", "/projects/new", "/definition", "/paysetup", "/workers",
  "/validation", "/intake", "/sources", "/where", "/handover", "/compose", "/owners",
  "/activate", "/partners", ...DASHBOARD_ROUTES, ...FINANCE_ROUTES,
];

function railFor(pathname: string): NavGroup[] {
  // No caption: the reference's J3 rail is five plain entries, and a caption
  // here would be one more thing to get wrong about whose console this is.
  if (DASHBOARD_ROUTES.includes(pathname)) return [{ items: DASHBOARD_RAIL }];
  if (FINANCE_ROUTES.includes(pathname)) return [{ items: FINANCE_RAIL }];
  return [{ items: SETUP_RAIL }];
}

const NAV: Record<PersonaKey, NavGroup[]> = {
  // P-1 and P-2 navigate by the reference's rails (railFor); these entries are
  // their default landing places.
  orgadmin: [{ caption: "Org admin · P-1", items: SETUP_RAIL }],
  configurator: [{ caption: "Project configurator · P-2", items: SETUP_RAIL }],
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
  rateowner: [{ caption: "Rate owner · F-1", items: [{ to: "/paysetup", label: "The rate" }] }],
  payowner: [{ caption: "Payment mechanism · F-2", items: [{ to: "/paysetup", label: "Rail & mechanism" }] }],
  instance: [
    { caption: "Instance admin · G-1", items: [{ to: "/instance", label: "The deployment" }] },
    {
      caption: "Project view",
      items: [
        { to: "/status", label: "Work status" },
        { to: "/payments", label: "Payments" },
        { to: "/trace", label: "Proof" },
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
        { to: "/status", label: "Work status" },
      ],
    },
  ],
};

const isJ3 = (key: PersonaKey) => key === "orgadmin" || key === "configurator";
const homeOf = (key: PersonaKey) => (isJ3(key) ? "/org" : NAV[key][0].items[0].to);
const allowed = (key: PersonaKey, path: string) =>
  (isJ3(key) && J3_ROUTES.includes(path)) || NAV[key].some((g) => g.items.some((i) => i.to === path));

function Shell() {
  const s = useConsole();
  const loc = useLocation();
  const nav = useNavigate();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!s.me || !s.persona)
    return (
      <SignIn
        onSignedIn={async (i) => {
          s.clearErr();
          try {
            const key = await s.login(i);
            nav(homeOf(key));
          } catch (e) {
            s.fail(e);
          }
        }}
      />
    );
  // The role boundary: a view outside this persona's flow is not rendered —
  // an approver who types #/definework lands back on their own home. For the
  // J3 actors nothing is hidden: both hold every route, and a screen they
  // cannot write to says who can instead of vanishing.
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
      nav={isJ3(s.persona) ? railFor(loc.pathname) : NAV[s.persona]}
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
      <Route path="/onboard/checks" element={<OnboardChecks />} />
      <Route path="/onboard/status" element={<OnboardStatus />} />
      {/* G-2 after registration: the organisation's standing view (g2_5–g2_10).
          Same open door as /onboard — the session acts as the organisation the
          flow registered, proven by the same first-login bind, not asserted. */}
      <Route path="/onboard/standalone" element={<OnboardStandalone />} />
      <Route path="/onboard/wider" element={<OnboardWider />} />
      <Route path="/onboard/documents" element={<OnboardDocuments />} />
      <Route path="/onboard/review" element={<OnboardReview />} />
      <Route path="/onboard/invited" element={<OnboardInvited />} />
      <Route path="/onboard/project" element={<OnboardProject />} />
      <Route element={<Shell />}>
        {/* J3 — setting up a project */}
        <Route path="/org" element={<OrgHome />} />
        <Route path="/projects" element={<Navigation />} />
        <Route path="/projects/new" element={<NewProject />} />
        <Route path="/people" element={<People />} />
        <Route path="/workers" element={<Workers />} />
        <Route path="/validation" element={<Validation />} />
        <Route path="/intake" element={<Intake />} />
        <Route path="/finance" element={<FinanceCode />} />
        <Route path="/finance/connect" element={<FinanceConnect />} />
        <Route path="/support" element={<SupportOwner />} />
        {/* J3 Phase B — the screens the project backend unblocked */}
        <Route path="/where" element={<Where />} />
        <Route path="/handover" element={<Handover />} />
        <Route path="/compose" element={<Compose />} />
        <Route path="/owners" element={<Owners />} />
        <Route path="/activate" element={<Activate />} />
        <Route path="/partners" element={<Partners />} />
        {/* J3 — the project's own dashboard wave */}
        <Route path="/status" element={<Status />} />
        <Route path="/stp" element={<Stp />} />
        <Route path="/quality" element={<Quality />} />
        <Route path="/payments" element={<Payments />} />
        <Route path="/trace" element={<Trace />} />
        <Route path="/reports" element={<Reports />} />
        <Route path="/definition" element={<Definition />} />
        <Route path="/sources" element={<Sources />} />
        <Route path="/definework" element={<DefineWork />} />
        <Route path="/ratify" element={<Ratify />} />
        <Route path="/paysetup" element={<PaySetup />} />
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
