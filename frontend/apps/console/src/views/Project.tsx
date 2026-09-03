// The two project reads that are not J3 dashboard frames: the definition as
// published (p3_19) and the evidence sources behind it (p2_9's evidence half).
// The J3 dashboard wave — Work status, Quality, Payments, Proof, Reports —
// lives in Dashboard.tsx, in the reference's own frames.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, Sidecar, OpenNote } from "@crest/ui";
import { errText, useConsole } from "../state";
import { SourcingRow } from "./Setup";
import {
  money, when, Mono, MonoShort, KVR, Title, Lede, Empty, Tbl,
  CardTitled, TierChip, useLoad, LoadFrame,
} from "../ui";

// p2's "Where the work definition comes from": the configurator's rail entry
// draws the origin choice, not the published read — the read (p3_19) belongs
// to the personas who resolve a definition rather than commission one.
function DefinitionOrigin() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [err, setErr] = useState<string | null>(null);
  const r = useLoad(async () => {
    const [comp, roles] = await Promise.all([
      api.get("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition`).catch(() => ({ choices: [] })),
      api.get("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/roles`).catch(() => ({ roles: [] })),
    ]);
    const rec = ((comp.choices || []) as Array<{ kind?: string; payload?: { value?: unknown } }>).find(
      (c) => (c.kind || "").replace(/^composition:/, "") === "definition-origin",
    );
    const grants = (roles.roles || []) as Array<{ partyId?: string; displayName?: string; functions?: string[]; state?: string }>;
    const holder = (fn: string) => grants.find((g) => g.state === "ACTIVE" && (g.functions || []).includes(fn));
    return {
      origin: rec ? String(rec.payload?.value || "") : "",
      author: holder("specify-definition"),
      approver: holder("ratify-definition"),
    };
  }, [s.projectId, gen]);
  const record = async (v: string) => {
    setErr(null);
    try {
      await api.put("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition/definition-origin`, { value: v });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(errText(e));
    }
  };
  return (
    <LoadFrame r={r}>
      {({ origin, author, approver }) => (
        <>
          <Title t="Where the work definition comes from" />
          <Lede>
            Author it, start from a template, or derive it from a source system that already describes this work.
          </Lede>
          {err ? <div className="errbar">{err}</div> : null}
          <div style={{ maxWidth: 780 }}>
            <SourcingRow
              t="Author it here"
              s="Define the work from scratch. The Work Definition Author walks the full flow."
              on={origin === "author-here"}
              onPick={() => record("author-here")}
            />
            <SourcingRow
              t="Start from a template"
              s="Curated by Instance Registry Maintainer, or reused from another organisation's ratified definition."
              on={origin === "template"}
              onPick={() => record("template")}
            />
            <SourcingRow
              t="Derive from a source system"
              s="Read the existing configuration out of a delivery platform and ratify it locally."
              on={origin === "derive"}
              onPick={() => record("derive")}
            />
            {origin ? null : (
              <p className="muted" style={{ margin: "2px 0 10px" }}>
                Nothing is answered yet — picking a row records the choice on the project, with your name and the date.
              </p>
            )}
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, padding: "14px 0", borderBottom: "1px solid var(--line, #E5E1DC)" }}>
              <div>
                <div style={{ font: "500 15px/1.4 Roboto,system-ui" }}>Work Definition Author (Work Definition Author)</div>
                <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
                  {author ? (author.displayName || author.partyId) : "Nobody is assigned — no active grant on this project carries specify-definition"}
                </div>
              </div>
              <Chip kind={author ? "ok" : "plain"}>{author ? "Assigned" : "Not assigned"}</Chip>
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, padding: "14px 0" }}>
              <div>
                <div className="muted" style={{ font: "500 15px/1.4 Roboto,system-ui" }}>Work Definition Approver (Work Definition Approver)</div>
                <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
                  {approver
                    ? (approver.displayName || approver.partyId)
                    : "Optional second sign-off. Not used by this project."}
                </div>
              </div>
              <Chip kind={approver ? "ok" : "plain"}>{approver ? "Assigned" : "Not used"}</Chip>
            </div>
            <div style={{ height: 12 }} />
            <Callout kind="teal" title="">
              A definition derived from a source system still has to be ratified locally and signed by a legitimate
              Work Definition Author. Deriving saves the authoring, not the accountability.
            </Callout>
            <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end", gap: 10 }}>
              <button className="btn secondary" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/workers")}>
                Back
              </button>
              <button className="btn dominant" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/validation")}>
                Continue
              </button>
            </div>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

export function Definition() {
  const { persona } = useConsole();
  // Two components, not an early return: the origin frame and the published
  // read carry different hook sets, and React needs each to keep its own.
  return persona === "orgadmin" || persona === "configurator" ? <DefinitionOrigin /> : <DefinitionRead />;
}

function DefinitionRead() {
  const { definitionId } = useConsole();
  const r = useLoad<{ d: any; v: any; lr: any } | null>(async () => {
    if (!definitionId) return null;
    const [d, v, lr] = await Promise.all([
      api.get("definitions", `/v1/definitions/${encodeURIComponent(definitionId)}`),
      api.get("definitions", `/v1/definitions/${encodeURIComponent(definitionId)}/faces/verifier`).catch(() => null),
      api.get("definitions", `/v1/definitions/${encodeURIComponent(definitionId)}/linked-records`).catch(() => ({ linkedRecords: [] })),
    ]);
    return { d, v, lr: lr.linkedRecords || [] };
  }, [definitionId]);
  if (!definitionId) {
    return (
      <>
        <Title t="The work, defined" />
        <Empty>No work definition exists yet. When one is authored and ratified, this screen reads it as published.</Empty>
      </>
    );
  }
  return (
    <LoadFrame r={r}>
      {(data) => {
        const { d, v, lr } = data!;
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

// w6_3, "What the project received, and where it sits" (§8). Grouped under
// P-10 (External Evidence Contact) in the reference, but the frame itself is
// a project-side read: the project that requested the evidence is who sees
// what arrived and where an unclear row sits, not the external institution
// that sent it (w6_1/w6_2, still illustrative, stay in the verify door's
// panel shell — they have no CREST account and see no console). So this
// screen lives here, beside the project's other reads, not there.
//
// Composes the existing batch, its units, their claims, and every unclear row
// the batch produced — nothing new is stored. Queue position for an unclear
// row comes from the same open-queue ordering /v1/unclear already uses.
export function Receipt() {
  const [batchId, setBatchId] = useState("");
  const [applied, setApplied] = useState("");
  const r = useLoad(async () => api.get("evidence", `/v1/batches/${encodeURIComponent(applied)}/receipt`), [applied]);
  return (
    <>
      <Title t="What the project received, and where it sits" />
      <Lede>
        The receipt for one submission: what arrived, its ingestion state per row, and — for anything nobody could
        attribute — where it sits in the open queue. This receipts the one ingestion door that exists,{" "}
        <span className="mono">POST /v1/batches</span>; w6_1/w6_2's scoped-link request and file-back answer stay
        illustrative because no scoped-link or upload endpoint exists to receipt against.
      </Lede>
      <form
        onSubmit={(ev) => { ev.preventDefault(); setApplied(batchId.trim()); }}
        style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 12 }}
      >
        <input value={batchId} onChange={(ev) => setBatchId(ev.target.value)} placeholder="batch id" style={{ minWidth: 340 }} />
        <button className="btn secondary" type="submit">Look up</button>
      </form>
      {!applied ? (
        <Empty>Enter a batch id — the one <span className="mono">POST /v1/batches</span> returned — to see its receipt.</Empty>
      ) : (
        <LoadFrame r={r}>
          {(d: {
            batch: { id: string; contextId: string; definitionId: string; submittedBy: string; createdAt: string };
            units: Array<{ unitId: string; outcome: { value: number; unit: string }; claims: Array<{ claimId: string; partyId: string; state: string }> }>;
            unclear: Array<{ id: string; rowRef: string; reason: string; resolvedAt?: string; queuePosition?: number }>;
          }) => {
            const units = d.units || [];
            const unclear = d.unclear || [];
            return (
              <>
                <CardTitled t={"Batch " + (d.batch?.id || applied)}>
                  <KVR
                    rows={[
                      ["context", <MonoShort id={d.batch?.contextId || ""} />],
                      ["definition", <Mono>{d.batch?.definitionId || ""}</Mono>],
                      ["submitted by", <MonoShort id={d.batch?.submittedBy || ""} />],
                      ["received", when(d.batch?.createdAt)],
                    ]}
                  />
                </CardTitled>
                <CardTitled t={units.length + " unit(s) arrived"}>
                  <Tbl
                    heads={["Unit", "Outcome", "Claims", "Ingestion state"]}
                    rows={units.map((u) => [
                      <MonoShort id={u.unitId} />,
                      String(u.outcome?.value ?? "") + " " + (u.outcome?.unit || ""),
                      u.claims.length,
                      u.claims.map((c) => c.state).join(", ") || "—",
                    ])}
                    empty="No units in this batch."
                  />
                </CardTitled>
                <CardTitled t={unclear.length + " row(s) nobody could attribute"}>
                  <Tbl
                    heads={["Row", "Reason", "Resolved", "Queue position"]}
                    rows={unclear.map((u) => [
                      u.rowRef,
                      u.reason,
                      u.resolvedAt ? when(u.resolvedAt) : "open",
                      u.queuePosition != null ? u.queuePosition : "—",
                    ])}
                    empty="Every row in this batch was attributed — nothing sits in the unclear queue."
                  />
                </CardTitled>
              </>
            );
          }}
        </LoadFrame>
      )}
    </>
  );
}

