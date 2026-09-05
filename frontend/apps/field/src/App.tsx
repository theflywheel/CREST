// CREST field door (journey J6, plus assisted confirmation — the W1/W4
// window exits made on an unreached worker's behalf; NOT J8 source
// attestation, which belongs to the delivery platform) as a desktop
// console: appbar + sidebar, the offline queue held
// on-device with its count in the appbar, and every flow ported 1:1 from
// apps/enrolment. Same hash routes, so a bookmarked screen keeps working.
import { useEffect, useState } from "react";
import { Navigate, Outlet, Route, Routes, useLocation, useSearchParams } from "react-router-dom";
import { ConsoleShell, ErrBar, Chip, Sidecar, type NavGroup } from "@crest/ui";
import { startEsignetLogin } from "@crest/api";
import { useField, devShortcut, errText, short, NO_PROJECT } from "./state";
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

/* The entry frame, shared by the sign-in fork and the eSignet return leg. */
function EntryShell(props: { children: React.ReactNode }) {
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST · Field</span>
        <span className="who" />
      </div>
      <div className="console-body">
        <main className="pane">
          <div className="pane-narrow screen">{props.children}</div>
        </main>
      </div>
    </div>
  );
}

function Login() {
  const s = useField();
  const doDevLogin = async () => {
    try {
      await s.devLogin();
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <EntryShell>
      {s.err ? <ErrBar>{s.err}</ErrBar> : null}
      <h2 className="scr-title">Who is carrying this device?</h2>
      <p className="muted">
        Sign in as yourself. The agent who signs in here is the agent whose name goes on every registration made from
        this device, and on every window exit made for a worker who could not be reached — which is not the delivery
        platform's source attestation (that system is not CREST's to build).
      </p>
      <div className="btn-row">
        <button className="btn" id="login-esignet" onClick={() => startEsignetLogin()}>
          Continue with eSignet
        </button>
      </div>
      {devShortcut ? (
        <div className="card quiet" style={{ marginTop: 12 }}>
          <span className="eyebrow">Dev build only</span>
          <p className="muted" style={{ marginTop: 6 }}>
            The local stack's shortcut: it mints a token from the stack's own identity provider and binds it through
            the real first-login path, for the seeded supervisor. It exists on this machine and nowhere else.
          </p>
          <div className="btn-row">
            <button className="btn secondary" data-login="1" onClick={doDevLogin}>
              Naomi (supervisor) · dev shortcut
            </button>
          </div>
        </div>
      ) : null}
      <Sidecar>
        Registrations made from this device carry the signed-in agent's identity; every assisted action is recorded
        with their name on it.
      </Sidecar>
    </EntryShell>
  );
}

/* The eSignet return leg: the registry's callback bounced back here with
   either a token or an error. A token that verifies but binds to no party is
   an honest dead end on this door — there is no self-registration for an
   agent, because an agent is invited by an organisation, not self-declared. */
function AuthReturn() {
  const s = useField();
  const [params] = useSearchParams();
  const [state, setState] = useState<"working" | "stranger" | "failed">("working");
  const [detail, setDetail] = useState("");

  useEffect(() => {
    const err = params.get("error");
    const token = params.get("token");
    if (err) {
      setState("failed");
      setDetail(err);
      return;
    }
    if (!token) {
      setState("failed");
      setDetail("the login returned neither a token nor an error");
      return;
    }
    s.completeEsignet(token)
      .then((outcome) => {
        if (outcome === "signed") location.hash = "#/registrations";
        else setState("stranger");
      })
      .catch((e) => {
        setState("failed");
        setDetail(errText(e));
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  if (state === "working")
    return (
      <EntryShell>
        <span className="eyebrow">CREST · Field</span>
        <p className="body-2">Completing your sign-in…</p>
      </EntryShell>
    );
  if (state === "failed")
    return (
      <EntryShell>
        <span className="eyebrow">CREST · Field</span>
        <h2 className="scr-title">That sign-in did not finish</h2>
        <ErrBar>{detail}</ErrBar>
        <a className="btn" href="#/">
          Try again
        </a>
      </EntryShell>
    );
  return (
    <EntryShell>
      <span className="eyebrow">CREST · Field</span>
      <h2 className="scr-title">You are signed in, but hold no role here</h2>
      <p className="body-2">
        Your identity checked out. Nobody in this deployment has given it a role, so there is nothing this door can
        let you do yet — and no registering agent creates their own. An agent is invited by their organisation
        through the console; ask them to invite you, and sign in again once they have.
      </p>
      <a className="btn secondary" href="#/">
        Back
      </a>
    </EntryShell>
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
          {s.contexts.length > 1 ? (
            <select
              id="context-select"
              aria-label="Project"
              value={s.contextId || ""}
              onChange={(e) => s.setContextId(e.target.value)}
              style={{ font: "inherit" }}
            >
              {s.contexts.map((id) => (
                <option key={id} value={id}>
                  {short(id)}
                </option>
              ))}
            </select>
          ) : null}
          <span className="who-label">{s.me.label}</span>
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
        {s.grantsLoaded && !s.contexts.length ? (
          <div className="card quiet" id="no-project">
            <p className="body-2">{NO_PROJECT}</p>
          </div>
        ) : null}
        <Outlet />
      </div>
    </ConsoleShell>
  );
}

export function App() {
  return (
    <Routes>
      {/* The eSignet return leg renders outside the shell: it runs before
          there is a session to gate on. */}
      <Route path="/auth" element={<AuthReturn />} />
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
