// The funders wave, F-1 and F-2 (reference f1_2–f1_5, f2_4–f2_10): rate
// ownership, rates as versioned terms, and the mechanism whose activation
// gate sits in front of DISBURSEMENT — never in front of a confirmation
// window exit.
//
// The two payment invariants these screens sit on, named:
//
//   * Every confirmation-window exit releases payment (W4). Nothing on these
//     screens can prevent an exit creating its payment obligation; what a
//     not-live mechanism produces is a HELD instruction, and MechQualify
//     (f2_9) exists to show exactly that boundary.
//   * Every held payment has a reason with an owner (W10). The held rows
//     rendered here always show held.ownerPartyId; a failed test
//     disbursement renders its reason and owner too.
//
// Layering: amounts, currencies, batching windows, file cadences and the
// reference's example values ("KES 150.00", "Daily, 23:00") are L2 —
// programme vocabulary this deployment displays and records opaquely. The
// acts — assignment, versioned publication, choice, test result, activation
// — are the L1 records the payments application keeps.
import { useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { api, FIX } from "@crest/api";
import { Callout, Chip, DisLi, OpenNote, RefField, StepCounter } from "@crest/ui";
import {
  Card, CardTitled, KVR, Lede, LoadFrame, Mono, MonoShort, Title, useLoad, when, money,
} from "../ui";
import { errText, useConsole } from "../state";

// ── shapes, as the payments service returns them ───────────────────────────
type Assignment = {
  id: string; definitionId: string; assigneePartyId: string;
  assignedByPartyId: string; assignedAt: string; supersededAt?: string;
};
type RatePayload = {
  ratePerOutcomeUnit?: { amountMinor: number; currency: string };
  payerPartyId?: string; effectiveFrom?: string;
  authoredByPartyId?: string; supersedesVersion?: number;
};
type RateVersion = { version: number; rate: RatePayload };
type HeldReason = { code: string; explanation: string; ownerPartyId: string };
type Mechanism = {
  id: string; contextId: string; ownerPartyId: string; state: string;
  createdByPartyId: string; createdAt: string;
  activatedAt?: string; activatedBy?: string;
};
type Condition = { name: string; satisfied: boolean; satisfiedAt?: string; because?: string };
type MechRecord = { id: string; kind: string; actorPartyId: string; payload?: Record<string, unknown>; at: string };
type TestDisbursement = {
  id: string; state: string; amountMinor: number; currency: string;
  destination: string; requestedBy: string; at: string;
  railRef?: string; failure?: HeldReason;
};
type MechView = {
  mechanism: Mechanism | null;
  standing: "not-configured" | "configured-not-live" | "live";
  conditions?: Condition[];
  records?: MechRecord[];
  tests?: TestDisbursement[];
};
type Instruction = {
  id: string; claimId: string; unitId: string; partyId: string; contextId?: string;
  amountMinor: number; currency: string; releasedBy: string; releasedAt: string;
  state: string; held?: HeldReason;
};

const DEF = FIX.definition;
const defPath = (tail: string) => `/v1/definitions/${encodeURIComponent(DEF)}${tail}`;

async function loadRates() {
  const [owner, rates, d] = await Promise.all([
    api.get("payments", defPath("/rate-owner")),
    api.get("payments", defPath("/rates")),
    api.get("definitions", `/v1/definitions/${encodeURIComponent(DEF)}`).catch(() => null),
  ]);
  return {
    current: (owner.current || null) as Assignment | null,
    history: (owner.history || []) as Assignment[],
    versions: (rates.rates || []) as RateVersion[],
    inForce: (rates.inForce || null) as RateVersion | null,
    outcomeUnit: (d && d.outcomeUnit) || "outcome unit",
    workerSummary:
      (d && d.faces && d.faces.worker && d.faces.worker.summary) || "",
  };
}

const useMech = (contextId: string, gen = 0) =>
  useLoad<MechView>(
    () => api.get("payments", `/v1/mechanisms/by-context/${encodeURIComponent(contextId)}`),
    [contextId, gen],
  );

const standingChip = (s: string) =>
  s === "live" ? (
    <Chip kind="ok">live</Chip>
  ) : s === "configured-not-live" ? (
    <Chip kind="warn">configured, not live</Chip>
  ) : (
    <Chip kind="plain">not configured</Chip>
  );

const rateMoney = (r?: RatePayload) =>
  r && r.ratePerOutcomeUnit ? money(r.ratePerOutcomeUnit.amountMinor, r.ratePerOutcomeUnit.currency) : "—";

// The reference's own draft values (f1_3). L2: what one deployment's
// programme happens to charge, held in the browser only between the
// authoring screen and the publish screen — the publication is the record.
const DRAFT_KEY = "crest.console.ratedraft";
const draft = () => {
  try {
    return JSON.parse(sessionStorage.getItem(DRAFT_KEY) || "{}") as { amount?: string; effective?: string };
  } catch {
    return {};
  }
};

// ── f1_2 · A request to put someone on payment ─────────────────────────────
// Anyone can ask. Only one person can assign — the recorded assignment is
// the answer, and a re-assignment supersedes without deleting.
export function RateOwner() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [assignee, setAssignee] = useState<string>(FIX.org);
  const r = useLoad(loadRates, [gen]);
  const me = s.me!.partyId;
  const assign = async () => {
    s.clearErr();
    try {
      await api.post("payments", defPath("/rate-owner"), {
        assigneePartyId: assignee.trim(),
        assignedByPartyId: me,
      });
      setGen((g) => g + 1);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ current, history }) => (
        <>
          <Title t="A request to put someone on payment" extra={current ? <Chip kind="ok">owner assigned</Chip> : <Chip kind="plain">no owner yet</Chip>} />
          <Lede>
            Anyone can ask for a rate. Only one person can assign who sets it — and the assignment is a record with
            the assigner's name on it, not a setting.
          </Lede>
          <Card>
            <RefField
              label="Who will own it"
              value="One person owns both halves They set the rate and choose how the money moves."
            />
            <RefField label="Rate Owner" hint="a registry party id — the assignment names a party, never an email alone">
              <input name="rateownerparty" value={assignee} onChange={(e) => setAssignee(e.target.value)} />
            </RefField>
            <RefField label="Payment Mechanism Owner" value="Same person" />
            <div style={{ display: "flex", gap: 10, marginTop: 12 }}>
              <button id="decline-assign" className="btn secondary inline" onClick={() => nav("/paysetup")}>
                Decline the request
              </button>
              <button id="assign-owner" className="btn inline" onClick={assign}>
                Accept and invite
              </button>
            </div>
          </Card>
          <CardTitled t="Who owns the rate now">
            <KVR
              rows={[
                ["current owner", current ? <MonoShort id={current.assigneePartyId} /> : "nobody — publishing a rate is refused until an assignment answers"],
                current ? ["assigned by", <><MonoShort id={current.assignedByPartyId} /> · {when(current.assignedAt)}</> ] : null,
              ]}
            />
          </CardTitled>
          {history.length ? (
            <CardTitled t="Every assignment, kept">
              {history.map((a) => (
                <div className="kv" key={a.id} data-assignment={a.id}>
                  <div className="row">
                    <span className="k">{a.supersededAt ? "superseded " + when(a.supersededAt) : "current"}</span>
                    <span className="v">
                      <MonoShort id={a.assigneePartyId} /> — assigned by <MonoShort id={a.assignedByPartyId} /> on {when(a.assignedAt)}
                    </span>
                  </div>
                </div>
              ))}
            </CardTitled>
          ) : null}
          <Callout kind="teal" title="The rule this screen sets">
            Anyone can ask. Only one person can assign. Handing the rate to someone else is a new assignment that
            supersedes this one — the old one stays on the record, because who set a rate is part of what the rate is.
          </Callout>
        </>
      )}
    </LoadFrame>
  );
}

// ── f1_3 · What one unit pays ──────────────────────────────────────────────
// The assigned owner prices a unit somebody else defined. The unit is
// read-only by construction: this screen has no control that could touch the
// definition, and the service refuses definition vocabulary in the payload.
export function RateAuthor() {
  const s = useConsole();
  const nav = useNavigate();
  const d0 = draft();
  const [amount, setAmount] = useState(d0.amount || "150.00");
  const [effective, setEffective] = useState(d0.effective || "");
  const r = useLoad(loadRates);
  const me = s.me!.partyId;
  return (
    <LoadFrame r={r}>
      {({ current, outcomeUnit, inForce }) => (
        <>
          <Title t="What one unit pays" />
          <Lede>
            The unit below was defined and ratified by other people; you price it. Nothing on this screen can change
            what the work is.
          </Lede>
          {current && current.assigneePartyId !== me ? (
            <Callout kind="green" title="What this screen never does">
              It never lets anyone but the assigned owner publish. The owner is <MonoShort id={current.assigneePartyId} />;
              the service refuses any other author.
            </Callout>
          ) : null}
          <Card>
            <RefField label="Unit of work" value={"Per " + outcomeUnit} hint="read-only: defined and ratified elsewhere — pricing cannot redefine it" />
            <RefField label="Rate per unit" hint="KES, per outcome unit">
              <input name="rateamount" value={amount} onChange={(e) => setAmount(e.target.value)} />
            </RefField>
            <RefField label="Calculation rule" value="Flat per unit" hint="the one rule built today; other rules are programme policy with no enforcement point yet" />
            <RefField label="Ceiling" value="KES 9,000 per worker per month" hint="declared programme policy — recorded here, not yet enforced by the payments application" />
            <RefField label="Effective from" hint="empty = effective now; a future date is a version that holds payments until it is in force">
              <input name="rateeffective" type="date" value={effective} onChange={(e) => setEffective(e.target.value)} />
            </RefField>
            <div style={{ display: "flex", gap: 10, marginTop: 12 }}>
              <button className="btn secondary inline" onClick={() => nav("/rateowner")}>Back</button>
              <button
                id="rate-continue"
                className="btn inline"
                onClick={() => {
                  sessionStorage.setItem(DRAFT_KEY, JSON.stringify({ amount, effective }));
                  nav("/ratepublish");
                }}
              >
                Continue
              </button>
            </div>
          </Card>
          <KVR rows={[["rate in force today", inForce ? rateMoney(inForce.rate) + " (v" + inForce.version + ")" : "none yet"]]} />
        </>
      )}
    </LoadFrame>
  );
}

// ── f1_4 · What the worker will see ────────────────────────────────────────
// A rate is terms, not a setting. There is deliberately no edit affordance
// anywhere on this screen: a published version is never changed, and a
// correction is a new version naming what it supersedes.
export function RatePublish() {
  const s = useConsole();
  const nav = useNavigate();
  const r = useLoad(loadRates);
  const me = s.me!.partyId;
  const d0 = draft();
  const publish = async () => {
    s.clearErr();
    const minor = Math.round(parseFloat(d0.amount || "0") * 100);
    try {
      await api.post("payments", defPath("/rates"), {
        authorPartyId: me,
        amountMinor: minor,
        currency: "KES",
        payerPartyId: FIX.org,
        ...(d0.effective ? { effectiveFrom: new Date(d0.effective).toISOString() } : {}),
      });
      sessionStorage.removeItem(DRAFT_KEY);
      nav("/ratestanding");
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <LoadFrame r={r}>
      {({ versions, outcomeUnit, workerSummary }) => (
        <>
          <Title t="What the worker will see" />
          <Lede>
            Publication is the act. Each publication is a new version on the definition's own record — one store,
            no copy in payments that could disagree with it.
          </Lede>
          <CardTitled t="The worker's own words">
            <div className="consent-quote">
              {workerSummary}
              <br />
              <br />
              <strong>
                One {outcomeUnit} pays {(d0.amount || "—") + " KES"} — set separately from the definition, versioned,
                never overwritten.
              </strong>
            </div>
          </CardTitled>
          <CardTitled t="Every published version — terms, kept">
            {versions.length ? (
              versions.map((v) => (
                <div className="kv" key={v.version} data-rateversion={v.version}>
                  <div className="row">
                    <span className="k">v{v.version}</span>
                    <span className="v">
                      {rateMoney(v.rate)} · effective {when(v.rate.effectiveFrom)}
                      {v.rate.authoredByPartyId ? <> · authored by <MonoShort id={v.rate.authoredByPartyId} /></> : null}
                      {v.rate.supersedesVersion ? <> · supersedes v{v.rate.supersedesVersion}</> : null}
                    </span>
                  </div>
                </div>
              ))
            ) : (
              <p className="muted">No version has been published yet. The first publication is v1.</p>
            )}
          </CardTitled>
          <Callout kind="green" title="What this screen never does">
            A rate is terms, not a setting. There is no edit here and never will be: a published version cannot be
            changed, and a payment is priced by the version in force when its work was released — a later
            publication never re-prices done work.
          </Callout>
          <div style={{ display: "flex", gap: 10 }}>
            <button className="btn secondary inline" onClick={() => nav("/rate")}>Back</button>
            <button id="publish-rate" className="btn inline" onClick={publish}>
              Publish the rate
            </button>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// ── f1_5 · The rate is live. The money still cannot move. ──────────────────
// Half done is a real state, derived on every read and never stored — the
// same rule as trust tiers.
export function RateStanding() {
  const s = useConsole();
  const nav = useNavigate();
  const rates = useLoad(loadRates);
  const mech = useMech(s.projectId);
  return (
    <LoadFrame r={rates}>
      {({ inForce, current }) => (
        <LoadFrame r={mech}>
          {(m) => (
            <>
              <Title t="The rate is live. The money still cannot move." extra={standingChip(m.standing)} />
              <Lede>
                Where payment set up stands for <Mono>{s.projectId}</Mono> — derived from the records on every read,
                never stored, so no screen can be out of date about it.
              </Lede>
              <Card>
                <div data-standing={m.standing} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                  <DisLi
                    on={m.standing !== "not-configured"}
                    t="not-configured"
                    s="No mechanism record exists for this project yet. A rate can exist before it — recognition works without money."
                  />
                  <DisLi
                    on={m.standing === "configured-not-live" || m.standing === "live"}
                    t="configured-not-live"
                    s="The mechanism exists and no real money can move. Half done is a real state with a name, not an error — and every payment obligation raised meanwhile is HELD with this as its reason, never lost."
                  />
                  <DisLi
                    on={m.standing === "live"}
                    t="live"
                    s="The activation gate passed on recorded acts. Disbursement flows; everything the gate was holding was re-priced and released."
                  />
                </div>
              </Card>
              <KVR
                rows={[
                  ["rate in force", inForce ? rateMoney(inForce.rate) + " (v" + inForce.version + ")" : "none — versions may exist that are not yet effective"],
                  ["rate owner", current ? <MonoShort id={current.assigneePartyId} /> : "unassigned"],
                  ["mechanism owner", m.mechanism ? <MonoShort id={m.mechanism.ownerPartyId} /> : "—"],
                ]}
              />
              <div style={{ display: "flex", gap: 10 }}>
                <button className="btn secondary inline" onClick={() => nav("/rateowner")}>
                  Hand this to someone else
                </button>
                <button id="choose-pathway" className="btn inline" onClick={() => nav("/paysetup")}>
                  Choose the pathway
                </button>
              </div>
            </>
          )}
        </LoadFrame>
      )}
    </LoadFrame>
  );
}

// ── the F-2 walk's shared frame ────────────────────────────────────────────

function MechFrame(props: {
  title: string;
  counter?: string;
  children: (m: MechView, reload: () => void) => ReactNode;
}) {
  const s = useConsole();
  const [gen, setGen] = useState(0);
  const r = useMech(s.projectId, gen);
  return (
    <LoadFrame r={r}>
      {(m) => (
        <>
          {props.counter ? <StepCounter>{props.counter}</StepCounter> : null}
          <Title t={props.title} extra={standingChip(m.standing)} />
          {props.children(m, () => setGen((g) => g + 1))}
        </>
      )}
    </LoadFrame>
  );
}

const recordsOf = (m: MechView, kind: string) => (m.records || []).filter((x) => x.kind === kind);

function RecordedAct(props: { rec?: MechRecord; missing: string }) {
  return props.rec ? (
    <Callout kind="teal" title="Recorded">
      {props.rec.kind} — by <MonoShort id={props.rec.actorPartyId} /> on {when(props.rec.at)} (<Mono>{props.rec.id}</Mono>)
    </Callout>
  ) : (
    <p className="muted">{props.missing}</p>
  );
}

function BackContinue(props: { back: string; next: string; nextLabel?: string }) {
  const nav = useNavigate();
  return (
    <div style={{ display: "flex", gap: 10 }}>
      <button className="btn secondary inline" onClick={() => nav(props.back)}>Back</button>
      <button className="btn inline" onClick={() => nav(props.next)}>{props.nextLabel || "Continue"}</button>
    </div>
  );
}

// ── f2_4 · Send one real payment ───────────────────────────────────────────
// Proving the whole path, once: one real amount through the configured rail,
// recorded either way. A failed test is still a record — with a reason and
// an owner — and what it is not is a satisfied condition.
export function MechTest() {
  const s = useConsole();
  const [dest, setDest] = useState("test-account-4412");
  const [amount, setAmount] = useState("10.00");
  const me = s.me!.partyId;
  return (
    <MechFrame title="Send one real payment">
      {(m, reload) => {
        const create = async () => {
          s.clearErr();
          try {
            await api.post("payments", "/v1/mechanisms", {
              contextId: s.projectId,
              ownerPartyId: me,
              createdByPartyId: me,
              config: { rail: "mobile-money · MockPay sandbox" },
            });
            reload();
          } catch (e) {
            s.fail(e);
          }
        };
        const send = async () => {
          s.clearErr();
          try {
            await api.post("payments", `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/test-disbursements`, {
              requestedByPartyId: me,
              amountMinor: Math.round(parseFloat(amount || "0") * 100),
              currency: "KES",
              destination: dest.trim(),
            });
            reload();
          } catch (e) {
            s.fail(e);
          }
        };
        return (
          <>
            <Lede>
              Before anyone's wages ride this mechanism, one real payment rides it first — to your own account, and
              the result is recorded whether it clears or not.
            </Lede>
            {m.mechanism === null ? (
              <Card>
                <p className="body-2">
                  No payment mechanism exists for <Mono>{s.projectId}</Mono> yet. Configuring one names its owner —
                  the person a held payment lands with (every held payment has a reason with an owner).
                </p>
                <button id="mech-create" className="btn inline" onClick={create} style={{ marginTop: 10 }}>
                  Configure the mechanism
                </button>
              </Card>
            ) : (
              <>
                <Card>
                  <RefField label="Test account" hint="your own — never a worker's">
                    <input name="testdest" value={dest} onChange={(e) => setDest(e.target.value)} />
                  </RefField>
                  <RefField label="Amount" hint="KES — small and real">
                    <input name="testamount" value={amount} onChange={(e) => setAmount(e.target.value)} />
                  </RefField>
                  <button id="send-test" className="btn inline" onClick={send} style={{ marginTop: 10 }}>
                    Send the test payment
                  </button>
                </Card>
                {(m.tests || []).length ? (
                  <CardTitled t="Every test, recorded either way">
                    {(m.tests || []).map((t) => (
                      <div className="kv" key={t.id} data-test-result={t.state}>
                        <div className="row">
                          <span className="k">{t.state}</span>
                          <span className="v">
                            {money(t.amountMinor, t.currency)} → <Mono>{t.destination}</Mono> · {when(t.at)}
                            {t.railRef ? <> · rail ref <Mono>{t.railRef}</Mono></> : null}
                            {t.failure ? (
                              <> — {t.failure.explanation} · owner <MonoShort id={t.failure.ownerPartyId} /></>
                            ) : null}
                          </span>
                        </div>
                      </div>
                    ))}
                  </CardTitled>
                ) : (
                  <p className="muted">No test has run yet. Activation is refused until one succeeds.</p>
                )}
              </>
            )}
            <Callout kind="teal" title="The rule this screen sets">
              Proving the whole path, once. A success satisfies the activation condition by act, not assertion; a
              failure is kept on the record with its reason and its owner, because a mechanism that cannot pay you is
              a fact somebody must own.
            </Callout>
            <BackContinue back="/paysetup" next="/mech/recon" />
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_5 · Agree the reconciliation file ───────────────────────────────────
export function MechRecon() {
  const s = useConsole();
  const me = s.me!.partyId;
  const file = useLoad(() => api.getText("payments", "/v1/reconciliation/file"));
  return (
    <MechFrame title="Agree the reconciliation file">
      {(m, reload) => {
        const agree = async () => {
          s.clearErr();
          try {
            await api.post("payments", `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/records`, {
              kind: "reconciliation-agreement",
              actorPartyId: me,
              payload: { format: "crest-recon-csv-v1", cadence: "daily-23:00" },
            });
            reload();
          } catch (e) {
            s.fail(e);
          }
        };
        const rec = recordsOf(m, "reconciliation-agreement")[0];
        return (
          <>
            <Lede>
              The file that makes a mismatch findable: every line ties back to one payment instruction by its id, its
              claim and its rail reference. Columns are append-only — added, never renamed or removed.
            </Lede>
            <Card>
              <RefField label="Format" value="CSV, one row per instruction" hint="contract crest-recon-csv-v1" />
              <RefField label="Cadence" value="Daily, 23:00" hint="programme policy (L2) — recorded with the agreement, not read by the substrate" />
            </Card>
            <LoadFrame r={file}>
              {(f) => (
                <CardTitled t={"The file today — " + (f.format || "crest-recon-csv-v1")}>
                  <div className="tblwrap">
                    <pre id="recon-preview" className="mono" style={{ fontSize: 11.5, whiteSpace: "pre", overflowX: "auto", margin: 0 }}>
                      {f.text.split("\n").slice(0, 12).join("\n")}
                    </pre>
                  </div>
                  <p className="muted" style={{ marginTop: 8 }}>
                    {Math.max(f.text.trim().split("\n").length - 1, 0)} lines, each tied to an instruction id — a
                    mismatch on the rail's side is findable by that id, not by guesswork.
                  </p>
                </CardTitled>
              )}
            </LoadFrame>
            {m.mechanism ? (
              <>
                <RecordedAct rec={rec} missing="Not yet agreed. Activation is refused until this agreement is a record." />
                {!rec ? (
                  <button id="agree-recon" className="btn inline" onClick={agree}>
                    Agree the reconciliation file
                  </button>
                ) : null}
              </>
            ) : (
              <p className="muted">Configure the mechanism first — the agreement is recorded against it.</p>
            )}
            <BackContinue back="/mech/test" next="/mech/batching" />
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_6 · Agree the statement ─────────────────────────────────────────────
// An advisory statement, and an honest limit — stated on the statement
// itself, every time, rather than assumed known.
export function MechStatement() {
  const s = useConsole();
  const me = s.me!.partyId;
  const month = new Date().toISOString().slice(0, 7);
  const stmt = useLoad(() =>
    api.get("payments", `/v1/statements?partyId=${encodeURIComponent(me)}&month=${month}`),
  );
  return (
    <MechFrame title="Agree the statement">
      {(m, reload) => {
        const agree = async () => {
          s.clearErr();
          try {
            await api.post("payments", `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/records`, {
              kind: "statement-agreement",
              actorPartyId: me,
              payload: { covers: "one-calendar-month", issuedOn: "1st-for-the-month-before" },
            });
            reload();
          } catch (e) {
            s.fail(e);
          }
        };
        const rec = recordsOf(m, "statement-agreement")[0];
        return (
          <>
            <Lede>
              A statement, and an honest limit: CREST never holds or moves money, so this statement is advisory and
              says so on itself — the rail's own statement is the authoritative record of what was paid.
            </Lede>
            <Card>
              <RefField label="Statement covers" value="One calendar month" />
              <RefField label="Issued on" value="The 1st, for the month before" />
            </Card>
            <LoadFrame r={stmt}>
              {(st: { limits?: string[]; heldCount?: number; instructions?: unknown[] }) => (
                <CardTitled t={"Your statement for " + month + " — with its limits on it"}>
                  <ul style={{ margin: 0, paddingLeft: 18 }}>
                    {(st.limits || []).map((l) => (
                      <li key={l} data-limit className="body-2" style={{ marginBottom: 6 }}>
                        {l}
                      </li>
                    ))}
                  </ul>
                  <p className="muted" style={{ marginTop: 8 }}>
                    {(st.instructions || []).length} instruction(s) this month for <MonoShort id={me} />, {st.heldCount || 0} held —
                    a held payment appears with its reason and owner; it is never missing.
                  </p>
                </CardTitled>
              )}
            </LoadFrame>
            {m.mechanism ? (
              <>
                <RecordedAct rec={rec} missing="Not yet agreed. The statement agreement is a recorded act — deliberately not an activation gate." />
                {!rec ? (
                  <button id="agree-statement" className="btn inline" onClick={agree}>
                    Agree the statement
                  </button>
                ) : null}
              </>
            ) : (
              <p className="muted">Configure the mechanism first — the agreement is recorded against it.</p>
            )}
            <BackContinue back="/paysetup" next="/mech/batching" />
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_7 · When is a payment raised? ───────────────────────────────────────
// Batching is paid for by the worker, in waiting time. So the choice is a
// record that names who chose and when, and a choice whose trade-off is not
// stated in a sentence is refused by the service.
export function MechBatching() {
  const s = useConsole();
  const me = s.me!.partyId;
  const [batchWin, setBatchWin] = useState("daily-17:00");
  const [tradeoff, setTradeoff] = useState("");
  return (
    <MechFrame title="When is a payment raised?">
      {(m, reload) => {
        const record = async () => {
          s.clearErr();
          try {
            await api.post("payments", `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/records`, {
              kind: "batching-choice",
              actorPartyId: me,
              payload: { window: batchWin.trim(), tradeoff: tradeoff.trim() },
            });
            reload();
          } catch (e) {
            s.fail(e);
          }
        };
        const choices = recordsOf(m, "batching-choice");
        return (
          <>
            <Lede>
              The obligation is raised the moment a confirmation window exits — all four exits, always. What this
              screen chooses is when the rail is asked to move the batch, and that wait lands on the worker.
            </Lede>
            <Card>
              <RefField
                label="Hold payment if a dispute is open"
                value="Never — a dispute contests the record, not the money; all four window exits release payment (W4)"
                hint="the reference frame offers this as a choice; in CREST it is not one, and this deployment says so instead of pretending"
              />
              <RefField label="Manual approval before release" value="Off" hint="no manual approval step exists between a window exit and its instruction" />
              <RefField label="Batching window" hint="programme policy (L2) — the substrate records the choice and does not read the value">
                <input name="batchwindow" value={batchWin} onChange={(e) => setBatchWin(e.target.value)} />
              </RefField>
              <RefField label="The trade-off, in a sentence" hint="who waits, and how long — a choice with the cost unstated is refused">
                <textarea name="batchtradeoff" rows={2} value={tradeoff} onChange={(e) => setTradeoff(e.target.value)} />
              </RefField>
              {m.mechanism ? (
                <button id="record-batching" className="btn inline" onClick={record} style={{ marginTop: 10 }}>
                  Record the batching choice
                </button>
              ) : (
                <p className="muted">Configure the mechanism first.</p>
              )}
            </Card>
            {choices.length ? (
              <CardTitled t="The choice, on the record">
                {choices.map((c) => (
                  <div className="kv" key={c.id} data-batching={c.id}>
                    <div className="row">
                      <span className="k">{String((c.payload || {}).window || "")}</span>
                      <span className="v">
                        chosen by <MonoShort id={c.actorPartyId} /> on {when(c.at)} — “{String((c.payload || {}).tradeoff || "")}”
                      </span>
                    </div>
                  </div>
                ))}
              </CardTitled>
            ) : null}
            <Callout kind="teal" title="The rule this screen sets">
              Batching is paid for by the worker. The choice is legitimate; making it silently is not — so the record
              carries who chose, when, and the trade-off in their own sentence, and a choice that hides the cost is
              refused.
            </Callout>
            <BackContinue back="/mech/recon" next="/mech/activate" />
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_8 · Before payment goes live ────────────────────────────────────────
// Configured is not the same as live. Each condition below is satisfied only
// by a recorded act — never by asserting it — and the refusal returns the
// whole list readable.
export function MechActivate() {
  const s = useConsole();
  const nav = useNavigate();
  const me = s.me!.partyId;
  const [refused, setRefused] = useState<Condition[] | null>(null);
  return (
    <MechFrame title="Before payment goes live">
      {(m, reload) => {
        const activate = async () => {
          s.clearErr();
          setRefused(null);
          try {
            const out = await api.post(
              "payments",
              `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/activate`,
              { activatedByPartyId: me },
            );
            sessionStorage.setItem(
              "crest.console.mechopened",
              JSON.stringify(out.releasedInstructionIds || []),
            );
            nav("/mech/live");
          } catch (e) {
            // A gates_unmet refusal carries the readable condition list; the
            // client surfaces the list, not a bare status code.
            reload();
            try {
              const parsed = JSON.parse(String((e as Error).message || ""));
              if (parsed && parsed.conditions) {
                setRefused(parsed.conditions as Condition[]);
                return;
              }
            } catch {
              /* not the structured refusal */
            }
            if (/gates_unmet|not all satisfied/i.test(errText(e))) {
              setRefused((m.conditions || []).slice());
              return;
            }
            s.fail(e);
          }
        };
        const conds = refused || m.conditions || [];
        return (
          <>
            <Lede>
              Configured is not the same as live. Nothing on this list can be ticked by hand: each condition reads a
              record — a test that ran, an agreement that was made, a choice with a name on it, a verification a
              different person recorded.
            </Lede>
            {m.mechanism ? (
              <Card>
                <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                  {conds.map((c) => (
                    <div key={c.name} data-cond={c.name} data-satisfied={String(c.satisfied)}>
                      <DisLi
                        on={c.satisfied}
                        t={c.name + (c.satisfiedAt ? " — " + when(c.satisfiedAt) : "")}
                        s={c.because || ""}
                      />
                    </div>
                  ))}
                </div>
                {refused ? (
                  <Callout kind="green" title="Refused, readably">
                    The mechanism's activation conditions are not all satisfied. Nothing above is a form to fill —
                    each unmet line names the act that satisfies it.
                  </Callout>
                ) : null}
              </Card>
            ) : (
              <p className="muted">No mechanism exists for this project yet — configure one on the test screen first.</p>
            )}
            <div style={{ display: "flex", gap: 10 }}>
              <button className="btn secondary inline" onClick={() => nav("/mech/batching")}>Back</button>
              {m.mechanism ? (
                <button id="activate-mech" className="btn inline" onClick={activate}>
                  Activate payment
                </button>
              ) : null}
            </div>
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_9 · Before any real money moves ─────────────────────────────────────
// THE INVARIANT SCREEN. The qualification gate — and the activation gate it
// feeds — sits in front of DISBURSEMENT, not in front of confirmation.
export function MechQualify() {
  const s = useConsole();
  const nav = useNavigate();
  const me = s.me!.partyId;
  const [gen, setGen] = useState(0);
  const instr = useLoad<{ instructions: Instruction[] }>(
    () => api.get("payments", "/v1/instructions"),
    [s.projectId, gen],
  );
  return (
    <MechFrame title="Before any real money moves" counter="Payment set up · 3 of 4">
      {(m, reload) => {
        const submit = async () => {
          s.clearErr();
          try {
            await api.post("payments", `/v1/mechanisms/${encodeURIComponent(m.mechanism!.id)}/records`, {
              kind: "qualification-submitted",
              actorPartyId: me,
              payload: { summary: "authority to move real money for this programme" },
            });
            reload();
            setGen((g) => g + 1);
            nav("/mech/live");
          } catch (e) {
            s.fail(e);
          }
        };
        const submitted = recordsOf(m, "qualification-submitted")[0];
        const verified = recordsOf(m, "qualification-verified")[0];
        return (
          <>
            <Lede>
              The organisation's authority to move real money is submitted here and verified by somebody else. What
              this gate holds is disbursement — nothing more.
            </Lede>
            <Card>
              <KVR
                rows={[
                  ["submitted", submitted ? <>by <MonoShort id={submitted.actorPartyId} /> on {when(submitted.at)}</> : "not yet"],
                  ["verified", verified ? <>by <MonoShort id={verified.actorPartyId} /> on {when(verified.at)}</> : "not yet — verification is another party's recorded act, and verifying nothing is refused"],
                ]}
              />
            </Card>
            <Callout kind="teal" title="Why this is the third step and not the first">
              None of this was asked when the rail was connected, and that is deliberate. A partner who turns out not
              to fit can discover it in an afternoon instead of after a document round.
            </Callout>
            <LoadFrame r={instr}>
              {({ instructions }) => {
                const held = (instructions || []).filter(
                  (i) => i.state === "HELD" && i.held && i.held.code === "mechanism_not_live" && i.contextId === s.projectId,
                );
                return (
                  <CardTitled t="The gate sits in front of disbursement, not in front of confirmation">
                    <p className="body-2" style={{ maxWidth: "72ch" }}>
                      Every confirmation-window exit already released its payment obligation — confirm, dispute,
                      auto-confirm, supervisor-assisted, all four. Nothing on this screen stopped one, and nothing
                      here ever can. Only disbursement waits on this gate, and each obligation it is holding is
                      below, with its reason and its owner.
                    </p>
                    {held.length ? (
                      held.map((i) => (
                        <div className="kv" key={i.id} data-heldinstruction={i.id}>
                          <div className="row">
                            <span className="k">
                              <Chip kind="warn" sm>HELD</Chip>
                            </span>
                            <span className="v">
                              <Mono>{i.id}</Mono> · claim <MonoShort id={i.claimId} /> · released by {i.releasedBy} exit on {when(i.releasedAt)} —{" "}
                              {i.held!.explanation} · owner <MonoShort id={i.held!.ownerPartyId} />
                            </span>
                          </div>
                        </div>
                      ))
                    ) : (
                      <p className="muted">
                        Nothing is held under mechanism_not_live for this project right now. When work is confirmed
                        before the mechanism is live, its instruction appears here — held, explained, owned — never
                        missing.
                      </p>
                    )}
                  </CardTitled>
                );
              }}
            </LoadFrame>
            <div style={{ display: "flex", gap: 10 }}>
              <button className="btn secondary inline" onClick={() => nav("/mech/test")}>Back</button>
              {m.mechanism ? (
                <button id="submit-qual" className="btn inline" onClick={submit}>
                  Submit for verification
                </button>
              ) : null}
            </div>
          </>
        );
      }}
    </MechFrame>
  );
}

// ── f2_10 · Verified — real payments can now run ───────────────────────────
// The last gate, and what it opened: activation flips ACTIVE and every
// instruction held under mechanism_not_live is re-priced at its own release
// moment and sent.
export function MechLive() {
  const s = useConsole();
  const nav = useNavigate();
  const opened: string[] = (() => {
    try {
      return JSON.parse(sessionStorage.getItem("crest.console.mechopened") || "[]");
    } catch {
      return [];
    }
  })();
  const instr = useLoad<{ instructions: Instruction[] }>(() => api.get("payments", "/v1/instructions"), [s.projectId]);
  return (
    <MechFrame title="Verified — real payments can now run" counter="Payment set up · 4 of 4">
      {(m) => {
        const verified = recordsOf(m, "qualification-verified")[0];
        return (
          <>
            <Lede>
              Verification is a record with a name on it, and activation is the act it unlocks. Neither is a switch
              somebody flipped quietly.
            </Lede>
            <Card>
              <KVR
                rows={[
                  ["qualification", verified ? <>verified by <MonoShort id={verified.actorPartyId} /> on {when(verified.at)}</> : "not verified yet"],
                  ["mechanism", m.mechanism ? <><Mono>{m.mechanism.id}</Mono> · {m.mechanism.state}</> : "not configured"],
                  m.mechanism && m.mechanism.activatedAt
                    ? ["went live", <>by <MonoShort id={m.mechanism.activatedBy} /> on {when(m.mechanism.activatedAt)}</>]
                    : null,
                ]}
              />
            </Card>
            {m.standing === "live" ? (
              <LoadFrame r={instr}>
                {({ instructions }) => {
                  const mine = (instructions || []).filter((i) => i.contextId === s.projectId);
                  const released = mine.filter((i) => i.state !== "HELD");
                  return (
                    <CardTitled t="What the last gate opened">
                      <p className="body-2">
                        The obligations this gate was holding were re-priced at their own release moment — the rate
                        version in force when the work's window exited, never today's — and sent.
                      </p>
                      {released.length ? (
                        released.map((i) => (
                          <div className="kv" key={i.id} data-released={i.id}>
                            <div className="row">
                              <span className="k">
                                <Chip kind="ok" sm>{i.state}</Chip>
                              </span>
                              <span className="v">
                                <Mono>{i.id}</Mono> · {money(i.amountMinor, i.currency)} · released by {i.releasedBy} exit on {when(i.releasedAt)}
                                {opened.includes(i.id) ? " · opened by this activation" : ""}
                              </span>
                            </div>
                          </div>
                        ))
                      ) : (
                        <p className="muted">No instruction has been raised in this project yet.</p>
                      )}
                    </CardTitled>
                  );
                }}
              </LoadFrame>
            ) : (
              <OpenNote>
                The mechanism is not live yet. Go live runs the activation gate; what it opens will be listed here —
                every held instruction re-priced and released, none quietly dropped.
              </OpenNote>
            )}
            <Callout kind="grey" title="What is not watched">
              Nothing re-checks this. If the organisation changes bank, or the authorised officer leaves, the
              verification stands until somebody notices. That is a gap, not a design.
            </Callout>
            <div style={{ display: "flex", gap: 10 }}>
              <button className="btn secondary inline" onClick={() => nav("/mech/recon")}>
                Reconciliation schedule
              </button>
              <button id="golive" className="btn inline" onClick={() => nav("/mech/activate")}>
                Go live
              </button>
            </div>
          </>
        );
      }}
    </MechFrame>
  );
}
