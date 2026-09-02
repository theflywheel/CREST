// Project view — the live heart of the console, ported 1:1 from
// apps/console/views-project.js. Every number is read from a running
// service; where the design promises one with no backend, the screen says so
// and names the issue instead of inventing it.
import { useState } from "react";
import { api, FIX } from "@crest/api";
import { Chip, Sidecar, OpenNote } from "@crest/ui";
import {
  short, money, when, agoDays, Mono, MonoShort, Stat, KVR, Title, Lede, Tbl,
  Card, CardTitled, TStep, TierChip, useLoad, LoadFrame,
} from "../ui";
import { useConsole } from "../state";
import { useNavigate } from "react-router-dom";

type Held = { code?: string; explanation?: string; ownerPartyId?: string };
type Instr = {
  partyId?: string; claimId?: string; amountMinor?: number; currency?: string;
  state?: string; releasedBy?: string; releasedAt?: string; held?: Held; heldReason?: string;
};

const unresolved = (arr?: Array<{ resolvedAt?: string; createdAt?: string; opensAt?: string }>) =>
  (arr || []).filter((x) => !x.resolvedAt);
const oldestAge = (arr: Array<{ createdAt?: string; opensAt?: string }>, field: "createdAt" | "opensAt") => {
  const days = (arr || [])
    .map((x) => agoDays((x as Record<string, string | undefined>)[field] || x.createdAt))
    .filter((d): d is number => d !== null);
  return days.length ? Math.max(...days) + "d oldest" : "";
};

async function funnelCounts() {
  const [claims, unclear, unreleased, unreached, instr, metrics] = await Promise.all([
    api.get("evidence", "/v1/claims").catch(() => ({ claims: [] })),
    api.get("evidence", "/v1/unclear").catch(() => ({ unclear: [] })),
    api.get("confirmation", "/v1/unreleased").catch(() => ({ windows: [], unreleased: [] })),
    api.get("confirmation", "/v1/unreached").catch(() => ({ windows: [], unreached: [] })),
    api.get("payments", "/v1/instructions").catch(() => ({ instructions: [] })),
    api.get("parties", "/v1/holds/metrics").catch(() => null),
  ]);
  return {
    claims: (claims.claims || []) as Array<{ state?: string }>,
    unclear: unresolved(unclear.unclear),
    unreleased: unreleased.windows || unreleased.unreleased || [],
    unreached: unreached.windows || unreached.unreached || [],
    instructions: (instr.instructions || []) as Instr[],
    metrics,
  };
}

export function Status() {
  const r = useLoad(funnelCounts);
  return (
    <LoadFrame r={r}>
      {(f) => {
        const byState = f.claims.reduce<Record<string, number>>((m, c) => {
          const k = c.state || "?";
          m[k] = (m[k] || 0) + 1;
          return m;
        }, {});
        const held = f.instructions.filter((i) => i.state === "HELD");
        const settled = f.instructions.filter((i) => i.state === "RELEASED" || i.state === "SETTLED");
        return (
          <>
            <Title t="PRJ-118 · a funnel, not totals" />
            <Lede>
              Riverside bednet campaign 2026. Each stage below is a real queue read from a service, and every stuck
              thing in it has an owning role — a count without an owner is a number nobody has to act on.
            </Lede>
            <div className="stats">
              <Stat n={f.claims.length} label={"claims on record — " + Object.entries(byState).map(([k, n]) => `${n} ${k}`).join(", ")} owner="evidence service" />
              <Stat n={f.unclear.length} label={`unclear rows waiting ${oldestAge(f.unclear, "createdAt")}`} owner="owner: registry custodian" />
              <Stat n={f.unreached.length} label={`open windows nobody could be told about ${oldestAge(f.unreached, "opensAt")}`} owner="owner: supervisor (assisted route)" />
            </div>
            <div className="stats">
              <Stat n={f.unreleased.length} label="windows exited, payment not yet confirmed released" owner="owner: payments service — the relay owes this" />
              <Stat n={f.instructions.length + " / " + held.length + " / " + settled.length} label="instructions raised / held / released-or-settled" owner="held ones each name an owning office below in Payments" />
              <Stat n={f.metrics ? (f.metrics.mergesWithoutConfirmation ?? f.metrics.merges_without_confirmation ?? 0) : "—"} label="merges without confirmation — the invariant as a number, must be 0" owner="owner: registry custodian" />
            </div>
            <OpenNote>
              The full metric contracts — straight-through rate, tier mix, time-to-say — are{" "}
              <a href="https://github.com/theflywheel/CREST/issues/31">#31</a> and are not built. Presentation counts
              exist only per worker (<span className="mono">/v1/presentations?subjectRef=</span>); a project-wide
              presentations total would need #31's contracts, so none is shown.
            </OpenNote>
          </>
        );
      }}
    </LoadFrame>
  );
}

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
    nav(s.persona === "custodian" ? "/supporttrace" : "/trace");
  };
  return (
    <LoadFrame r={r}>
      {({ list, rec }) => {
        const held = list.filter((i) => i.state === "HELD");
        const groups = held.reduce<Record<string, Instr[]>>((m, i) => {
          const k = i.held?.code || i.heldReason || "held";
          (m[k] = m[k] || []).push(i);
          return m;
        }, {});
        return (
          <>
            <Title t="Payments, and where the delay is" />
            <Lede>
              Money delayed and people waiting are different sentences. Every held payment carries a reason with an
              owner — the record itself refuses one without.
            </Lede>
            <div className="stats">
              <Stat n={list.length} label="instructions" />
              <Stat n={held.length} label="held — each with a named owner" />
              <Stat n={rec ? (rec.gaps || []).length : "—"} label={rec ? "reconciliation gaps" : "reconciliation not answering"} />
            </div>
            {Object.entries(groups).map(([code, items]) => (
              <div className="held" key={code}>
                <div className="top">
                  <span className="amt">
                    {code} · {items.length} instruction(s) ·{" "}
                    {money(items.reduce((sum, i) => sum + (i.amountMinor || 0), 0), items[0].currency)}
                  </span>
                  <Chip kind="warn">held</Chip>
                </div>
                <div className="why">{items[0].held?.explanation || "No explanation string — the code is the reason."}</div>
                <div className="who">
                  Owner: {short(items[0].held?.ownerPartyId || "") || "(unnamed — that is itself a defect to raise)"}
                </div>
              </div>
            ))}
            <CardTitled t="The delay, by reason">
              <Tbl
                heads={["Reason", "Count", "Amount held", "Owner"]}
                rows={Object.entries(groups).map(([code, items]) => [
                  code,
                  String(items.length),
                  money(items.reduce((sum, i) => sum + (i.amountMinor || 0), 0), items[0].currency),
                  <MonoShort id={items[0].held?.ownerPartyId || ""} />,
                ])}
                empty="No payment is held. Delay taxonomy rows appear the moment one is."
              />
            </CardTitled>
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
          </>
        );
      }}
    </LoadFrame>
  );
}

export function Trace() {
  const s = useConsole();
  const [input, setInput] = useState(s.traceClaim);
  const claimId = s.traceClaim;
  const r = useLoad(async () => {
    if (!claimId) return null;
    const [win, instr, claim] = await Promise.all([
      api.get("confirmation", `/v1/windows/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("payments", `/v1/instructions/by-claim/${encodeURIComponent(claimId)}`).catch(() => null),
      api.get("evidence", `/v1/claims/${encodeURIComponent(claimId)}`).catch(() => null),
    ]);
    return { win, instr, claim };
  }, [claimId]);
  const st = (ok: boolean, active: boolean) => (ok ? "" : active ? "active" : "todo");
  return (
    <>
      <Title t="Trace a claim — one search, one checkable trail" />
      <Lede>
        From "the money did not arrive" to the step that owes an answer, without guessing. Each fact comes from a
        different service, so a gap names the service responsible for it.
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
            const i = d.instr && (d.instr.instruction || d.instr);
            const c = d.claim && (d.claim.claim || d.claim);
            const win = d.win;
            return (
              <CardTitled t={"Claim " + short(claimId)}>
                <div className="tline">
                  <TStep state={st(!!c, true)} label="Work recorded and attributed" meta={c ? <>unit <MonoShort id={c.unitId} /> · state {c.state || ""}</> : "the evidence service has no such claim — the trail stops before it starts"} />
                  <TStep state={st(!!win?.exitRoute, !!win)} label="The worker had their say" meta={win ? (win.exitRoute ? "exit: " + win.exitRoute + " · " + when(win.exitedAt || win.closedAt) : "window still open — closes " + when(win.closesAt)) : "no confirmation window found — the confirmation service owes this step"} />
                  <TStep state={st(!!win?.credentialId, !!win?.exitRoute)} label="Credential signed" meta={win?.credentialId ? <MonoShort id={win.credentialId} /> : "issued when the window exits"} />
                  <TStep state={st(!!i, !!win?.exitRoute)} label="Payment instruction raised" meta={i ? money(i.amountMinor, i.currency) + " · " + i.state : "none — if the window exited, this gap is the payments service's to explain"} last />
                </div>
                {i?.held ? <KVR rows={[["held", <>{i.held.explanation || i.held.code} — owner <MonoShort id={i.held.ownerPartyId || ""} /></>]]} /> : null}
                <p className="muted" style={{ marginTop: 8 }}>
                  A trace that ends early is an answer, not a failure: it names the step that owes the next fact.
                </p>
              </CardTitled>
            );
          }}
        </LoadFrame>
      ) : null}
    </>
  );
}

export function Definition() {
  const r = useLoad(async () => {
    const [d, v, lr] = await Promise.all([
      api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}`),
      api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/faces/verifier`).catch(() => null),
      api.get("definitions", `/v1/definitions/${encodeURIComponent(FIX.definition)}/linked-records`).catch(() => ({ linkedRecords: [] })),
    ]);
    return { d, v, lr: lr.linkedRecords || [] };
  });
  return (
    <LoadFrame r={r}>
      {({ d, v, lr }) => {
        const tm: Array<{ tier: number; sourceClassIn?: string[]; captureMethodIn?: string[]; minIdentityAssurance?: string; requiresFields?: string[] }> =
          (v && v.tierMap) || d.tierMap || [];
        const pays = lr.find((x: { type?: string }) => x.type === "payment-setup");
        return (
          <>
            <Title t="The work, defined" extra={<Chip kind={d.state === "ACTIVE" ? "ok" : "info"}>{"v" + d.version + " · " + d.state}</Chip>} />
            <Lede>
              A definition is versioned and immutable once active; author and ratifier are two people by construction.
              What it pays lives in a separate record, referenced by id — the rate can change without touching what the
              work <em>is</em>.
            </Lede>
            <CardTitled t={d.activity?.label || String(d.activity || "")}>
              <KVR
                rows={[
                  ["id", <Mono>{d.id}</Mono>],
                  ["counted in", d.outcomeUnit || ""],
                  ["skill code (the part that travels)", <Mono>{d.skillCode || ""}</Mono>],
                  ["authored by", <MonoShort id={d.authoredByPartyId || ""} />],
                  d.ratifiedByPartyId && ["ratified by", <><MonoShort id={d.ratifiedByPartyId} /> — not its author, by construction</>],
                  ["activated", when(d.activatedAt)],
                ]}
              />
            </CardTitled>
            <CardTitled t="How evidence becomes a tier — first matching rule wins">
              <KVR
                rows={tm.map((rule) => [
                  "Tier " + rule.tier,
                  <>
                    <TierChip t={rule.tier} /> source in {(rule.sourceClassIn || []).join(" / ")}; captured by{" "}
                    {(rule.captureMethodIn || []).join(" / ")}; identity ≥ {rule.minIdentityAssurance || "any"}
                    {(rule.requiresFields || []).length ? "; needs " + rule.requiresFields!.join(", ") : ""}
                  </>,
                ])}
              />
              <p className="muted" style={{ marginTop: 8 }}>
                The map is public to verifiers on purpose — a verifier who cannot see it can only be told a tier, never
                check one. The tier itself is computed at query time and stored nowhere.
              </p>
            </CardTitled>
            {lr.length ? (
              <CardTitled t="Linked records">
                <KVR
                  rows={lr.map((x: { type: string; version: number; state: string; payload?: { ratePerOutcomeUnit: { amountMinor: number; currency: string }; effectiveFrom?: string } }) => [
                    x.type,
                    "v" + x.version + " · " + x.state + (x.type === "payment-setup" && x.payload
                      ? " — " + money(x.payload.ratePerOutcomeUnit.amountMinor, x.payload.ratePerOutcomeUnit.currency) + " per unit, effective " + when(x.payload.effectiveFrom)
                      : ""),
                  ])}
                />
              </CardTitled>
            ) : null}
            <Sidecar ok>
              The 28-screen authoring wizard is the <strong>Define work</strong> journey in the sidebar — it walks this
              same signed definition step by step, as the screens would have captured it.
              {pays ? "" : " No payment-setup record is attached; the work is recognised, and recognition is a use of its own."}
            </Sidecar>
          </>
        );
      }}
    </LoadFrame>
  );
}

export function Sources() {
  const r = useLoad(async () => {
    const [out, assess] = await Promise.all([
      api.get("evidence", "/v1/sources").catch(() => ({ sources: [], silent: 0 })),
      api.get("verification", "/v1/source-assessments").catch(() => ({ assessments: [] })),
    ]);
    return { sources: out.sources || [], assessments: assess.assessments || [] };
  });
  return (
    <LoadFrame r={r}>
      {({ sources, assessments }) => {
        const neverSeen = sources.filter((x: { state?: string }) => x.state === "NEVER_SEEN");
        return (
          <>
            <Title t="Where evidence comes from" />
            <Lede>
              A source going quiet is the only failure that produces nothing a worker can see or report — their record
              simply stops growing. So every feed is registered with a cadence and an owner, and the system notices
              silence unaided.
            </Lede>
            <Tbl
              heads={["System", "Adapter", "Cadence", "Last seen", "State", "Owner"]}
              rows={sources.map((x: { systemRef?: string; adapterRef?: string; expectedEvery?: string; lastSeenAt?: string; state?: string; ownerPartyId?: string }) => [
                <Mono>{x.systemRef || ""}</Mono>,
                <Mono>{x.adapterRef}</Mono>,
                x.expectedEvery || "",
                when(x.lastSeenAt),
                <Chip kind={x.state === "HEALTHY" ? "ok" : x.state === "SILENT" ? "err" : "warn"}>{x.state}</Chip>,
                <MonoShort id={x.ownerPartyId || ""} />,
              ])}
              empty="No sources registered — fixtures submit as canonical CSV."
            />
            {neverSeen.length ? (
              <OpenNote>
                {neverSeen.length} source(s) read NEVER_SEEN even though this project's evidence arrived through them.
                Known bug, design finding <a href="https://github.com/theflywheel/CREST/issues/117">#117</a>: the
                heartbeat joins the batch's <span className="mono">systemRef</span> against the source's{" "}
                <span className="mono">adapter_ref</span>, so a feed named differently in the two places never registers
                a beat. Reported, deliberately not patched around here.
              </OpenNote>
            ) : null}
            <CardTitled t="Current assessments">
              {assessments.length ? (
                <KVR rows={assessments.map((a: { adapterRef: string; maxTier: number; reason?: string }) => [a.adapterRef, "capped at tier " + a.maxTier + " — " + (a.reason || "")])} />
              ) : (
                <div className="muted">
                  No source is downgraded. A downgrade moves every affected credential's tier instantly, with nothing
                  reissued — the tier is derived, never stored.
                </div>
              )}
            </CardTitled>
          </>
        );
      }}
    </LoadFrame>
  );
}

export function Reports() {
  const r = useLoad(funnelCounts);
  return (
    <LoadFrame r={r}>
      {(f) => {
        const released = f.instructions.filter((i) => i.state === "RELEASED" || i.state === "SETTLED");
        const paidMinor = released.reduce((sum, i) => sum + (i.amountMinor || 0), 0);
        const cur = (released[0] || f.instructions[0] || {}).currency || "";
        return (
          <>
            <Title t="Reports — what a funder is told" />
            <Lede>
              Rendered from the same live queues as Status — a report that could disagree with the console it came from
              would be two versions of the truth.
            </Lede>
            <div className="stats">
              <Stat n={f.claims.length} label="claims on record" owner="evidence service" />
              <Stat n={released.length} label="payments released — all four window exits release, a dispute included" owner="payments service" />
              <Stat n={money(paidMinor, cur)} label="released to workers, total" owner="payments service" />
            </div>
            <div className="stats">
              <Stat n={f.unclear.length} label="rows still unattributed" owner="owner: registry custodian" />
              <Stat n={f.instructions.filter((i) => i.state === "HELD").length} label="payments held — each names an owner in Payments" owner="see Payments for the taxonomy" />
              <Stat n={f.metrics ? (f.metrics.mergesWithoutConfirmation ?? 0) : "—"} label="merges without confirmation (must be 0)" owner="owner: registry custodian" />
            </div>
            <OpenNote>
              Pre-built funder report formats — straight-through rate, tier mix, cost per verified outcome, exportable
              periods — are the metric contracts of <a href="https://github.com/theflywheel/CREST/issues/31">#31</a> and
              are not built. The counts above are live; nothing else is pretended.
            </OpenNote>
          </>
        );
      }}
    </LoadFrame>
  );
}
