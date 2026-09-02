// J3's second half — the Project Configurator's dashboard wave, p2_11–p2_16.
// These are the reference's own frames (Work status · Quality · Payments ·
// Proof · Reports), with the reference's titles, ledes and callouts verbatim
// and every number read from a running service.
//
// Where the reference shows a metric CREST has no contract for — the
// straight-through headline, the project-wide tier mix, the funder's
// allocation — the frame renders in full, the figure reads "—" and the note
// on the frame says which contract is missing. A fabricated 84.4% on a screen
// that decides whether a project looks healthy is the exact failure this
// project cannot afford.
import { useState } from "react";
import { api, FIX } from "@crest/api";
import { Callout, Chip, GridTable, NextBlock, OpenNote } from "@crest/ui";
import {
  agoDays, Card, CardTitled, ILLUSTRATIVE, KVR, Lede, LoadFrame, Mono, MonoShort, money,
  Stat, TStep, Title, Tbl, TierChip, short, useLoad, when,
} from "../ui";
import { useConsole } from "../state";
import { useNavigate } from "react-router-dom";

type Held = { code?: string; explanation?: string; ownerPartyId?: string };
type Instr = {
  partyId?: string; claimId?: string; amountMinor?: number; currency?: string;
  state?: string; releasedBy?: string; releasedAt?: string; createdAt?: string;
  held?: Held; heldReason?: string;
};

const unresolved = (arr?: Array<{ resolvedAt?: string }>) => (arr || []).filter((x) => !x.resolvedAt);
const oldest = (arr: Array<Record<string, string | undefined>>, field: string) => {
  const days = (arr || []).map((x) => agoDays(x[field] || x.createdAt)).filter((d): d is number => d !== null);
  return days.length ? Math.max(...days) : null;
};
const median = (xs: number[]) => (xs.length ? xs.sort((a, b) => a - b)[Math.floor(xs.length / 2)] : null);

async function projectRead() {
  const [claims, unclear, unreleased, unreached, instr, metrics, def] = await Promise.all([
    api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
    api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] })),
    api.get("confirmation", "/v1/unreleased").catch(() => ({ windows: [] })),
    api.get("confirmation", "/v1/unreached").catch(() => ({ windows: [] })),
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    api.get("parties", "/v1/holds/metrics").catch(() => null),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`).catch(() => null),
  ]);
  return {
    claims: (claims.claims || []) as Array<{ state?: string; createdAt?: string }>,
    unclear: unresolved(unclear.unclear) as Array<{ reason?: string; kind?: string; createdAt?: string }>,
    unreleased: (unreleased.windows || unreleased.unreleased || []) as Array<Record<string, string | undefined>>,
    unreached: (unreached.windows || unreached.unreached || []) as Array<Record<string, string | undefined>>,
    instructions: (instr.instructions || []) as Instr[],
    metrics,
    def,
  };
}

const projectTitle = (def: { activity?: { label?: string } } | null) =>
  short(FIX.project) + " · " + (def?.activity?.label || "this project's work");

// ── p2_11 · A funnel, not a set of totals ───────────────────────────────────
export function Status() {
  const nav = useNavigate();
  const r = useLoad(projectRead);
  return (
    <LoadFrame r={r}>
      {(f) => {
        const held = f.instructions.filter((i) => i.state === "HELD");
        const ownerless = held.filter((i) => !(i.held?.ownerPartyId || "").trim());
        const byState = f.claims.reduce<Record<string, number>>((m, c) => {
          const k = c.state || "?";
          m[k] = (m[k] || 0) + 1;
          return m;
        }, {});
        const heldGroups = held.reduce<Record<string, Instr[]>>((m, i) => {
          const k = i.held?.code || i.heldReason || "held";
          (m[k] = m[k] || []).push(i);
          return m;
        }, {});
        const oldestUnclear = oldest(f.unclear, "createdAt");
        return (
          <>
            <Title t={projectTitle(f.def)} />
            <Lede>This month to date · read from the services just now.</Lede>
            <div className="stats">
              <Stat n={f.claims.length} label="Work units received" owner="evidence service" />
              <Stat n={<span data-stat="stp">—</span>} label="Cleared without a person" owner="no straight-through contract exists (#31)" />
              <Stat n={f.unreleased.length} label="Cleared, not yet paid" owner="owner: payments relay" />
              <Stat n={ownerless.length} label="Stuck, nobody holding" owner="must be 0 — a held payment without an owner is a defect" />
            </div>
            <CardTitled t="Where the month sits">
              <KVR rows={Object.entries(byState).map(([state, n]) => [state, String(n) + " work unit(s)"])} />
              <p className="muted" style={{ marginTop: 8 }}>
                The reference's five buckets (cleared automatically · by a person · waiting · sent back · stuck) need
                the exit route on every window; what the evidence service reports is the claim state, so these are the
                real states rather than the reference's names for them.
              </p>
            </CardTitled>
            <CardTitled t="Needs somebody today">
              <Tbl
                heads={["What", "How many", "Oldest", "Who owns it"]}
                rows={[
                  ["Rows nobody could attribute", String(f.unclear.length), oldestUnclear !== null ? oldestUnclear + " days" : "—", "Registry custodian"],
                  ["Open windows nobody could be told about", String(f.unreached.length), (oldest(f.unreached, "opensAt") ?? "—") + " days", "Supervisor — the assisted route"],
                  ...Object.entries(heldGroups).map(([code, items]) => [
                    "Payment held · " + code,
                    String(items.length),
                    (oldest(items as Array<Record<string, string | undefined>>, "createdAt") ?? "—") + " days",
                    items[0].held?.ownerPartyId ? <MonoShort id={items[0].held.ownerPartyId} /> : "nobody — this is a defect",
                  ]),
                ]}
                empty="Nothing needs anybody today."
              />
            </CardTitled>
            <CardTitled t="Handoffs open on this project">
              <Tbl
                heads={["What", "How many", "Who owns it"]}
                rows={[
                  ["Cleared work whose payment is not confirmed released", String(f.unreleased.length), "Payments relay"],
                  ["Merges that happened without confirmation", String(f.metrics ? (f.metrics.mergesWithoutConfirmation ?? f.metrics.merges_without_confirmation ?? 0) : "—"), "Registry custodian — must stay 0"],
                ]}
              />
            </CardTitled>
            <OpenNote>
              Nothing defines when a stuck item becomes an incident, and nothing escalates on its own — the reference
              names that gap on this frame and it is still a gap. The straight-through, tier-mix and time-to-say
              contracts are <a href="https://github.com/theflywheel/CREST/issues/31">#31</a>, unbuilt: this screen
              shows “—” rather than a number it cannot derive.
            </OpenNote>
            <div className="btn-row" style={{ maxWidth: 520 }}>
              <button className="btn secondary" data-act="secondary" onClick={() => nav("/payments")}>
                Payments
              </button>
              <button className="btn dominant" data-act="primary" onClick={() => nav("/stp")}>
                Straight-through
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_12 · The headline, with the causes underneath it ─────────────────────
export function Stp() {
  const nav = useNavigate();
  const r = useLoad(projectRead);
  return (
    <LoadFrame r={r}>
      {(f) => {
        const held = f.instructions.filter((i) => i.state === "HELD");
        // The ranked causes ARE real: every unclear row and every held payment
        // carries a reason, and every reason carries the party who can fix it.
        const causes: Array<{ what: string; owner: string; n: number }> = [];
        const bump = (what: string, owner: string) => {
          const row = causes.find((c) => c.what === what);
          row ? row.n++ : causes.push({ what, owner, n: 1 });
        };
        f.unclear.forEach((u) => bump(u.reason || u.kind || "unattributed row", "Worker Registry Custodian"));
        held.forEach((i) => bump(i.held?.code || i.heldReason || "held", i.held?.ownerPartyId ? short(i.held.ownerPartyId) : "nobody — this is a gap"));
        causes.sort((a, b) => b.n - a.n);
        return (
          <>
            <Title t="Straight-through rate" extra={ILLUSTRATIVE} />
            <div className="stats">
              <Stat n="—" label="Cleared with no human touch" owner="the metric contract is #31, unbuilt" />
              <Stat n={causes.reduce((s, c) => s + c.n, 0)} label="Needed a person, or failed" owner="every one of these has a cause, and every cause has an owner" />
            </div>
            <CardTitled t="Why the others fell out — ranked">
              <GridTable cols="auto 1.8fr 1.2fr auto" head={["#", "Cause", "Fixed by", "Count"]}>
                {causes.map((c, i) => (
                  <div className="g-row" key={c.what}>
                    <span className="mono">{i + 1}</span>
                    <span>{c.what}</span>
                    <span className="muted">Fixed by · {c.owner}</span>
                    <span className="num">{c.n}</span>
                  </div>
                ))}
                {causes.length ? null : (
                  <div className="g-row">
                    <span />
                    <span className="muted">Nothing fell out. The ranking appears the moment something does.</span>
                    <span />
                    <span />
                  </div>
                )}
              </GridTable>
            </CardTitled>
            <Callout kind="grey" title="How this could be gamed">
              <b>Straight-through rate</b> rises if you lower a definition’s tier ceiling — the work gets no better,
              the number does. <span className="guard">Guarded by · showing it beside the tier mix, and flagging any definition whose ceiling moved this period.</span>
            </Callout>
            <OpenNote>
              The headline percentage is the illustrative half of this frame: computing it needs the exit route and the
              tier on every work unit as a project-wide read, which is metric contract{" "}
              <a href="https://github.com/theflywheel/CREST/issues/31">#31</a>. The ranked causes underneath it are
              real — they are the live unclear queue and the live held-payment reasons, each with the party who can fix
              it. No target exists either: nobody has said what rate this project should reach.
            </OpenNote>
            <div className="btn-row" style={{ maxWidth: 640 }}>
              <button className="btn secondary" data-act="secondary" onClick={() => nav("/status")}>
                Work status
              </button>
              <button className="btn secondary" onClick={() => nav("/unclear")}>
                Open cause 1
              </button>
              <button className="btn dominant" data-act="primary" onClick={() => nav("/quality")}>
                Quality
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_13 · Two different problems that look identical in a pie chart ───────
export function Quality() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const [sources, assess, def] = await Promise.all([
      api.get("evidence", "/v1/sources").catch(() => ({ sources: [] })),
      api.get("verification", "/v1/source-assessments").catch(() => ({ assessments: [] })),
      api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`).catch(() => null),
    ]);
    return {
      sources: (sources.sources || []) as Array<{ systemRef?: string; adapterRef?: string; state?: string }>,
      assessments: (assess.assessments || []) as Array<{ adapterRef: string; maxTier: number; reason?: string }>,
      tierMap: (def?.tierMap || []) as Array<{ tier: number; sourceClassIn?: string[]; captureMethodIn?: string[] }>,
    };
  });
  return (
    <LoadFrame r={r}>
      {({ sources, assessments, tierMap }) => (
        <>
          <Title t="Data quality by tier" extra={ILLUSTRATIVE} />
          <Lede>
            Tier is derived by CREST from the source and the capture mode, never asserted by the sender.
          </Lede>
          <div className="stats">
            {[1, 2, 3].map((t) => (
              <Stat key={t} n="—" label={<>Tier {t} share</>} owner="no project-wide credential read exists" />
            ))}
          </div>
          <CardTitled t="Source, and the ceiling it puts on a tier">
            <GridTable cols="1.4fr 1fr 1.6fr" head={["Source", "Ceiling", "Why"]}>
              {sources.map((s) => {
                const cap = assessments.find((a) => a.adapterRef === (s.adapterRef || s.systemRef));
                return (
                  <div className="g-row" key={s.systemRef || s.adapterRef}>
                    <span className="mono">{s.systemRef || s.adapterRef}</span>
                    <span>{cap ? <TierChip t={cap.maxTier} /> : <Chip kind="plain">no assessment</Chip>}</span>
                    <span className="muted">{cap ? cap.reason || "capped by assessment" : "uncapped: the definition's tier map decides"}</span>
                  </div>
                );
              })}
              {sources.length ? null : (
                <div className="g-row">
                  <span className="muted">No source is registered, so nothing is capped by a source yet.</span>
                  <span />
                  <span />
                </div>
              )}
            </GridTable>
          </CardTitled>
          <CardTitled t="Capped by the source, or fell short on evidence">
            <KVR
              rows={[
                ["Capped by the source", "No amount of better field practice raises these. Connect the source, or accept the ceiling."],
                ["Fell short on evidence", "These could have been Tier 1. Practice, not plumbing."],
              ]}
            />
            <div style={{ height: 8 }} />
            <KVR
              rows={tierMap.map((rule) => [
                "Tier " + rule.tier,
                (rule.sourceClassIn || []).join(" / ") + " · captured by " + (rule.captureMethodIn || []).join(" / "),
              ])}
            />
          </CardTitled>
          <Callout kind="grey" title="How this could be gamed">
            <b>Tier mix</b> improves if low-tier work is simply moved out of the definition, or the ceiling lowered.{" "}
            <span className="guard">Guarded by · reading it beside straight-through rate and the ceiling-changed flag.</span>
          </Callout>
          <Callout kind="green" title="Why the split matters">
            These need opposite fixes. A record capped by its source will never improve until the source is integrated,
            however good field practice becomes. A record that fell short is a practice problem. Reporting them as one
            number sends a manager to train people who were already doing it right.
          </Callout>
          <OpenNote>
            The tier-share figures read “—” on purpose: credentials are listable per worker only
            (<span className="mono">/v1/credentials?partyId=</span>), by design, so a project-wide tier mix needs the
            metric contract of <a href="https://github.com/theflywheel/CREST/issues/31">#31</a>. Everything else on
            this frame is live: the registered sources, their assessed ceilings, and the definition's own tier map.
          </OpenNote>
          <div className="btn-row" style={{ maxWidth: 640 }}>
            <button className="btn secondary" data-act="secondary" onClick={() => nav("/stp")}>
              Straight-through
            </button>
            <button className="btn secondary" onClick={() => nav("/sources")}>
              Connect the source
            </button>
            <button className="btn dominant" data-act="primary" onClick={() => nav("/payments")}>
              Payments
            </button>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// ── p2_14 · Money delayed and people waiting are different sentences ────────
export function Payments() {
  const nav = useNavigate();
  const s = useConsole();
  const r = useLoad(async () => {
    const [out, rec] = await Promise.all([
      api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
      api.get("payments", "/v1/reconciliation").catch(() => null),
    ]);
    return { list: (out.instructions || []) as Instr[], rec };
  });
  const trace = (claimId?: string) => {
    s.setTraceClaim(claimId || "");
    nav(s.persona === "custodian" || s.persona === "support" ? "/supporttrace" : "/trace");
  };
  return (
    <LoadFrame r={r}>
      {({ list, rec }) => {
        const held = list.filter((i) => i.state === "HELD");
        const paid = list.filter((i) => i.state === "SETTLED");
        const released = list.filter((i) => i.state === "RELEASED");
        const failed = list.filter((i) => i.state === "FAILED" || i.state === "REJECTED");
        const cur = (list[0] || {}).currency || "";
        const sum = (xs: Instr[]) => xs.reduce((t, i) => t + (i.amountMinor || 0), 0);
        const groups = held.reduce<Record<string, Instr[]>>((m, i) => {
          const k = i.held?.code || i.heldReason || "held";
          (m[k] = m[k] || []).push(i);
          return m;
        }, {});
        const oldestUnpaid = oldest(
          [...held, ...released] as Array<Record<string, string | undefined>>, "createdAt");
        return (
          <>
            <Title t="Payments, and where the delay is" />
            <Lede>
              {money(sum(list), cur)} instructed against {list.length} cleared record(s).
            </Lede>
            <div className="stats">
              <Stat n={money(sum(paid), cur)} label={"confirmed paid · " + paid.length + " worker(s)"} />
              <Stat n={money(sum(released), cur)} label={"instructed, not confirmed · " + released.length + " worker(s)"} />
              <Stat n={money(sum(failed), cur)} label={"failed at the rail · " + failed.length + " worker(s)"} />
              <Stat n={oldestUnpaid !== null ? oldestUnpaid + " d" : "—"} label="oldest unpaid" />
            </div>
            <CardTitled t="Why it is delayed">
              <GridTable cols="1.8fr auto 1fr auto 1.2fr" head={["Reason", "Workers", "Amount", "Median age", "Who owns it"]}>
                {Object.entries(groups).map(([code, items]) => (
                  <div className="g-row" key={code}>
                    <span>{items[0].held?.explanation || code}</span>
                    <span className="num">{items.length}</span>
                    <span className="num">{money(sum(items), items[0].currency)}</span>
                    <span className="num">
                      {(median(items.map((i) => agoDays(i.createdAt) ?? 0)) ?? "—") + " d"}
                    </span>
                    <span>
                      {items[0].held?.ownerPartyId ? (
                        <MonoShort id={items[0].held.ownerPartyId} />
                      ) : (
                        "nobody — this is a gap"
                      )}
                    </span>
                  </div>
                ))}
                {Object.keys(groups).length ? null : (
                  <div className="g-row">
                    <span className="muted">
                      No payment is held. A delay row appears the moment one is — with a reason and an owner, because
                      the record refuses one without.
                    </span>
                    <span />
                    <span />
                    <span />
                    <span />
                  </div>
                )}
              </GridTable>
            </CardTitled>
            <Callout kind="grey" title="How this could be gamed">
              <b>Confirmed-paid percentage</b> looks better if slow records are excluded from the period rather than
              chased. <span className="guard">Guarded by · ageing buckets and an oldest-unpaid figure that cannot be netted away.</span>
            </Callout>
            <Callout kind="green" title="Every held payment has a reason with an owner">
              A worker must never see a missing payment with no explanation attached. Each row above names the office
              that owes the next move; a row that could not name one would itself be the defect to raise.
            </Callout>
            <Tbl
              heads={["Worker", "Amount", "State", "Released by", "When", "If held: why — owner", ""]}
              rows={list.slice(0, 100).map((i) => [
                <MonoShort id={i.partyId} />,
                money(i.amountMinor || 0, i.currency),
                <Chip kind={i.state === "RELEASED" || i.state === "SETTLED" ? "ok" : "warn"}>{i.state}</Chip>,
                i.releasedBy || "—",
                when(i.releasedAt),
                i.held ? (
                  <>
                    {i.held.explanation || i.held.code} — <MonoShort id={i.held.ownerPartyId || ""} />
                  </>
                ) : (
                  "—"
                ),
                <button className="btn secondary" style={{ padding: "7px 12px", width: "auto" }} onClick={() => trace(i.claimId)}>
                  Trace
                </button>,
              ])}
              empty="No payment has been instructed yet. Instructions appear the moment a confirmation window exits — all four exits release one, a dispute included: a dispute contests the record, never the money."
            />
            {rec ? (
              <KVR rows={[["reconciliation gaps", String((rec.gaps || []).length)]]} />
            ) : (
              <OpenNote>The reconciliation endpoint did not answer, so no gap count is shown rather than a zero.</OpenNote>
            )}
            <div className="btn-row" style={{ maxWidth: 640 }}>
              <button className="btn secondary" data-act="secondary" onClick={() => nav("/quality")}>
                Quality
              </button>
              <button className="btn dominant" data-act="primary" onClick={() => nav("/trace")}>
                Proof
              </button>
            </div>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p2_15 · One search, one trail, independently checkable ──────────────────
export function Trace() {
  const s = useConsole();
  const nav = useNavigate();
  const [input, setInput] = useState(s.traceClaim);
  const claimId = s.traceClaim;
  const r = useLoad(async () => {
    if (!claimId) return null;
    const [win, instr, claim] = await Promise.all([
      api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("payments", `/v1/instructions/by-claim/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("evidence", `/v1/claims/${encodeURIComponent(claimId)}`).catch(() => null),
    ]);
    const c = claim && (claim.claim || claim);
    const who = c?.partyId
      ? await api.get("parties", `/v1/parties/${encodeURIComponent(c.partyId)}`).catch(() => null)
      : null;
    return { win, instr: instr && (instr.instruction || instr), claim: c, who: who && (who.party || who) };
  }, [claimId]);
  const st = (ok: boolean, active: boolean) => (ok ? "" : active ? "active" : "todo");
  return (
    <>
      <Title t={"One search, one trail, independently checkable"} />
      <Lede>
        This project's own records — no worker consent needed. Anything from another project needs their explicit
        consent through the disclosure flow, and would arrive as a narrower disclosure than this.
      </Lede>
      <Card>
        <form
          id="traceform"
          style={{ display: "flex", gap: 10 }}
          onSubmit={(ev) => {
            ev.preventDefault();
            s.setTraceClaim(input.trim());
          }}
        >
          <input name="claim" placeholder="crest:claim:…" value={input} required onChange={(e) => setInput(e.target.value)} className="mono" style={{ flex: 1 }} />
          <button className="btn" style={{ width: "auto", padding: "10px 22px" }}>
            Trace
          </button>
        </form>
      </Card>
      {claimId ? (
        <LoadFrame r={r}>
          {(d) => {
            if (!d) return null;
            const { win, instr: i, claim: c, who } = d;
            return (
              <>
                <Title t={(who?.displayName || "This worker") + " · " + short(c?.partyId || claimId)} />
                <GridTable cols="1.2fr 1fr 1fr 1.4fr 1fr 1fr" head={["Work definition", "Unit", "Cleared", "Evidence", "Credential", "Payment"]}>
                  <div className="g-row">
                    <span>{c?.definitionId ? <Mono>{short(c.definitionId)}</Mono> : "—"}</span>
                    <span>{c?.unitId ? <Mono>{short(c.unitId)}</Mono> : "—"}</span>
                    <span>{win?.exitRoute || (win ? "window open" : "—")}</span>
                    <span className="muted">{c?.state || "no claim on record"}</span>
                    <span>{win?.credentialId ? <Mono>{short(win.credentialId)}</Mono> : "—"}</span>
                    <span>{i ? i.state : "none"}</span>
                  </div>
                </GridTable>
                <CardTitled t={"Claim " + short(claimId)}>
                  <div className="tline">
                    <TStep state={st(!!c, true)} label="Work recorded and attributed" meta={c ? <>unit <MonoShort id={c.unitId} /> · state {c.state || ""}</> : "the evidence service has no such claim — the trail stops before it starts"} />
                    <TStep state={st(!!win?.exitRoute, !!win)} label="The worker had their say" meta={win ? (win.exitRoute ? "exit: " + win.exitRoute + " · " + when(win.exitedAt || win.closedAt) : "window still open — closes " + when(win.closesAt)) : "no confirmation window found — the confirmation service owes this step"} />
                    <TStep state={st(!!win?.credentialId, !!win?.exitRoute)} label="Credential signed" meta={win?.credentialId ? <MonoShort id={win.credentialId} /> : "issued when the window exits"} />
                    <TStep state={st(!!i, !!win?.exitRoute)} label="Payment instruction raised" meta={i ? money(i.amountMinor, i.currency) + " · " + i.state : "none — if the window exited, this gap is the payments service's to explain"} last />
                  </div>
                  {i?.held ? (
                    <KVR rows={[["held", <>{i.held.explanation || i.held.code} — owner <MonoShort id={i.held.ownerPartyId || ""} /></>]]} />
                  ) : null}
                </CardTitled>
              </>
            );
          }}
        </LoadFrame>
      ) : null}
      <Callout kind="teal" title="The consent posture, stated">
        You can see this without asking her, because these are your project’s own records. Anything from another
        project — and she may have worked on two — needs her explicit consent through the disclosure flow, and would
        arrive as a narrower disclosure than this.
      </Callout>
      <OpenNote>
        The reference's frame opens on a named worker's whole history and exports it as a bundle an auditor can verify
        without CREST. What exists here is one claim's trail, hop by hop, each hop a real service read: there is no
        per-worker project history endpoint and no bundle export, so neither is drawn as if there were. A trace that
        ends early is an answer — it names the step that owes the next fact.
      </OpenNote>
      <div className="btn-row" style={{ maxWidth: 520 }}>
        <button className="btn secondary" data-act="secondary" onClick={() => nav("/payments")}>
          Payments
        </button>
        <button className="btn dominant" data-act="primary" onClick={() => nav("/reports")}>
          Reports
        </button>
      </div>
    </>
  );
}

// ── p2_16 · Two things a funder always wants, pre-built ─────────────────────
export function Reports() {
  const r = useLoad(projectRead);
  return (
    <LoadFrame r={r}>
      {(f) => {
        const cur = (f.instructions[0] || {}).currency || "";
        const sum = (xs: Instr[]) => xs.reduce((t, i) => t + (i.amountMinor || 0), 0);
        const instructed = sum(f.instructions);
        const paidList = f.instructions.filter((i) => i.state === "SETTLED" || i.state === "RELEASED");
        const paid = sum(paidList);
        return (
          <>
            <Title t="Fund utilisation — reconciled, with the difference explained" extra={ILLUSTRATIVE} />
            <Lede>
              Two things a funder always wants: where the money went, and how strong the evidence behind it is.
            </Lede>
            <CardTitled t="Stage by stage">
              <GridTable cols="1.4fr 1fr 1fr 1.8fr" head={["Stage", "Amount", "Difference from the stage above", "Why"]}>
                <div className="g-row">
                  <span>Allocated</span>
                  <span>—</span>
                  <span>—</span>
                  <span className="muted">No funding ledger exists in CREST; an allocation figure would be invented.</span>
                </div>
                <div className="g-row">
                  <span>Instructed</span>
                  <span className="num">{money(instructed, cur)}</span>
                  <span>—</span>
                  <span className="muted">{f.instructions.length} instruction(s) raised — every window exit raises one.</span>
                </div>
                <div className="g-row">
                  <span>Confirmed paid</span>
                  <span className="num">{money(paid, cur)}</span>
                  <span className="num">{money(paid - instructed, cur)}</span>
                  <span className="muted">
                    The difference is what is held or in flight — the Payments frame names the reason and the owner for
                    each.
                  </span>
                </div>
              </GridTable>
            </CardTitled>
            <CardTitled t="Proof of work — volume with the strength of evidence attached">
              <GridTable cols="1.6fr 1fr 1fr" head={["Work definition", "Validated units", "Tier mix"]}>
                <div className="g-row">
                  <span>{f.def?.activity?.label || short(FIX.definition)}</span>
                  <span className="num">{f.claims.length}</span>
                  <span className="muted">— · no project-wide tier read exists</span>
                </div>
              </GridTable>
            </CardTitled>
            <Callout kind="green" title="Why tier is in a funder report">
              The tier columns are what make this proof rather than a claim. A funder reading 92% Tier 1 on household
              visits knows the number came from a system, not from a form somebody filled in afterwards.
            </Callout>
            <OpenNote>
              Illustrative where the reference is not: there is no report endpoint, no saved-report object and no
              scheduling, and the allocation and tier-mix columns have no contract behind them
              (<a href="https://github.com/theflywheel/CREST/issues/31">#31</a>). The instructed and confirmed-paid
              rows are live. Nothing constrains what a saved report may expose either — individual-level cuts would
              need a governance decision, not a checkbox.
            </OpenNote>
            <NextBlock
              happened={<>This project's records are readable end to end · <Mono>{short(FIX.project)}</Mono></>}
              who="Work definitions, then workers submitting against them"
              when="Records arrive as evidence is submitted; the confirmation window then runs its course"
              told="Nothing is pushed to you — this console is the read"
              ifnot="A stuck item with no owner will sit indefinitely. Nothing escalates on its own — this is a known gap, named on the Work status frame too."
            />
          </>
        );
      }}
    </LoadFrame>
  );
}
