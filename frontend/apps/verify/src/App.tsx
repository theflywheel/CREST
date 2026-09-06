// CREST verify door (journey J9, "Checking a credential") as a desktop
// console. Verification is deliberately account-free: trust rides the
// credential's signature, not a login, so V-1 runs logged out; V-2's routes
// sign in as the onboarded institution on entry and the session is dropped on
// leaving — the same behaviour the vanilla app had.
import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup } from "@crest/ui";
import { useVerify } from "./state";
import { V11, V12, V13 } from "./screens/V1";
import { V21, V22, V23, Person } from "./screens/V2";
import { Requests } from "./screens/Requests";
import { W61, W62 } from "./screens/Panel";
import { AuthReturn } from "./screens/Auth";

const NAV: NavGroup[] = [
  {
    caption: "V-1 · A pass-only verifier",
    items: [
      { to: "/v1_1", label: "Get a pass" },
      { to: "/v1_2", label: "Scan or enter" },
      { to: "/v1_3", label: "The answer" },
    ],
  },
  {
    caption: "V-2 · An onboarded institution",
    items: [
      { to: "/v2_1", label: "Checking as an institution" },
      { to: "/v2_2", label: "Verified, with the disclosure list" },
      { to: "/v2_3", label: "Batch — checking many" },
      { to: "/person", label: "Resolve a person" },
      { to: "/requests", label: "Ask to see more" },
    ],
  },
  {
    caption: "P-10 · External institution",
    items: [
      { to: "/w6_1", label: "A request for one attestation" },
      { to: "/w6_2", label: "Three tiers, four kinds of evidence" },
    ],
  },
];

const ORG_ROUTES = new Set(["/v2_1", "/v2_2", "/v2_3", "/person", "/requests"]);
const PANEL_ROUTES = new Set(["/w6_1", "/w6_2"]);

function ConsoleScreen(props: { children: React.ReactNode }) {
  const s = useVerify();
  const loc = useLocation();
  // Entering V-2 signs in as the institution; leaving drops the session
  // (V-1 is deliberately logged out). A navigation clears the last error.
  useEffect(() => {
    s.clearErr();
    if (ORG_ROUTES.has(loc.pathname)) {
      s.ensureOrg().catch(s.fail);
    } else if (!PANEL_ROUTES.has(loc.pathname)) {
      s.dropOrg();
    }
  }, [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  const who =
    s.orgSession && ORG_ROUTES.has(loc.pathname)
      ? (s.orgParty?.displayName || "Signed in institution") + " — onboarded verifier"
      : "Not signed in — verification does not need an account";
  return (
    <ConsoleShell appName="CREST · Checking a credential" who={who} nav={NAV}>
      <div className="pane-narrow screen" key={loc.pathname}>
        {s.err ? <ErrBar>{s.err}</ErrBar> : null}
        {props.children}
      </div>
    </ConsoleShell>
  );
}

function PanelScreen(props: { children: React.ReactNode }) {
  const s = useVerify();
  const loc = useLocation();
  const nav = useNavigate();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  const w61 = loc.pathname === "/w6_1";
  return (
    <div className="panel-shell screen">
      {s.err ? <ErrBar>{s.err}</ErrBar> : null}
      {props.children}
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav(w61 ? "/w6_2" : "/w6_1")}>
          {w61 ? "How tiers are decided" : "Back to the request"}
        </button>
        <button className="btn secondary" onClick={() => nav("/v1_2")}>
          The verifier itself
        </button>
      </div>
    </div>
  );
}

export function App() {
  return (
    <Routes>
      <Route path="/auth" element={<AuthReturn />} />
      <Route path="/v1_1" element={<ConsoleScreen><V11 /></ConsoleScreen>} />
      <Route path="/v1_2" element={<ConsoleScreen><V12 /></ConsoleScreen>} />
      <Route path="/v1_3" element={<ConsoleScreen><V13 /></ConsoleScreen>} />
      <Route path="/v2_1" element={<ConsoleScreen><V21 /></ConsoleScreen>} />
      <Route path="/v2_2" element={<ConsoleScreen><V22 /></ConsoleScreen>} />
      <Route path="/v2_3" element={<ConsoleScreen><V23 /></ConsoleScreen>} />
      <Route path="/person" element={<ConsoleScreen><Person /></ConsoleScreen>} />
      <Route path="/requests" element={<ConsoleScreen><Requests /></ConsoleScreen>} />
      <Route path="/w6_1" element={<PanelScreen><W61 /></PanelScreen>} />
      <Route path="/w6_2" element={<PanelScreen><W62 /></PanelScreen>} />
      <Route path="*" element={<Navigate to="/v1_1" replace />} />
    </Routes>
  );
}
