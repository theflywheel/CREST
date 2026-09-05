// n1 — Sign in to CREST Console. Our design, not the reference's
// (docs/design/j3-connective-tissue/README.md): every J3 frame opens with the
// actor already inside their console, so the door itself is never drawn.
//
// The one rule this screen enforces: there is NO role selector. A role is
// granted in the registry and read back here; picking your own would make
// authority a matter of self-declaration. eSignet is the only door — the
// demo-persona rows this screen used to offer on local stacks are gone, and
// the e2e suite signs in programmatically through the same mint-and-bind path
// they performed.
import { ErrBar } from "@crest/ui";
import { ApiError, claimInvitation, setSession, startEsignetLogin } from "@crest/api";
import { useConsole } from "../state";
import { claimRefusal, clearClaim, pendingClaim } from "./Claim";

export function AppbarOnly(props: { children: React.ReactNode }) {
  return (
    <div className="console-shell">
      <div className="appbar">
        <span className="mark" />
        <span className="t">CREST Console</span>
        <span className="who">
          <span className="who-label">Not signed in</span>
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

export function SignIn() {
  const s = useConsole();
  return (
    <AppbarOnly>
      <h1 className="scr-title" id="signin-title">
        Sign in to CREST Console
      </h1>
      {s.err ? <ErrBar>{s.err}</ErrBar> : null}

      <div className="card" style={{ maxWidth: 700 }} data-panel="signin-with">
        <span className="eyebrow">Sign in with</span>
        <div style={{ height: 10 }} />
        <button className="btn" id="signin-esignet" onClick={() => startEsignetLogin()}>
          Continue with eSignet
        </button>
        <p className="muted" style={{ marginTop: 8 }}>
          Your national identity provider. CREST never sees a credential of yours.
        </p>
      </div>
    </AppbarOnly>
  );
}

// The eSignet return leg (#155): the callback bounced back to #/auth with a
// token or an error in the route's query. A token that verifies but binds to
// no party is an honest refusal — the console has no self-registration; an
// organisation gets here by being onboarded.
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

export function AuthReturn() {
  const s = useConsole();
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
    (async () => {
      const outcome = await s.completeEsignet(token);
      if (outcome === "enrolled") {
        // An already-bound login has nothing to claim; a code left over from
        // an earlier attempt must not follow them around.
        clearClaim();
        nav("/", { replace: true });
        return;
      }
      // A stranger holding an invitation is the whole point of #/claim: bind
      // this login to the record somebody created, then ask the registry
      // again — the persona derives from the grants that binding revealed,
      // never from anything this browser decided.
      const code = pendingClaim();
      if (!code) {
        setState("stranger");
        return;
      }
      // completeEsignet drops the session on "stranger"; the claim is made
      // with the claimant's own token, so put it back for that one call.
      setSession(token);
      try {
        await claimInvitation(code);
      } catch (e) {
        setSession(null);
        clearClaim();
        setState("failed");
        setDetail(claimRefusal(e instanceof ApiError ? e.code : null, String((e as Error)?.message || e)));
        return;
      }
      clearClaim();
      const after = await s.completeEsignet(token);
      if (after === "enrolled") nav("/", { replace: true });
      else setState("stranger");
    })().catch((e) => {
      setState("failed");
      setDetail(String((e as Error)?.message || e));
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  if (state === "working")
    return (
      <AppbarOnly>
        <p className="body-2">Completing your sign-in…</p>
      </AppbarOnly>
    );
  if (state === "failed")
    return (
      <AppbarOnly>
        <h1 className="scr-title">That sign-in did not finish</h1>
        <div className="errbar">{detail}</div>
        <a className="btn" href="#/">Try again</a>
      </AppbarOnly>
    );
  return (
    <AppbarOnly>
      <h1 className="scr-title">You are signed in, but hold no role here</h1>
      <p className="muted" style={{ maxWidth: 700 }}>
        Your identity checked out, and no party in this deployment is bound to it. The console has no
        self-registration — an organisation reaches it by being onboarded and granted a role.
      </p>
      <a className="btn" href="#/">Back</a>
    </AppbarOnly>
  );
}
