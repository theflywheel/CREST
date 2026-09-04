// The eSignet return leg (#155): the callback bounced back to #/auth with
// either a token or an error in the route's query. A token that verifies but
// binds to no party lands on the honest "you are not enrolled here yet"
// screen — phase B turns that into self-registration.
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { DisLi, Sidecar } from "@crest/ui";
import { EntryShell } from "./Login";
import { useSession, describeError } from "../session";

export function AuthReturn() {
  const s = useSession();
  const nav = useNavigate();
  const [params] = useSearchParams();
  const [state, setState] = useState<"working" | "stranger" | "failed">("working");
  const [detail, setDetail] = useState<string>("");

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
        if (outcome === "enrolled") nav("/home", { replace: true });
        else setState("stranger");
      })
      .catch((e) => {
        setState("failed");
        setDetail(describeError(e));
      });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  if (state === "working") {
    return (
      <EntryShell>
        <div className="pane-narrow screen">
          <span className="eyebrow">CREST · Worker</span>
          <p className="body-2">Completing your sign-in…</p>
        </div>
      </EntryShell>
    );
  }
  if (state === "failed") {
    return (
      <EntryShell>
        <div className="pane-narrow screen">
          <span className="eyebrow">CREST · Worker</span>
          <h1 className="scr-title">That sign-in did not finish</h1>
          <div className="errbar">{detail}</div>
          <a className="btn" href="#/">
            Try again
          </a>
        </div>
      </EntryShell>
    );
  }
  return <SignUp />;
}

/* The consent script a self-enrolling worker agrees to (reference w1_5).
   The exact sentence is posted as the consent's purpose, so what was agreed
   to is what is recorded — not a paraphrase. */
const CONSENT_PURPOSE =
  "hold my enrolment details and fetch records of my work for this programme, until I withdraw";

/* Phase B (#155): the authenticated stranger creates their own record —
   with the enrollment-consent step FIRST (reference w1_5): the record is
   never created before the worker has said yes. */
function SignUp() {
  const s = useSession();
  const nav = useNavigate();
  const [consented, setConsented] = useState(false);
  const [agree, setAgree] = useState(false);
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  if (!consented) {
    return (
      <EntryShell>
        <div className="pane-narrow screen">
          <span className="eyebrow">CREST · Worker · Enrollment consent</span>
          <h1 className="scr-title">What you are agreeing to</h1>
          <p className="body-2">
            Nothing exists about you here yet, and nothing will until you say yes. If you agree, CREST will:{" "}
            {CONSENT_PURPOSE}.
          </p>
          <div className="card">
            <div className="dis">
              <DisLi on t="We can fetch your work records" s="From systems your employer already uses, when you ask us to" />
              <DisLi on t="We store your work history" s="In this country. It never leaves." />
              <DisLi on t="You approve every share" s="Nobody sees your record without you saying yes, each time" />
            </div>
            <p className="muted" style={{ marginTop: 8 }}>
              You can withdraw any of these later without losing credentials you already hold.
            </p>
          </div>
          <form
            id="consentform"
            onSubmit={(ev) => {
              ev.preventDefault();
              if (agree) setConsented(true);
            }}
            style={{ display: "flex", flexDirection: "column", gap: 10 }}
          >
            <label className="body-2" style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
              <input
                type="checkbox"
                id="consentbox"
                checked={agree}
                onChange={(e) => setAgree(e.target.checked)}
                style={{ marginTop: 3 }}
              />
              <span>I agree — recorded on screen, in my name, scoped to this programme.</span>
            </label>
            <button className="btn" id="consentcontinue" disabled={!agree}>
              I agree — continue
            </button>
          </form>
          <Sidecar>
            This consent is recorded through the real consents API the moment your record exists, and shows on your
            profile under Consents, where withdrawing it is one tap.
          </Sidecar>
          <a className="btn secondary" href="#/">
            No — take me back
          </a>
        </div>
      </EntryShell>
    );
  }

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await s.signUp(name.trim(), phone.trim(), CONSENT_PURPOSE);
      nav("/home", { replace: true });
    } catch (e) {
      setErr(describeError(e));
      setBusy(false);
    }
  };
  return (
    <EntryShell>
        <div className="pane-narrow screen">
        <span className="eyebrow">CREST · Worker</span>
        <h1 className="scr-title">You are signed in — create your record</h1>
        <p className="body-2">
          Your identity checked out, and nobody in this deployment is bound to it yet. Give a name to show and one way
          to reach you, and this becomes your own record — yours from the first minute, before any programme enrols
          you.
        </p>
        <form id="signupform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label className="body-2">
            Name to show
            <input required value={name} onChange={(e) => setName(e.target.value)} placeholder="How you want to appear" style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Phone
            <input required value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="+254…" style={{ width: "100%", marginTop: 4 }} />
            <span className="muted" style={{ display: "block", marginTop: 4 }}>
              This number is how you sign in and how projects reach you. The reference verifies it with a texted code;
              this deployment sends no messages (#150), so the number is recorded unverified.
            </span>
          </label>
          {err ? <div className="errbar">{err}</div> : null}
          <button className="btn" disabled={busy}>
            {busy ? "Creating your record…" : "Create my record"}
          </button>
        </form>
        <Sidecar>
          What is stored: this name, this phone, the consent you just gave, and a pairwise reference that means
          nothing outside this deployment. Not your ID number — the sign-in already proved it and it never comes to
          rest here.
        </Sidecar>
        <a className="btn secondary" href="#/">
          Back
        </a>
      </div>
    </EntryShell>
  );
}
