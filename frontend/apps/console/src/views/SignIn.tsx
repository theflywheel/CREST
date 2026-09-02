// n1 — Sign in to CREST Console. Our design, not the reference's
// (docs/design/j3-connective-tissue/README.md): every J3 frame opens with the
// actor already inside their console, so the door itself is never drawn.
//
// The one rule this screen enforces: there is NO role selector. A role is
// granted in the registry and read back here; picking your own would make
// authority a matter of self-declaration. The demo persona block is
// instance-configured — it renders only where a mock identity provider
// answers, and never where a real one does.
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Callout, ErrBar } from "@crest/ui";
import { isLocalStack, services, startEsignetLogin } from "@crest/api";
import { personas, useConsole } from "../state";

function AppbarOnly(props: { children: React.ReactNode }) {
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

export function SignIn(props: { onSignedIn: (idx: number) => Promise<void> }) {
  const s = useConsole();
  const nav = useNavigate();
  // Is there a mock identity provider on this instance? isLocalStack is the
  // build-time guess; the discovery probe is the answer. A deployment with a
  // real provider shows no persona rows at all.
  const [demo, setDemo] = useState<boolean | null>(isLocalStack ? null : false);
  useEffect(() => {
    if (!isLocalStack) return;
    let live = true;
    fetch(services.oidc + "/.well-known/openid-configuration")
      .then((r) => live && setDemo(r.ok))
      .catch(() => live && setDemo(false));
    return () => {
      live = false;
    };
  }, []);

  return (
    <AppbarOnly>
      <h1 className="scr-title" id="signin-title">
        Sign in to CREST Console
      </h1>
      <p className="muted" style={{ maxWidth: 700 }}>
        One door, every console role. What you see after signing in is decided by the roles you hold — never by which
        link you opened.
      </p>
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

      {demo === null ? <p className="muted">Checking what this instance offers…</p> : null}
      {demo ? (
        <div style={{ maxWidth: 700, display: "flex", flexDirection: "column", gap: 10 }} data-panel="demo-personas">
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span style={{ flex: 1, height: 1, background: "var(--divider)" }} />
            <span className="eyebrow">Or, on a demo instance</span>
            <span style={{ flex: 1, height: 1, background: "var(--divider)" }} />
          </div>
          {personas.map((p, i) => (
            <button
              className="card"
              data-p={i}
              data-persona={p.key}
              key={p.key}
              style={{ textAlign: "left", cursor: "pointer" }}
              onClick={() => props.onSignedIn(i)}
            >
              <div style={{ font: "500 14px/1.4 Roboto" }}>
                {p.who} <span className="muted">· {p.role} · {p.ref}</span>
              </div>
              <div className="muted">{p.what}</div>
            </button>
          ))}
          <p className="muted">
            Each row signs in as a person the registry already knows, and mints its token from this stack's own
            identity provider — the same first-login path a real sign-in takes. The row names the role that person
            holds; it does not grant one.
          </p>
        </div>
      ) : null}
      {demo === false ? (
        <p className="muted" style={{ maxWidth: 700 }}>
          This instance has no mock identity provider, so there are no demo personas to offer. eSignet is the only door.
        </p>
      ) : null}

      <button
        className="card hi"
        id="onboard-org"
        style={{ textAlign: "left", cursor: "pointer", maxWidth: 700 }}
        onClick={() => nav("/onboard")}
      >
        <div style={{ font: "500 14px/1.4 Roboto" }}>Onboard your organisation</div>
        <div className="muted">
          Apply, accept the published terms, get approved — the real flow, no seeded party. Applying is deliberately
          open: an organisation that is not on the platform yet has no role to sign in with.
        </div>
      </button>

      <div style={{ maxWidth: 700, display: "flex", flexDirection: "column", gap: 10 }}>
        <Callout kind="teal" title="Why this screen exists">
          Every J3 frame in the design reference opens with somebody already inside their console, already holding a
          role, already looking at the right organisation. This door is the step the reference never draws, and it is
          where the console's whole authority story starts.
        </Callout>
        <Callout kind="green" title="What this screen never does">
          It never asks which role you want, and never offers a role you do not hold. A role is granted in the registry
          and read back here — picking your own would make authority a matter of self-declaration.
        </Callout>
      </div>
    </AppbarOnly>
  );
}
