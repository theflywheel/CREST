import { useEffect, useState, type ChangeEvent } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import QRCode from "qrcode";
import { api, links } from "@crest/api";
import { Chip, DisLi, GridTable, KV, OpenNote, Sidecar } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { loadCredsStatus, short, tierOf, when } from "../data";
import { exportWallet, importWallet, loadWallet, saveWallet } from "../walletStore";

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

function mergeCredentials(server: any[], local: any[] | null): any[] {
  const byID = new Map<string, any>();
  for (const c of server) if (c && c.id) byID.set(String(c.id), c);
  for (const c of local || []) if (c && c.id) byID.set(String(c.id), c);
  return Array.from(byID.values());
}

/* w1_12 — wallet list, as a grid-row list on desktop */
export function Wallet() {
  const s = useSession();
  const remote = useLoad(() => loadCredsStatus(s.me!));
  const creds = remote?.credentials || [];
  const [localCreds, setLocalCreds] = useState<any[] | null>(null);
  const [passphrase, setPassphrase] = useState("");
  const [walletMessage, setWalletMessage] = useState<string | null>(null);
  const visibleCreds = mergeCredentials(creds, localCreds);
  const unlock = async () => {
    try {
      const restored = await loadWallet(passphrase);
      if (!restored) {
        setWalletMessage("There is no encrypted wallet saved in this browser yet.");
        return;
      }
      setLocalCreds(restored);
      setWalletMessage(`Unlocked ${restored.length} credential${restored.length === 1 ? "" : "s"}.`);
    } catch (e) {
      setWalletMessage(e instanceof Error ? e.message : "Could not unlock the wallet.");
    }
  };
  const secure = async () => {
    try {
      // If custody was already transferred, the server list contains only
      // metadata. Load the existing encrypted wallet before writing so a
      // repeat acknowledgement cannot replace a complete local history with
      // those metadata rows.
      const existing = await loadWallet(passphrase);
      const complete = mergeCredentials(visibleCreds, existing);
      await saveWallet(passphrase, complete);
      setLocalCreds(complete);
      const transferable = complete.filter((c) => c && c.id && c.credentialSubject);
      const failures: string[] = [];
      for (const c of transferable) {
        try {
          // The list endpoint returns the signed document itself, so it
          // cannot add the issuer-side digest without changing that signed
          // JSON. Read the private record envelope to obtain the digest that
          // the custody acknowledgement must bind to.
          const record = await api.get("verification", `/v1/credentials/${encodeURIComponent(c.id)}`);
          const digest = String(record?.digest || "");
          if (!digest) throw new Error("the issuer record has no digest");
          await api.post("verification", `/v1/credentials/${encodeURIComponent(c.id)}/custody-transfer`, {
            storage: "encrypted-wallet",
            durable: true,
            digest,
          });
        } catch {
          failures.push(String(c.id));
        }
      }
      setWalletMessage(
        failures.length
          ? `Encrypted ${complete.length} credentials, but ${failures.length} custody transfer${failures.length === 1 ? "" : "s"} still need retrying.`
          : `Encrypted ${complete.length} credential${complete.length === 1 ? "" : "s"}; custody transfer recorded for ${transferable.length}.`,
      );
    } catch (e) {
      setWalletMessage(e instanceof Error ? e.message : "Could not secure the wallet.");
    }
  };
  const download = async () => {
    try {
      if (creds.some((c) => c && c.custody === "transferred") && !localCreds) {
        throw new Error("Unlock the browser wallet before exporting; central records contain metadata only after custody transfer.");
      }
      const blob = await exportWallet(passphrase, visibleCreds);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "crest-wallet.json";
      link.click();
      URL.revokeObjectURL(url);
      setWalletMessage(`Exported the complete history (${visibleCreds.length} credentials) as an encrypted file.`);
    } catch (e) {
      setWalletMessage(e instanceof Error ? e.message : "Could not export the wallet.");
    }
  };
  const restore = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const restored = await importWallet(passphrase, file);
      setLocalCreds(restored);
      setWalletMessage(`Restored ${restored.length} credential${restored.length === 1 ? "" : "s"}.`);
    } catch (e) {
      setWalletMessage(e instanceof Error ? e.message : "Could not import the wallet.");
    } finally {
      event.target.value = "";
    }
  };
  return (
    <>
      <div className="pagehead">
        <h2 className="scr-title m">My credentials</h2>
      </div>
      <p className="muted" style={{ maxWidth: 640 }}>
        Each one is a signed document you hold. It is provable to a stranger in a minute, offline — it does not need
        CREST to be believed.
      </p>
      {remote?.unavailable ? (
        <OpenNote>
          CREST is unavailable. You can still unlock or import the encrypted wallet on this device and show its full
          signed credential history offline.
        </OpenNote>
      ) : null}
      {visibleCreds.length ? (
        <GridTable cols="1.6fr 1fr .9fr 1.4fr 1.2fr" head={["Activity", "Outcome", "When", "Trust", "Credential"]}>
          {visibleCreds.map((c, i) => {
            const we = (c.credentialSubject || {}).workEvent || {};
            return (
              <Link className="g-row" to={`/wallet/${i}`} state={{ credential: c }} key={c.id || i}>
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
      {creds.some((c) => c && c.custody === "transferred") && !localCreds ? (
        <OpenNote>
          Some older records have been transferred to your encrypted wallet and are shown here as metadata. Unlock this
          browser wallet or import your encrypted backup to restore their signed documents.
        </OpenNote>
      ) : null}
      <div className="pane-narrow">
        <div className="card">
          <h3 className="scr-sub">Encrypted backup</h3>
          <p className="body-2">
            Keep a durable copy under your control. The passphrase encrypts the complete credential history before it
            is stored in this browser or downloaded. After you transfer custody, CREST keeps only claim, digest, status,
            and audit metadata; recovery of the signed documents requires this backup or another encrypted copy.
          </p>
          <label className="field">
            <span>Wallet passphrase</span>
            <input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="new-password" />
          </label>
          <div className="btn-row">
            <button className="btn secondary" type="button" onClick={unlock} disabled={!passphrase}>
              Unlock browser wallet
            </button>
            <button className="btn" type="button" onClick={secure} disabled={!passphrase || !s.me || !navigator.onLine}>
              Store here and transfer custody
            </button>
            <button className="btn secondary" type="button" onClick={download} disabled={!passphrase || !visibleCreds.length}>
              Export full history
            </button>
            <label className="btn secondary">
              Import encrypted wallet
              <input type="file" accept="application/json" onChange={restore} hidden disabled={!passphrase} />
            </label>
          </div>
          {walletMessage ? <p className="muted">{walletMessage}</p> : null}
        </div>
        <div className="card" id="inji-import">
          <h3 className="scr-sub">Keep it in your own wallet</h3>
          <p className="body-2">
            Long-term custody of your record belongs to <b>you</b>, in your Inji wallet — not to this site. In the
            wallet, choose the <b>CREST</b> issuer and sign in with the same identity you used here; your confirmed
            work events are issued straight into the wallet, signed by this deployment's issuer, and checkable by
            anyone without asking CREST.
          </p>
          <div className="btn-row">
            <a className="btn" href={links.injiWeb} target="_blank" rel="noopener noreferrer">
              Open the Inji wallet
            </a>
          </div>
        </div>
        <Sidecar>
          This page is the CREST-side view of your complete credential history. The encrypted backup above is a
          portable copy you can keep and restore without handing your passphrase to CREST.
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
  const location = useLocation();
  const s = useSession();
  const remote = useLoad(() => loadCredsStatus(s.me!));
  const creds = remote?.credentials || [];
  // The wallet can restore a full document locally after the issuer has
  // transferred custody and now returns metadata. Preserve that document
  // across the list-to-detail navigation without putting it in the URL.
  const c = (location.state as { credential?: any } | null)?.credential || creds[Number(idx)];
  if (!c)
    return (
      <>
        {remote?.unavailable ? (
          <OpenNote>CREST is unavailable. Return to the wallet to unlock or import the signed document stored on this device.</OpenNote>
        ) : null}
        <div className="card quiet">
          <p className="body-2">
            {remote ? "That credential is not in your wallet." : "Loading your wallet…"} <Link to="/wallet">Back to the wallet.</Link>
          </p>
        </div>
      </>
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
        This credential is yours. It resolves without trusting CREST — a verifier checks the signatures themselves.
      </Sidecar>
      <div className="btn-row">
        <Link className="btn secondary" to="/pay">
          Payment
        </Link>
        <Link className="btn" to={`/wallet/${idx}/show`} state={{ credential: c }}>
          Show to someone
        </Link>
      </div>
    </div>
  );
}

/* the "show to someone" face of a credential */
export function CredShow() {
  const { idx = "" } = useParams();
  const location = useLocation();
  const s = useSession();
  const remote = useLoad(() => loadCredsStatus(s.me!));
  const creds = remote?.credentials || [];
  const c = (location.state as { credential?: any } | null)?.credential || creds[Number(idx)];
  if (!c)
    return (
      <>
        {remote?.unavailable ? (
          <OpenNote>CREST is unavailable. Return to the wallet to unlock or import the signed document stored on this device.</OpenNote>
        ) : null}
        <div className="card quiet">
          <p className="body-2">
            {remote ? "That credential is not in your wallet." : "Loading your wallet…"} <Link to="/wallet">Back.</Link>
          </p>
        </div>
      </>
    );
  return (
    <div className="pane-cols">
      <div>
        <h2 className="scr-title m">Show to someone</h2>
        <p className="body-2">
          Hand them the phone, or let them scan the printed card. They will see that your record is real, what you can
          do, and how strongly it is backed up. They will not see your name, your number, or where you live. This —
          and only this — is what a scan gives away:
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
          Checks submitted to CREST appear in "Who checked me". A signature checked entirely offline does not contact CREST or create a trail entry.
        </Sidecar>
        <div className="card quiet">
          <span className="eyebrow">If they want more</span>
          <p className="body-2" style={{ marginTop: 6 }}>
            If they need more than what the scan shows, they have to ask, and you can say no. Saying no does not make
            your record look worse to them — they simply see that you declined.
          </p>
        </div>
      </div>
      <div>
        <CredQR c={c} />
        <details className="card" style={{ marginTop: 12 }}>
          <summary className="eyebrow" style={{ cursor: "pointer" }} id="show-json">
            The signed document itself — show the JSON
          </summary>
          <div style={{ overflowX: "auto", marginTop: 8 }}>
            <pre className="mono" style={{ fontSize: "10.5px", lineHeight: 1.5 }}>
              {JSON.stringify(c, null, 2)}
            </pre>
          </div>
        </details>
      </div>
    </div>
  );
}

/* The offline presentation (reference w1_18): the whole signed credential as
   a QR, generated on this device — a verifier scans and checks the signature
   without asking CREST, which is the "provable to a stranger in a minute,
   offline" promise. Rendered locally; nothing leaves the phone. */
function CredQR({ c }: { c: any }) {
  const [src, setSrc] = useState<string | null>(null);
  const [tooBig, setTooBig] = useState(false);
  useEffect(() => {
    let live = true;
    QRCode.toDataURL(JSON.stringify(c), { errorCorrectionLevel: "L", margin: 2, scale: 3 }).then(
      (url) => live && setSrc(url),
      // A QR holds ~3KB; a credential past that cannot fit in one code.
      () => live && setTooBig(true),
    );
    return () => {
      live = false;
    };
  }, [c]);
  return (
    <div className="card" style={{ textAlign: "center" }}>
      <span className="eyebrow">Scan this — it works with no signal</span>
      {src ? (
        <img
          id="cred-qr"
          src={src}
          alt="This credential as a QR code"
          style={{ width: "100%", maxWidth: 280, imageRendering: "pixelated", marginTop: 8 }}
        />
      ) : tooBig ? (
        <OpenNote>
          This credential is larger than one QR code can carry. The reference's answer is a compressed PixelPass-style
          encoding, which is not built yet (traceability w1_18) — until then, show the JSON below or share from your
          Inji wallet.
        </OpenNote>
      ) : (
        <p className="muted">Drawing the code…</p>
      )}
      <p className="muted" style={{ marginTop: 8 }}>
        The code carries the signed credential itself. The signature, not this site, is what the verifier believes.
      </p>
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
