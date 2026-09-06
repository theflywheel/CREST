// The Definition Approver's surface (reference p3_15, p3_16) and the record
// the signature produces.
//
// The approver ratifies what an author drafted and may never draft it. That is
// not a UI convention here: the definitions service refuses a version whose
// ratifier is its author (409 `self_ratified`), and this console's author
// persona is a different party from its approver persona, so the refusal is
// load-bearing rather than decorative. The authoring wizard is not in this
// session's navigation at all, and the route guard turns away a hand-typed
// #/define/* .
//
// Two things this pair has to get right:
//
//   * **Ratification with pending fields is a real recorded state.** The
//     reference's "Wait for the delegates" is not the only honest answer to an
//     incomplete definition: an approver may judge that what is still open is
//     acceptable to leave open, and say so. `pendingFields` is that
//     declaration, and only the ratifier may make it — recording it under the
//     author's hand would put the approver's name on a list they never saw.
//     Ratifying with nothing pending is equally real, and the two are
//     distinguishable afterwards: absence is not an empty list.
//
//   * **Two records, one signature (p3_16).** Ratifying changes the version's
//     state *and* appends a governance event naming the actor, in one
//     transaction. So the signature either produced both records or neither,
//     and the event log — not this screen — is where a dispute about a
//     ratification goes.
//
// Invariant: definitions sit under evidence and payments, and the rule in play
// is that a version is immutable once it exists. Ratifying does not rewrite the
// document; it records a decision about it. A credential pinned to v1 resolves
// to the same v1 afterwards, which is what makes activating a version safe for
// claims already made against earlier ones.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "@crest/api";
import { Callout, Chip, GridTable, NextBlock, OpenNote, RefField, Sidecar } from "@crest/ui";
import { Card, CardTitled, Empty, KVR, Lede, LoadFrame, Mono, MonoShort, Title, useLoad, when } from "../ui";
import { useConsole } from "../state";
import { currentDraftId } from "./Define";

type Draft = {
  id: string; definitionId?: string; state: string; submittedVersion?: number;
  createdByPartyId: string; updatedAt: string;
  doc: { activity?: { label?: string; outcomeUnit?: string }; sources?: { connections?: Array<{ credentialRef?: string; systemRef?: string }> } };
};
type Definition = {
  id: string; version: number; state: string;
  activity?: { code?: string; label?: string };
  outcomeUnit?: string;
  authoredByPartyId?: string;
  ratifiedByPartyId?: string;
  pendingFields?: string[];
  activatedAt?: string;
  faces?: { worker?: { summary?: string; tierCeiling?: number } };
};
type DefEvent = {
  id: number; version: number; action: string; actorPartyId: string; at: string;
  detail?: Record<string, unknown>;
};
type LinkedRec = { id: string; type: string; payload?: Record<string, unknown> };

const defpath = (id: string, tail = "") => `/v1/definitions/${encodeURIComponent(id)}${tail}`;

// Which version this session most recently signed, so p3_16 can report the act
// it just performed rather than guessing at the newest thing in the service.
const SIGNED_KEY = "crest.console.signed";
const readSigned = (): { id: string; version: number } | null => {
  try {
    const raw = sessionStorage.getItem(SIGNED_KEY);
    if (!raw) return null;
    const [id, v] = raw.split("@@");
    return id ? { id, version: Number(v) || 0 } : null;
  } catch {
    return null;
  }
};
const writeSigned = (id: string, version: number) => {
  try {
    sessionStorage.setItem(SIGNED_KEY, `${id}@@${version}`);
  } catch {
    /* the screen falls back to reading the draft */
  }
};

// ── p3_15 · review and sign ────────────────────────────────────────────────
export function Ratify() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const [chosen, setChosen] = useState("");
  const [pending, setPending] = useState<string[]>([]);
  const [extra, setExtra] = useState("");
  const me = s.me!.partyId;

  const r = useLoad(async () => {
    // The queue: drafts that were submitted, each naming the version it became.
    const drafts = ((await api.get("definitions", "/v1/definition-drafts?state=SUBMITTED")).drafts || []) as Draft[];
    const withDefs = await Promise.all(
      drafts
        .filter((d) => d.definitionId && d.submittedVersion)
        .slice(0, 12)
        .map(async (d) => {
          const def: Definition | null = await api
            .get("definitions", defpath(d.definitionId!, `?version=${d.submittedVersion}`))
            .catch(() => null);
          const lr = await api
            .get("definitions", defpath(d.definitionId!, "/linked-records"))
            .then((x) => (x.linkedRecords || []) as LinkedRec[])
            .catch(() => [] as LinkedRec[]);
          return { draft: d, def, linked: lr };
        }),
    );
    return { queue: withDefs };
  }, [gen]);

  return (
    <LoadFrame r={r}>
      {({ queue }) => {
        const awaiting = queue.filter((q) => q.def && q.def.state === "DRAFT");
        const pick = chosen ? queue.find((q) => q.draft.id === chosen) : awaiting[0];
        const def = pick?.def || null;
        // Candidate pending fields, derived from the real record rather than
        // offered as a list somebody typed. Each is a thing genuinely still
        // open about a version that is otherwise complete enough to submit.
        const candidates: string[] = [];
        if (pick) {
          if (!pick.linked.some((x) => x.type === "payment-setup"))
            candidates.push("ratePerOutcomeUnit — nothing prices this unit yet");
          const conns = pick.draft.doc.sources?.connections || [];
          if (conns.length && !conns.some((c) => c.credentialRef))
            candidates.push("sources.credentialRef — the connection is described but not credentialled");
          if (!pick.linked.some((x) => x.type === "payment-structure"))
            candidates.push("payment structure — no tranches, preconditions or deductions were recorded");
        }
        const selfRatify = !!def && def.authoredByPartyId === me;
        const sign = async () => {
          if (!def) return;
          const fields = [...pending, ...extra.split(",").map((f) => f.trim()).filter(Boolean)];
          await api.post("definitions", defpath(def.id, `/versions/${def.version}/ratify`), {
            ratifiedByPartyId: me,
            ...(fields.length ? { pendingFields: fields } : {}),
            publish: true,
          });
          writeSigned(def.id, def.version);
          setGen((g) => g + 1);
          nav("/ratified");
        };
        return (
          <>
            <Title
              t="Review and sign"
              extra={
                awaiting.length ? (
                  <Chip kind="warn">{awaiting.length} awaiting ratification</Chip>
                ) : (
                  <Chip kind="plain">nothing awaiting</Chip>
                )
              }
            />
            <Lede>
              Ratification is a signature over what the author drafted, exactly as drafted. An approver who could
              edit while approving would be an author with a second hat — so this session reads everything and writes
              only one thing: the decision.
            </Lede>

            <CardTitled t="Awaiting your signature" chip={<Chip kind="info">{queue.length} submitted</Chip>}>
              <GridTable cols="1.4fr 1fr .8fr 1fr .9fr" head={["Definition", "Author", "Version", "State", ""]}>
                {queue.length ? (
                  queue.map((q) => (
                    <div className="g-row" key={q.draft.id}>
                      <span>
                        <MonoShort id={q.draft.definitionId || ""} />
                        <div style={{ fontSize: 12, color: "var(--text-2)" }}>
                          {q.draft.doc.activity?.label || q.def?.activity?.label || "—"}
                        </div>
                      </span>
                      <span>
                        <MonoShort id={q.draft.createdByPartyId} />
                      </span>
                      <span>v{q.draft.submittedVersion}</span>
                      <span>
                        {q.def ? (
                          <Chip kind={q.def.state === "ACTIVE" ? "ok" : q.def.state === "RATIFIED" ? "info" : "warn"}>
                            {q.def.state}
                          </Chip>
                        ) : (
                          <Chip kind="err">unreadable</Chip>
                        )}
                      </span>
                      <span>
                        {q.def && q.def.state === "DRAFT" ? (
                          <button
                            className={"btn" + (pick?.draft.id === q.draft.id ? "" : " secondary")}
                            data-review={q.draft.id}
                            style={{ width: "auto", padding: "5px 11px", fontSize: 12 }}
                            onClick={() => {
                              setChosen(q.draft.id);
                              setPending([]);
                            }}
                          >
                            Review
                          </button>
                        ) : (
                          <span className="muted" style={{ fontSize: 12 }}>
                            {q.def?.ratifiedByPartyId ? "already signed" : "—"}
                          </span>
                        )}
                      </span>
                    </div>
                  ))
                ) : (
                  <div className="g-row">
                    <span style={{ gridColumn: "1 / -1", color: "var(--text-2)" }}>
                      Nothing has been submitted for ratification. An empty queue is the absence of submitted drafts,
                      not a screen that failed to load.
                    </span>
                  </div>
                )}
              </GridTable>
            </CardTitled>

            {def ? (
              <>
                <CardTitled t="What you would be signing" chip={<Chip kind="warn">{def.state}</Chip>} >
                  <div id="ratify-read">
                    <KVR
                      rows={[
                        ["definition", <Mono>{def.id}</Mono>],
                        ["version", "v" + def.version],
                        ["activity", def.activity?.label || def.activity?.code || "—"],
                        ["unit of work", <Mono>{def.outcomeUnit || "—"}</Mono>],
                        ["tier ceiling", def.faces?.worker?.tierCeiling != null ? "Tier " + def.faces.worker.tierCeiling : "—"],
                        ["drafted by", <MonoShort id={def.authoredByPartyId || ""} />],
                        ["you", <MonoShort id={me} />],
                      ]}
                    />
                  </div>
                  {def.faces?.worker?.summary ? (
                    <div className="consent-quote" style={{ marginTop: 10 }}>
                      {def.faces.worker.summary}
                    </div>
                  ) : null}
                  <p className="muted" style={{ marginTop: 8 }}>
                    Read the worker's own sentence last and on purpose: it is the only part of this record the person
                    whose pay depends on it will ever see.
                  </p>
                </CardTitled>

                {selfRatify ? (
                  <OpenNote>
                    <b>You drafted this, so you cannot sign it.</b> The service refuses a version whose ratifier is
                    its author with 409 <span className="mono">self_ratified</span>, and it is right to: a signature
                    that only says "I approve of my own work" records nothing anybody downstream can rely on. Someone
                    else has to sign this one.
                  </OpenNote>
                ) : null}

                <CardTitled
                  t="What stays pending"
                  chip={pending.length || extra.trim() ? <Chip kind="warn">named</Chip> : <Chip kind="ok">nothing pending</Chip>}
                >
                  <p className="muted" style={{ marginBottom: 10 }}>
                    An approver has two honest answers to an incomplete definition, and waiting is only one of them.
                    The other is to sign it and say what is still open — that is a judgement, it is yours, and
                    recording it is what makes it reviewable later.
                  </p>
                  {candidates.length ? (
                    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                      {candidates.map((c) => (
                        <label key={c} style={{ display: "flex", gap: 9, alignItems: "flex-start", font: "400 13px/1.45 Roboto" }}>
                          <input
                            type="checkbox"
                            data-pending={c.split(" ")[0]}
                            checked={pending.includes(c)}
                            onChange={(e) => setPending(e.target.checked ? [...pending, c] : pending.filter((x) => x !== c))}
                          />
                          <span>{c}</span>
                        </label>
                      ))}
                    </div>
                  ) : (
                    <Empty>
                      Nothing about this version reads as still open: it has a rate, a credentialled connection and a
                      payment structure. Signing it with no pending fields is the accurate answer.
                    </Empty>
                  )}
                  <div style={{ marginTop: 12 }}>
                    <RefField
                      label="Anything else, in your own words"
                      hint="Comma-separated. Recorded on the version and in the event log, under your name."
                    >
                      <input
                        name="pendingextra"
                        value={extra}
                        onChange={(e) => setExtra(e.target.value)}
                        placeholder="the district office has not confirmed the sampling rate"
                      />
                    </RefField>
                  </div>
                </CardTitled>

                <div className="btn-row" style={{ maxWidth: 620 }}>
                  <button className="btn secondary" data-btn="Wait for the delegates" onClick={() => nav("/definework")}>
                    Wait for the delegates
                  </button>
                  <button
                    className="btn"
                    data-btn="Sign and publish"
                    disabled={selfRatify}
                    onClick={async () => {
                      s.clearErr();
                      try {
                        await sign();
                      } catch (e) {
                        s.fail(e);
                      }
                    }}
                  >
                    Sign and publish
                  </button>
                </div>
                <Callout kind="teal" title="What the signature actually produces">
                  Two records, in one transaction: the version moves out of DRAFT carrying your name as its ratifier,
                  and the definition's append-only event log gains a RATIFIED row with your party id, the timestamp
                  and any pending fields you named. Neither exists without the other, so there is no state in which a
                  version claims a ratifier the log cannot account for.
                </Callout>
                <Sidecar>
                  Signing here also activates the version and enqueues its publication, so a verifier can resolve it.
                  The reference draws that as one act — "Sign and publish" — and it is one act in the service too.
                </Sidecar>
              </>
            ) : queue.length ? (
              <Empty>
                Everything submitted has already been signed. Pick a row above to read what was signed and by whom.
              </Empty>
            ) : null}

            <Callout kind="green" title="What this session can never do">
              Draft. There is no authoring screen in this navigation, the route guard turns away a hand-typed
              wizard URL, and the service would refuse the signature anyway if the author and the ratifier were the
              same party. Three independent mechanisms, because separation of duties is the only rule the definitions
              service exists to enforce.
            </Callout>
          </>
        );
      }}
    </LoadFrame>
  );
}

// ── p3_16 · two records, one signature ─────────────────────────────────────
// The reference draws this as a narrow panel rather than a console frame, so
// it renders in the panel shell. What it shows is the event log: the second
// record the signature produced, read back for real.
export function Ratified() {
  const nav = useNavigate();
  const signed = readSigned();
  const draftId = currentDraftId();
  const r = useLoad(async () => {
    let id = signed?.id || "";
    let version = signed?.version || 0;
    if (!id && draftId) {
      // The author's own path here, after their submit: the draft names the
      // version it became.
      const d: Draft = await api.get("definitions", `/v1/definition-drafts/${encodeURIComponent(draftId)}`);
      id = d.definitionId || "";
      version = d.submittedVersion || 0;
    }
    if (!id) return { def: null as Definition | null, events: [] as DefEvent[] };
    const [def, ev] = await Promise.all([
      api.get("definitions", defpath(id, `?version=${version}`)).catch(() => null),
      api.get("definitions", defpath(id, "/events")).catch(() => ({ events: [] })),
    ]);
    return { def: def as Definition | null, events: (ev.events || []) as DefEvent[] };
  }, [signed?.id, signed?.version, draftId]);

  return (
    <LoadFrame r={r}>
      {({ def, events }) => {
        if (!def)
          return (
            <div className="panel-shell">
              <Title t="Nothing has been signed in this session" />
              <Empty>
                This screen reports an act, and no act has been performed here. It is not a status page for the
                service — there is nothing to read until a version is signed or submitted in this session.
              </Empty>
              <button className="btn" onClick={() => nav("/definework")}>
                Back to definitions
              </button>
            </div>
          );
        const mine = events.filter((e) => e.version === def.version);
        const ratified = mine.find((e) => e.action === "RATIFIED");
        const pendingNamed = (ratified?.detail?.pendingFields as string[]) || def.pendingFields || [];
        return (
          <div className="panel-shell" style={{ maxWidth: 560 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h2 className="scr-title m" data-ratified-title>
                {def.id.replace(/^crest:definition:/, "")} v{def.version} is {def.state.toLowerCase()}
              </h2>
              <Chip kind={def.state === "ACTIVE" ? "ok" : "info"}>{def.state}</Chip>
            </div>
            <p className="muted" style={{ fontSize: 13 }}>
              One signature, two records. The version below carries who ratified it; the trail under it is the
              append-only account of how it got here, and the two were written in the same transaction.
            </p>
            <Card>
              <KVR
                rows={[
                  ["activity", def.activity?.label || def.activity?.code || "—"],
                  ["unit of work", <Mono>{def.outcomeUnit || "—"}</Mono>],
                  ["authored by", <MonoShort id={def.authoredByPartyId || ""} />],
                  ["ratified by", <MonoShort id={def.ratifiedByPartyId || ""} />],
                  ["activated", when(def.activatedAt)],
                ]}
              />
            </Card>
            {pendingNamed.length ? (
              <div className="callout teal" data-callout="teal">
                <div className="c-title">Ratified, with these left open</div>
                <div className="c-body">
                  <ul style={{ margin: "4px 0 0 16px", padding: 0 }}>
                    {pendingNamed.map((f) => (
                      <li key={f} data-pendingfield={f}>
                        {f}
                      </li>
                    ))}
                  </ul>
                  <p style={{ marginTop: 7 }}>
                    The ratifier named these, not the author. RATIFIED-with-pending is a real state rather than a
                    blocked one: the definition is active and usable, and what is unfinished about it is written down
                    with somebody's name against it.
                  </p>
                </div>
              </div>
            ) : (
              <div className="callout green" data-callout="green">
                <div className="c-title">Ratified with nothing pending</div>
                <div className="c-body" data-nothing-pending>
                  The ratifier named no open fields. That is a different record from a ratification with an empty
                  list — the absence of a declaration is not a declaration of nothing, and the event log shows which
                  of the two happened.
                </div>
              </div>
            )}
            <CardTitled t="The trail" chip={<Chip kind="plain">{mine.length} acts</Chip>}>
              <GridTable cols=".9fr 1fr .9fr" head={["Act", "Actor", "When"]}>
                {mine.length ? (
                  mine.map((e) => (
                    <div className="g-row" key={e.id} data-event={e.action}>
                      <span>
                        <Chip kind={e.action === "RATIFIED" ? "ok" : "plain"} sm>
                          {e.action}
                        </Chip>
                      </span>
                      <span>
                        <MonoShort id={e.actorPartyId} />
                      </span>
                      <span style={{ fontSize: 12.3 }}>{new Date(e.at).toLocaleString()}</span>
                    </div>
                  ))
                ) : (
                  <div className="g-row">
                    <span style={{ gridColumn: "1 / -1", color: "var(--text-2)" }}>
                      No events on this version. For a version that reached ACTIVE that would be a defect, not a
                      quiet state.
                    </span>
                  </div>
                )}
              </GridTable>
              <p className="muted" style={{ marginTop: 8 }}>
                Every row names an actor. This is where a dispute about a ratification goes — not to the document,
                which only shows the current state, but here, which shows who decided it and when.
              </p>
            </CardTitled>
            <NextBlock
              happened={`v${def.version} is ${def.state.toLowerCase()}, and its publication is enqueued so a verifier can resolve it.`}
              who="Whoever owns the rate. The definition is signed, and nothing prices the unit it declares yet."
              when="As soon as they are assigned; evidence can be recorded against this version in the meantime."
              told="The definition's event log carries every act on it, including the pricing handoff when it happens."
              ifnot="The definition stays active and unpriced. Work is recognised and recorded; no payment obligation can be computed until a rate exists, which the handoff screen exists to chase rather than leave silent."
            />
            <div className="btn-row">
              <button className="btn secondary" data-btn="See the record itself" onClick={() => nav("/define/anatomy")}>
                See the record itself
              </button>
              <button className="btn" data-btn="Back to definitions" onClick={() => nav("/definework")}>
                Back to definitions
              </button>
            </div>
            <button className="btn secondary" data-btn="Hand pricing to a rate owner" onClick={() => nav("/handoff")}>
              Hand pricing to a rate owner
            </button>
          </div>
        );
      }}
    </LoadFrame>
  );
}
