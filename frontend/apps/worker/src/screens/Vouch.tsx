// The recovery confirmer's own screens (reference w4_1–w4_3, journey W-5):
// somebody a worker nominated, being asked "is this really them?".
//
//   /vouch             — a request arrives (w4_1). The reference draws it as
//                        an SMS; this deployment has no SMS channel adapter
//                        (#150, Blueprint §16), so the same request is
//                        delivered in-app and the gap is said on-screen. The
//                        record — GET /v1/recoveries?confirmerPartyId= — is
//                        what any returning channel would deliver from.
//   /vouch/:id         — two of three (w4_2): confirmations recorded, what
//                        completion does, all read live from the record.
//   /vouch/:id/refused — the refusal (w4_3). The reference frame is an
//                        unresolved-question card; the questions it poses now
//                        have recorded answers (Blueprint §16): a permanent
//                        refusal with an owner and a required reason, the
//                        recovery held OPEN, and the opener owing the next
//                        step on the ?refused=true queue.
//
// Invariant this could break: verification (a recovery re-establishes who may
// speak for a record). Everything here is the confirmer's own bearer-checked
// voice; quorum stays the service's 2-of-distinct-authorities rule — this
// screen never counts votes itself.
import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, KV, OpenNote, Sidecar } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { short, when } from "../data";

type Recovery = {
  id: string;
  partyId: string;
  openedByPartyId: string;
  reason: string;
  state: string;
  createdAt: string;
  confirmations: Array<{ confirmerPartyId: string; authorityPartyId: string; confirmedAt: string }>;
  refusals?: Array<{ refuserPartyId: string; reason: string; refusedAt: string }>;
  completedAt?: string;
};

type RecoveryRouteState = { recovery?: Recovery; viewer?: string };

function usePartyName(partyId?: string): string | null {
  const out = useLoad(
    async () =>
      partyId ? api.get("parties", `/v1/parties/${encodeURIComponent(partyId)}`).catch(() => null) : null,
    [partyId],
  );
  if (!partyId || out === undefined) return null;
  return (out && out.displayName) || short(partyId);
}

/* w4_1 — a request arrives. */
export function VouchInbox() {
  const s = useSession();
  const out = useLoad(() =>
    api.get("parties", `/v1/recoveries?confirmerPartyId=${encodeURIComponent(s.me!)}`).catch(() => null),
  );
  if (out === undefined) return null;
  const list: Recovery[] = (out && out.recoveries) || [];
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Vouch for someone</h2>
      <p className="body-2">
        People who trust you named you a recovery contact. When one of them loses their phone, the request to confirm
        it is really them lands here.
      </p>
      <OpenNote>
        <b>The reference delivers this by SMS; this deployment has no SMS channel.</b> Notifications were dropped with
        the notify service (#150) and the gap is held deliberately (Blueprint §16). What you see below is the request
        itself — the same content an SMS adapter would deliver; which channel carries it is deployment configuration.
      </OpenNote>
      {list.map((r) => (
        <VouchCard key={r.id} r={r} />
      ))}
      {!list.length ? (
        <div className="card quiet">
          <p className="body-2">Nobody who nominated you has an open recovery. Nothing is asked of you.</p>
        </div>
      ) : null}
    </div>
  );
}

function VouchCard({ r }: { r: Recovery }) {
  const s = useSession();
  const nav = useNavigate();
  const name = usePartyName(r.partyId);
  const mine = (r.confirmations || []).some((c) => c.confirmerPartyId === s.me);
  const [answering, setAnswering] = useState<"yes" | "no" | null>(null);
  // The authority behind this YES is a real fact about the confirmer, not a
  // default: their own active grants each name the body that stands behind
  // them, so the first is offered and the confirmer can name another.
  const [authority, setAuthority] = useState<string>("");
  const authorities = useLoad<string[]>(
    async () => {
      const out = await api.get("parties", "/v1/authorizations/mine").catch(() => null);
      return Array.from(
        new Set(
          ((out && out.authorizations) || [])
            .map((a: { authorityPartyId?: string }) => a.authorityPartyId)
            .filter(Boolean) as string[],
        ),
      );
    },
    [s.me],
  );
  useEffect(() => {
    if (!authority && authorities && authorities.length) setAuthority(authorities[0]);
  }, [authorities]); // eslint-disable-line react-hooks/exhaustive-deps
  const [reason, setReason] = useState("");
  const yes = async () => {
    try {
      const recovery = await api.post("parties", `/v1/recoveries/${encodeURIComponent(r.id)}/confirmations`, {
        confirmerPartyId: s.me,
        authorityPartyId: authority.trim(),
      });
      nav(`/vouch/${encodeURIComponent(r.id)}`, { state: { recovery, viewer: s.me } });
    } catch (e) {
      s.fail(e);
    }
  };
  const no = async () => {
    try {
      const recovery = await api.post("parties", `/v1/recoveries/${encodeURIComponent(r.id)}/refusals`, {
        refuserPartyId: s.me,
        reason,
      });
      nav(`/vouch/${encodeURIComponent(r.id)}/refused`, { state: { recovery, viewer: s.me } });
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <div className="card" data-recovery={r.id}>
      <span className="eyebrow">CREST · {when(r.createdAt)}</span>
      <p className="body-2" style={{ marginTop: 6 }}>
        {name || "…"} says they lost their phone and is asking to recover their Crest record — “{r.reason}”. You are
        one of the people they nominated. Reply YES if this is really them, or NO if you are not sure.
      </p>
      <p className="muted">
        Two of the nominees must reply YES — each backed by a different authority. Your reply alone does not complete
        recovery.
      </p>
      {mine ? (
        <Link className="btn secondary" to={`/vouch/${encodeURIComponent(r.id)}`} state={{ recovery: r, viewer: s.me }}>
          You said YES — see where it stands
        </Link>
      ) : answering === "yes" ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 8 }}>
          <label className="body-2">
            The organisation that stands behind you — your YES counts under an authority that vouches for you, and two
            voices from the same authority count once.
            <input name="authority" value={authority} onChange={(e) => setAuthority(e.target.value)} placeholder="the party id of the organisation you hold a role under" style={{ width: "100%", marginTop: 4 }} />
            {!authority ? (
              <span className="muted" style={{ display: "block", marginTop: 4 }}>
                You hold no role here that names an authority, so there is none to fill in for you. Name the
                organisation that stands behind you — a reply with no authority behind it cannot be counted.
              </span>
            ) : null}
          </label>
          <button className="btn" id="vouch-yes-confirm" disabled={!authority.trim()} onClick={yes}>
            Reply YES
          </button>
        </div>
      ) : answering === "no" ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 8 }}>
          <label className="body-2">
            Why not? A refusal records its reason — it is what the next step is read from, never a silent dead end.
            <input name="refusereason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="e.g. the voice on the phone was not theirs" style={{ width: "100%", marginTop: 4 }} />
          </label>
          <button className="btn secondary" id="vouch-no-confirm" onClick={no}>
            Reply NO
          </button>
        </div>
      ) : (
        <div className="btn-row" style={{ marginTop: 8 }}>
          <button className="btn secondary" id="vouch-no" onClick={() => setAnswering("no")}>
            Reply NO
          </button>
          <button className="btn" id="vouch-yes" onClick={() => setAnswering("yes")}>
            Reply YES
          </button>
        </div>
      )}
    </div>
  );
}

/* w4_2 — two of three, and the old key dies. */
export function VouchProgress() {
  const s = useSession();
  const { id = "" } = useParams();
  const location = useLocation();
  const routeState = location.state as RecoveryRouteState | null;
  const initial = routeState?.viewer === s.me && routeState?.recovery?.id === id ? routeState.recovery : undefined;
  const r = useLoad<Recovery | null>(
    () => initial ? Promise.resolve(initial) : api.get("parties", `/v1/recoveries/${encodeURIComponent(id)}`).catch(() => null),
    [id, initial],
  );
  const name = usePartyName(r?.partyId);
  if (r === undefined) return null;
  if (!r) return <OpenNote>That recovery could not be read.</OpenNote>;
  const nConf = (r.confirmations || []).length;
  const distinct = new Set((r.confirmations || []).map((c) => c.authorityPartyId)).size;
  const done = r.state === "CONFIRMED" || r.state === "COMPLETED" || !!r.completedAt;
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">CREST · {when(r.createdAt)}</span>
      <h2 className="scr-title m">{done ? "Thank you — the record is theirs again" : "Thank you — your YES is recorded"}</h2>
      {done ? (
        <p className="body-2">
          {name || "…"} has recovered their record. Recovery ends in a NEW identity binding appended to their record —
          the old bindings stay for audit, and the old device no longer speaks for them. Everything already earned is
          unchanged.
        </p>
      ) : (
        <p className="body-2">
          Your reply alone does not complete recovery: two voices, each backed by a different authority, must agree.
          The record below is live.
        </p>
      )}
      <KV
        rows={[
          [
            "Confirmations",
            <>
              {distinct} of 2 needed{nConf !== distinct ? ` (${nConf} replies, distinct authorities counted)` : ""}{" "}
              <Chip sm kind={done ? "ok" : "warn"}>
                {r.state}
              </Chip>
            </>,
          ],
          ["New key", done ? "issued as an appended identity binding of class “recovery”" : "not yet — waits for the second voice"],
          [
            "Old key",
            done
              ? "no longer speaks for the record; the old bindings stay on it for audit"
              : "still the record's only voice until recovery completes",
          ],
          ["Credential history", "unchanged either way — a recovery never rewrites what was earned"],
        ]}
      />
      <Sidecar>
        A recovered worker re-enters at IA-1 — vouching is community knowledge, not a national identity check — and
        upgrades the moment they re-anchor, with nothing rewritten. Derived, never stored.
      </Sidecar>
      <OpenNote>
        The reference says the third nominee is told the recovery completed without them, so silence is never mistaken
        for refusal. This deployment sends no messages (#150) — the record above is where they would learn it today.
      </OpenNote>
      <div className="btn-row">
        <Link className="btn" to="/vouch" id="vouch-done">
          Done
        </Link>
      </div>
    </div>
  );
}

/* w4_3 — a refusal, and the path this deployment defines after it. */
export function VouchRefused() {
  const { id = "" } = useParams();
  const s = useSession();
  const location = useLocation();
  const routeState = location.state as RecoveryRouteState | null;
  const initial = routeState?.viewer === s.me && routeState?.recovery?.id === id ? routeState.recovery : undefined;
  const r = useLoad<Recovery | null>(
    () => initial ? Promise.resolve(initial) : api.get("parties", `/v1/recoveries/${encodeURIComponent(id)}`).catch(() => null),
    [id, initial],
  );
  const opener = usePartyName(r?.openedByPartyId);
  if (r === undefined) return null;
  if (!r) return <OpenNote>That recovery could not be read.</OpenNote>;
  const mine = (r.refusals || []).find((f) => f.refuserPartyId === s.me) || (r.refusals || [])[0];
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <Chip sm kind="warn">
        Unresolved in the reference
      </Chip>
      <h2 className="scr-title m">What if two never agree?</h2>
      <p className="body-2">
        The reference design leaves this frame as open questions: the worker holds credentials they can no longer add
        to, and cannot prove they are the owner. Is there an institutional override? Who holds it, and how is it
        audited? Can the worker re-nominate before recovery? “This is an exclusion risk, not an edge case. Nominees
        move, change numbers and die.”
      </p>
      <Callout kind="teal" title="What this deployment does with your NO">
        Your refusal is a permanent record with your name and your reason on it. The recovery stays{" "}
        <b>{r.state}</b> — one NO never closes it, and any two vouched voices from different authorities may still
        carry it. It now sits on the refused-attention queue, where the person who opened it owes the next step.
      </Callout>
      {mine ? (
        <KV
          rows={[
            ["Refused by", <span className="mono">{short(mine.refuserPartyId)}</span>],
            ["Reason", mine.reason],
            ["Recorded", when(mine.refusedAt)],
            ["Recovery state", <Chip sm kind="warn">{r.state}</Chip>],
            [
              "Who owns the next step",
              <>
                {opener || "…"} — <span className="mono">{short(r.openedByPartyId)}</span>, who opened this recovery
              </>,
            ],
          ]}
        />
      ) : null}
      <Sidecar>
        The override the reference asks about exists and is operator-only, never the worker's own supervisor, and is
        refused without a reason, an owner and a review-by date (Blueprint §16). It is audited on the recovery record
        itself.
      </Sidecar>
      <div className="btn-row">
        <Link className="btn secondary" to="/vouch" id="vouch-back">
          Back
        </Link>
      </div>
    </div>
  );
}
