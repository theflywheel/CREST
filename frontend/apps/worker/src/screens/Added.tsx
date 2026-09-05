// w1_17 — "One action, and everything already earned changes state."
//
// The moment an identity anchor lands on a worker's record. Nothing here is
// written by this screen: an anchor is an APPENDED identity binding (the
// eSignet first-login path, or a later re-anchor), and everything shown is
// re-DERIVED from the record as it stands now — assurance from the bindings
// (GET /v1/parties/{id}/assurance), and each credential's trust tier at
// verify time. No stored tier exists to upgrade; that is exactly why the
// upgrade is instant and retroactive (§4.1, "trust strength is derived,
// never stored").
//
// Invariant this could break: verification — and the way it would break is a
// tier shown as if stored. Every number on this screen is served derived, and
// the re-check button runs the real verifier.
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, KV, NextBlock, OpenNote, Sidecar, Stat } from "@crest/ui";
import { useSession, describeError } from "../session";
import { useLoad } from "../App";
import { loadCreds, monthOf, short, when } from "../data";

export function Added() {
  const s = useSession();
  const data = useLoad(async () => {
    const [party, assurance, creds] = await Promise.all([
      api.get("parties", `/v1/parties/${encodeURIComponent(s.me!)}`).catch(() => null),
      api.get("parties", `/v1/parties/${encodeURIComponent(s.me!)}/assurance`).catch(() => null),
      loadCreds(s.me!),
    ]);
    return { party, assurance, creds };
  });
  const [verdict, setVerdict] = useState<any>(null);
  const [verr, setVerr] = useState<string | null>(null);
  if (!data) return null;
  const bindings: any[] = (data.party && data.party.identityBindings) || [];
  const newest = bindings.length ? bindings[bindings.length - 1] : null;
  const ia = data.assurance && data.assurance.identityAssurance;
  const because: string[] = (data.assurance && data.assurance.because) || [];
  const oldest = data.creds.length
    ? data.creds[0]
    : null;
  const recheck = async () => {
    setVerr(null);
    try {
      const v = await api.post("verification", "/v1/verify", { credential: data.creds[0] });
      setVerdict(v);
    } catch (e) {
      setVerr(describeError(e));
    }
  };
  // w1_16 — the pre-anchor face: what the missing document blocks, and only
  // that. Everything counted here is the worker's real record.
  if (!newest) {
    return (
      <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
        <span className="eyebrow">Before your first credential</span>
        <h2 className="scr-title m">One more thing, and only once</h2>
        <p className="body-2">
          You have {data.creds.length} validated {data.creds.length === 1 ? "piece" : "pieces"} of work. To turn them
          into something you can show another employer, we need a document with your name on it.
        </p>
        <div className="card">
          <span className="eyebrow">What counts</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            National ID, passport, voter card, or a letter from your chief. Any of these. If you have none of them, a
            registering agent can vouch for you in person instead.
          </p>
        </div>
        <div className="card">
          <span className="eyebrow">What you cannot do until then</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            Your work is safe, recorded and paid. What you cannot yet do is carry it to a different employer as proof,
            because a credential has to name somebody a stranger can check.
          </p>
        </div>
        <Sidecar>
          What to do: add a document, or ask a registering agent to vouch for you. Both take a few minutes and you
          only do it once. Who can help: your project's support agent, or any registering agent.
        </Sidecar>
        <div className="btn-row">
          <Link className="btn secondary" to="/home">
            Later
          </Link>
          <Link className="btn" to="/">
            Add it now — sign in through eSignet
          </Link>
        </div>
      </div>
    );
  }
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <Chip kind="ok">✓ Added</Chip>
      <h2 className="scr-title m">
        Identity anchor · {String(newest.providerClass || newest.provider)} — asserted {when(newest.assertedAt)}
      </h2>
      <div className="stats" style={{ maxWidth: 560 }}>
        <Stat n={data.creds.length} label="pieces of work, all already validated" />
        <Stat n={ia || "—"} label="identity assurance, derived right now" />
      </div>
      {oldest ? (
        <p className="muted">
          including work from {monthOf(((oldest.credentialSubject || {}).workEvent || {}).period?.start || oldest.issuedAt)}
        </p>
      ) : null}
      <Callout kind="green" title="Your eight months just became portable">
        Nothing you did before today was wasted or has to be done again. The work was always recorded properly — it was
        waiting on this one step to become something you can show elsewhere.
      </Callout>
      <div className="card">
        <span className="eyebrow">Why the strength changes with no rewrite</span>
        <p className="body-2">
          CREST never stored a trust tier on your credentials. A verifier derives it fresh at every check, from the
          credential's provenance and your identity assurance as it stands <b>then</b> — so the anchor above lifts
          everything already earned the moment it lands, and would lift it again if a stronger one landed later.
        </p>
        <KV
          rows={[
            ["Assurance now", <>{ia || "not served"} — {because.join("; ") || "no reason given"}</>],
            ["Bindings on the record", `${bindings.length} — appended, never edited; the old ones stay for audit`],
          ]}
        />
        {data.creds.length ? (
          <>
            <div style={{ height: 8 }} />
            <button className="btn secondary" id="recheck" onClick={recheck}>
              Check how a credential reads now
            </button>
            {verr ? <OpenNote>{verr}</OpenNote> : null}
            {verdict ? (
              <div id="recheck-out" style={{ marginTop: 8 }}>
                <KV
                  rows={[
                    ["Credential", <span className="mono">{short(data.creds[0].id)}</span>],
                    [
                      "Reads as",
                      <>
                        {verdict.valid ? "valid" : "not valid"}
                        {verdict.tier ? <> · Tier {verdict.tier} — derived at this check, stored nowhere</> : null}
                      </>,
                    ],
                    ...(verdict.notEstablished && verdict.notEstablished.length
                      ? [["Not established", verdict.notEstablished.join("; ")] as [string, string]]
                      : []),
                  ]}
                />
              </div>
            ) : null}
          </>
        ) : (
          <OpenNote>
            No credentials yet — the anchor still counts: the first credential you earn will derive its strength from
            this identity, with nothing to migrate.
          </OpenNote>
        )}
      </div>
      <NextBlock
        happened="Your identity anchor was added."
        who="Nobody — this is finished."
        when="Your credentials read at the new strength from now on, at every check."
        told="They are already in your wallet; nothing was re-issued because nothing needed to be."
        ifnot="If a credential is missing, your project's support agent can check it. Nothing else is pending."
      />
      <Sidecar ok>
        A verifier who checked you yesterday and checks you again today derives a different — stronger — answer from
        the same credentials. That freedom is the point of never storing the tier.
      </Sidecar>
      <div className="btn-row">
        <Link className="btn secondary" to="/home">
          Done
        </Link>
        <Link className="btn" to="/wallet">
          See your record
        </Link>
      </div>
    </div>
  );
}
