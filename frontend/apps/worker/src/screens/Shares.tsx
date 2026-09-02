// Per-share disclosure consent — the worker's own side of a presentation
// request (reference w1_19, w1_15, w1_20; §9; Blueprint's consent rule).
//
// Three surfaces over one record:
//   /shares       — who is asking, and why, BEFORE any disclosure list (w1_19's
//                   arrival: the inbox names the requester and the purpose;
//                   the list itself is only shown once the worker opens it)
//   /shares/:id   — consent, per share, every time (w1_19's tick list +
//                   w1_15's approve/decline-with-reason). Nothing is shared
//                   before the decision: the list on this screen is WHAT WOULD
//                   BE SHARED, ids and dates only; the documents move only on
//                   an approved collect, and a collect before approval is
//                   refused by the service (409 not_approved).
//   /shares/:id/sent — the worker sees the same list the verifier does
//                   (w1_20): both faces read one record and one resolved
//                   disclosureList, so "what they got / what you kept" here is
//                   structurally the verifier's own view, not a retelling.
//
// Invariant this could break: consent. Every write here is the subject's own
// (bearer-checked, #102); an approval names only credentials from the list the
// worker was shown; a decline records its reason.
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, DisLi, NextBlock, OpenNote, Sidecar } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { short, when } from "../data";

type Disclosed = { credentialId: string; issuedAt: string; revoked: boolean };
type ShareView = {
  request: {
    id: string;
    subjectPartyId: string;
    requestedByPartyId: string;
    purpose: string;
    approvedCredentialIds?: string[];
    declineReason?: string;
    createdAt: string;
    expiresAt: string;
    decidedAt?: string;
    fulfilledAt?: string;
  };
  state: string;
  disclosureList: Disclosed[] | null;
  disclosureListError?: string;
};

async function loadShares(me: string): Promise<ShareView[]> {
  const out = await api
    .get("verification", `/v1/presentation-requests?subjectPartyId=${encodeURIComponent(me)}`)
    .catch(() => null);
  return (out && out.requests) || [];
}

// The requester's display name, read from the registry — a share decision
// starts with WHO is asking, and an unresolvable id is said plainly.
function useRequesterName(partyId?: string): string | null {
  const out = useLoad(
    async () =>
      partyId ? api.get("parties", `/v1/parties/${encodeURIComponent(partyId)}`).catch(() => null) : null,
    [partyId],
  );
  if (!partyId || out === undefined) return null;
  return (out && out.displayName) || short(partyId);
}

function StateChip({ state }: { state: string }) {
  const kind =
    state === "REQUESTED" ? "warn" : state === "APPROVED" || state === "FULFILLED" ? "ok" : "plain";
  return (
    <Chip sm kind={kind}>
      {state}
    </Chip>
  );
}

/* w1_19 — the arrival. Requester and purpose first; the disclosure list only
   after the worker opens the request. */
export function SharesInbox() {
  const s = useSession();
  const shares = useLoad(() => loadShares(s.me!));
  if (!shares) return null;
  const pending = shares.filter((v) => v.state === "REQUESTED");
  const past = shares.filter((v) => v.state !== "REQUESTED");
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">Someone is asking</span>
      <h2 className="scr-title m">Requests to see your record</h2>
      <p className="body-2">
        Nobody sees more than your public record without asking you first. Each request names who is asking and why —
        you decide line by line, per share, every time.
      </p>
      {pending.map((v) => (
        <ShareRow key={v.request.id} v={v} pending />
      ))}
      {!pending.length ? (
        <div className="card quiet">
          <p className="body-2">Nothing is waiting on you. When someone asks to see more, the request appears here.</p>
        </div>
      ) : null}
      {past.length ? (
        <>
          <span className="eyebrow">Answered and expired requests</span>
          {past.map((v) => (
            <ShareRow key={v.request.id} v={v} />
          ))}
        </>
      ) : null}
      <Sidecar>
        Every share — approved or collected — also leaves a line in{" "}
        <Link to="/profile/checks">Who checked me</Link>, like any other check of your record.
      </Sidecar>
    </div>
  );
}

function ShareRow({ v, pending }: { v: ShareView; pending?: boolean }) {
  const name = useRequesterName(v.request.requestedByPartyId);
  return (
    <div className="card" data-share={v.request.id}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
        <span style={{ font: "500 14px/1.4 Roboto" }}>
          {name || "…"} {pending ? "wants to see more" : "asked"}
        </span>
        <StateChip state={v.state} />
      </div>
      <div className="muted">
        “{v.request.purpose}” · asked {when(v.request.createdAt)}
      </div>
      <div style={{ height: 8 }} />
      {pending ? (
        <Link className="btn" to={`/shares/${encodeURIComponent(v.request.id)}`} data-open-share={v.request.id}>
          See what they are asking for
        </Link>
      ) : (
        <Link className="btn secondary" to={`/shares/${encodeURIComponent(v.request.id)}/sent`}>
          What was decided
        </Link>
      )}
    </div>
  );
}

/* w1_15 / w1_19 — consent, per share, every time: the tick list, and the
   worker's answer. One decision surface serves both reference frames — they
   draw the same consent moment in the phone and console idioms, and this door
   is a console. */
export function ShareDecide() {
  const { id = "" } = useParams();
  const s = useSession();
  const nav = useNavigate();
  const [bump, setBump] = useState(0);
  const [ticked, setTicked] = useState<Record<string, boolean> | null>(null);
  const [reason, setReason] = useState("");
  const [refusing, setRefusing] = useState(false);
  const v = useLoad<ShareView | null>(
    () => api.get("verification", `/v1/presentation-requests/${encodeURIComponent(id)}`).catch(() => null),
    [id, bump],
  );
  const name = useRequesterName(v?.request?.requestedByPartyId);
  if (v === undefined) return null;
  if (!v)
    return (
      <OpenNote>
        That request could not be read — it may not exist, or it may not be yours to see.{" "}
        <Link to="/shares">Back to requests.</Link>
      </OpenNote>
    );
  if (v.state !== "REQUESTED")
    return (
      <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
        <OpenNote>
          This request is already {v.state === "EXPIRED" ? "expired — the verifier asks again, and you decide again" : "answered — a settled answer stays settled"}.
        </OpenNote>
        <Link className="btn secondary" to={`/shares/${encodeURIComponent(id)}/sent`}>
          What was decided
        </Link>
      </div>
    );
  const list = v.disclosureList || [];
  // Every line starts ticked, as the reference draws it; unticking is the
  // per-line refusal.
  const on = ticked || Object.fromEntries(list.map((d) => [d.credentialId, true]));
  const n = list.filter((d) => on[d.credentialId]).length;
  const decide = async (approve: boolean) => {
    try {
      const body = approve
        ? { approve: true, approvedCredentialIds: list.map((d) => d.credentialId).filter((cid) => on[cid]) }
        : { approve: false, reason };
      await api.post("verification", `/v1/presentation-requests/${encodeURIComponent(id)}/decision`, body);
      nav(`/shares/${encodeURIComponent(id)}/sent`);
    } catch (e) {
      s.fail(e);
      setBump((b) => b + 1);
    }
  };
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">Someone is asking</span>
      <h2 className="scr-title m">{name || "…"} wants to see more</h2>
      <div className="muted">
        “{v.request.purpose}” · asked {when(v.request.createdAt)} ·{" "}
        <span className="mono">{short(v.request.requestedByPartyId)}</span>
      </div>
      <div className="card">
        <span className="eyebrow">What they would see — nothing moves before you approve</span>
        {v.disclosureListError ? <OpenNote>{v.disclosureListError}</OpenNote> : null}
        {list.length ? (
          <div className="dis" style={{ marginTop: 8 }}>
            {list.map((d) => (
              <label
                key={d.credentialId}
                className="li"
                style={{ cursor: "pointer" }}
                data-dis={d.credentialId}
              >
                <input
                  type="checkbox"
                  checked={!!on[d.credentialId]}
                  onChange={(e) => setTicked({ ...on, [d.credentialId]: e.target.checked })}
                />
                <span>
                  <div className="t">
                    Credential <span className="mono">{short(d.credentialId)}</span>
                    {d.revoked ? " · revoked" : ""}
                  </div>
                  <div className="s">
                    issued {when(d.issuedAt)} · {on[d.credentialId] ? "will be sent if you approve" : "you have unticked this"}
                  </div>
                </span>
              </label>
            ))}
          </div>
        ) : (
          <p className="body-2">Your record holds nothing this request could share.</p>
        )}
      </div>
      <Callout kind="green" title="What they already have">
        They can already see your record is real, that you do community health visits, and that it is system-backed.
        This request is only about the {list.length === 4 ? "four" : list.length} lines above.
      </Callout>
      <Callout kind="teal" title="Either way, it is on your record">
        Whatever you decide is recorded, and you can see later who asked and what you sent. Refusing a line shows as
        refused — they will know you declined rather than that you have nothing.
      </Callout>
      <p className="muted">
        This approval covers this one request — a second look means a second ask. It expires {when(v.request.expiresAt)}
        {" "}if unanswered.
      </p>
      {refusing ? (
        <div className="card">
          <label className="body-2">
            Why are you refusing? The reason is your answer on the record, not an absence.
            <input
              name="sharereason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="e.g. I do not know this requester"
              style={{ width: "100%", marginTop: 4 }}
            />
          </label>
          <div className="btn-row" style={{ marginTop: 8 }}>
            <button className="btn secondary" id="share-refuse-confirm" onClick={() => decide(false)}>
              Refuse
            </button>
          </div>
        </div>
      ) : null}
      <div className="btn-row">
        {!refusing ? (
          <button className="btn secondary" id="share-refuse" onClick={() => setRefusing(true)}>
            Refuse
          </button>
        ) : null}
        <button className="btn" id="share-approve" disabled={n === 0} onClick={() => decide(true)}>
          Send the {n === 2 ? "two" : n} ticked
        </button>
      </div>
      {n === 0 ? (
        <p className="muted">Approving nothing is declining — untick every line and the honest button is Refuse.</p>
      ) : null}
    </div>
  );
}

/* w1_20 — what was decided, from the same record the verifier reads. */
export function ShareSent() {
  const { id = "" } = useParams();
  const s = useSession();
  const v = useLoad<ShareView | null>(() =>
    api.get("verification", `/v1/presentation-requests/${encodeURIComponent(id)}`).catch(() => null),
  );
  const all = useLoad(() => loadShares(s.me!));
  const name = useRequesterName(v?.request?.requestedByPartyId);
  if (v === undefined || all === undefined) return null;
  if (!v)
    return (
      <OpenNote>
        That request could not be read. <Link to="/shares">Back to requests.</Link>
      </OpenNote>
    );
  const approved = new Set(v.request.approvedCredentialIds || []);
  const list = v.disclosureList || [];
  const got = list.filter((d) => approved.has(d.credentialId));
  const kept = list.filter((d) => !approved.has(d.credentialId));
  const declined = v.state === "DECLINED";
  const asked = (all || []).length;
  const agreed = (all || []).filter((x) => x.state === "APPROVED" || x.state === "FULFILLED").length;
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">{declined ? "Refused" : "Sent"}</h2>
      <div className="muted">
        To {name || "…"} · {when(v.request.decidedAt || v.request.createdAt)} · <StateChip state={v.state} />
      </div>
      {declined ? (
        <div className="card">
          <span className="eyebrow">Your answer, on the record</span>
          <p className="body-2">
            You refused this request{v.request.declineReason ? <> — “{v.request.declineReason}”</> : null}. They see a
            refusal, not a gap: declining is an answer, never an absence.
          </p>
        </div>
      ) : (
        <div className="pane-cols">
          <div className="card">
            <span className="eyebrow">What they got</span>
            <div className="dis" style={{ marginTop: 8 }}>
              {got.map((d) => (
                <DisLi
                  key={d.credentialId}
                  on
                  t={<span className="mono">{short(d.credentialId)}</span>}
                  s={`issued ${when(d.issuedAt)}`}
                />
              ))}
              {!got.length ? <p className="body-2">Nothing yet.</p> : null}
            </div>
            {v.state === "APPROVED" ? (
              <p className="muted" style={{ marginTop: 6 }}>
                Approved, not yet collected — the documents move only when they collect, and only once.
              </p>
            ) : null}
          </div>
          <div className="card">
            <span className="eyebrow">What you kept</span>
            <div className="dis" style={{ marginTop: 8 }}>
              {kept.map((d) => (
                <DisLi
                  key={d.credentialId}
                  on={false}
                  t={<span className="mono">{short(d.credentialId)}</span>}
                  s="you unticked this — shown to them as refused"
                />
              ))}
              {!kept.length ? <p className="body-2">You sent every line they asked about.</p> : null}
            </div>
          </div>
        </div>
      )}
      <Sidecar ok>
        This is the very list the verifier sees — one record, one resolved disclosure list, served to both of you. What
        was sent and what was kept cannot read differently on their screen.
      </Sidecar>
      <NextBlock
        happened={
          declined ? (
            <>You refused, with your reason on the record.</>
          ) : (
            <>
              You {v.state === "FULFILLED" ? "shared" : "approved"} {got.length}{" "}
              {got.length === 1 ? "credential" : "credentials"} and kept {kept.length}.
            </>
          )
        }
        who="Nobody — this is finished. A second look means a second request to you."
        when={v.state === "FULFILLED" ? "They have it now." : declined ? "Nothing moves." : "When they collect — once."}
        told="It stays in your list of who has asked, and each collected credential lands in Who checked me."
        ifnot="If you think this was a mistake, your project's support agent can note it — but what was sent cannot be called back."
      />
      <Callout kind="teal" title="Your history of requests">
        You can open this again any time from your record. {asked} {asked === 1 ? "request" : "requests"} so far; you
        agreed to {agreed} of them. (The reference frame quotes a worked example — twelve asked, nine agreed; these are
        your real counts.)
      </Callout>
      <div className="btn-row">
        <Link className="btn secondary" to="/profile/checks">
          See everyone who has asked
        </Link>
        <Link className="btn" to="/shares">
          Next request
        </Link>
      </div>
    </div>
  );
}
