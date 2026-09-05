import { type ReactNode } from "react";
import { startEsignetLogin } from "@crest/api";

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

export function Login() {
  return (
    <EntryShell>
      <h1 className="scr-title">
        Your work, on the record. Your money, explained.
      </h1>
      <p className="muted" style={{ maxWidth: 700 }}>
        Two ways in, and neither is a fallback. Both pathways issue the same Crest DID and the same enrollment consent
        record. A verifier can never tell which was used.
      </p>
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
            arrives with a pilot (#53). The flow, and what CREST sees, is identical. CREST never stores the document.
            Only the result of the check is kept.
          </p>
        </div>
        <div className="card" data-pathway="assisted">
          <span className="eyebrow">Pathway B — be enrolled with help</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            No phone, no document, or you would rather someone walked through it with you: a registering agent enrols
            you on a shared device, reads the consent aloud, and your record carries their name as the enroller —
            equal rigour, the same CREST ID at the end.
          </p>
          <p className="muted" style={{ marginTop: 8 }}>
            No document at all? Your record will show that your identity was established without a document. This does
            not limit what work you can do or be paid for.
          </p>
          <div style={{ height: 10 }} />
          <a className="btn secondary" id="login-assisted" href="/enrolment/">
            Find a registering agent · the field door
          </a>
        </div>
      </div>
      <p className="muted" style={{ marginTop: 12 }}>
        Section 11, Choice One also allows a third route: skip registration entirely and import an existing worker
        identifier. This deployment has not configured it.
      </p>
    </EntryShell>
  );
}
