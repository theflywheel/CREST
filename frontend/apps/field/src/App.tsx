// CREST field door (journey J6, plus assisted confirmation — the W1/W4
// window exits made on an unreached worker's behalf; NOT J8 source
// attestation, which belongs to the delivery platform) as a desktop
// console: appbar + sidebar, the offline queue held
// on-device with its count in the appbar, and every flow ported 1:1 from
// apps/enrolment. Same hash routes, so a bookmarked screen keeps working.
import { useEffect } from "react";
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { ConsoleShell, ErrBar, Chip, Sidecar, type NavGroup } from "@crest/ui";
import { useField } from "./state";
import { useQueue, useOnline } from "./queue";
import { Registrations, Register, Consent, Confidence, Hold, Registered } from "./screens/Enrol";
import { ToConfirm, ConfirmSaw, AssistedDone, Differ, DifferDone, Roster, Handoff } from "./screens/Attest";

const NAV: NavGroup[] = [
  {
    caption: "Registering agent · J6",
    items: [
      { to: "/registrations", label: "Registrations" },
      { to: "/register", label: "New worker" },
      { to: "/confidence", label: "No document — confidence check" },
    ],
  },
  {
    // Not J8/W-4 source attestation: those screens live in the delivery
    // platform (reference w3_1–w3_4, traceability manifest). What this door
    // does is assisted confirmation — W1/W4 window exits made for a worker
    // who cannot be reached — and evidence intake into CREST.
    caption: "Assisted confirmation · window exits",
    items: [
      { to: "/toconfirm", label: "To confirm" },
      { to: "/roster", label: "Evidence intake" },
      { to: "/handoff", label: "Who holds it next" },
    ],
  },
];

function Login() {
  const s = useField();
  const doLogin = async () => {
    try {
      await s.login();
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST · Field</span>
        <span className="who" />
      </div>
      <div className="console-body">
        <main className="pane">
          <div className="pane-narrow screen">
            {s.err ? <ErrBar>{s.err}</ErrBar> : null}
            <h2 className="scr-title">Who is carrying this device?</h2>
            <p className="muted">
              In this dev build, signing in mints a token from the stack's own identity provider and binds it through
              the real first-login path. Naomi carries both hats here: registering agent (J6) and assisted
              confirmer — a supervisor exiting CREST windows for workers who cannot be reached, which is not the
              delivery platform's source attestation (that system is not CREST's to build).
            </p>
            <div className="btn-row">
              <button className="btn" data-login="1" onClick={doLogin}>
                Naomi (supervisor)
              </button>
            </div>
            <Sidecar>
              Registrations made from this device carry Naomi's agent identity; every assisted action is recorded with
              her name on it.
            </Sidecar>
          </div>
        </main>
      </div>
    </div>
  );
}

function Shell() {
  const s = useField();
  const loc = useLocation();
  const q = useQueue();
  const online = useOnline();
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!s.me) return <Login />;
  return (
    <ConsoleShell
      appName="CREST · Field"
      who={
        <>
          {q.length ? <Chip sm kind="warn">{q.length} held on this device</Chip> : null}
          <span className="who-label">Naomi · supervisor</span>
        </>
      }
      nav={NAV}
    >
      {!online ? (
        <div className="offline-banner">
          You are offline. Registrations are held on this device and sync when you have signal.
        </div>
      ) : null}
      <div className="pane-narrow screen" key={loc.pathname}>
        {s.err ? <ErrBar>{s.err}</ErrBar> : null}
        <Outlet />
      </div>
    </ConsoleShell>
  );
}

export function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route path="/" element={<Navigate to="/registrations" replace />} />
        <Route path="/registrations" element={<Registrations />} />
        <Route path="/register" element={<Register />} />
        <Route path="/confidence" element={<Confidence />} />
        <Route path="/consent" element={<Consent />} />
        <Route path="/hold" element={<Hold />} />
        <Route path="/registered" element={<Registered />} />
        <Route path="/toconfirm" element={<ToConfirm />} />
        <Route path="/confirmsee/:claimId" element={<ConfirmSaw />} />
        <Route path="/confirmed/:claimId" element={<AssistedDone />} />
        <Route path="/differ/:claimId" element={<Differ />} />
        <Route path="/differed/:claimId" element={<DifferDone />} />
        <Route path="/roster" element={<Roster />} />
        <Route path="/handoff" element={<Handoff />} />
        <Route path="*" element={<Navigate to="/registrations" replace />} />
      </Route>
    </Routes>
  );
}
