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
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup, type NavItem } from "@crest/ui";
import { useConsole, type PersonaKey } from "./state";
import { Definition, Sources, Receipt } from "./views/Project";
import { Status, Stp, Quality, Payments, Trace, Reports } from "./views/Dashboard";
import { PaySetup, Instance, Portfolio } from "./views/Admin";
import { Ratify, Ratified } from "./views/Ratify";
import {
  Registry, Sector, CountingBasis, Category, Unit, Cascade, Period, Outcome, Parties as DefParties,
  Evidence, Source, TemplateScreen, Adaptors, Mapping, Connect, DryRun, Live, Validation as DefValidation,
  Payment as DefPayment, Roles as DefRoles, Tranches, Rules as DefRules, Extend, OpenQuestions, Anatomy, Handoff,
} from "./views/Define";
import { Find, Dupes, Unclear, Recoveries, Review, Cases, SupportTraceNote, Coverage, QualityWorklist, RegistryReuse } from "./views/Custodian";
import { OnboardApply, OnboardTerms, OnboardStatus, OnboardChecks } from "./views/Onboard";
import {
  OnboardStandalone, OnboardWider, OnboardDocuments, OnboardReview, OnboardInvited, OnboardProject,
} from "./views/OnboardOrg";
import { SignIn, AuthReturn } from "./views/SignIn";
import { Claim } from "./views/Claim";
import {
  Projects as OrgHome, NewProject, People, Workers, Validation, Intake, SpreadsheetArrived, Finance as FinanceConnect, Navigation,
} from "./views/Setup";
import {
  Where, Handover, Compose, Owners, Activate, FinanceCode, SupportOwner, Partners, Vocabulary,
} from "./views/J3";
import {
  G1Setup, G1Covers, G1Consent, G1Invite, G1Services, G1People, Admissions, AdmissionDetail,
} from "./views/G1";
import {
  RateOwner, RateAuthor, RatePublish, RateStanding,
  MechWhere, MechRails, MechConnect,
  MechTest, MechRecon, MechStatement, MechBatching, MechActivate, MechQualify, MechLive,
} from "./views/Funders";

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
  "/validation", "/intake", "/intake/file", "/sources", "/where", "/handover", "/compose", "/vocabulary", "/owners",
  "/activate", "/partners", "/rateowner", ...DASHBOARD_ROUTES, ...FINANCE_ROUTES,
];

// The funders wave (F-1, F-2): the reference draws these frames on the same
// five-entry setup rail as J3, and its actors reach them by the walk's own
// buttons. /rateowner is also in J3_ROUTES because f1_2's assigner is the
// Org Admin.
const FUNDERS_ROUTES = [
  "/rateowner", "/rate", "/ratepublish", "/ratestanding",
  "/mech/where", "/mech/rails", "/mech/connect",
  "/mech/test", "/mech/recon", "/mech/statement", "/mech/batching",
  "/mech/activate", "/mech/qualify", "/mech/live",
];

// The P-3 authoring wave (reference p3_1–p3_28, p3_pay). The wizard's screens
// are reached by each frame's own buttons rather than by a rail entry, exactly
// like the funders wave — the reference draws a five-entry rail on these
// frames and navigates between them with Back/Continue.
//
// The author holds every wizard route and the two read-only screens the
// approver also holds. The approver holds NEITHER a wizard route nor the
// handoff: an approver who could open the wizard would be an author with a
// second hat, and the guard below is the third of three independent
// mechanisms that stop it (the others being this navigation and the service's
// own self-ratification refusal).
const DEFINE_ROUTES = [
  "/define/sector", "/define/counting", "/define/category", "/define/unit", "/define/cascade",
  "/define/period", "/define/outcome", "/define/parties", "/define/evidence", "/define/source",
  "/define/template", "/define/adaptors", "/define/mapping", "/define/connect", "/define/dryrun",
  "/define/live", "/define/validation", "/define/payment", "/define/roles", "/define/tranches",
  "/define/rules", "/define/extend",
];
// Read-only screens both halves of P-3 hold: the open-questions list and the
// document under the form. The approver reads everything and drafts nothing,
// and these two are reads.
const P3_READS = ["/define/open", "/define/anatomy", "/ratified"];
const AUTHOR_ROUTES = [...DEFINE_ROUTES, ...P3_READS, "/handoff"];

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
  //
  // The reference draws these frames on a five-entry rail — "Define work ·
  // Projects · Payment set up · Roles & invites · Templates". Four of those
  // five are real screens in this wizard and carry the reference's own labels;
  // "Projects" is the J3 console section's entry and is deliberately not
  // faked into the author's session, because an entry that led nowhere the
  // author holds would be worse than an absent one.
  author: [
    {
      caption: "Definition author · P-3",
      items: [
        { to: "/definework", label: "Define work" },
        { to: "/define/payment", label: "Payment set up" },
        { to: "/define/roles", label: "Roles & invites" },
        { to: "/define/template", label: "Templates" },
        { to: "/define/open", label: "Open questions" },
        { to: "/definition", label: "As published" },
      ],
    },
  ],
  // P-3, the approver's half: ratifies, never drafts — no wizard route here,
  // and the route guard refuses one even typed by hand.
  approver: [
    {
      caption: "Definition approver · P-3",
      items: [
        { to: "/ratify", label: "Ratify" },
        { to: "/define/anatomy", label: "The record itself" },
        { to: "/definition", label: "As published" },
      ],
    },
  ],
  // F-1 and F-2 navigate by the reference's own setup rail (railFor), exactly
  // like the J3 actors: the funders frames all draw the same five entries, and
  // the wave's screens are reached by each frame's own buttons. These entries
  // are their default landing places only.
  rateowner: [{ items: SETUP_RAIL }],
  payowner: [{ items: SETUP_RAIL }],
  instance: [
    // The reference's own G-1 rail: Instance · Organisations · Consent & data ·
    // Platform services · People & roles (g1_1–g1_6, g4_1–g4_3).
    {
      caption: "Instance admin · G-1",
      items: [
        { to: "/instance/covers", label: "Instance" },
        { to: "/admissions", label: "Organisations" },
        { to: "/instance/consent", label: "Consent & data" },
        { to: "/instance/services", label: "Platform services" },
        { to: "/instance/people", label: "People & roles" },
      ],
    },
    { caption: "The deployment", items: [{ to: "/instance", label: "Self-description" }] },
    {
      caption: "Project view",
      items: [
        { to: "/status", label: "Work status" },
        { to: "/payments", label: "Payments" },
        { to: "/trace", label: "Proof" },
        { to: "/definition", label: "Definition" },
        { to: "/sources", label: "Sources" },
        { to: "/receipt", label: "Evidence receipt" },
      ],
    },
  ],
  custodian: [
    {
      caption: "Registry custodian · G-4",
      items: [
        { to: "/find", label: "Find a worker" },
        { to: "/coverage", label: "Coverage" },
        { to: "/registry-quality", label: "Quality" },
        { to: "/dupes", label: "Duplicates" },
        { to: "/reuse", label: "Reuse" },
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
const isFunder = (key: PersonaKey) => key === "rateowner" || key === "payowner";
const homeOf = (key: PersonaKey) =>
  isJ3(key) ? "/org" : isFunder(key) ? "/paysetup" : NAV[key][0].items[0].to;
// The G-1 screens the rail does not name: the stand-up front door and the
// invite frame are reached by the walk's own buttons, and an admission detail
// is reached from the queue.
const G1_EXTRA = ["/instance/setup", "/instance/invite"];
const allowed = (key: PersonaKey, path: string) =>
  (isJ3(key) && J3_ROUTES.includes(path)) ||
  (key === "author" && AUTHOR_ROUTES.includes(path)) ||
  // The approver reads the record and the act, and holds no wizard route.
  (key === "approver" && P3_READS.includes(path)) ||
  (isFunder(key) && (J3_ROUTES.includes(path) || FUNDERS_ROUTES.includes(path))) ||
  (key === "instance" && (G1_EXTRA.includes(path) || path.startsWith("/admissions/"))) ||
  NAV[key].some((g) => g.items.some((i) => i.to === path));

function Shell() {
  const s = useConsole();
  const loc = useLocation();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  // The onboarding surface swaps the api session to the organisation's own
  // token; entering (or navigating) the shell puts the person back.
  useEffect(() => s.assertSession(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  // The eSignet return leg must render before the signed-out gate: the
  // callback bounces here with the token still in the route's query.
  if (loc.pathname === "/auth") return <AuthReturn />;
  if (!s.me || !s.persona) return <SignIn />;
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
      nav={isJ3(s.persona) || isFunder(s.persona) ? railFor(loc.pathname) : NAV[s.persona]}
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
      {/* Claiming an invitation (#123): reachable with no session by design —
          the person claiming has no party here yet, which is the point. */}
      <Route path="/claim/:code" element={<Claim />} />
        <Route path="/workers" element={<Workers />} />
        <Route path="/validation" element={<Validation />} />
        <Route path="/intake" element={<Intake />} />
        <Route path="/intake/file" element={<SpreadsheetArrived />} />
        <Route path="/finance" element={<FinanceCode />} />
        <Route path="/finance/connect" element={<FinanceConnect />} />
        <Route path="/support" element={<SupportOwner />} />
        {/* J3 Phase B — the screens the project backend unblocked */}
        <Route path="/where" element={<Where />} />
        <Route path="/handover" element={<Handover />} />
        <Route path="/compose" element={<Compose />} />
        <Route path="/vocabulary" element={<Vocabulary />} />
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
        {/* P-3 — the definition-authoring wizard (p3_1–p3_28, p3_pay). Every
            screen edits one real draft in the definitions service; one
            server-side compile turns it into the next immutable version. */}
        <Route path="/definework" element={<Registry />} />
        <Route path="/define/sector" element={<Sector />} />
        <Route path="/define/counting" element={<CountingBasis />} />
        <Route path="/define/category" element={<Category />} />
        <Route path="/define/unit" element={<Unit />} />
        <Route path="/define/cascade" element={<Cascade />} />
        <Route path="/define/period" element={<Period />} />
        <Route path="/define/outcome" element={<Outcome />} />
        <Route path="/define/parties" element={<DefParties />} />
        <Route path="/define/evidence" element={<Evidence />} />
        <Route path="/define/source" element={<Source />} />
        <Route path="/define/template" element={<TemplateScreen />} />
        <Route path="/define/adaptors" element={<Adaptors />} />
        <Route path="/define/mapping" element={<Mapping />} />
        <Route path="/define/connect" element={<Connect />} />
        <Route path="/define/dryrun" element={<DryRun />} />
        <Route path="/define/live" element={<Live />} />
        <Route path="/define/validation" element={<DefValidation />} />
        <Route path="/define/payment" element={<DefPayment />} />
        <Route path="/define/roles" element={<DefRoles />} />
        <Route path="/define/tranches" element={<Tranches />} />
        <Route path="/define/rules" element={<DefRules />} />
        <Route path="/define/extend" element={<Extend />} />
        <Route path="/define/open" element={<OpenQuestions />} />
        <Route path="/define/anatomy" element={<Anatomy />} />
        <Route path="/handoff" element={<Handoff />} />
        <Route path="/ratify" element={<Ratify />} />
        <Route path="/ratified" element={<Ratified />} />
        <Route path="/paysetup" element={<PaySetup />} />
        {/* The funders wave (F-1 f1_2–f1_5, F-2 f2_4–f2_10): rate ownership,
            rates as versioned terms, and the mechanism whose activation gate
            sits in front of disbursement only. */}
        <Route path="/rateowner" element={<RateOwner />} />
        <Route path="/rate" element={<RateAuthor />} />
        <Route path="/ratepublish" element={<RatePublish />} />
        <Route path="/ratestanding" element={<RateStanding />} />
        <Route path="/mech/test" element={<MechTest />} />
        <Route path="/mech/recon" element={<MechRecon />} />
        <Route path="/mech/statement" element={<MechStatement />} />
        <Route path="/mech/batching" element={<MechBatching />} />
        <Route path="/mech/activate" element={<MechActivate />} />
        <Route path="/mech/qualify" element={<MechQualify />} />
        <Route path="/mech/live" element={<MechLive />} />
        <Route path="/instance" element={<Instance />} />
        {/* w6_3, the project-side receipt for what a batch brought in
            (#197's GET /v1/batches/{id}/receipt) — lives beside Definition
            and Sources, the project's other reads, not in the verify door's
            external panel shell where w6_1/w6_2 stay. */}
        <Route path="/receipt" element={<Receipt />} />
        {/* G-1 — setting up the instance (g1_1–g1_6): the reference's frames,
            read against the deployment's real self-description. */}
        <Route path="/instance/setup" element={<G1Setup />} />
        <Route path="/instance/covers" element={<G1Covers />} />
        <Route path="/instance/consent" element={<G1Consent />} />
        <Route path="/instance/invite" element={<G1Invite />} />
        <Route path="/mech/where" element={<MechWhere />} />
        <Route path="/mech/rails" element={<MechRails />} />
        <Route path="/mech/connect" element={<MechConnect />} />
        <Route path="/instance/services" element={<G1Services />} />
        <Route path="/instance/people" element={<G1People />} />
        {/* G-4 admission review (g4_1–g4_3): the real queue and the real
            decision — POST /v1/organisations/{id}/decision, decider checked. */}
        <Route path="/admissions" element={<Admissions />} />
        <Route path="/admissions/:pid" element={<AdmissionDetail />} />
        <Route path="/portfolio" element={<Portfolio />} />
        <Route path="/find" element={<Find />} />
        {/* g4_4/g4_5/g4_7: coverage, the quality worklist, and registry reuse
            — real reads over services/core/parties and services/core/evidence
            (#197), not the completeness-chart shape the reference frames
            deliberately avoid. */}
        <Route path="/coverage" element={<Coverage />} />
        <Route path="/registry-quality" element={<QualityWorklist />} />
        <Route path="/reuse" element={<RegistryReuse />} />
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
