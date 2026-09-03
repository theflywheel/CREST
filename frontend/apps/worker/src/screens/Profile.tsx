import { useState } from "react";
import { Link } from "react-router-dom";
import { Chip, KV, NextBlock, OpenNote, Sidecar } from "@crest/ui";
import { api } from "@crest/api";
import { useSession } from "../session";
import { useLoad } from "../App";
import { short, when } from "../data";
import { Ussd } from "./Login";

export function Profile() {
  const s = useSession();
  const rows: Array<[string, string, string]> = [
    ["/profile/consents", "What I agreed to", "Consent, per programme — and withdrawing it"],
    ["/profile/checks", "Who checked me", "Every look at your record leaves a line"],
    ["/profile/messages", "Messages to me", "Everything the system ever sent you, kept"],
    ["/profile/recovery", "If I lose this phone", "The people who can confirm it is you"],
    ["/shares", "Requests to see my record", "Who is asking, and why — you decide, per share"],
    ["/vouch", "Vouch for someone", "People who named you a recovery contact"],
    ["/added", "My identity anchor", "What an anchor changes — derived, never stored"],
  ];
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Profile</h2>
      <div className="person-name">Grace</div>
      <div className="mono" style={{ color: "var(--text-2)" }}>
        {s.me}
      </div>
      {rows.map(([go, t, sub]) => (
        <Link className="card" to={go} key={go} style={{ textDecoration: "none" }}>
          <div style={{ font: "500 14px/1.4 Roboto", color: "var(--text-1)" }}>{t}</div>
          <div className="muted">{sub}</div>
        </Link>
      ))}
      <Ussd />
    </div>
  );
}

export function Consents() {
  const s = useSession();
  const [bump, setBump] = useState(0);
  const out = useLoad(async () => {
    void bump;
    return api.get("parties", `/v1/parties/${encodeURIComponent(s.me!)}/consents`).catch(() => null);
  });
  if (out === undefined) return null;
  const list: any[] = (out && out.consents) || [];
  const states: Record<string, string> = (out && out.enrolmentConsent) || {};

  const withdraw = async (id: string) => {
    try {
      await api.post("parties", `/v1/consents/${encodeURIComponent(id)}/withdraw`, {
        reason: "withdrawn by the worker",
      });
      s.setFlash({
        route: "consents",
        node: (
          <NextBlock
            happened={
              <>
                Your consent <b className="mono">{short(id)}</b> is withdrawn. No new evidence about you will be
                accepted under it.
              </>
            }
            who="Nobody — it is yours now. The programme is told; nothing is asked of you."
            when="Immediately. The withdrawal is on the record from this moment."
            told="The state above changes now — notifications are switched off in this deployment (#150)."
            ifnot="If new work still appears on your record under this programme, that is a breach — tell your project support agent, and the record of this withdrawal is your proof."
          />
        ),
      });
      setBump((b) => b + 1);
    } catch (e) {
      s.fail(e);
    }
  };

  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">What I agreed to</h2>
      <p className="body-2">
        Consent is per programme. Withdrawing it stops new evidence being collected about you — it never touches what
        you were already paid.
      </p>
      {Object.keys(states).length ? (
        <KV
          rows={Object.entries(states).map(([ctx, st]) => [
            <span className="mono">{short(ctx)}</span>,
            <Chip sm kind={st === "GRANTED" ? "ok" : "warn"}>
              {st}
            </Chip>,
          ])}
        />
      ) : null}
      {list.length ? (
        list.map((c) => (
          <div className="card" key={c.id}>
            <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
              <span style={{ font: "500 13.5px/1.4 Roboto" }}>{c.moment || c.kind || "consent"}</span>
              <Chip sm kind={c.state === "GRANTED" ? "ok" : "warn"}>
                {c.state || ""}
              </Chip>
            </div>
            <div className="muted">
              {c.captureMethod || ""} · {when(c.capturedAt || c.createdAt)}
            </div>
            {c.state === "GRANTED" ? (
              <>
                <div style={{ height: 8 }} />
                <button className="btn secondary" onClick={() => withdraw(c.id)}>
                  Withdraw this consent
                </button>
              </>
            ) : null}
          </div>
        ))
      ) : !out ? (
        <OpenNote>The parties service did not answer; your consents cannot be shown right now.</OpenNote>
      ) : (
        <div className="card quiet">
          <p className="body-2">
            No consent record is stored about you. Nothing here means nothing was captured — ask your registering
            agent if that seems wrong.
          </p>
        </div>
      )}
      {s.flash && s.flash.route === "consents" ? s.flash.node : null}
      <div className="consent-quote">
        "CREST keeps a record of work you do, so you can prove it and be paid for it. It never stores your ID number.
        You can say stop at any time, and stopping never takes back money you were paid."
        <br />
        <span className="muted">— the script read to you when you enrolled</span>
      </div>
    </div>
  );
}

export function Checks() {
  const s = useSession();
  const out = useLoad(() =>
    api.get("verification", `/v1/presentations?subjectRef=${encodeURIComponent(s.me!)}`).catch(() => null),
  );
  if (out === undefined) return null;
  const list: any[] = (out && out.presentations) || [];
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Who checked me</h2>
      <p className="body-2">
        Every look at your record leaves a line here — one per credential, even inside a batch, and even when the check
        failed.
      </p>
      {list.length ? (
        <KV
          rows={list.map((p) => [
            when(p.createdAt),
            <>
              {short(p.requestedByPartyId) || "(bare scan)"} · {p.purpose || "no purpose given"} ·{" "}
              <Chip sm kind={p.outcome === "valid" ? "ok" : "plain"}>
                {p.outcome || ""}
              </Chip>
            </>,
          ])}
        />
      ) : !out ? (
        <OpenNote>The verification service did not answer; the trail cannot be shown right now.</OpenNote>
      ) : (
        <div className="card quiet">
          <p className="body-2">
            Nobody has checked your record. When someone does — a scan, a batch check, anything — the line appears
            here whether or not they say who they are.
          </p>
        </div>
      )}
    </div>
  );
}

export function Messages() {
  // Notifications are dropped (#150): no service sends messages, so this
  // screen says so instead of showing an inbox that can never fill.
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Messages to me</h2>
      <OpenNote>
        <b>Notifications are switched off in this deployment (#150).</b> Nothing sends you a message when a window
        opens or a payment is held — you learn by opening this app. The record itself is unaffected; the gap is the
        telling.
      </OpenNote>
    </div>
  );
}

/* w1_7 — "Who can confirm it is you?": recovery-contact nomination.
   Party-linked picks, never phone numbers — a number is re-issued and lost
   with the handset, the exact failure recovery exists for. Nomination is
   ROUTING: it decides who is asked, and never widens who may confirm (that
   stays the 2-of-3 rule with its distinct-authority constraint). */
export function Recovery() {
  const s = useSession();
  const [bump, setBump] = useState(0);
  const [pick, setPick] = useState("");
  const out = useLoad(
    () => api.get("parties", `/v1/parties/${encodeURIComponent(s.me!)}/recovery-contacts`).catch(() => null),
    [bump],
  );
  if (out === undefined) return null;
  const contacts: any[] = (out && out.contacts) || [];
  const live = contacts.filter((c) => !c.revokedAt);
  const nominate = async (contactPartyId: string) => {
    try {
      await api.post("parties", `/v1/parties/${encodeURIComponent(s.me!)}/recovery-contacts`, { contactPartyId });
      setPick("");
      setBump((b) => b + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  const revoke = async (contactPartyId: string) => {
    try {
      await api.post(
        "parties",
        `/v1/parties/${encodeURIComponent(s.me!)}/recovery-contacts/${encodeURIComponent(contactPartyId)}/revoke`,
      );
      setBump((b) => b + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Who can confirm it is you?</h2>
      <p className="body-2">
        Choose three people. If you lose your phone, any two of them can vouch for you and a new key is issued to you.
        A contact is a person on the registry — never a phone number, because numbers are lost with the handset.
      </p>
      {live.map((c) => (
        <ContactRow key={c.contactPartyId} c={c} onRevoke={() => revoke(c.contactPartyId)} />
      ))}
      {live.length < 3 ? (
        <div className="card quiet" data-slot="open">
          <span className="eyebrow">
            {live.length === 2 ? "Add a third contact · Recommended" : `Add a contact (${live.length} of 3 named)`}
          </span>
          <form
            style={{ display: "flex", gap: 8, marginTop: 8 }}
            onSubmit={(ev) => {
              ev.preventDefault();
              if (pick.trim()) nominate(pick.trim());
            }}
          >
            <input
              name="contactpartyid"
              placeholder="did:crest:party:… — the person's registry id"
              value={pick}
              onChange={(e) => setPick(e.target.value)}
              style={{ flex: 1 }}
            />
            <button className="btn" id="nominate" type="submit">
              Nominate
            </button>
          </form>
          <p className="muted" style={{ marginTop: 6 }}>
            In a pilot you would pick from people you know on this programme; on this stack, paste their party id.
          </p>
        </div>
      ) : null}
      <Sidecar>
        These contacts can confirm your identity. They can never see your work history or your payments.
      </Sidecar>
      <Sidecar ok>
        Naming someone routes the request to them — it never by itself makes their voice count. A confirmation counts
        only with an authority standing behind it, and two must come from different authorities.
      </Sidecar>
      {contacts.some((c) => c.revokedAt) ? (
        <div className="card quiet">
          <span className="eyebrow">No longer nominated</span>
          {contacts
            .filter((c) => c.revokedAt)
            .map((c) => (
              <div className="muted" key={c.contactPartyId + c.revokedAt}>
                <span className="mono">{short(c.contactPartyId)}</span> · revoked {when(c.revokedAt)} — the row stays:
                who you trusted, and when you stopped, is what a later dispute is answered from.
              </div>
            ))}
        </div>
      ) : null}
      <div className="btn-row">
        <Link className="btn" to="/home" id="recovery-finish">
          Finish
        </Link>
      </div>
    </div>
  );
}

function ContactRow(props: { c: any; onRevoke: () => void }) {
  const party = useLoad(() =>
    api.get("parties", `/v1/parties/${encodeURIComponent(props.c.contactPartyId)}`).catch(() => null),
  );
  const name = party === undefined ? "…" : (party && party.displayName) || short(props.c.contactPartyId);
  return (
    <div className="card" data-contact={props.c.contactPartyId}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "center" }}>
        <span>
          <div style={{ font: "500 14px/1.4 Roboto" }}>{name}</div>
          <div className="muted">
            nominated {when(props.c.nominatedAt)} · <span className="mono">{short(props.c.contactPartyId)}</span>
          </div>
        </span>
        <button className="btn secondary" data-revoke={props.c.contactPartyId} onClick={props.onRevoke}>
          Revoke
        </button>
      </div>
    </div>
  );
}
