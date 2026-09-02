import { useState, type ReactNode } from "react";
import { Sidecar } from "@crest/ui";
import { FIX, isLocalStack, startEsignetLogin } from "@crest/api";
import { useSession, describeError } from "../session";

export const Ussd = () => (
  <p className="muted" style={{ textAlign: "center" }}>
    Dial <b className="mono">*384*77#</b> to hear this on any phone — the channel-parity promise (#29): every screen
    here has a voice and USSD equivalent.
  </p>
);

/* The worker door's entry frame, in the reference's desktop console grammar:
   teal appbar with the orange mark, wide pane — no more 480px centered card.
   Shared by the entry fork (w1_1) and the eSignet return leg (Auth.tsx). */
export function EntryShell(props: { who?: string; children: ReactNode }) {
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST · Worker</span>
        <span className="who">
          <span className="who-label">{props.who || "Not signed in"}</span>
        </span>
      </div>
      <div className="console-body">
        <main className="pane">
          <div className="screen pane-wide">{props.children}</div>
        </main>
      </div>
    </div>
  );
}

/* Dev-only: sign in as any fixture party by id — how a freshly enrolled
   worker (w1_4's confidence-check route) first opens their own door on the
   local stack, and how tests hold identities the persona buttons do not name.
   The mock issuer mints the token; the first-login self-bind is the same real
   path Grace's button uses. */
function DevPartyLogin(props: {
  busy: boolean;
  setBusy: (b: boolean) => void;
  setErr: (e: string | null) => void;
}) {
  const s = useSession();
  const [pid, setPid] = useState("");
  return (
    <form
      style={{ marginTop: 10, display: "flex", gap: 8 }}
      onSubmit={async (ev) => {
        ev.preventDefault();
        if (!pid.trim()) return;
        props.setBusy(true);
        try {
          await s.login(pid.trim(), "Signed in by party id (dev)");
        } catch (e) {
          props.setErr(describeError(e));
          props.setBusy(false);
        }
      }}
    >
      <input
        id="login-partyid"
        placeholder="did:crest:party:… (dev)"
        value={pid}
        onChange={(e) => setPid(e.target.value)}
        style={{ flex: 1 }}
      />
      <button className="btn secondary" id="login-party-go" disabled={props.busy} type="submit">
        Sign in
      </button>
    </form>
  );
}

export function Login() {
  const s = useSession();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <EntryShell>
      <h1 className="scr-title">
        Your work, on the record. Your money, explained.
      </h1>
      <p className="muted" style={{ maxWidth: 700 }}>
        Two ways in, and neither is a fallback — both give the same record, the same consent, the same CREST ID. A
        verifier can never tell which you used.
      </p>
      {err ? <div className="errbar">{err}</div> : null}
      <div className="form-grid" style={{ alignItems: "stretch" }}>
        <div className="card hi" data-pathway="self">
          <span className="eyebrow">Pathway A — enroll yourself</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            On your own phone. You prove who you are to the identity system, not to CREST — CREST only ever sees a
            pairwise reference, never your ID number. Then you say yes to enrollment before any record is created.
          </p>
          <div style={{ height: 10 }} />
          <button className="btn" id="login-esignet" onClick={() => startEsignetLogin()}>
            Continue with eSignet
          </button>
          <p className="muted" style={{ marginTop: 8 }}>
            In this deployment eSignet authenticates against a test identity registry — a real national registry
            arrives with a pilot (#53). The flow, and what CREST sees, is identical.
          </p>
        </div>
        <div className="card" data-pathway="assisted">
          <span className="eyebrow">Pathway B — be enrolled with help</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            No phone, no document, or you would rather someone walked through it with you: a registering agent enrols
            you on a shared device, reads the consent aloud, and your record carries their name as the enroller —
            equal rigour, the same CREST ID at the end.
          </p>
          <div style={{ height: 10 }} />
          <a className="btn secondary" id="login-assisted" href="/enrolment/">
            Find a registering agent · the field door
          </a>
        </div>
      </div>
      <div className="pane-cols">
        {isLocalStack ? (
          <div className="card">
            <span className="eyebrow">Dev login — local stack only</span>
            <div className="person-name">Grace</div>
            <p className="muted">Community health worker · bednet distribution, PRJ-118</p>
            <div style={{ height: 10 }} />
            <button
              className="btn secondary"
              id="login-grace"
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                try {
                  await s.login(FIX.workerA, "Grace · community health worker");
                } catch (e) {
                  setErr(describeError(e));
                  setBusy(false);
                }
              }}
            >
              {busy ? "Signing in…" : "Continue as Grace"}
            </button>
            <div style={{ height: 12 }} />
            <p className="muted">
              This door holds whoever signs in — a recovery confirmer is by definition somebody other than the worker
              being recovered, so other fixture people can carry it too.
            </p>
            <div className="btn-row" style={{ marginTop: 6 }}>
              <button
                className="btn secondary"
                id="login-supervisor"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    await s.login(FIX.supervisor, "District Supervisor · recovery confirmer");
                  } catch (e) {
                    setErr(describeError(e));
                    setBusy(false);
                  }
                }}
              >
                District Supervisor
              </button>
            </div>
            <DevPartyLogin busy={busy} setBusy={setBusy} setErr={setErr} />
          </div>
        ) : <span />}
        <div>
          <Sidecar>
            Signing in never uploads anything about you. It only proves to CREST that the person holding this phone
            is the person the record is about.
          </Sidecar>
          <Ussd />
        </div>
      </div>
    </EntryShell>
  );
}
