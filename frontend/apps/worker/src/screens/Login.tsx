import { useState } from "react";
import { Sidecar } from "@crest/ui";
import { isLocalStack, startEsignetLogin } from "@crest/api";
import { useSession, describeError } from "../session";

export const Ussd = () => (
  <p className="muted" style={{ textAlign: "center" }}>
    Dial <b className="mono">*384*77#</b> to hear this on any phone — the channel-parity promise (#29): every screen
    here has a voice and USSD equivalent.
  </p>
);

export function Login() {
  const s = useSession();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <div className="login-shell">
      <div className="login-card screen">
        <span className="eyebrow">CREST · Worker</span>
        <h1 className="scr-title">
          Your work, on the record.
          <br />
          Your money, explained.
        </h1>
        <p className="muted">
          Two ways in, and neither is a fallback — both give the same record, the same consent, the same CREST ID. A
          verifier can never tell which you used.
        </p>
        {err ? <div className="errbar">{err}</div> : null}
        <div className="card hi" data-pathway="self">
          <span className="eyebrow">Pathway A — enroll yourself</span>
          <p className="body-2">
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
          <p className="body-2">
            No phone, no document, or you would rather someone walked through it with you: a registering agent enrols
            you on a shared device, reads the consent aloud, and your record carries their name as the enroller —
            equal rigour, the same CREST ID at the end.
          </p>
          <div style={{ height: 10 }} />
          <a className="btn secondary" id="login-assisted" href="/enrolment/">
            Find a registering agent · the field door
          </a>
        </div>
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
                  await s.login();
                } catch (e) {
                  setErr(describeError(e));
                  setBusy(false);
                }
              }}
            >
              {busy ? "Signing in…" : "Continue as Grace"}
            </button>
          </div>
        ) : null}
        <Sidecar>
          Signing in never uploads anything about you. It only proves to CREST that the person holding this phone is
          the person the record is about.
        </Sidecar>
        <Ussd />
      </div>
    </div>
  );
}
