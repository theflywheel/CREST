// The two project reads that are not J3 dashboard frames: the definition as
// published (p3_19) and the evidence sources behind it (p2_9's evidence half).
// The J3 dashboard wave — Work status, Quality, Payments, Proof, Reports —
// lives in Dashboard.tsx, in the reference's own frames.
import { api, FIX } from "@crest/api";
import { Chip, Sidecar, OpenNote } from "@crest/ui";
import {
  money, when, Mono, MonoShort, KVR, Title, Lede, Tbl,
  CardTitled, TierChip, useLoad, LoadFrame,
} from "../ui";

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

