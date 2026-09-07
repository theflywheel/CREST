import { useEffect, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api } from "@crest/api";
import { Chip, KV, OpenNote, Sidecar } from "@crest/ui";
import { short, whenFull } from "../data";

/* The notification link carries a one-time token. It is submitted only in
   the POST body; it is never copied into an API URL or a diagnostic message. */
export function Review() {
  const nav = useNavigate();
  const { claimId = "" } = useParams();
  const [params] = useSearchParams();
  const [reviewWindow, setReviewWindow] = useState<any>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [token] = useState(() => params.get("token") || "");

  useEffect(() => {
    if (token) {
      // The notification URL is a one-time transport. Remove the secret from
      // browser history and address-bar copies before rendering the form.
      window.history.replaceState(null, "", `${window.location.pathname}#/review/${encodeURIComponent(claimId)}`);
    }
  }, [claimId, token]);

  useEffect(() => {
    let live = true;
    api
      .get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`)
      .then((out) => live && setReviewWindow(out))
      .catch((e) => live && setError(String((e as any)?.message || "This review could not be loaded.")));
    return () => {
      live = false;
    };
  }, [claimId]);

  const acknowledge = async () => {
    if (!token) {
      setError("This review link has no acknowledgement token.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.post("confirmation", `/v1/windows/${encodeURIComponent(claimId)}/ack`, { token });
      nav("/work", { replace: true });
    } catch (e) {
      setError(String((e as any)?.message || "This review could not be acknowledged."));
      setBusy(false);
    }
  };

  if (error && !reviewWindow) {
    return (
      <div className="pane-narrow">
        <h2 className="scr-title m">Review unavailable</h2>
        <OpenNote>{error}</OpenNote>
      </div>
    );
  }
  if (!reviewWindow) return null;
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">Your say · review</span>
      <h2 className="scr-title m">Check this work record</h2>
      <p className="body-2">This record was sent to you for review. Read the details, then acknowledge that you have received it.</p>
      <div className="card">
        <KV
          rows={[
            ["Claim", <span className="mono">{short(claimId)}</span>],
            ["Programme", <span className="mono">{short(reviewWindow.contextId)}</span>],
            ["Review by", whenFull(reviewWindow.closesAt)],
            ["Notification", reviewWindow.reach === "reached" ? <Chip kind="ok">reached</Chip> : <Chip kind="warn">pending</Chip>],
          ]}
        />
      </div>
      {error ? <div className="errbar">{error}</div> : null}
      <button className="btn dominant" type="button" onClick={acknowledge} disabled={busy || !token}>
        {busy ? "Recording…" : "I have received this review"}
      </button>
      <Sidecar>
        Acknowledging receipt does not confirm the work or release a payment. You still decide on the record from Work;
        this link only tells CREST that the review reached you.
      </Sidecar>
    </div>
  );
}
