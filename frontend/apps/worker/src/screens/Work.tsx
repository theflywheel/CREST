import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Chip, DisLi, KV, NextBlock, OpenNote, Sidecar } from "@crest/ui";
import { api } from "@crest/api";
import { useSession } from "../session";
import { useLoad } from "../App";
import { loadFace, loadWindows, money, short, when } from "../data";

/* w1_9 + w1_10 — the work page: definition on the left, the say on the right */
export function Work() {
  const s = useSession();
  const [bump, setBump] = useState(0);
  const data = useLoad(async () => {
    const [fr, wins] = await Promise.all([loadFace(), loadWindows(s.me!)]);
    return { ...fr, wins, bump };
  });
  if (!data) return null;
  const f = data.face;
  const rate = data.rate;
  const wkr = (f && f.worker) || {};
  const open = data.wins.filter((w) => !w.exitRoute);
  const closed = data.wins.filter((w) => w.exitRoute);

  const confirm = async (claimId: string) => {
    try {
      await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/confirm`, { route: "self" });
      s.setFlash({
        route: "work",
        node: (
          <NextBlock
            happened={
              <>
                You confirmed the record <b className="mono">{short(claimId)}</b>. The window is closed; your payment
                is released and the signed credential is being issued.
              </>
            }
            who="Nobody — it is yours now. The credential lands in your Wallet on its own."
            when="The payment instruction exists already; the money moves as fast as the rail does — usually today."
            told="Notifications are switched off in this deployment (#150) — the record updates here, under Work and My money."
            ifnot="If the money has not arrived in 3 days, open My money — the payment will be there with a reason and a named owner, and your project support agent can chase it."
          />
        ),
      });
      setBump((b) => b + 1);
    } catch (e) {
      s.fail(e);
    }
  };

  return (
    <div className="pane-cols">
      <div>
        <h2 className="scr-title m">Waiting for your say</h2>
        {s.flash && s.flash.route === "work" ? s.flash.node : null}
        {open.length ? (
          open.map((w) => (
            <div className="card" key={w.claimId}>
              <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
                <span style={{ font: "500 14px/1.4 Roboto" }}>A record of your work</span>
                <Chip kind="warn" sm>
                  reply by {when(w.closesAt)}
                </Chip>
              </div>
              <div className="mono" style={{ margin: "4px 0 2px", color: "var(--text-2)" }}>
                {short(w.claimId)}
              </div>
              <p className="body-2">
                Confirm it, tell us what does not match, or let seven days pass. <b>You are paid on every one of those
                paths</b> — a dispute contests the record, never the money.
              </p>
              <div style={{ height: 8 }} />
              <div className="btn-row">
                <button className="btn dominant" data-confirm={w.claimId} onClick={() => confirm(w.claimId)}>
                  It is right
                </button>
                <Link className="btn secondary" to={`/work/dispute/${encodeURIComponent(w.claimId)}`}>
                  Tell us what does not match
                </Link>
              </div>
            </div>
          ))
        ) : (
          <div className="card quiet">
            <p className="body-2">
              Nothing is waiting for you. When new work is recorded about you, it appears here before it counts.
            </p>
          </div>
        )}
        {closed.length ? (
          <div className="card">
            <span className="eyebrow">Settled</span>
            <div style={{ height: 8 }} />
            <KV
              rows={closed.map((w) => [
                <span className="mono">{short(w.claimId)}</span>,
                <>
                  <Chip sm kind={w.exitRoute === "dispute" ? "warn" : "ok"}>
                    {w.exitRoute}
                  </Chip>{" "}
                  {w.paymentReleasedAt ? (
                    <Chip sm kind="ok">
                      paid
                    </Chip>
                  ) : null}
                </>,
              ])}
            />
            <p className="muted" style={{ marginTop: 8 }}>
              Every exit — confirm, dispute, auto-confirm after seven days, or a supervisor confirming with you —
              releases the payment. All four.
            </p>
          </div>
        ) : null}
        <p className="muted">
          <Link to="/work/declined">Work you declined</Link> — what saying no looks like here.
        </p>
      </div>
      <div>
        <h2 className="scr-title m">What counts as done</h2>
        {f ? (
          <>
            <div className="card">
              <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "flex-start" }}>
                <div style={{ font: "500 15px/1.4 Roboto" }}>{(f.activity && f.activity.label) || f.activity || ""}</div>
                <Chip kind="info" sm>
                  v{f.version} · {f.state || ""}
                </Chip>
              </div>
              <p className="body-2" style={{ marginTop: 4 }}>
                {wkr.summary || ""}
              </p>
              <div style={{ marginTop: 8 }}>
                <KV
                  rows={[
                    ["Counted in", f.outcomeUnit || ""],
                    [
                      "One unit pays",
                      rate ? money(rate.ratePerOutcomeUnit.amountMinor, rate.ratePerOutcomeUnit.currency) : "no rate attached",
                    ],
                  ]}
                />
              </div>
            </div>
            <div className="card">
              <span className="eyebrow">What stands as evidence</span>
              <div style={{ height: 8 }} />
              <div className="dis">
                {(wkr.evidenceInPlainLanguage || []).length ? (
                  (wkr.evidenceInPlainLanguage as string[]).map((l) => <DisLi key={l} on t={l} s="counts as evidence" />)
                ) : (
                  <p className="muted">The definition carries no worker-language evidence list.</p>
                )}
              </div>
              <p className="muted" style={{ marginTop: 10 }}>
                Nothing on this page asks you to enter work into CREST — that path does not exist, and will not be
                built. Evidence arrives from the programme's systems; your part is the say you get before it counts.
              </p>
            </div>
          </>
        ) : (
          <OpenNote>The definitions service did not answer; the definition cannot be shown.</OpenNote>
        )}
      </div>
    </div>
  );
}

/* w1_11 — dispute */
export function Dispute() {
  const { claimId = "" } = useParams();
  const s = useSession();
  const nav = useNavigate();
  const [reason, setReason] = useState("");
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Tell us what does not match</h2>
      <p className="body-2">
        You are disputing the <i>record</i>, not your payment. <b>Your payment is released either way</b> — CREST holds
        that as a rule, not a courtesy.
      </p>
      <KV rows={[["Record", <span className="mono">{short(claimId)}</span>]]} />
      <form
        id="dispute-form"
        style={{ display: "flex", flexDirection: "column", gap: 10 }}
        onSubmit={async (ev) => {
          ev.preventDefault();
          const r = reason.trim();
          if (!r) return;
          try {
            await api.post("confirmation", `/v1/claims/${encodeURIComponent(claimId)}/dispute`, {
              raisedByPartyId: s.me,
              reason: r,
            });
            s.setFlash({
              route: "work",
              node: (
                <NextBlock
                  happened={
                    <>
                      Your dispute on <b className="mono">{short(claimId)}</b> is on the record — and{" "}
                      <b>your payment is released anyway</b>. A dispute contests the record, never the money.
                    </>
                  }
                  who="The issuer of the record — the programme that submitted it — must answer your dispute."
                  when="The contest is visible to any verifier from this moment until the issuer answers."
                  told="Notifications are switched off in this deployment (#150) — check back here for the issuer's answer."
                  ifnot="If nobody has answered in 14 days, tell your project support agent — an unanswered dispute is the issuer's failure, never yours, and it stays visible until resolved."
                />
              ),
            });
            nav("/work");
          } catch (e) {
            s.fail(e);
          }
        }}
      >
        <textarea
          name="reason"
          rows={4}
          required
          placeholder="What is wrong? The count, the day, the place — say it in your own words."
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <div className="btn-row">
          <button className="btn" type="submit">
            Send the dispute
          </button>
          <Link className="btn secondary" to="/work">
            Go back
          </Link>
        </div>
      </form>
      <Sidecar>
        A disputed record is never destroyed. Your dispute sits beside it, visible to anyone who checks, until the
        issuer answers.
      </Sidecar>
    </div>
  );
}

/* w1_14 — declined work (no backend) */
export function Declined() {
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <h2 className="scr-title m">Work you declined</h2>
      <OpenNote>
        <b>Illustrative — no L1 endpoint serves this yet.</b> The journeys (w1_14) show declined offers kept on your
        side only, never on your record. The services expose no offers or declines API today; when one lands it
        belongs to the definitions surface. Nothing is drawn here because nothing real exists to draw.
      </OpenNote>
      <p className="body-2">
        The promise this screen will keep: declining work is not recorded about you. A verifier can never see what you
        said no to.
      </p>
    </div>
  );
}
