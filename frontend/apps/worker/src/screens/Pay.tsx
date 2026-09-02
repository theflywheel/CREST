import { Link, useParams } from "react-router-dom";
import { Chip, KV, Sidecar } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { daysOld, loadInstr, money, monthOf, short, when, whenFull } from "../data";

function HeldCard({ i }: { i: any }) {
  const age = daysOld(i.heldAt || i.createdAt);
  return (
    <div className="held">
      <div className="top">
        <span className="amt">{money(i.amountMinor, i.currency)}</span>
        <Chip sm kind={age >= 7 ? "err" : "warn"}>
          {age} day{age === 1 ? "" : "s"}
        </Chip>
      </div>
      <div className="why">{i.held.explanation || i.held.code || "Held — and the record carries why."}</div>
      <div className="who">Waiting on: {short(i.held.ownerPartyId || "") || "a named owner"}</div>
    </div>
  );
}

/* w1_13 / w1_23 / w1_24 — payments */
export function Pay() {
  const s = useSession();
  const list = useLoad(() => loadInstr(s.me!));
  if (!list) return null;
  const held = list.filter((i) => i.held);
  const flowing = list.filter((i) => !i.held);
  const groups: Record<string, any[]> = {};
  for (const i of flowing) (groups[monthOf(i.releasedAt || i.createdAt)] ||= []).push(i);
  return (
    <div className="pane-cols">
      <div>
        <h2 className="scr-title m">My money</h2>
        {s.flash && s.flash.route === "pay" ? s.flash.node : null}
        {!list.length ? (
          <div className="card quiet">
            <p className="body-2">
              No payments yet. A payment instruction is created the moment your confirmation window closes — on any of
              its four exits.
            </p>
          </div>
        ) : null}
        {Object.entries(groups).map(([m, items]) => (
          <div key={m} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <span className="eyebrow">{m}</span>
            {items.map((i) => {
              const idx = list.indexOf(i);
              return (
                <Link className="card" to={`/pay/${idx}`} key={i.id || idx} style={{ textDecoration: "none" }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 10 }}>
                    <span style={{ font: "500 15px/1.3 Roboto", color: "var(--text-1)" }}>
                      {money(i.amountMinor, i.currency)}
                    </span>
                    <Chip sm kind={i.state === "RELEASED" ? "ok" : "info"}>
                      {i.state || ""}
                    </Chip>
                  </div>
                  <div className="muted">
                    {i.releasedBy ? "via " + i.releasedBy : ""} · {when(i.releasedAt || i.createdAt)}
                  </div>
                </Link>
              );
            })}
          </div>
        ))}
        {list.length ? (
          <Sidecar>
            Every held payment carries a reason and a named owner — the record itself refuses one without both.
          </Sidecar>
        ) : null}
      </div>
      <div>
        {held.length ? (
          <>
            <h2 className="scr-title m">What is waiting</h2>
            {held.map((i, k) => (
              <HeldCard i={i} key={k} />
            ))}
            <Sidecar ok>You do not need to do anything about these — your project support agent can.</Sidecar>
            <Sidecar>
              Nothing here is a mark against you. Being held is a problem between two offices, not a judgement about
              your work.
            </Sidecar>
          </>
        ) : null}
      </div>
    </div>
  );
}

/* payment detail — w1_13 */
export function PayDetail() {
  const { idx = "" } = useParams();
  const s = useSession();
  const list = useLoad(() => loadInstr(s.me!));
  if (!list) return null;
  const i = list[Number(idx)];
  if (!i)
    return (
      <div className="card quiet">
        <p className="body-2">
          That payment is not on your record. <Link to="/pay">Back to My money.</Link>
        </p>
      </div>
    );
  const released = i.state === "RELEASED";
  return (
    <div className="pane-narrow" style={{ display: "flex", flexDirection: "column", gap: 15 }}>
      <span className="eyebrow">{monthOf(i.releasedAt || i.createdAt)}</span>
      <div className="big-amount">{money(i.amountMinor, i.currency)}</div>
      <span style={{ alignSelf: "flex-start" }}>
        <Chip kind={released ? "ok" : i.held ? "warn" : "info"}>{i.state || ""}</Chip>
      </span>
      <div className="tline" style={{ marginTop: 6 }}>
        <div className="step">
          <div className="rail">
            <div className="dot" />
            <div className="conn" />
          </div>
          <div>
            <div className="lbl">Credential signed</div>
            <div className="meta">Your confirmation window closed; the record became a signed credential.</div>
          </div>
        </div>
        <div className={"step" + (released ? "" : " active")}>
          <div className="rail">
            <div className="dot" />
            <div className="conn" />
          </div>
          <div>
            <div className="lbl">Amount calculated</div>
            <div className="meta">
              {money(i.amountMinor, i.currency)} — from the rate attached to the definition, versioned, never
              overwritten.
            </div>
          </div>
        </div>
        <div className={"step" + (released ? "" : " todo")}>
          <div className="rail">
            <div className="dot" />
          </div>
          <div>
            <div className="lbl">Sent to rail</div>
            <div className="meta">
              {released
                ? (i.releasedBy ? "Via " + i.releasedBy + " · " : "") + whenFull(i.releasedAt)
                : i.held
                  ? "Held — see the reason below."
                  : "Not yet sent."}
            </div>
          </div>
        </div>
      </div>
      {i.held ? (
        <div className="held">
          <div className="why">{i.held.explanation || i.held.code || ""}</div>
          <div className="who">Waiting on: {short(i.held.ownerPartyId || "") || "a named owner"}</div>
        </div>
      ) : null}
      <KV
        rows={[
          ["Instruction", <span className="mono">{short(i.id)}</span>],
          ["For claim", <span className="mono">{short(i.claimId || "—")}</span>],
        ]}
      />
      <Sidecar>
        CREST did not move this money — it told M-Pesa to. What you see here is the instruction and its trace, which is
        exactly what a delayed payment needs you to have.
      </Sidecar>
    </div>
  );
}
