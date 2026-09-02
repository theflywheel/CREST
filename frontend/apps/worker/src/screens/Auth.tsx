// The eSignet return leg (#155): the callback bounced back to #/auth with
// either a token or an error in the route's query. A token that verifies but
// binds to no party lands on the honest "you are not enrolled here yet"
// screen — phase B turns that into self-registration.
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { OpenNote, Sidecar } from "@crest/ui";
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
  return (
    <div className="login-shell">
      <div className="login-card screen">
        <span className="eyebrow">CREST · Worker</span>
        <h1 className="scr-title">You are signed in — and not yet enrolled here</h1>
        <p className="body-2">
          Your identity checked out. But no party in this CREST deployment is bound to it, so there is no record for
          this app to show you — being unknown here is a fact, not an error.
        </p>
        <OpenNote>
          <b>Self-registration is not built yet (#155 phase B).</b> Today a worker is enrolled by a programme's
          registering agent (the field app), and your identity is bound to that record on your first login after
          enrolment. When self-registration lands, this screen becomes the place you create your own party.
        </OpenNote>
        <Sidecar>
          Nothing about you was stored by this sign-in attempt beyond a pairwise reference — not your ID number, not
          your name. Signing in and walking away leaves no record that matters.
        </Sidecar>
        <a className="btn secondary" href="#/">
          Back
        </a>
      </div>
    </div>
  );
}
