// The eSignet return leg (#155): the callback bounced back to #/auth with
// either a token or an error in the route's query. A token that verifies but
// binds to no party lands on the honest "you are not enrolled here yet"
// screen — phase B turns that into self-registration.
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Sidecar } from "@crest/ui";
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
      <div className="login-shell">
        <div className="login-card screen">
          <span className="eyebrow">CREST · Worker</span>
          <p className="body-2">Completing your sign-in…</p>
        </div>
      </div>
    );
  }
  if (state === "failed") {
    return (
      <div className="login-shell">
        <div className="login-card screen">
          <span className="eyebrow">CREST · Worker</span>
          <h1 className="scr-title">That sign-in did not finish</h1>
          <div className="errbar">{detail}</div>
          <a className="btn" href="#/">
            Try again
          </a>
        </div>
      </div>
    );
  }
  return <SignUp />;
}

/* Phase B (#155): the authenticated stranger creates their own record. */
function SignUp() {
  const s = useSession();
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await s.signUp(name.trim(), phone.trim());
      nav("/home", { replace: true });
    } catch (e) {
      setErr(describeError(e));
      setBusy(false);
    }
  };
  return (
    <div className="login-shell">
      <div className="login-card screen">
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
          </label>
          {err ? <div className="errbar">{err}</div> : null}
          <button className="btn" disabled={busy}>
            {busy ? "Creating your record…" : "Create my record"}
          </button>
        </form>
        <Sidecar>
          What is stored: this name, this phone, and a pairwise reference that means nothing outside this deployment.
          Not your ID number — the sign-in already proved it and it never comes to rest here.
        </Sidecar>
        <a className="btn secondary" href="#/">
          Back
        </a>
      </div>
    </div>
  );
}
