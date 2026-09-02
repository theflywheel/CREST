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

/* w1_7 — recovery contacts */
export function Recovery() {
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">If I lose this phone</h2>
      <p className="body-2">
        You named people when you enrolled — people who can confirm it is you, so a lost phone never means a lost
        record.
      </p>
      <OpenNote>
        <b>The nomination endpoint is not yet public.</b> The parties service runs recoveries (a custodian opens one, a
        nominated contact confirms), but exposes no read API for a worker's own nominated contacts — so this screen
        will not pretend to know who yours are. When the endpoint lands, they appear here by name.
      </OpenNote>
      <Sidecar ok>
        Losing the phone loses nothing. Your credentials are re-issued to you after recovery; your work record was
        never only on the phone.
      </Sidecar>
    </div>
  );
}
