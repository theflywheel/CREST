import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api } from "@crest/api";
import { Chip, OpenNote, Sidecar } from "@crest/ui";
import { describeError, useSession } from "../session";
import { short } from "../data";
import { takePendingReview } from "../reviewState";

/*
 * A duplicate-hold merge has two separate decisions. The registry custodian
 * chooses whether the records are duplicates; a worker who is one of the
 * candidates confirms which record survives. This page performs only the
 * worker action. It never calls the custodian's resolve endpoint and it never
 * accepts a confirmer id from the browser.
 */
export function MergeConfirm() {
  const nav = useNavigate();
  const s = useSession();
  const { holdId = "" } = useParams();
  const [params] = useSearchParams();
  const survivorPartyId = params.get("survivor") || "";
  const [agree, setAgree] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const confirm = async () => {
    if (busy) return;
    if (!agree) {
      setError("Check the confirmation statement before submitting.");
      return;
    }
    if (!holdId || !survivorPartyId) {
      setError("This confirmation link is incomplete. Ask the registry custodian for a new one.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.post("parties", `/v1/holds/${encodeURIComponent(holdId)}/confirm`, {
        survivorPartyId,
        confirmationMethod: "worker-web",
      });
      takePendingReview();
      setDone(true);
    } catch (e) {
      setError(describeError(e));
      setBusy(false);
    }
  };

  if (!holdId || !survivorPartyId) {
    return (
      <div className="pane-narrow">
        <h2 className="scr-title m">Confirmation link unavailable</h2>
        <OpenNote>{error || "The link is missing the hold or survivor record."}</OpenNote>
      </div>
    );
  }

  if (done) {
    return (
      <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
        <span className="eyebrow">Worker confirmation</span>
        <h2 className="scr-title m">Your confirmation was recorded</h2>
        <div className="card">
          <Chip kind="ok">confirmed</Chip>
          <p className="body-2" style={{ marginTop: 8 }}>
            The registry custodian can now review and close this duplicate hold. Your confirmation does not merge records by itself.
          </p>
        </div>
        <button className="btn secondary" type="button" onClick={() => nav("/home", { replace: true })}>
          Back to Home
        </button>
      </div>
    );
  }

  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">Worker confirmation</span>
      <h2 className="scr-title m">Confirm the surviving record</h2>
      <p className="body-2">
        The registry found two records that may belong to the same person. If you are one of the people on that hold,
        confirm which record should survive. The custodian still makes the separate merge decision.
      </p>
      <div className="card">
        <div className="muted">Hold</div>
        <div className="mono">{short(holdId)}</div>
        <div className="muted" style={{ marginTop: 8 }}>Surviving record</div>
        <div className="mono">{short(survivorPartyId)}</div>
      </div>
      {error ? <div className="errbar">{error}</div> : null}
      <label className="body-2" style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
        <input type="checkbox" checked={agree} onChange={(e) => setAgree(e.target.checked)} style={{ marginTop: 3 }} />
        <span>
          I confirm that these two records are both mine, that I am <span className="mono">{s.me}</span>, and that
          <span className="mono"> {survivorPartyId}</span> is the record to keep.
        </span>
      </label>
      <button className="btn dominant" type="button" onClick={confirm} disabled={busy || !agree}>
        {busy ? "Recording…" : "Confirm this surviving record"}
      </button>
      <Sidecar>
        This action is tied to the worker account you signed in with. CREST records who confirmed and when; it does not
        disclose either record to the custodian through this page.
      </Sidecar>
    </div>
  );
}
