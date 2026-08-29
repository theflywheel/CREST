import { Link, useParams } from "react-router-dom";
import { Chip, DisLi, GridTable, KV, OpenNote, Sidecar } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { loadCreds, short, tierOf, when } from "../data";

function TierChip({ c }: { c: any }) {
  const { tier, captureMethod } = tierOf(c);
  if (!tier) return <Chip kind="plain">Tier derived when checked</Chip>;
  const cls = tier === "1" ? "tier1" : tier === "2" ? "tier2" : "tier3";
  return (
    <Chip kind={cls}>
      Tier {tier}
      {captureMethod ? " · " + captureMethod.replace(/-/g, " ") : ""}
    </Chip>
  );
}

/* w1_12 — wallet list, as a grid-row list on desktop */
export function Wallet() {
  const s = useSession();
  const creds = useLoad(() => loadCreds(s.me!));
  if (!creds) return null;
  return (
    <>
      <div className="pagehead">
        <h2 className="scr-title m">My credentials</h2>
      </div>
      <p className="muted" style={{ maxWidth: 640 }}>
        Each one is a signed document you hold. It is provable to a stranger in a minute, offline — it does not need
        CREST to be believed.
      </p>
      {creds.length ? (
        <GridTable cols="1.6fr 1fr .9fr 1.4fr 1.2fr" head={["Activity", "Outcome", "When", "Trust", "Credential"]}>
          {creds.map((c, i) => {
            const we = (c.credentialSubject || {}).workEvent || {};
            return (
              <Link className="g-row" to={`/wallet/${i}`} key={c.id || i}>
                <span style={{ font: "500 13.5px/1.4 Roboto" }}>{we.activity || "Work event"}</span>
                <span>{we.outcome ? `${we.outcome.value} ${we.outcome.unit}` : ""}</span>
                <span>{when((we.period || {}).start)}</span>
                <span>
                  <TierChip c={c} />
                </span>
                <span className="mono" style={{ color: "var(--text-2)" }}>
                  {short(c.id)}
                </span>
              </Link>
            );
          })}
        </GridTable>
      ) : (
        <div className="card quiet">
          <p className="body-2">
            No credentials yet. One is issued each time a confirmation window closes — after you have had your say, or
            seven days have passed.
          </p>
        </div>
      )}
      <div className="pane-narrow">
        <Sidecar>
          Long-term custody of these credentials belongs to your <b>Inji wallet</b> — the deployed browser wallet at{" "}
          <a href="https://crest-inji-web-production.up.railway.app" target="_blank" rel="noopener noreferrer">
            crest-inji-web-production.up.railway.app
          </a>
          . This page is the CREST-side view of the same credentials, not a second copy you must manage. One gap,
          named: the demo fleet does not yet issue through Certify/OpenID4VCI, so the import path into Inji is not
          wired (blueprint §5, docs/crest-inji-architecture.html).
        </Sidecar>
      </div>
      <p className="muted">
        <Link to="/wallet/share">Share a link instead of showing the phone</Link> ·{" "}
        <Link to="/wallet/deferred">A skill still being assessed</Link>
      </p>
    </>
  );
}

/* w1_18 — credential detail */
export function Cred() {
  const { idx = "" } = useParams();
  const s = useSession();
  const creds = useLoad(() => loadCreds(s.me!));
  if (!creds) return null;
  const c = creds[Number(idx)];
  if (!c)
    return (
      <div className="card quiet">
        <p className="body-2">
          That credential is not in your wallet. <Link to="/wallet">Back to the wallet.</Link>
        </p>
      </div>
    );
  const we = (c.credentialSubject || {}).workEvent || {};
  const issuer = typeof c.issuer === "object" ? c.issuer.name || c.issuer.id : c.issuer;
  const ceiling = c.tierCeiling || (c.trust && c.trust.tierCeiling) || null;
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <div className="pagehead">
        <h2 className="scr-title m">{we.activity || "Credential"}</h2>
        <TierChip c={c} />
      </div>
      <KV
        rows={[
          ["Issued by", issuer || "—"],
          [
            "Under definition",
            <span className="mono">
              {short(we.definitionId || c.definitionId || "")}
              {we.definitionVersion ? ` · v${we.definitionVersion}` : ""}
            </span>,
          ],
          ["Tier ceiling", ceiling ? String(ceiling) : "derived when a verifier checks — never stored"],
          ["Skill code", <span className="mono">{we.skillCode || "—"}</span>],
          ["Outcome", we.outcome ? `${we.outcome.value} ${we.outcome.unit}` : "—"],
          ["Credential id", <span className="mono">{short(c.id)}</span>],
        ]}
      />
      <Sidecar ok>
        This credential is yours. It resolves without trusting CREST — the signature, not the server, is what a
        verifier believes.
      </Sidecar>
      <div className="btn-row">
        <Link className="btn secondary" to="/pay">
          Payment
        </Link>
        <Link className="btn" to={`/wallet/${idx}/show`}>
          Show to someone
        </Link>
      </div>
    </div>
  );
}

/* the "show to someone" face of a credential */
export function CredShow() {
  const { idx = "" } = useParams();
  const s = useSession();
  const creds = useLoad(() => loadCreds(s.me!));
  if (!creds) return null;
  const c = creds[Number(idx)];
  if (!c)
    return (
      <div className="card quiet">
        <p className="body-2">
          That credential is not in your wallet. <Link to="/wallet">Back.</Link>
        </p>
      </div>
    );
  return (
    <div className="pane-cols">
      <div>
        <h2 className="scr-title m">Show to someone</h2>
        <p className="body-2">
          Hand them the phone, or let them scan the printed card. This — and only this — is what a scan gives away:
        </p>
        <div className="card">
          <div className="dis">
            <DisLi on t="That the work happened" s="the activity, the outcome, the period" />
            <DisLi on t="That it was confirmed" s="how the window closed, and the trust tier they derive" />
            <DisLi on t="The issuer's signature" s="checkable offline, without asking CREST" />
            <DisLi on={false} t="Your name" s="the credential names a pairwise reference, not you" />
            <DisLi on={false} t="Your ID number or biometrics" s="CREST never held them, so a scan cannot leak them" />
            <DisLi on={false} t="Your other work, or your pay" s="one credential proves one thing" />
          </div>
        </div>
        <Sidecar>
          Every scan leaves a line in "Who checked me", on your Profile — even a failed one, even inside a batch.
        </Sidecar>
      </div>
      <div>
        <div className="card">
          <span className="eyebrow">The signed document itself</span>
          <div style={{ overflowX: "auto", marginTop: 8 }}>
            <pre className="mono" style={{ fontSize: "10.5px", lineHeight: 1.5 }}>
              {JSON.stringify(c, null, 2)}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
}

/* w1_16/17 — deferred qualification (no backend) */
export function Deferred() {
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">A skill still being assessed</h2>
      <OpenNote>
        <b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_16–w1_17) show a qualification that
        arrives later than the work it rests on, with the wallet honest about the gap. No verification or definitions
        endpoint exposes deferred qualifications today; Blueprint §7 (definition faces) is where it will hang. Nothing
        live is drawn.
      </OpenNote>
      <p className="body-2">
        The promise this screen will keep: while an assessment is pending, the wallet says "being assessed" — it never
        shows a credential that does not exist, and never hides the work that does.
      </p>
    </div>
  );
}

/* w1_19/20 — share links (no backend) */
export function Share() {
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Share a link instead</h2>
      <OpenNote>
        <b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_19–w1_20) show a time-boxed share link —
        "anyone with this link, for 7 days, sees these two credentials and nothing else." The verification service has
        no share-link endpoint today. Nothing live is drawn.
      </OpenNote>
      <p className="body-2">
        The promise this screen will keep: a link you can revoke, scoped to exactly what you chose, with every open of
        it logged in "Who checked me".
      </p>
    </div>
  );
}
