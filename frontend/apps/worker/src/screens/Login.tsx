import { useState } from "react";
import { Sidecar } from "@crest/ui";
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
          This is the dev login. It stands in for eSignet: in a real deployment you would tap "Continue with eSignet"
          and prove who you are to the national identity system — CREST would only ever see a pairwise reference,
          never your ID number. Here, you just pick the person.
        </p>
        {err ? <div className="errbar">{err}</div> : null}
        <div className="card hi">
          <div className="person-name">Grace</div>
          <p className="muted">Community health worker · bednet distribution, PRJ-118</p>
          <div style={{ height: 10 }} />
          <button
            className="btn"
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
        <Sidecar>
          Signing in never uploads anything about you. It only proves to CREST that the person holding this phone is
          the person the record is about.
        </Sidecar>
        <Ussd />
      </div>
    </div>
  );
}
