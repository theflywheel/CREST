import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { ConsoleShell } from "@crest/ui";
import { useVerify, errText } from "../state";

/** The server callback returns only a short lived access token in the hash. */
export function AuthReturn() {
  const s = useVerify();
  const nav = useNavigate();
  const [params] = useSearchParams();
  const [message, setMessage] = useState("Completing your sign-in…");
  useEffect(() => {
    const error = params.get("error");
    const token = params.get("token");
    if (error) {
      setMessage("The identity provider declined this sign-in: " + error);
      return;
    }
    if (!token) {
      setMessage("The login returned neither a token nor an error. Start again.");
      return;
    }
    s.completeEsignet(token).then((outcome) => {
      if (outcome === "enrolled") nav("/v2_1", { replace: true });
      else setMessage("This identity is not enrolled in this CREST deployment.");
    }).catch((e) => setMessage(errText(e)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <ConsoleShell appName="CREST · Checking a credential" who="Not signed in" nav={[]}>
      <div className="pane-narrow screen"><p className="body-2">{message}</p></div>
    </ConsoleShell>
  );
}
