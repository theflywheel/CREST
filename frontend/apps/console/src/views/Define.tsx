// The definition-authoring wizard, P-3 (reference p3_1–p3_28 and p3_pay).
//
// The largest screens build in the project, and the one with the most ways to
// go quietly wrong, so the rules it runs on are written down here rather than
// rediscovered per screen.
//
// ── What this wizard is ────────────────────────────────────────────────────
// A work definition is the object credentials pin and verifiers resolve. It is
// immutable per version. The wizard therefore never edits a definition: it
// edits a `definition_draft` — the one mutable object in the definitions
// service — and a single server-side `compile()` turns that draft into the
// next immutable version at submit. Every Continue on every screen is a real
// `PUT /v1/definition-drafts/{id}/sections/{section}`, whole-section, because
// that is what a Continue means: the screen's state, as the author left it.
//
// ── The invariant these screens sit under ──────────────────────────────────
// Definitions sit *under* evidence and payments: a credential names a
// definition version, and a rate prices the unit a definition declares. So the
// rule this wave could break is **trust strength is derived, never stored**.
// It does not, and three screens are where that is visible rather than merely
// claimed:
//
//   * p3_9 (Evidence) records a tier *map* — rules over `sourceClass` and
//     `captureMethod`, which are provenance facts — plus a ceiling. No screen
//     writes a tier onto anything.
//   * p3_22 (Source) shows the ceiling a source class *implies* and labels it
//     derived: it is computed here, for display, from the draft's own L2 tier
//     map. Nothing stores the number.
//   * p3_27 (Dry run) proves it: the real `strength.Evaluate` judges real
//     parsed rows and returns a tier with its reasons, committing nothing.
//
// The second invariant in play is **version immutability protects claims
// already made**. Submit only ever appends version n+1; a credential pinned to
// v1 keeps resolving against v1 forever. p3_28 is the screen that states the
// consequence — a source is registered against exactly one version — and the
// refusal path (a submitted draft refuses further edits) is what makes it true.
//
// ── Layering ───────────────────────────────────────────────────────────────
// Sector, category, performer role, check intensity, validation posture,
// frequency, aggregation level, tranche labels and shares: all L2. Two
// deployments could reasonably disagree about every one of those words and
// both still be CREST, so none of them is an enum in the infrastructure. Where
// the deployment declares a vocabulary this renders it; where it declares none
// the screen says so and takes a typed answer — the g2/J3 composition
// precedent (views/J3.tsx Compose).
//
// ── Where these screens depart from the reference, and why ─────────────────
// Named here rather than buried, because a silent deviation is the failure
// mode this project cannot afford:
//
//  1. The reference's frames show "Dr. Alice Mutua"; this console's author
//     persona is Amina Yusuf (state.tsx). Personas are ours.
//  2. The reference's rail for these frames is "Define work · Projects ·
//     Payment set up · Roles & invites · Templates". Four of those five are
//     real author screens here; "Projects" belongs to the J3 console section
//     and is not faked into the author's session.
//  3. The reference's example version numbers (v1.2 → v1.3) are replaced by
//     the draft's real version arithmetic. The sentences are the reference's.
//  4. The reference's tier vocabulary is inverted relative to CREST's: its
//     "Tier 1 — outcome-linked" is its strongest, while CREST numbers the
//     strongest 3. p3_9 renders CREST's numbering and says so on its face,
//     because a definition whose ceiling means the opposite of what the author
//     read would cap every worker's evidence at the wrong end.
//  5. The reference's own button graph leaves p3_13 unreachable (nothing
//     navigates to it) and sends p3_21 onward to the time-based branch even on
//     the event branch. Both are noted where they occur and one extra,
//     labelled link exists so the wizard is walkable end to end.
//
// ── One structural note about this file ────────────────────────────────────
// Every screen is a pair: an exported gate that loads the draft, and a body
// component that renders it. The bodies are components rather than callbacks
// because the gate's loader returns early while it waits, and a callback
// holding hooks would change the hook count between renders. Bodies therefore
// receive the draft as a prop and own their own state.
import { useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { api, FIX, services } from "@crest/api";
import {
  Callout, Chip, GridTable, NextBlock, OpenNote, OptionCard, ProgressBar, RefField, Sidecar, Stat, StepCounter,
  WizardContent,
} from "@crest/ui";
import { Card, CardTitled, Empty, KVR, Lede, LoadFrame, Mono, MonoShort, Title, useLoad, when } from "../ui";
import { useConsole } from "../state";

// ── the draft, as the definitions service returns it ───────────────────────
type Counting = {
  basis?: "event" | "time-period" | "outcome";
  frequency?: string;
  aggregationLevel?: string;
  description?: string;
  outcome?: { indicator?: string; baseline?: string; target?: string; measuredBy?: string };
};
type TierRule = {
  tier: number;
  sourceClassIn?: string[];
  captureMethodIn?: string[];
  requiresFields?: string[];
  minIdentityAssurance?: string;
};
type Connection = {
  systemRef: string;
  adapterRef?: string;
  endpoint?: string;
  credentialRef?: string;
  mapping?: { columns?: Record<string, string>; enrichment?: Record<string, string>; constants?: Record<string, string> };
  settings?: Record<string, string>;
};
type Doc = {
  scope?: { sector?: string; category?: string };
  activity?: { code?: string; label?: string; skillCode?: string; outcomeUnit?: string; counting?: Counting };
  parties?: { performerRole?: string; partyType?: string; attesterFunctions?: string[] };
  evidence?: {
    summary?: string; evidenceInPlainLanguage?: string[]; tierCeiling?: number;
    checkIntensity?: string; tierMap?: TierRule[];
  };
  validation?: { authorisedIssuers?: string[]; specifierPartyId?: string; posture?: string; delayDays?: number; decidedBy?: string };
  sources?: { sourceSystems?: string[]; requiredFields?: string[]; schemaRef?: string; connections?: Connection[] };
  cascade?: { roleLevel?: string; trainedByDefinitionId?: string; trainedByVersion?: number };
  extensions?: Record<string, { label: string; valueType: string; value: string }>;
  payment?: {
    roles?: Record<string, string>;
    tranches?: Array<{ label: string; share?: string; condition?: string }>;
    preconditions?: string[];
    deductions?: Array<{ label: string; rule: string }>;
  };
};
type Draft = {
  id: string; definitionId?: string; baseVersion?: number; state: string; doc: Doc;
  createdByPartyId: string; createdAt: string; updatedAt: string; submittedVersion?: number;
};
type Problem = { section: string; field?: string; reason: string };
type Validation = { ready: boolean; problems: Problem[]; preview: Record<string, unknown> };
type Adaptor = { ref?: string; class: string; status: string; note?: string };
type Template = {
  definitionId: string; version: number; activity: string; filename: string;
  columns: string[]; requiredEnrichment: string[];
};
type DefEvent = { id: number; version: number; action: string; actorPartyID?: string; actorPartyId: string; at: string; detail?: Record<string, unknown> };
type LinkedRec = { id: string; type: string; payload?: Record<string, unknown>; createdAt: string };

const DRAFTS = "/v1/definition-drafts";
const dpath = (id: string, tail = "") => `${DRAFTS}/${encodeURIComponent(id)}${tail}`;
const defpath = (id: string, tail = "") => `/v1/definitions/${encodeURIComponent(id)}${tail}`;

// Which draft this session is authoring. Held in sessionStorage rather than in
// React state because every route remounts (App.tsx keys the screen on the
// path): the draft id is the only thing the wizard carries across screens, and
// everything else is re-read from the service, which is the point — the draft
// on the server is the state, not anything this browser remembers.
const DRAFT_KEY = "crest.console.draftid";
export const currentDraftId = (): string => {
  try {
    return sessionStorage.getItem(DRAFT_KEY) || "";
  } catch {
    return "";
  }
};
export const setCurrentDraftId = (id: string) => {
  try {
    if (id) sessionStorage.setItem(DRAFT_KEY, id);
    else sessionStorage.removeItem(DRAFT_KEY);
  } catch {
    /* a console with no session storage still authors; it just cannot resume */
  }
};

// ── shared wizard chrome ───────────────────────────────────────────────────
type Btn = {
  label: string;
  to?: string;
  onClick?: () => void | Promise<unknown>;
  role?: "primary" | "secondary";
  // note renders an honest on-screen gap under the row: a reference button
  // that cannot do what the reference implies says why, rather than looking
  // live and doing nothing.
  note?: ReactNode;
};

function Buttons(props: { btns: Btn[] }) {
  const nav = useNavigate();
  const s = useConsole();
  const [busy, setBusy] = useState("");
  const notes = props.btns.filter((b) => b.note);
  return (
    <>
      <div className="btn-row" style={{ maxWidth: 780, flexWrap: "wrap" }}>
        {props.btns.map((b) => (
          <button
            key={b.label}
            data-btn={b.label}
            className={"btn" + (b.role === "primary" ? "" : " secondary")}
            disabled={busy === b.label}
            onClick={async () => {
              s.clearErr();
              setBusy(b.label);
              try {
                if (b.onClick) await b.onClick();
                if (b.to) nav(b.to);
              } catch (e) {
                s.fail(e);
              } finally {
                setBusy("");
              }
            }}
          >
            {busy === b.label ? "Working…" : b.label}
          </button>
        ))}
      </div>
      {notes.map((b, i) => (
        <OpenNote key={i}>{b.note}</OpenNote>
      ))}
    </>
  );
}

// counterProgress reads the "N of M" a wizard counter already carries
// ("Sector · 1 of 9") so the progress track can share it rather than every
// screen threading a second step/total pair through.
const counterProgress = (counter?: string): { value: number; max: number } | null => {
  const m = counter?.match(/(\d+)\s+of\s+(\d+)/i);
  return m ? { value: Number(m[1]), max: Number(m[2]) } : null;
};

// Frame draws one reference wizard frame: the progress track and step
// counter above the title, the title, and the frame's own buttons at the
// foot — all inside the reference's ~820px wizard content pane.
function Frame(props: {
  counter?: string; title: string; chip?: ReactNode; lede?: ReactNode;
  children: ReactNode; btns: Btn[];
  // wide opts a caller out of the ~820px wizard content pane. Only the
  // registry front door (/definework, a list screen, not a wizard step)
  // uses it — every gated /define/* step keeps the reference's narrower
  // wizard density.
  wide?: boolean;
}) {
  const progress = counterProgress(props.counter);
  const body = (
    <>
      {progress ? (
        <ProgressBar value={progress.value} max={progress.max} label={<StepCounter>{props.counter}</StepCounter>} />
      ) : props.counter ? (
        <StepCounter>{props.counter}</StepCounter>
      ) : null}
      <Title t={props.title} extra={props.chip} />
      {props.lede ? <Lede>{props.lede}</Lede> : null}
      {props.children}
      <Buttons btns={props.btns} />
    </>
  );
  return props.wide ? body : <WizardContent>{body}</WizardContent>;
}

type BodyProps = { d: Draft; reload: () => void };

// gate wraps a screen body in the draft load. A screen with no draft is not an
// error state and not an empty form: it is an author who has not started, so
// it says so and points at the registry.
function gate(Body: (p: BodyProps) => ReactNode) {
  return function Gated() {
    const [gen, setGen] = useState(0);
    const id = currentDraftId();
    const r = useLoad<Draft | null>(
      () => (id ? api.get("definitions", dpath(id)) : Promise.resolve(null)),
      [id, gen],
    );
    if (!id)
      return (
        <>
          <Title t="No draft is open" />
          <Empty>
            This session is not authoring a definition yet. A wizard screen edits a real draft in the definitions
            service, so there is nothing here to fill in until one exists — start or clone one from the definition
            registry.
          </Empty>
          <Buttons btns={[{ label: "Go to the registry", to: "/definework", role: "primary" }]} />
        </>
      );
    return (
      <LoadFrame r={r}>
        {(d) => (d ? <Body d={d} reload={() => setGen((g) => g + 1)} /> : null)}
      </LoadFrame>
    );
  };
}

// save writes one whole section and returns the re-read draft. Whole-section
// by contract, so a screen that owns part of a section merges over what the
// draft already holds rather than blanking its sibling screen's answers.
const save = (id: string, section: string, body: unknown): Promise<Draft> =>
  api.put("definitions", dpath(id, `/sections/${section}`), body);

const stateChip = (d: Draft) =>
  d.state === "OPEN" ? (
    <Chip kind="info">draft · open</Chip>
  ) : d.state === "SUBMITTED" ? (
    <Chip kind="ok">submitted as v{d.submittedVersion}</Chip>
  ) : (
    <Chip kind="plain">{d.state.toLowerCase()}</Chip>
  );

// The refusal every screen past submit has to state rather than discover: a
// SUBMITTED draft is the provenance of a version that exists, and editing it
// would edit that provenance.
const ClosedNote = (p: { d: Draft }) =>
  p.d.state === "OPEN" ? null : (
    <OpenNote>
      <b>This draft is closed and its sections refuse writes.</b> It was submitted as v{p.d.submittedVersion}, and
      that version is immutable — a credential pinned to it must resolve to the same document forever. Editing the
      draft would edit the provenance of a record that already exists, so the service refuses it (409
      <span className="mono"> draft_closed</span>). To change the definition, clone it into a new draft and submit
      v{(p.d.submittedVersion || 0) + 1}.
    </OpenNote>
  );

// The L2 vocabulary read. Two places a deployment can declare its words, both
// its own: the project-creation payload (configuration.definitionVocabulary)
// and — the self-serve path — the definition-vocabulary composition choice the
// configurator writes through /vocabulary. The composition record wins because
// it is the one with a decider and a timestamp attached, and it is the one
// that can change after the project exists. Where neither declares anything,
// the screen says why there is no list and takes a typed answer (the Compose
// precedent).
const useVocab = (projectId: string) =>
  useLoad<Record<string, string[]>>(async () => {
    const pp = `/v1/projects/${encodeURIComponent(projectId)}`;
    const [p, comp] = await Promise.all([
      api.get("parties", pp).catch(() => null),
      api.get("parties", `${pp}/composition`).catch(() => ({ choices: [] })),
    ]);
    const cfg = p ? (((p.project || p).configuration || {}) as Record<string, unknown>) : {};
    const base = (cfg.definitionVocabulary || {}) as Record<string, string[]>;
    const rec = ((comp.choices || []) as Array<{ kind?: string; payload?: { value?: unknown } }>).find(
      (c) => (c.kind || "").replace(/^composition:/, "") === "definition-vocabulary",
    );
    const declared = ((rec?.payload as { value?: unknown } | undefined)?.value || {}) as Record<string, string[]>;
    return { ...base, ...declared };
  }, [projectId]);

function Vocab(props: {
  label: string; hint?: ReactNode; declared?: string[]; value: string;
  onChange: (v: string) => void; placeholder?: string; name: string;
}) {
  const has = props.declared && props.declared.length > 0;
  return (
    <>
      <RefField label={props.label} hint={props.hint}>
        {has ? (
          // The reference's card grid. A declared entry is "value" or
          // "value | descriptor": the value is what the definition records,
          // the descriptor only helps the author choose.
          <div className="vocab-cards" data-vocab={props.name}>
            {props.declared!.map((entry) => {
              const bar = entry.indexOf("|");
              const val = (bar >= 0 ? entry.slice(0, bar) : entry).trim();
              const hint = bar >= 0 ? entry.slice(bar + 1).trim() : "";
              const on = props.value === val;
              return (
                <button
                  key={entry}
                  type="button"
                  className={"vcard" + (on ? " on" : "")}
                  data-pick={val}
                  aria-pressed={on}
                  onClick={() => props.onChange(val)}
                >
                  <span className="v-t">{val}</span>
                  {hint ? <span className="v-s">{hint}</span> : null}
                </button>
              );
            })}
          </div>
        ) : (
          <input
            name={props.name}
            value={props.value}
            placeholder={props.placeholder}
            onChange={(e) => props.onChange(e.target.value)}
          />
        )}
      </RefField>
      {has ? null : (
        <p className="muted" style={{ fontSize: 12.3 }}>
          There is no list to pick from because there is no list to have: this is L2 vocabulary, and CREST carries no
          enum of it anywhere. A deployment declares its own under the project's{" "}
          <span className="mono">configuration.definitionVocabulary</span>, and this field renders whatever it finds
          there. This deployment declares none — which is not the same as there being none to declare.
        </p>
      )}
    </>
  );
}

const grid = (min = 230): React.CSSProperties => ({
  display: "grid",
  gap: 12,
  gridTemplateColumns: `repeat(auto-fit,minmax(${min}px,1fr))`,
});

const areaStyle: React.CSSProperties = {
  width: "100%",
  border: "1px solid var(--divider)",
  borderRadius: 6,
  padding: "10px 12px",
  font: "400 13.3px/1.45 Roboto",
};

const lines = (s: string) => s.split("\n").map((l) => l.trim()).filter(Boolean);
const commas = (s: string) => s.split(",").map((l) => l.trim()).filter(Boolean);

// ── p3_1 · the definition registry ─────────────────────────────────────────
// The wizard's front door. "Define new work" and "Clone a version" are both
// real POSTs: a draft exists on the server before the first screen renders,
// which is why no screen here has to hold a form in the browser.
export function Registry() {
  const s = useConsole();
  const nav = useNavigate();
  const [gen, setGen] = useState(0);
  const me = s.me!.partyId;
  const r = useLoad(async () => {
    const [drafts, defs] = await Promise.all([
      api.get("definitions", DRAFTS),
      api.get("definitions", "/v1/definitions"),
    ]);
    return {
      drafts: (drafts.drafts || []) as Draft[],
      defs: (defs.definitions || []) as Array<{
        id: string; version: number; state: string; activity?: { label?: string };
      }>,
    };
  }, [gen]);
  const start = async (clone?: { id: string; version: number }) => {
    if (!s.projectId) throw new Error("choose the project this definition belongs to before creating a draft");
    const body: Record<string, unknown> = { createdByPartyId: me, contextId: s.projectId };
    if (clone) {
      body.cloneFromDefinitionId = clone.id;
      body.cloneFromVersion = clone.version;
    }
    const d: Draft = await api.post("definitions", DRAFTS, body);
    setCurrentDraftId(d.id);
    nav("/define/sector");
  };
  const [filter, setFilter] = useState<"all" | "active" | "draft">("all");
  return (
    <LoadFrame r={r}>
      {({ drafts, defs }) => {
        const open = drafts.filter((d) => d.state === "OPEN");
        const latest = defs[0];
        const total = drafts.length + defs.length;
        const showActive = filter !== "draft";
        const showDrafts = filter !== "active";
        const fchip = (key: "all" | "active" | "draft", label: string, n: number) => (
          <button
            key={key}
            className={"fchip" + (filter === key ? " on" : "")}
            data-filter={key}
            onClick={() => setFilter(key)}
          >
            {label} {n}
          </button>
        );
        return (
          <Frame
            title="Work definitions"
            btns={[
              {
                label: "Clone a version",
                role: "secondary",
                onClick: () => (latest ? start({ id: latest.id, version: latest.version }) : Promise.resolve()),
              },
              { label: "Define new work", role: "primary", onClick: () => start() },
            ]}
          >
            <div className="fchips">
              {fchip("all", "All", total)}
              {fchip("active", "Active", defs.length)}
              {fchip("draft", "Draft", drafts.length)}
            </div>
            <div className="def-list">
              {showActive &&
                defs.map((def) => (
                  <div className="def-row" key={def.id}>
                    <div style={{ flex: 1 }}>
                      <div className="t">{def.activity?.label || def.id}</div>
                      <div className="sub">
                        <MonoShort id={def.id} /> v{def.version} · published · what credentials pin and verifiers
                        resolve
                      </div>
                    </div>
                    {def.state === "ACTIVE" ? (
                      <Chip kind="ok">Active</Chip>
                    ) : (
                      <Chip kind="plain">{def.state.toLowerCase()}</Chip>
                    )}
                    <button
                      className="btn secondary"
                      data-clone={def.id}
                      style={{ width: "auto", minWidth: 0, padding: "4px 9px", fontSize: 11.5 }}
                      onClick={() => start({ id: def.id, version: def.version })}
                    >
                      Clone
                    </button>
                  </div>
                ))}
              {showDrafts &&
                drafts.slice(0, 12).map((d) => (
                  <div className="def-row dim" key={d.id}>
                    <div style={{ flex: 1 }}>
                      <div className="t">{d.doc?.activity?.label || "Untitled definition"}</div>
                      <div className="sub">
                        <MonoShort id={d.id} /> · {Object.keys(d.doc || {}).length} of 9 sections ·{" "}
                        {when(d.updatedAt)}
                      </div>
                    </div>
                    {d.id === currentDraftId() ? <Chip kind="brand" sm>open · this session</Chip> : null}
                    {stateChip(d)}
                    {d.state === "OPEN" && d.id !== currentDraftId() ? (
                      <button
                        className="btn secondary"
                        data-resume={d.id}
                        style={{ width: "auto", minWidth: 0, padding: "4px 9px", fontSize: 11.5 }}
                        onClick={() => {
                          setCurrentDraftId(d.id);
                          setGen((g) => g + 1);
                        }}
                      >
                        Resume
                      </button>
                    ) : null}
                    {d.state === "OPEN" && d.id === currentDraftId() ? (
                      // The draft this session already holds: Resume would be
                      // a no-op, so the row carries the way back into the
                      // wizard instead of hiding its button.
                      <button
                        className="btn secondary"
                        data-continue={d.id}
                        style={{ width: "auto", minWidth: 0, padding: "4px 9px", fontSize: 11.5 }}
                        onClick={() => nav("/define/sector")}
                      >
                        Continue
                      </button>
                    ) : null}
                  </div>
                ))}
              {(showDrafts && drafts.length) || (showActive && defs.length) ? null : (
                <div className="def-row">
                  <span className="sub">
                    Nothing here. An unstarted definition is the absence of a record, not an empty one.
                  </span>
                </div>
              )}
            </div>
            {currentDraftId() ? (
              <Callout kind="blue" title="Authoring in progress">
                <span data-authoring>
                  This session is authoring <Mono>{currentDraftId()}</Mono>
                </span>
                {open.length > 1 ? (
                  <> — {open.length} drafts are open; a draft can sit half-finished for weeks, because only a
                  submitted version is immutable.</>
                ) : (
                  <>. A draft is the only mutable object this service has; submitting compiles it into the next
                  immutable version.</>
                )}
              </Callout>
            ) : null}
          </Frame>
        );
      }}
    </LoadFrame>
  );
}

// ── p3_2 · sector-first scoping ────────────────────────────────────────────
function SectorBody({ d }: BodyProps) {
  const s = useConsole();
  const v = useVocab(s.projectId);
  const [sector, setSector] = useState(d.doc.scope?.sector || "");
  return (
    <Frame
      counter="Sector · 1 of 9"
      title="What sector is this work in?"
      lede="This scopes what we ask you next. It never limits what you can choose."
      btns={[
        { label: "Back", to: "/definework", role: "secondary" },
        {
          label: "Continue",
          to: "/define/counting",
          role: "primary",
          onClick: () => save(d.id, "scope", { ...(d.doc.scope || {}), sector: sector.trim() }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <LoadFrame r={v}>
        {(vocab) => (
          <Vocab
            name="sector"
            label=""
            declared={vocab.sectors}
            value={sector}
            onChange={setSector}
            placeholder="health"
          />
        )}
      </LoadFrame>
    </Frame>
  );
}
export const Sector = gate(SectorBody);

// ── p3_3 · the counting-basis fork ─────────────────────────────────────────
// The consequential question. Everything downstream — what a unit is, what
// evidence can prove it, what the rate prices, what the worker's credential
// says — follows from the answer, and it is the one answer a later screen
// cannot quietly correct.
function CountingBody({ d }: BodyProps) {
  const c = d.doc.activity?.counting;
  const [basis, setBasis] = useState<Counting["basis"]>(c?.basis || "event");
  const write = (b: Counting["basis"]) =>
    save(d.id, "activity", { ...(d.doc.activity || {}), counting: { ...(c || {}), basis: b } });
  return (
    <Frame
      counter="Counting basis · 2 of 9"
      title="How is this work counted?"
      lede="This decides what we ask you next. Get it right and the rest of the form gets shorter."
      btns={[
        { label: "Back", to: "/define/sector", role: "secondary" },
        { label: "Time-based instead", to: "/define/period", role: "secondary", onClick: () => write("time-period") },
        { label: "Continue as event", to: "/define/category", role: "primary", onClick: () => write("event") },
      ]}
    >
      <ClosedNote d={d} />
      <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
        <OptionCard
          t="Event"
          s="Discrete things that happen and can be counted"
          ex="A bednet handed over, a household visited, a dose given"
          next="Next: category, then unit of work"
          on={basis === "event"}
          onPick={() => setBasis("event")}
        />
        <OptionCard
          t="Time"
          s="Paid for a period, not for a countable output"
          ex="A monthly honorarium, a daily wage, a retainer"
          next="Next: frequency. No category, no unit — they do not apply"
          on={basis === "time-period"}
          onPick={() => setBasis("time-period")}
        />
        <OptionCard
          t="Outcome"
          s="Paid on a result, often measured across a population"
          ex="Coverage rate, treatment completion, learning gain"
          next="Next: outcome indicator and aggregation level"
          on={basis === "outcome"}
          onPick={() => setBasis("outcome")}
        />
      </div>
      <Sidecar warm>
        A peer mental-health worker paid partly on retainer and partly per session is still an open question. Either
        counting basis holds two values, or it is honestly two linked work definitions.
      </Sidecar>
    </Frame>
  );
}
export const CountingBasis = gate(CountingBody);

// ── p3_4 · the category picker, scoped to the sector ───────────────────────
function CategoryBody({ d }: BodyProps) {
  const s = useConsole();
  const v = useVocab(s.projectId);
  const [cat, setCat] = useState(d.doc.scope?.category || "");
  const [desc, setDesc] = useState("");
  const [showAll, setShowAll] = useState(false);
  const sector = d.doc.scope?.sector || "";
  return (
    <Frame
      counter="Category · 3 of 9"
      title="What kind of work is it?"
      lede={<>Showing categories typical for {sector ? <Mono>{sector}</Mono> : "this sector"}. None of these is a restriction.</>}
      btns={[
        { label: "Back", to: "/define/counting", role: "secondary" },
        {
          label: "Continue",
          to: "/define/unit",
          role: "primary",
          onClick: () => save(d.id, "scope", { ...(d.doc.scope || {}), category: cat.trim() }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <LoadFrame r={v}>
        {(vocab) => {
          // A deployment that declares categories per sector gets the sector's
          // own list; one that declares a flat list gets that; one that
          // declares nothing gets a typed answer.
          const perSector = sector ? vocab["categories:" + sector] : undefined;
          const flat = vocab.categories;
          const scoped = perSector || flat;
          const hasWider = !!(perSector && flat && flat.some((e) => !perSector.includes(e)));
          const declared = showAll && flat ? Array.from(new Set([...(perSector || []), ...flat])) : scoped;
          if (!declared || !declared.length)
            return (
              <Vocab
                name="category"
                label="Category"
                value={cat}
                onChange={setCat}
                placeholder="community-outreach"
                hint="Recorded on the version as classification.category."
              />
            );
          // The reference frame: the card grid, a "none of these fit?" row,
          // a free-text description, and an honest suggestion drawn only from
          // the deployment's own declared words — never one CREST invented.
          const stop = new Set(["and", "the", "out", "for", "how", "are", "per", "with", "that", "many"]);
          const words = Array.from(
            new Set(desc.toLowerCase().split(/[^a-z0-9-]+/).filter((w) => w.length > 2 && !stop.has(w))),
          );
          let best: { val: string; hit: string[] } | null = null;
          for (const entry of declared) {
            const low = entry.toLowerCase();
            const hit = words.filter((w) => low.includes(w));
            if (hit.length && (!best || hit.length > best.hit.length))
              best = { val: (entry.split("|")[0] || "").trim(), hit };
          }
          return (
            <>
              <Vocab name="category" label="" value={cat} onChange={setCat} declared={declared} />
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <span className="muted" style={{ fontSize: 12.5 }}>None of these fit?</span>
                {hasWider && !showAll ? (
                  <button type="button" className="fchip" data-showall onClick={() => setShowAll(true)}>
                    Show all categories
                  </button>
                ) : null}
              </div>
              <input
                name="categoryDesc"
                value={desc}
                placeholder="Describe the work in your own words"
                onChange={(e) => setDesc(e.target.value)}
                style={{ width: "100%", border: "1px solid var(--divider)", borderRadius: 6, padding: "12px 13px", font: "400 13.3px/1.4 Roboto" }}
              />
              {best && best.val !== cat ? (
                <div className="suggest" data-suggest={best.val}>
                  <div>
                    <div className="s-t">Suggested: {best.val}</div>
                    <div className="s-s">Matched on {best.hit.map((w) => `“${w}”`).join(", ")} — from this deployment's own list</div>
                  </div>
                  <button type="button" className="s-use" data-use onClick={() => setCat(best!.val)}>
                    Use this
                  </button>
                </div>
              ) : null}
            </>
          );
        }}
      </LoadFrame>
    </Frame>
  );
}
export const Category = gate(CategoryBody);

// ── p3_5 · the unit of work, and the one field the rate will price ─────────
const AGGREGATIONS: Array<[string, string]> = [
  ["Per individual event", "One record per unit counted"],
  ["Per worker, per pay cycle", "Totalled before pricing"],
  ["Per group or cooperative", "Gang earthwork, cooperative bonuses"],
];

function UnitBody({ d }: BodyProps) {
  const a = d.doc.activity || {};
  const c = a.counting || {};
  const [unit, setUnit] = useState(a.outcomeUnit || "");
  const [code, setCode] = useState(a.code || "");
  const [label, setLabel] = useState(a.label || "");
  const [skill, setSkill] = useState(a.skillCode || "");
  const [freq, setFreq] = useState(c.frequency || "");
  const [model, setModel] = useState(c.description || "");
  const [agg, setAgg] = useState(c.aggregationLevel || "");
  return (
    <Frame
      counter="Unit · 4 of 9"
      title="What exactly is counted?"
      btns={[
        { label: "Back", to: "/define/category", role: "secondary" },
        {
          label: "Continue",
          to: "/define/cascade",
          role: "primary",
          onClick: () =>
            save(d.id, "activity", {
              ...a,
              code: code.trim(),
              label: label.trim(),
              skillCode: skill.trim() || undefined,
              outcomeUnit: unit.trim(),
              counting: {
                ...c,
                basis: c.basis || "event",
                frequency: freq.trim() || undefined,
                description: model.trim() || undefined,
                aggregationLevel: agg || undefined,
              },
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <div style={grid()}>
        <RefField label="Unit of work" hint="One record equals one of these. The rate prices exactly this.">
          <input name="outcomeUnit" value={unit} onChange={(e) => setUnit(e.target.value)} placeholder="bednets-distributed" />
        </RefField>
        <RefField label="Frequency">
          <input name="frequency" value={freq} onChange={(e) => setFreq(e.target.value)} placeholder="Per campaign day" />
        </RefField>
      </div>
      <RefField label="Counting model">
        <input name="countingModel" value={model} onChange={(e) => setModel(e.target.value)} placeholder="Individually countable — one record per unit" />
      </RefField>
      <div style={grid()}>
        <RefField label="Activity code" hint="This deployment's own word for the work.">
          <input name="activityCode" value={code} onChange={(e) => setCode(e.target.value)} placeholder="bednet-distribution" />
        </RefField>
        <RefField label="Activity label">
          <input name="activityLabel" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Bednet distribution" />
        </RefField>
        <RefField label="Skill code" hint="Optional. The part of the record that travels between deployments.">
          <input name="skillCode" value={skill} onChange={(e) => setSkill(e.target.value)} placeholder="CREST-SKILL:chw.bednet-distribution.v2" />
        </RefField>
      </div>
      <RefField label="Aggregation level">
        <div style={grid()}>
          {AGGREGATIONS.map(([t, sub]) => (
            <OptionCard key={t} t={t} s={sub} on={agg === t} onPick={() => setAgg(t)} />
          ))}
        </div>
      </RefField>
      <Sidecar>
        This unit{unit.trim() ? <> — <b>{unit.trim()}</b> —</> : null} is referenced by ID by any rate that prices
        it. It is never re-entered as text on the rate record, so the two cannot silently drift apart.
      </Sidecar>
    </Frame>
  );
}
export const Unit = gate(UnitBody);

// ── p3_21 · the training cascade, as linked definitions ────────────────────
function CascadeBody({ d }: BodyProps) {
  const c = d.doc.cascade || {};
  const [level, setLevel] = useState(c.roleLevel || "");
  const [trainer, setTrainer] = useState(c.trainedByDefinitionId || "");
  const [tv, setTv] = useState(c.trainedByVersion ? String(c.trainedByVersion) : "");
  const basis = d.doc.activity?.counting?.basis || "event";
  return (
    <Frame
      title="Is this work part of a cascade?"
      chip={
        <>
          <Chip kind="err">Proposed</Chip> <Chip kind="plain">Built nowhere</Chip>
        </>
      }
      btns={[
        { label: "Back", to: "/define/unit", role: "secondary" },
        {
          label: "Continue",
          to: basis === "event" ? "/define/parties" : basis === "outcome" ? "/define/outcome" : "/define/period",
          role: "primary",
          onClick: () =>
            save(d.id, "cascade", {
              roleLevel: level.trim() || undefined,
              trainedByDefinitionId: trainer.trim() || undefined,
              trainedByVersion: tv.trim() ? Number(tv.trim()) : undefined,
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <div style={grid()}>
          <RefField label="Role level" hint="Where this work sits in the cascade. L2.">
            <input name="roleLevel" value={level} onChange={(e) => setLevel(e.target.value)} placeholder="2" />
          </RefField>
          <RefField label="Trained by" hint="The definition whose completion qualifies a worker for this one.">
            <input name="trainedBy" value={trainer} onChange={(e) => setTrainer(e.target.value)} placeholder={FIX.definition} />
          </RefField>
          <RefField label="Trained-by version" hint="Pinned: a training prerequisite names a version, not a moving definition.">
            <input name="trainedByVersion" value={tv} onChange={(e) => setTv(e.target.value)} placeholder="1" />
          </RefField>
        </div>
        <p className="muted" style={{ marginTop: 10 }}>
          Submitting writes this as a <Mono>linked-definition</Mono> record keyed to the new version, with relation{" "}
          <Mono>trained-by</Mono>. The service refuses a definition that names itself as its own prerequisite.
        </p>
      </Card>
      <Sidecar>
        Each level is a separate work definition with its own evidence rules and its own rate. The reference is what
        makes the cascade auditable — a verifier can walk up the chain.
      </Sidecar>
      <Sidecar warm>
        Undecided: whether a level-three definition can exist with no level two above it, and whether a trainer's own
        credential should carry the outcomes of the people they trained.
      </Sidecar>
      <Sidecar>
        The reference sends this screen's Continue on to the time-based period screen. On this draft the counting
        basis is <Mono>{basis}</Mono>, so Continue goes to that branch's next screen instead — writing the period
        screen's answers over an event-based draft would change the fork underneath it.
      </Sidecar>
    </Frame>
  );
}
export const Cascade = gate(CascadeBody);

// ── p3_6 · the time-based path ─────────────────────────────────────────────
function PeriodBody({ d }: BodyProps) {
  const a = d.doc.activity || {};
  const c = a.counting || {};
  const [freq, setFreq] = useState(c.frequency || "");
  const [agg, setAgg] = useState(c.aggregationLevel || "");
  const [desc, setDesc] = useState(c.description || "");
  return (
    <Frame
      counter="Period · 3 of 7"
      title="What period is this paid for?"
      chip={
        <>
          <Chip kind="info">Time-based</Chip> <Chip kind="ok">No category or unit required</Chip>
        </>
      }
      lede="There is no countable unit here, and the form no longer pretends there is."
      btns={[
        { label: "Back", to: "/define/counting", role: "secondary" },
        { label: "Outcome-based", to: "/define/outcome", role: "secondary" },
        {
          label: "Continue",
          to: "/define/parties",
          role: "primary",
          onClick: () =>
            save(d.id, "activity", {
              ...a,
              counting: {
                ...c,
                basis: "time-period",
                frequency: freq.trim() || undefined,
                aggregationLevel: agg.trim() || undefined,
                description: desc.trim() || undefined,
              },
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <div style={grid()}>
          <RefField label="Frequency">
            <input name="periodFrequency" value={freq} onChange={(e) => setFreq(e.target.value)} placeholder="Monthly" />
          </RefField>
          <RefField label="Aggregation level">
            <input name="periodAggregation" value={agg} onChange={(e) => setAgg(e.target.value)} placeholder="Per worker, per pay cycle" />
          </RefField>
        </div>
        <div style={{ marginTop: 12 }}>
          <RefField
            label="Plain-language description"
            hint="What holding the post actually involves. This is the sentence a worker will recognise."
          >
            <input
              name="periodDescription"
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder="Runs the daily centre: pre-school activity, growth monitoring, ration distribution"
            />
          </RefField>
        </div>
      </Card>
      <Callout kind="green">
        Under the flat taxonomy this role had to be filed under a category built for countable campaign work. That is
        the failure this fork removes.
      </Callout>
    </Frame>
  );
}
export const Period = gate(PeriodBody);

// ── p3_7 · the outcome path, and population-level proof ────────────────────
function OutcomeBody({ d }: BodyProps) {
  const a = d.doc.activity || {};
  const c = a.counting || {};
  const o = c.outcome || {};
  const [ind, setInd] = useState(o.indicator || "");
  const [agg, setAgg] = useState(c.aggregationLevel || "");
  const [base, setBase] = useState(o.baseline || "");
  const [target, setTarget] = useState(o.target || "");
  const [by, setBy] = useState(o.measuredBy || "");
  return (
    <Frame
      counter="Outcome · 3 of 7"
      title="What result is being paid for?"
      chip={
        <>
          <Chip kind="info">Outcome-based</Chip> <Chip kind="warn">Proposal, not built</Chip>
        </>
      }
      btns={[
        { label: "Back", to: "/define/counting", role: "secondary" },
        {
          label: "Continue",
          to: "/define/parties",
          role: "primary",
          onClick: () =>
            save(d.id, "activity", {
              ...a,
              counting: {
                ...c,
                basis: "outcome",
                aggregationLevel: agg.trim() || undefined,
                outcome: {
                  indicator: ind.trim(),
                  baseline: base.trim() || undefined,
                  target: target.trim() || undefined,
                  measuredBy: by.trim() || undefined,
                },
              },
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <div style={grid()}>
          <RefField label="Outcome indicator">
            <input name="indicator" value={ind} onChange={(e) => setInd(e.target.value)} placeholder="Full immunisation coverage" />
          </RefField>
          <RefField label="Aggregation level">
            <input name="outcomeAggregation" value={agg} onChange={(e) => setAgg(e.target.value)} placeholder="Per group or cooperative" />
          </RefField>
          <RefField label="Baseline">
            <input name="baseline" value={base} onChange={(e) => setBase(e.target.value)} placeholder="61%" />
          </RefField>
          <RefField label="Target">
            <input name="target" value={target} onChange={(e) => setTarget(e.target.value)} placeholder="80%" />
          </RefField>
          <RefField label="Measured by">
            <input name="measuredBy" value={by} onChange={(e) => setBy(e.target.value)} placeholder="District Health Information System" />
          </RefField>
        </div>
      </Card>
      <Sidecar warm>
        Who measures the outcome, and whether the cooperative can contest that measurement, is not specified
        anywhere. It is the same question a worker-level dispute answers — at population scale. An indicator moving
        is evidence about a district, not about a person.
      </Sidecar>
      <Sidecar warm>
        If the group is paid as a group, how the payment splits back to individuals is a genuinely open question. It
        needs a formula, not another field.
      </Sidecar>
    </Frame>
  );
}
export const Outcome = gate(OutcomeBody);

// ── p3_8 · parties: who works, who pays, who sits between ──────────────────
function PartiesBody({ d }: BodyProps) {
  const p = d.doc.parties || {};
  const [role, setRole] = useState(p.performerRole || "");
  const [kind, setKind] = useState(p.partyType || "Individual");
  const [fns, setFns] = useState((p.attesterFunctions || ["submit-work-evidence"]).join(", "));
  return (
    <Frame
      counter="Parties · 5 of 9"
      title="Who is involved?"
      btns={[
        { label: "Back", to: "/define/unit", role: "secondary" },
        {
          label: "Continue",
          to: "/define/evidence",
          role: "primary",
          onClick: () =>
            save(d.id, "parties", {
              performerRole: role.trim() || undefined,
              partyType: kind.trim() || undefined,
              attesterFunctions: commas(fns),
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <div style={grid()}>
        <RefField label="Who does the work" hint="Recorded as classification.performerRole — L2.">
          <input name="performerRole" value={role} onChange={(e) => setRole(e.target.value)} placeholder="Community health worker" />
        </RefField>
        <RefField label="Party type">
          <select name="partyType" value={kind} onChange={(e) => setKind(e.target.value)}>
            {["Individual", "Group", "Organisation"].map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </RefField>
      </div>
      <RefField
        label="Who may attest evidence"
        hint="Authorization functions, comma-separated. This one IS infrastructure: evidence submitted without one of these is refused."
      >
        <input name="attesterFunctions" value={fns} onChange={(e) => setFns(e.target.value)} />
      </RefField>
      <CardTitled t="Who sits between, and where each is recorded">
        <KVR
          rows={[
            ["performs the work", <>{role || "—"} · a <Mono>Claim</Mono> links the party to each unit</>],
            ["attests the evidence", <>holders of <Mono>{fns || "—"}</Mono> — checked on submission</>],
            ["pays", "a rate owner and a payer, on a linked record with its own version and its own owner"],
            ["validates", "answered on the validation screen; a different party again, and deliberately so"],
          ]}
        />
        <p className="muted" style={{ marginTop: 8 }}>
          A unit and a claim are separable on purpose: the unit of work exists whether or not anyone's claim to it
          survives, so disputing who did the work never destroys the record that it was done.
        </p>
      </CardTitled>
      <div className="proposal">
        <div className="p-head">
          <span className="p-t">Intermediary party — proposed</span>
          <Chip kind="warn">Not built</Chip>
        </div>
        <div className="p-s">
          Where a worker is engaged through a contractor or an implementing partner, that party sits between the fund
          source and the worker. Nothing records it today.
        </div>
      </div>
      <Sidecar warm>
        Whether a fund split applies to every single unit or to the aggregate pay cycle is a proposed field,{" "}
        <Mono>fundSplitLevel</Mono>. It changes what a worker is owed when only one payer confirms.
      </Sidecar>
    </Frame>
  );
}
export const Parties = gate(PartiesBody);

// ── p3_9 · evidence tiers ──────────────────────────────────────────────────
const SOURCE_CLASSES = [
  "national-system", "institutional-system", "programme-system", "supervised-capture", "self-reported",
];
const CAPTURE_METHODS = ["system-of-record", "digital-capture", "supervised-manual", "unsupervised-manual"];
const ASSURANCES = ["IA-0", "IA-1", "IA-2", "IA-3"];

// A sensible starting map, offered rather than imposed: the floor with no
// requirements at all is what keeps the weakest worker payable, and an author
// who deletes it should have to do so deliberately.
const STARTER_MAP: TierRule[] = [
  {
    tier: 1, sourceClassIn: ["national-system", "institutional-system"],
    captureMethodIn: ["system-of-record"], minIdentityAssurance: "IA-3", requiresFields: [],
  },
  {
    tier: 2, sourceClassIn: ["national-system", "institutional-system", "programme-system"],
    captureMethodIn: ["system-of-record", "digital-capture"], minIdentityAssurance: "IA-1", requiresFields: [],
  },
  { tier: 3, sourceClassIn: SOURCE_CLASSES, captureMethodIn: CAPTURE_METHODS },
];

function EvidenceBody({ d }: BodyProps) {
  const e = d.doc.evidence || {};
  const [summary, setSummary] = useState(e.summary || "");
  const [plain, setPlain] = useState((e.evidenceInPlainLanguage || []).join("\n"));
  const [ceiling, setCeiling] = useState(e.tierCeiling ? String(e.tierCeiling) : "1");
  const [intensity, setIntensity] = useState(e.checkIntensity || "");
  const [map, setMap] = useState<TierRule[]>(e.tierMap && e.tierMap.length ? e.tierMap : STARTER_MAP);
  const cap = Number(ceiling);
  const over = map.filter((r) => r.tier < cap);
  const setRule = (i: number, patch: Partial<TierRule>) =>
    setMap(map.map((r, j) => (i === j ? { ...r, ...patch } : r)));
  const toggle = (i: number, key: "sourceClassIn" | "captureMethodIn", v: string) => {
    const cur = map[i][key] || [];
    setRule(i, { [key]: cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v] } as Partial<TierRule>);
  };
  return (
    <Frame
      counter="Evidence · 6 of 9"
      title="What counts as proof?"
      btns={[
        { label: "Back", to: "/define/parties", role: "secondary" },
        {
          label: "Continue",
          to: "/define/source",
          role: "primary",
          onClick: () =>
            save(d.id, "evidence", {
              summary: summary.trim() || undefined,
              evidenceInPlainLanguage: lines(plain),
              tierCeiling: cap,
              checkIntensity: intensity.trim() || undefined,
              tierMap: map,
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <div style={grid()}>
          <RefField
            label="Tier ceiling"
            hint="The most this definition is willing to stand behind, whatever the map could award."
          >
            <select name="tierCeiling" value={ceiling} onChange={(ev) => setCeiling(ev.target.value)}>
              <option value="1">Tier 1 — independent system evidence, identity assured</option>
              <option value="2">Tier 2 — a programme system's digital capture</option>
              <option value="3">Tier 3 — worker asserted evidence</option>
            </select>
          </RefField>
          <RefField label="Check intensity" hint="How much of it gets checked. Programme policy (L2).">
            <input name="checkIntensity" value={intensity} onChange={(ev) => setIntensity(ev.target.value)} placeholder="Sample — 1 in 10" />
          </RefField>
        </div>
        <div style={{ marginTop: 12, display: "grid", gap: 12 }}>
          <RefField label="What the worker will read" hint="The worker's own face of this definition, in their words.">
            <input
              name="workerSummary"
              value={summary}
              onChange={(ev) => setSummary(ev.target.value)}
              placeholder="You handed out bednets and recorded each household you visited."
            />
          </RefField>
          <RefField
            label="What counts, in plain language"
            hint="One line per kind of proof. These are the lines a worker sees, not the rules above."
          >
            <textarea
              name="evidencePlain"
              rows={3}
              style={areaStyle}
              value={plain}
              onChange={(ev) => setPlain(ev.target.value)}
              placeholder={"The programme's own system has your visit recorded.\nYour supervisor confirmed the day's round."}
            />
          </RefField>
        </div>
      </Card>
      <p className="muted">Tier 1 is strongest, Tier 2 is supervised evidence, and Tier 3 is worker asserted evidence.</p>
      <CardTitled t="The evidence-to-tier map" chip={<Chip kind="info">{map.length} rules</Chip>}>
        {map.map((r, i) => (
          <div key={i} className="card quiet" style={{ marginBottom: 9 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 7, flexWrap: "wrap" }}>
              <Chip kind={"tier" + r.tier}>Tier {r.tier}</Chip>
              {r.tier < cap ? <Chip kind="err">above the ceiling</Chip> : null}
              {(r.sourceClassIn || []).length === SOURCE_CLASSES.length &&
              (r.captureMethodIn || []).length === CAPTURE_METHODS.length &&
              !(r.requiresFields || []).length ? (
                <Chip kind="ok" sm>the floor — no requirements</Chip>
              ) : null}
            </div>
            <div style={grid(240)}>
              <div>
                <span className="eyebrow">Source class</span>
                <div style={{ display: "flex", gap: 5, flexWrap: "wrap", marginTop: 5 }}>
                  {SOURCE_CLASSES.map((sc) => (
                    <button
                      key={sc}
                      type="button"
                      className={"chip " + ((r.sourceClassIn || []).includes(sc) ? "info" : "plain")}
                      onClick={() => toggle(i, "sourceClassIn", sc)}
                    >
                      {sc}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <span className="eyebrow">Capture method</span>
                <div style={{ display: "flex", gap: 5, flexWrap: "wrap", marginTop: 5 }}>
                  {CAPTURE_METHODS.map((cm) => (
                    <button
                      key={cm}
                      type="button"
                      className={"chip " + ((r.captureMethodIn || []).includes(cm) ? "info" : "plain")}
                      onClick={() => toggle(i, "captureMethodIn", cm)}
                    >
                      {cm}
                    </button>
                  ))}
                </div>
              </div>
              <RefField label="Identity assurance at least">
                <select
                  value={r.minIdentityAssurance || ""}
                  onChange={(ev) => setRule(i, { minIdentityAssurance: ev.target.value || undefined })}
                >
                  <option value="">any</option>
                  {ASSURANCES.map((a) => (
                    <option key={a} value={a}>
                      {a}
                    </option>
                  ))}
                </select>
              </RefField>
              <RefField label="Requires fields" hint="Comma-separated. A record missing one cannot reach this tier.">
                <input
                  data-requires={r.tier}
                  value={(r.requiresFields || []).join(", ")}
                  onChange={(ev) => setRule(i, { requiresFields: commas(ev.target.value) })}
                />
              </RefField>
            </div>
          </div>
        ))}
        {over.length ? (
          <div className="errbar">
            {over.length} rule{over.length > 1 ? "s" : ""} grant a tier above the ceiling {cap} the worker face
            promises. The service refuses this at submit: the two faces are one record and may not disagree.
          </div>
        ) : null}
      </CardTitled>
      <div className="proposal">
        <div className="p-head">
          <span className="p-t">Paired proof — proposed</span>
          <Chip kind="warn">Not built</Chip>
        </div>
        <div className="p-s">
          A before-and-after photo is one requirement, not two. Proof types are flat labels today and cannot express
          that.
        </div>
      </div>
    </Frame>
  );
}
export const Evidence = gate(EvidenceBody);

// capFor derives the highest tier the draft's own map could award to a record
// with this provenance. Derived for display, from L2 data, at the moment it is
// shown — the authoritative judgement is the strength function, and p3_27
// proves the two agree on real rows.
function capFor(map: TierRule[], sourceClass: string, captureMethod: string, ceiling: number): number | null {
  let best: number | null = null;
  for (const r of map) {
    if (!(r.sourceClassIn || []).includes(sourceClass)) continue;
    if (!(r.captureMethodIn || []).includes(captureMethod)) continue;
    const t = Math.max(r.tier, ceiling || r.tier);
    if (best === null || t < best) best = t;
  }
  return best;
}

// ── p3_22 · the source-class choice that caps the tier ─────────────────────
function SourceBody({ d }: BodyProps) {
  const src = d.doc.sources || {};
  const map = d.doc.evidence?.tierMap || [];
  const ceiling = d.doc.evidence?.tierCeiling || 1;
  const [sc, setSc] = useState(src.connections?.[0]?.settings?.sourceClass || "programme-system");
  const [cm, setCm] = useState(src.connections?.[0]?.settings?.captureMethod || "digital-capture");
  const [systems, setSystems] = useState((src.sourceSystems || []).join(", "));
  const [req, setReq] = useState((src.requiredFields || []).join(", "));
  const cap = capFor(map, sc, cm, ceiling);
  // sourceClass and captureMethod are the deployment's knowledge of the source,
  // so they are kept as connection settings rather than as a definition field:
  // the definition's rules are about classes of source, and which class a given
  // system belongs to is configuration.
  const persist = () =>
    save(d.id, "sources", {
      ...src,
      sourceSystems: commas(systems),
      requiredFields: commas(req),
      connections: [
        {
          ...(src.connections?.[0] || { systemRef: "" }),
          settings: { ...(src.connections?.[0]?.settings || {}), sourceClass: sc, captureMethod: cm },
        },
        ...(src.connections || []).slice(1),
      ],
    });
  return (
    <Frame
      counter="Source · 7 of 9"
      title="Where does this evidence come from?"
      lede={
        <>
          Pick how records of this work will reach CREST. Every option is somewhere else: CREST reads or receives,
          and never records. You can add more than one later, and the lowest-trust source in use caps the tier.
        </>
      }
      btns={[
        { label: "Back", to: "/define/evidence", role: "secondary" },
        { label: "Use a spreadsheet", to: "/define/template", role: "secondary", onClick: persist },
        { label: "Connect a system", to: "/define/adaptors", role: "primary", onClick: persist },
      ]}
    >
      <ClosedNote d={d} />
      {/* The frame's four arrival routes. The first two are the forward
          buttons below; the third is stated so nobody wonders whether it is
          hidden — CREST attesting its own capture would break provenance. */}
      <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
        <OptionCard
          t="A system CREST connects to"
          s="An adaptor pulls records in on a schedule. Nobody retypes anything, which is the only way the strongest tier is reachable."
        />
        <OptionCard
          t="A spreadsheet the team uploads"
          s="You get a template generated from this definition. Sensible where the work happens outside any system."
        />
        <OptionCard
          t="A worker typing it into CREST"
          unavailable
          s="Does not exist. CREST has no screen on which work is recorded, because evidence CREST captured itself would be CREST vouching for its own record."
        />
        <OptionCard
          t="A supervisor confirming in the delivery platform's own app"
          s="A named person vouches for it. Already covered by the attestation rule on the previous step."
        />
      </div>
      <Card>
        <div style={grid()}>
          <RefField label="Source class" hint="Attached by the adaptor, never asserted by the source itself.">
            <select name="sourceClass" value={sc} onChange={(e) => setSc(e.target.value)}>
              {SOURCE_CLASSES.map((x) => (
                <option key={x} value={x}>
                  {x}
                </option>
              ))}
            </select>
          </RefField>
          <RefField label="Capture method" hint="How the fact was captured where it happened.">
            <select name="captureMethod" value={cm} onChange={(e) => setCm(e.target.value)}>
              {CAPTURE_METHODS.map((x) => (
                <option key={x} value={x}>
                  {x}
                </option>
              ))}
            </select>
          </RefField>
          <RefField label="Source systems" hint="Comma-separated system references.">
            <input name="sourceSystems" value={systems} onChange={(e) => setSystems(e.target.value)} placeholder="dhis2-riverside, csv-batch" />
          </RefField>
          <RefField label="Required fields" hint="What a record must carry to be read at all.">
            <input name="requiredFields" value={req} onChange={(e) => setReq(e.target.value)} placeholder="household_id, beneficiary_count" />
          </RefField>
        </div>
      </Card>
      <CardTitled t="The ceiling this choice implies" chip={<Chip kind="warn">derived, not stored</Chip>}>
        {map.length ? (
          <>
            <KVR
              rows={[
                ["provenance chosen", <><Mono>{sc}</Mono> captured by <Mono>{cm}</Mono></>],
                [
                  "highest tier reachable",
                  cap === null ? (
                    <Chip kind="err">no rule admits this provenance — records would not be acceptable at all</Chip>
                  ) : (
                    <Chip kind={"tier" + cap}>Tier {cap}</Chip>
                  ),
                ],
                ["the definition's own ceiling", <Chip kind={"tier" + ceiling}>Tier {ceiling}</Chip>],
              ]}
            />
            <p className="muted" style={{ marginTop: 8 }}>
              Computed here, now, from this draft's own tier map — the strongest rule whose source class and capture
              method both admit this provenance. It is not written anywhere, on the definition or on a record: it is
              a consequence of the rules, recalculated every time anyone asks. The dry run two screens on proves the
              same answer against real parsed rows using the real strength function.
            </p>
            <GridTable cols="auto 1fr 1fr" head={["Tier", "Admits this source class", "Admits this capture"]}>
              {map.map((r, i) => (
                <div className="g-row" key={i}>
                  <span>
                    <Chip kind={"tier" + r.tier} sm>
                      {r.tier}
                    </Chip>
                  </span>
                  <span>{(r.sourceClassIn || []).includes(sc) ? "yes" : "no"}</span>
                  <span>{(r.captureMethodIn || []).includes(cm) ? "yes" : "no"}</span>
                </div>
              ))}
            </GridTable>
          </>
        ) : (
          <Empty>
            This draft has no tier map yet, so there is no ceiling to derive. Answer the evidence screen first — a
            ceiling invented here would be a number with no rules behind it.
          </Empty>
        )}
      </CardTitled>
      <Callout kind="teal" title="Why this matters">
        Whatever you choose here, the evidence rules you just set still apply. The source decides the highest tier
        that is structurally possible; the rules decide whether a given record reaches it.
      </Callout>
    </Frame>
  );
}
export const Source = gate(SourceBody);

// ── p3_23 · the template the definition writes for you ─────────────────────
function TemplateBody({ d }: BodyProps) {
  // The draft endpoint derives this against the version the draft will become
  // without minting it. After submit, the immutable-version endpoint derives
  // the same file again; neither path stores a template that could drift.
  const defId = d.definitionId || "";
  const templatePath = defId && d.submittedVersion
    ? defpath(defId, `/versions/${d.submittedVersion}/template`)
    : dpath(d.id, "/template");
  const r = useLoad<Template>(
    () => api.get("definitions", templatePath),
    [templatePath, d.updatedAt],
  );
  const next = d.submittedVersion || (d.baseVersion || 0) + 1;
  return (
    <Frame
      title="Your spreadsheet template"
      lede={
        <>
          Nobody writes this file's header by hand. The columns are derived from the definition version itself — the
          canonical evidence contract, plus exactly the extra fields this version's platform face and tier rules
          demand — so the template and the rules it serves cannot drift apart.
        </>
      }
      btns={[
        { label: "Back", to: "/define/source", role: "secondary" },
        {
          label: "Download template",
          role: "secondary",
          onClick: () => {
            // The real endpoint, in CSV form: ?format=csv returns the header
            // row with a filename naming the version it serves.
            window.open(`${services.definitions}${templatePath}?format=csv`, "_blank");
          },
        },
        { label: "Continue", to: "/define/validation", role: "primary" },
      ]}
    >
      <ClosedNote d={d} />
      <LoadFrame r={r}>
        {(t) =>
          (
            <CardTitled t={"Columns for v" + t.version} chip={<Chip kind="ok">{t.columns.length} columns</Chip>}>
              <KVR
                rows={[
                  ["file", <Mono>{t.filename}</Mono>],
                  ["definition", <MonoShort id={t.definitionId} />],
                  ["activity", <Mono>{t.activity}</Mono>],
                ]}
              />
              <div className="consent-quote" style={{ marginTop: 10, overflowX: "auto" }} data-template-header>
                <Mono>{t.columns.join(",")}</Mono>
              </div>
              <p className="muted" style={{ marginTop: 8 }}>
                The first nine are the evidence contract's own fields and exist for every definition. The rest{" "}
                {t.requiredEnrichment.length ? (
                  <>
                    — <Mono>{t.requiredEnrichment.join(", ")}</Mono> — are here because this version's platform face
                    or tier rules name them.
                  </>
                ) : (
                  <>are absent: this version demands no fields beyond the canonical set.</>
                )}
              </p>
            </CardTitled>
          )
        }
      </LoadFrame>
      <Callout kind="teal" title="What to watch">
        The template is tied to this version (v{next}) of the definition. Publish v{next + 1} and a fresh template is generated;
        files built on the old one stop being accepted.
      </Callout>
    </Frame>
  );
}
export const TemplateScreen = gate(TemplateBody);

// ── p3_24 · the adaptor library, told honestly ─────────────────────────────
function AdaptorsBody({ d }: BodyProps) {
  const r = useLoad<Adaptor[]>(
    () => api.get("definitions", "/v1/adaptors").then((x) => (x.adaptors || []) as Adaptor[]),
    [],
  );
  const src = d.doc.sources || {};
  const [sel, setSel] = useState(src.connections?.[0]?.adapterRef || "");
  const pick = (ref: string) => {
    setSel(ref);
    return save(d.id, "sources", {
      ...src,
      connections: [
        { ...(src.connections?.[0] || { systemRef: "" }), adapterRef: ref },
        ...(src.connections || []).slice(1),
      ],
    });
  };
  return (
    <LoadFrame r={r}>
      {(list) => {
        const real = list.filter((a) => a.status === "available");
        const absent = list.filter((a) => a.status !== "available");
        const digit = list.find((a) => a.class === "digit-hcm");
        const selClass = list.find((a) => (a.ref || a.class) === sel)?.class;
        return (
          <Frame
            counter="Connection · 1 of 5"
            title="Choose a class adaptor"
            lede="Pick the one that matches the system on the other side. If nothing matches, build a new adaptor from a sample of their data."
            btns={[
              { label: "Back", to: "/define/source", role: "secondary" },
              { label: "Start a new one", to: "/define/mapping", role: "secondary" },
              // A selected row makes the way forward the primary button; the
              // reference's DIGIT HCM button stays, demoted, with its refusal.
              ...(selClass
                ? [{ label: `Continue with ${selClass}`, to: "/define/mapping", role: "primary" as const }]
                : []),
              {
                label: "Use DIGIT HCM",
                role: (selClass ? "secondary" : "primary") as "primary" | "secondary",
                onClick: () =>
                  document.querySelector(".open-note")?.scrollIntoView({ behavior: "smooth", block: "center" }),
                note: (
                  <>
                    <b>DIGIT HCM is not an implemented adaptor class.</b>{" "}
                    {digit?.note || "The reference's library shows it; CREST does not have it."} So this button does
                    not choose it. A DIGIT- or DHIS2-shaped source is served today by the batch-file class plus a
                    per-source column mapping — that is the option below, and it is a real one rather than a shorter
                    path to the same place.
                  </>
                ),
              },
            ]}
          >
            <ClosedNote d={d} />
            <CardTitled t="Built and tested" chip={<Chip kind="ok">{real.length} available</Chip>}>
              <GridTable cols="1.2fr 2fr auto" head={["Adaptor", "What it reads", "Status"]}>
                {real.map((a) => {
                  const on = sel === (a.ref || a.class);
                  return (
                    <button
                      key={a.class}
                      type="button"
                      className={"g-row" + (on ? " on" : "")}
                      aria-pressed={on}
                      data-pick={a.ref || a.class}
                      onClick={() => pick(a.ref || a.class)}
                    >
                      <span className="g-strong">{a.ref ? `${a.class} · ${a.ref}` : a.class}</span>
                      <span>{a.note || ""}</span>
                      <Chip kind="ok" sm>{on ? "selected" : "available"}</Chip>
                    </button>
                  );
                })}
              </GridTable>
              {real.length ? null : <Empty>The service reports no implemented adaptor class.</Empty>}
            </CardTitled>
            <CardTitled t="Named, not built" chip={<Chip kind="warn">{absent.length} not implemented</Chip>}>
              <GridTable cols="1.2fr 2fr auto" head={["Adaptor", "Why it is not here yet", "Status"]}>
                {absent.map((a) => (
                  <div key={a.class} className="g-row dim">
                    <span className="g-strong">{a.class}</span>
                    <span>{a.note || ""}</span>
                    <Chip kind="warn" sm>not-implemented</Chip>
                  </div>
                ))}
              </GridTable>
            </CardTitled>
            <CardTitled t="Start a new adaptor from a sample payload" chip={<Chip kind="info">open to anyone</Chip>}>
              <p className="muted" style={{ margin: 0 }}>
                Start a new one opens the mapping screen: name the source system's columns against CREST's canonical
                fields. The result is a per-source mapping on this definition — honestly not yet a reusable adaptor
                others inherit.
              </p>
            </CardTitled>
          </Frame>
        );
      }}
    </LoadFrame>
  );
}
export const Adaptors = gate(AdaptorsBody);

// ── p3_25 · mapping their fields onto yours ────────────────────────────────
const CANONICAL = [
  "activity", "worker_id_kind", "worker_id", "period_start", "period_end",
  "outcome_value", "outcome_unit", "geography", "source_record_ref",
];

function MappingBody({ d }: BodyProps) {
  const src = d.doc.sources || {};
  const conn = src.connections?.[0] || { systemRef: "" };
  const [cols, setCols] = useState<Record<string, string>>(conn.mapping?.columns || {});
  const [enrich, setEnrich] = useState<Record<string, string>>(conn.mapping?.enrichment || {});
  const [consts, setConsts] = useState<Record<string, string>>(conn.mapping?.constants || {});
  const required = src.requiredFields || [];
  // Unmapped and required: the definition names a field, and nothing in this
  // mapping supplies it — no renamed column, no constant. Computed against the
  // real draft rather than illustrated.
  const unmapped = required.filter((f) => !enrich[f] && !consts[f] && !cols[f]);
  const persist = () =>
    save(d.id, "sources", {
      ...src,
      connections: [
        { ...conn, mapping: { columns: cols, enrichment: enrich, constants: consts } },
        ...(src.connections || []).slice(1),
      ],
    });
  const row = (
    store: Record<string, string>,
    set: (v: Record<string, string>) => void,
    key: string,
    placeholder: string,
  ) => (
    <RefField key={key} label={key}>
      <input
        data-map={key}
        value={store[key] || ""}
        placeholder={placeholder}
        onChange={(e) => {
          const next = { ...store };
          if (e.target.value) next[key] = e.target.value;
          else delete next[key];
          set(next);
        }}
      />
    </RefField>
  );
  return (
    <Frame
      counter="Connection · 2 of 5"
      title="Map their fields onto yours"
      lede={
        <>
          The source system's vocabulary is its own, and asking a partner to rename their columns is not an
          integration. Three mechanisms, because real exports need all three: rename a column, rename an extra column
          the definition asks for, or supply a value the file does not carry at all.
        </>
      }
      btns={[
        { label: "Back", to: "/define/adaptors", role: "secondary" },
        { label: "Continue", to: "/define/connect", role: "primary", onClick: persist },
      ]}
    >
      <ClosedNote d={d} />
      <CardTitled t="Canonical fields — their column name in your file">
        <div style={grid(220)}>{CANONICAL.map((f) => row(cols, setCols, f, "same name — leave blank"))}</div>
        <p className="muted" style={{ marginTop: 8 }}>
          Blank means the file already uses the canonical name. A mapped column always wins over a constant: the row
          is closer to the work than anything this deployment knows about the source in general.
        </p>
      </CardTitled>
      <CardTitled
        t="Fields this definition requires"
        chip={unmapped.length ? <Chip kind="err">{unmapped.length} unmapped</Chip> : <Chip kind="ok">all supplied</Chip>}
      >
        {required.length ? (
          <div style={grid(220)}>{required.map((f) => row(enrich, setEnrich, f, "their column name"))}</div>
        ) : (
          <Empty>This draft's source section names no required fields yet.</Empty>
        )}
      </CardTitled>
      <CardTitled t="Constants — what this deployment knows about every row from here">
        <div style={grid(220)}>
          {["outcome_unit", "activity", "geography"].map((f) => row(consts, setConsts, f, "a fixed value"))}
        </div>
        <p className="muted" style={{ marginTop: 8 }}>
          A DHIS2 event export has no column for the unit of measure. That is not missing data — it is a fact about
          the programme rather than about the row, and it belongs here next to the rest of what this deployment knows
          about this source.
        </p>
      </CardTitled>
      {unmapped.length ? (
        <Callout kind="teal" title="Unmapped, and required">
          <b>{unmapped.join(", ")}</b> {unmapped.length > 1 ? "do" : "does"} not exist in the source registry,
          because that registry describes people rather than work. Options: derive it as a count of related task
          records, ask the partner to add a field, or drop the tier ceiling to Tier 2 and take the number from a
          supervisor.
        </Callout>
      ) : null}
      <Callout kind="grey" title="Matched, but wrong">
        A field can match by type and not by meaning — a visit date mapped to a registration date is when the worker
        joined the registry, not when the visit happened. Left as-is, every record would land in the wrong pay
        period. CREST cannot detect this: both are dates, and both parse. The dry run two screens on is where a human
        sees the values and catches it.
      </Callout>
    </Frame>
  );
}
export const Mapping = gate(MappingBody);

// ── p3_26 · connection details, credentialRef only ─────────────────────────
function ConnectBody({ d }: BodyProps) {
  const src = d.doc.sources || {};
  const conn = src.connections?.[0] || { systemRef: "" };
  const [systemRef, setSystemRef] = useState(conn.systemRef || "");
  const [endpoint, setEndpoint] = useState(conn.endpoint || "");
  const [credRef, setCredRef] = useState(conn.credentialRef || "");
  const [key, setKey] = useState("");
  const [val, setVal] = useState("");
  const [settings, setSettings] = useState<Record<string, string>>(conn.settings || {});
  const [refusal, setRefusal] = useState<Problem[] | null>(null);
  const persist = async () => {
    const updated = await save(d.id, "sources", {
      ...src,
      connections: [
        {
          ...conn,
          systemRef: systemRef.trim(),
          endpoint: endpoint.trim() || undefined,
          credentialRef: credRef.trim() || undefined,
          settings,
        },
        ...(src.connections || []).slice(1),
      ],
    });
    // The refusal is the service's, not this screen's: validate runs the same
    // compile submit runs, so a secret-shaped key is named here in the same
    // words it would be named at submit.
    const v: Validation = await api.post("definitions", dpath(updated.id, "/validate"));
    const secrets = v.problems.filter((p) => p.section === "sources");
    setRefusal(secrets.length ? secrets : null);
    if (secrets.length) throw new Error("the connection was saved, and the service refuses it: see below");
  };
  return (
    <Frame
      counter="Connection · 3 of 5"
      title="Connect to the system"
      lede="Set where the records come from and how often. The partner's platform team supplies the credential separately — never here."
      btns={[
        { label: "Back", to: "/define/mapping", role: "secondary" },
        { label: "Continue", to: "/define/dryrun", role: "primary", onClick: persist },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <div style={grid()}>
          <RefField label="System reference" hint="The name this deployment knows the source instance by.">
            <input name="systemRef" value={systemRef} onChange={(e) => setSystemRef(e.target.value)} placeholder="dhis2-riverside" />
          </RefField>
          <RefField label="Endpoint" hint="Where it answers. Not a secret.">
            <input name="endpoint" value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="https://dhis2.example.org/api" />
          </RefField>
          <RefField
            label="Credential reference"
            hint="A vault path or a variable name — where the platform team keeps the secret. Never the value."
          >
            <input
              name="credentialRef"
              value={credRef}
              onChange={(e) => setCredRef(e.target.value)}
              placeholder="vault:crest/sources/dhis2-riverside#token"
            />
          </RefField>
        </div>
      </Card>
      <CardTitled t="Other settings">
        <form
          id="settingform"
          style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}
          onSubmit={(ev) => {
            ev.preventDefault();
            if (!key.trim()) return;
            setSettings({ ...settings, [key.trim()]: val.trim() });
            setKey("");
            setVal("");
          }}
        >
          <RefField label="Key">
            <input name="settingkey" value={key} onChange={(e) => setKey(e.target.value)} placeholder="orgUnitLevel" />
          </RefField>
          <RefField label="Value">
            <input name="settingvalue" value={val} onChange={(e) => setVal(e.target.value)} placeholder="4" />
          </RefField>
          <button className="btn inline" type="submit">
            Add it
          </button>
        </form>
        {Object.keys(settings).length ? (
          <div style={{ marginTop: 10 }}>
            <GridTable cols="1fr 1fr auto" head={["Key", "Value", ""]}>
              {Object.entries(settings).map(([k, v]) => (
                <div className="g-row" key={k}>
                  <span>
                    <Mono>{k}</Mono>
                  </span>
                  <span>{v}</span>
                  <span>
                    {/* A refused key has to be removable, or a draft that
                        picked up a secret-shaped name could never be
                        submitted at all — the refusal would become a dead end
                        rather than a correction. */}
                    <button
                      className="btn secondary"
                      data-unset={k}
                      style={{ width: "auto", padding: "4px 9px", fontSize: 11.5 }}
                      onClick={() => {
                        const next = { ...settings };
                        delete next[k];
                        setSettings(next);
                        setRefusal(null);
                      }}
                    >
                      Remove
                    </button>
                  </span>
                </div>
              ))}
            </GridTable>
          </div>
        ) : null}
        <p className="muted" style={{ marginTop: 8 }}>
          Settings are configuration the adaptor reads. A key whose <i>name</i> looks like a secret is refused at
          validation and at submit — the check is deliberately broad, because a false positive costs a rename and a
          false negative persists a credential.
        </p>
      </CardTitled>
      {refusal ? (
        <div className="errbar" data-secret-refusal>
          {refusal.map((p, i) => (
            <div key={i}>
              <span className="mono">{p.field}</span> — {p.reason}
            </div>
          ))}
        </div>
      ) : null}
      <Callout kind="green" title="What is deliberately absent">
        Nothing on this screen is a secret. The definition can be reviewed, signed and published with the connection
        described but not yet credentialled — the adaptor simply will not run until the platform team supplies it.
      </Callout>
      <Sidecar ok>
        There is no password field on this screen and no place to type a token. The only credential-shaped input is a
        reference to where one is kept, and the service refuses a value in it that looks like credential material
        rather than a name.
      </Sidecar>
    </Frame>
  );
}
export const Connect = gate(ConnectBody);

// ── p3_27 · the dry run ────────────────────────────────────────────────────
const SAMPLE_CSV = [
  "activity,worker_id_kind,worker_id,period_start,period_end,outcome_value,outcome_unit,geography,source_record_ref,household_id,beneficiary_count",
  "bednet-distribution,roster-id,RID-0091,2026-03-02,2026-03-02,1,bednets-distributed,Riverside,DHIS2-88401,HH-1201,4",
  "bednet-distribution,roster-id,RID-0092,2026-03-02,2026-03-02,1,bednets-distributed,Riverside,DHIS2-88402,HH-1202,3",
  "bednet-distribution,roster-id,RID-0093,2026-03-03,2026-03-03,1,bednets-distributed,Riverside,DHIS2-88403,,5",
].join("\n");

type DryRunResult = {
  definitionId: string; version: number; committed: boolean; note: string;
  rows: Array<{
    ref: string; activity: string; matchesDefinition: boolean; tier?: number;
    because?: string[]; missingRequiredFields?: string[]; problems?: string[];
  }>;
  rejections: Array<{ ref: string; reason: string }>;
};

function DryRunBody({ d }: BodyProps) {
  const src = d.doc.sources || {};
  const conn = src.connections?.[0];
  const [csv, setCsv] = useState(SAMPLE_CSV);
  const [sc, setSc] = useState(conn?.settings?.sourceClass || "programme-system");
  const [cm, setCm] = useState(conn?.settings?.captureMethod || "digital-capture");
  const [ia, setIa] = useState("IA-1");
  const [out, setOut] = useState<DryRunResult | null>(null);
  const run = async () => {
    const r: DryRunResult = await api.post("definitions", dpath(d.id, "/dry-run"), {
      csv,
      sourceClass: sc,
      captureMethod: cm,
      adapterRef: conn?.adapterRef || "",
      identityAssurance: ia,
      systemRef: conn?.systemRef || undefined,
      mapping: conn?.mapping || {},
    });
    setOut(r);
  };
  return (
    <Frame
      counter="Connection · 4 of 5"
      title="Test it against real records"
      chip={out ? <Chip kind="ok">nothing committed</Chip> : stateChip(d)}
      lede={
        out ? (
          <>
            {out.rows.length} record{out.rows.length === 1 ? "" : "s"} parsed from the sample. Nothing was written —
            this is a dry run.
          </>
        ) : (
          <>
            The same CSV adaptor and the same strength function the real pipeline uses — not a simulation of them. So
            what this screen shows is what ingestion would actually do with these rows, including the tier each one
            would reach and why.
          </>
        )
      }
      btns={[
        { label: "Back", to: "/define/connect", role: "secondary" },
        { label: "Fix the mapping", to: "/define/mapping", role: "secondary" },
        { label: "Run the sample", role: "secondary", onClick: run },
        { label: "Go live", to: "/define/live", role: "primary" },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <RefField label="Sample records" hint="A few real rows from the source, pasted verbatim.">
          <textarea
            name="samplecsv"
            rows={6}
            style={{ ...areaStyle, font: "400 12px/1.5 var(--mono)" }}
            value={csv}
            onChange={(e) => setCsv(e.target.value)}
          />
        </RefField>
        <div style={{ ...grid(200), marginTop: 12 }}>
          <RefField label="Source class">
            <select name="drySourceClass" value={sc} onChange={(e) => setSc(e.target.value)}>
              {SOURCE_CLASSES.map((x) => (
                <option key={x} value={x}>
                  {x}
                </option>
              ))}
            </select>
          </RefField>
          <RefField label="Capture method">
            <select name="dryCaptureMethod" value={cm} onChange={(e) => setCm(e.target.value)}>
              {CAPTURE_METHODS.map((x) => (
                <option key={x} value={x}>
                  {x}
                </option>
              ))}
            </select>
          </RefField>
          <RefField
            label="Identity assurance to assume"
            hint="A dry run resolves nobody, so this is assumed rather than looked up."
          >
            <select name="dryIdentity" value={ia} onChange={(e) => setIa(e.target.value)}>
              {ASSURANCES.map((x) => (
                <option key={x} value={x}>
                  {x}
                </option>
              ))}
            </select>
          </RefField>
        </div>
        <p className="muted" style={{ marginTop: 10 }}>
          The source facts come from this form, never from the file. A source that could assert its own class could
          assert its own strength, which is the one thing provenance exists to prevent.
        </p>
      </Card>
      {out ? (
        <>
          {/* The frame's verdict trio, computed from the real rows rather
              than illustrated: acceptable / not acceptable / refused. */}
          <div style={{ display: "flex", gap: 12 }}>
            <Stat n={out.rows.filter((r) => r.tier).length} label="would be acceptable" />
            <Stat n={out.rows.filter((r) => !r.tier).length} label="would go to a human" />
            <Stat n={out.rejections.length} label="would be refused" />
          </div>
          <CardTitled
            t="What the pipeline would do with these rows"
            chip={<Chip kind={out.committed ? "err" : "ok"}>committed: {String(out.committed)}</Chip>}
          >
            <GridTable cols=".7fr .9fr auto 1.6fr 1fr" head={["Row", "Activity", "Tier", "Because", "Missing"]}>
              {out.rows.map((r) => (
                <div className="g-row" key={r.ref} data-dryrow={r.ref}>
                  <span>
                    <Mono>{r.ref}</Mono>
                  </span>
                  <span>
                    {r.activity}
                    {r.matchesDefinition ? null : <Chip kind="err" sm>not this definition</Chip>}
                  </span>
                  <span>
                    {r.tier ? <Chip kind={"tier" + r.tier}>Tier {r.tier}</Chip> : <Chip kind="plain">not acceptable</Chip>}
                  </span>
                  <span style={{ fontSize: 12.2 }}>{(r.because || []).join("; ") || "—"}</span>
                  <span style={{ fontSize: 12.2 }}>
                    {(r.missingRequiredFields || []).length ? <Mono>{r.missingRequiredFields!.join(", ")}</Mono> : "—"}
                    {(r.problems || []).length ? <div>{r.problems!.join("; ")}</div> : null}
                  </span>
                </div>
              ))}
            </GridTable>
            {out.rejections.length ? (
              <div style={{ marginTop: 10 }}>
                <span className="eyebrow">Refused rows</span>
                <KVR rows={out.rejections.map((x) => [<Mono>{x.ref}</Mono>, x.reason])} />
              </div>
            ) : null}
            <p className="muted" style={{ marginTop: 8 }} data-dryrun-note>
              {out.note}
            </p>
          </CardTitled>
          <Callout kind="green" title="Nothing was written">
            No unit, no claim, no source registration, no queue entry. The endpoint read the draft, compiled it in
            memory and answered — the tiers above were derived by the real strength function from the provenance you
            supplied, and stored nowhere. Running it again with a different source class gives a different answer from
            the same rows, which is what "derived, never stored" means in practice.
          </Callout>
        </>
      ) : (
        <Empty>
          No dry run has been made on this draft. Run the sample above to see the real adaptor's verdict on each row.
        </Empty>
      )}
      <Callout kind="teal" title="Read this one carefully">
        Unresolved workers are not a mapping fault. The source system knows them by an identifier CREST has never
        seen, which is the identity-resolution step, and it has no owner in the roles master. A dry run resolves
        nobody at all — at ingestion, a row whose worker cannot be resolved goes to the unclear queue with a reason,
        and never to a silent guess.
      </Callout>
    </Frame>
  );
}
export const DryRun = gate(DryRunBody);

// ── p3_28 · registered against one version only ────────────────────────────
function LiveBody({ d }: BodyProps) {
  const bound = d.submittedVersion || (d.baseVersion || 0) + 1;
  const next = bound + 1;
  const conn = d.doc.sources?.connections?.[0];
  return (
    <Frame
      counter="Connection · 5 of 5"
      title="This source is live"
      chip={d.submittedVersion ? <Chip kind="ok">bound to v{bound}</Chip> : <Chip kind="info">will bind to v{bound}</Chip>}
      lede={
        <>
          A source is registered against exactly one definition version — never against "the definition". That is the
          whole point of versioning: the rules a record was judged by are the rules that existed when it arrived, and
          a later version cannot reach back and rejudge it.
        </>
      }
      btns={[
        { label: "Back", to: "/define/dryrun", role: "secondary" },
        { label: "Continue to validation", to: "/define/validation", role: "primary" },
      ]}
    >
      <ClosedNote d={d} />
      <Card>
        <KVR
          rows={[
            ["source system", <Mono>{conn?.systemRef || "— not named yet —"}</Mono>],
            ["adaptor", <Mono>{conn?.adapterRef || "— not chosen yet —"}</Mono>],
            [
              "credential",
              conn?.credentialRef ? <Mono>{conn.credentialRef}</Mono> : "not supplied — the adaptor will not run until it is",
            ],
            [
              "bound to",
              d.submittedVersion ? (
                <>
                  v{bound} of <MonoShort id={d.definitionId || ""} />
                </>
              ) : (
                <>v{bound}, once this draft is submitted — a draft has no version to bind to</>
              ),
            ],
          ]}
        />
      </Card>
      <Callout kind="teal" title="What happens on the next version">
        This binding is to v{bound}. Publishing v{next} unbinds the adaptor, and records will queue rather than clear
        until someone re-tests it. That is deliberate — but nothing yet tells the source owner it has happened.
      </Callout>
      <OpenNote>
        <b>That last sentence is still true, and it is this wave's largest honest gap.</b> There is no notification to
        a source owner when a version bump unbinds their adaptor. The queue is the correct behaviour — clearing
        records against rules nobody re-tested is worse — but a partner discovering it by noticing that nothing
        arrived is not a design, and no screen here pretends otherwise.
      </OpenNote>
    </Frame>
  );
}
export const Live = gate(LiveBody);

// ── p3_10 · validation posture ─────────────────────────────────────────────
function ValidationBody({ d }: BodyProps) {
  const v = d.doc.validation || {};
  const [issuers, setIssuers] = useState((v.authorisedIssuers || ["did:crest:issuer:local"]).join(", "));
  const [who, setWho] = useState(v.posture || "");
  const [delay, setDelay] = useState(v.delayDays != null ? String(v.delayDays) : "");
  const [spec, setSpec] = useState(v.specifierPartyId || "");
  const [decidedBy, setDecidedBy] = useState(v.decidedBy || "author");
  return (
    <Frame
      counter="Validation · 8 of 9"
      title="Who decides how this is validated?"
      chip={
        <>
          <Chip kind="warn">Recommended, not built</Chip> <Chip kind="info">Symmetric with payment delegation</Chip>
        </>
      }
      btns={[
        { label: "Back", to: "/define/evidence", role: "secondary" },
        {
          label: "Continue",
          to: "/define/payment",
          role: "primary",
          onClick: () =>
            save(d.id, "validation", {
              authorisedIssuers: commas(issuers),
              specifierPartyId: spec.trim() || undefined,
              posture: who.trim() || undefined,
              delayDays: delay.trim() ? Number(delay.trim()) : undefined,
              decidedBy,
            }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <div style={grid()}>
        <OptionCard
          t="I'll set it now"
          s="You pick who validates, the timing and the intensity"
          on={decidedBy === "author"}
          onPick={() => setDecidedBy("author")}
        />
        <OptionCard
          t="Someone else will"
          s="Invite them. The field stays pending until they act."
          on={decidedBy === "delegate"}
          onPick={() => setDecidedBy("delegate")}
        />
      </div>
      <Card>
        <div style={grid()}>
          <RefField label="Who validates" hint="Recorded as classification.validationPosture — L2.">
            <input name="posture" value={who} onChange={(e) => setWho(e.target.value)} placeholder="District health office" />
          </RefField>
          <RefField
            label="Validation delay"
            hint="Days after payment. Programme policy (L2), read by nothing in the infrastructure."
          >
            <input name="delayDays" value={delay} onChange={(e) => setDelay(e.target.value)} placeholder="30" />
          </RefField>
          <RefField
            label="Authorised issuers"
            hint="This one IS infrastructure: verification refuses a credential from an issuer this list does not name."
          >
            <input name="issuers" value={issuers} onChange={(e) => setIssuers(e.target.value)} />
          </RefField>
          <RefField label="Specifier party" hint="Who specified these rules. Defaults to the author.">
            <input name="specifier" value={spec} onChange={(e) => setSpec(e.target.value)} placeholder={d.createdByPartyId} />
          </RefField>
        </div>
      </Card>
      <RefField label="Validator party types">
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {["External Evidence Contact", "Supervisor", "Collective or community body", "External verifier"].map((t) => (
            <Chip key={t} kind="plain">{t}</Chip>
          ))}
        </div>
      </RefField>
      <Callout kind="green">
        A village water-point committee approving a mechanic broke the flat taxonomy: the validator list assumed
        institutional hierarchy only, even though a matching validation tier already existed.
      </Callout>
      <Sidecar>
        Validation timing is now independent of payment timing. Paid immediately, validated 30 days later is a real
        pattern the coupled version could not express — a validation that happens after payment is a check on the
        programme, not a gate in front of the worker.
      </Sidecar>
    </Frame>
  );
}
export const Validation = gate(ValidationBody);

// ── p3_11 · the payment split, with delegates ──────────────────────────────
const RATE_CHOICES = ["Someone else will — invite sent", "I'll set this"];

function PaymentBody({ d }: BodyProps) {
  const p = d.doc.payment || {};
  const roles = p.roles || {};
  const [rate, setRate] = useState(roles.rateSetter || RATE_CHOICES[0]);
  const [mech, setMech] = useState(roles.mechanismSetter || RATE_CHOICES[1]);
  const persist = () => save(d.id, "payment", { ...p, roles: { ...roles, rateSetter: rate, mechanismSetter: mech } });
  return (
    <Frame
      counter="Payment · 9 of 9"
      title="What does this pay?"
      lede="Optional. A definition is fully valid for issuing credentials with no rate attached at all."
      btns={[
        { label: "Back", to: "/define/validation", role: "secondary" },
        { label: "Rate structures", to: "/define/tranches", role: "secondary", onClick: persist },
        { label: "Continue", to: "/define/roles", role: "primary", onClick: persist },
      ]}
    >
      <ClosedNote d={d} />
      {/* The frame's two-record split: the definition and the payment setup
          are separate records with separate owners, joined by reference. */}
      <div style={grid()}>
        <OptionCard t="Work definition" s="Owns the unit of work and the minimum evidence tier" />
        <OptionCard t="Payment set up" s="Owns the rate and the calculation once validated" />
      </div>
      <Card>
        <div style={grid(250)}>
          <RefField label="Who sets the rate">
            <select name="rateSetter" value={rate} onChange={(e) => setRate(e.target.value)}>
              {RATE_CHOICES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </RefField>
          <RefField label="Who sets the mechanism">
            <select name="mechanismSetter" value={mech} onChange={(e) => setMech(e.target.value)}>
              {RATE_CHOICES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </RefField>
        </div>
        <p className="muted" style={{ marginTop: 10 }}>
          Delegating is a complete answer. A definition with no rate attached is finished and usable — recognition is
          a use of its own, and a worker's record of what they did does not depend on anyone having priced it yet.
        </p>
      </Card>
      <Sidecar>
        A project can require stronger evidence to release money than it requires to issue the credential. A tier may
        be enough for the credential to exist and count toward a history, and still not enough to pay against.
      </Sidecar>

    </Frame>
  );
}
export const Payment = gate(PaymentBody);

// ── p3_20 · the four project roles ─────────────────────────────────────────
const PROJECT_ROLES: Array<[string, string, string]> = [
  ["rateSetter", "Rate owner", "Publishes the rate on the unit this definition declares. F-1's authority, and the only party who can price it."],
  ["mechanismSetter", "Payment mechanism owner", "Owns the rail money leaves by. F-2's authority; its activation gate sits in front of disbursement only."],
  ["validator", "Validator", "Checks the work after the fact, under the posture the validation screen recorded."],
  ["approver", "Definition approver", "Ratifies this definition. Cannot be the author — the service refuses a self-ratified version."],
];

function RolesBody({ d }: BodyProps) {
  const p = d.doc.payment || {};
  const [roles, setRoles] = useState<Record<string, string>>(p.roles || {});
  const persist = () => save(d.id, "payment", { ...p, roles });
  return (
    <Frame
      title="Who holds which role"
      lede={
        <>
          Four roles, four different parties, and the reason they are separate is that each is a place a decision can
          be traced back to. A role recorded here is descriptive; the authority itself lives in the service that
          enforces it.
        </>
      }
      btns={[
        { label: "Back to payment setup", to: "/define/payment", role: "secondary" },
        {
          label: "Preconditions & deductions",
          to: "/define/rules",
          role: "secondary",
          onClick: persist,
          note: (
            <>
              <b>This link is not in the reference's button graph.</b> The reference sends this frame's Continue to
              the tranche screen and sends the preconditions frame's Back to the payment screen — so nothing in it
              navigates <i>to</i> preconditions and deductions, and that screen is unreachable. It is a real screen
              with real answers, so this labelled link exists to reach it.
            </>
          ),
        },
        { label: "Continue", to: "/define/tranches", role: "primary", onClick: persist },
      ]}
    >
      <ClosedNote d={d} />
      <CardTitled t="The four roles">
        <div style={{ display: "grid", gap: 12 }}>
          {PROJECT_ROLES.map(([key, label, why]) => (
            <RefField key={key} label={label} hint={why}>
              <input
                data-role={key}
                value={roles[key] || ""}
                placeholder="a party id, or the role name the programme uses"
                onChange={(e) => setRoles({ ...roles, [key]: e.target.value })}
              />
            </RefField>
          ))}
        </div>
      </CardTitled>
      <Callout kind="green">
        Two roles held by the same person remain distinct roles — a Ministry may delegate issuing to district offices
        while keeping specification central. This screen writes names onto a payment-structure record; the authority
        itself lives in the service that enforces it, which is why the handoff screen after signing exists at all.
      </Callout>
      <Sidecar warm>
        A standing role invite covers every definition in the project. A one-off validation delegation would cover
        exactly one. Which of the two validation delegation should use is undecided.
      </Sidecar>
    </Frame>
  );
}
export const Roles = gate(RolesBody);

// ── p3_12 · stacked pay and tranches ───────────────────────────────────────
function TranchesBody({ d }: BodyProps) {
  const p = d.doc.payment || {};
  const [list, setList] = useState(p.tranches || []);
  const [label, setLabel] = useState("");
  const [share, setShare] = useState("");
  const [cond, setCond] = useState("");
  return (
    <Frame
      title="Payment tranches"
      chip={
        <>
          <Chip kind="err">Proposed</Chip> <Chip kind="plain">Built nowhere</Chip>
        </>
      }
      lede={
        <>
          Work that pays in stages — some on completion, some on a later check. Each tranche is a share of a rate
          somebody else will publish, and a condition for releasing it. Shares, never amounts: pricing belongs to the
          rate owner and this screen cannot preempt it.
        </>
      }
      btns={[
        { label: "Back", to: "/define/payment", role: "secondary" },
        {
          label: "Continue",
          to: "/define/rules",
          role: "primary",
          onClick: () => save(d.id, "payment", { ...p, tranches: list }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <CardTitled t="Tranches" chip={<Chip kind="info">{list.length}</Chip>}>
        <GridTable cols="1.3fr .6fr 1.6fr auto" head={["Tranche", "Share", "Released when", ""]}>
          {list.length ? (
            list.map((t, i) => (
              <div className="g-row" key={i}>
                <span>{t.label}</span>
                <span>{t.share || "—"}</span>
                <span>{t.condition || "—"}</span>
                <span>
                  <button
                    className="btn secondary"
                    style={{ width: "auto", padding: "4px 9px", fontSize: 11.5 }}
                    onClick={() => setList(list.filter((_, j) => j !== i))}
                  >
                    Remove
                  </button>
                </span>
              </div>
            ))
          ) : (
            <div className="g-row">
              <span style={{ gridColumn: "1 / -1", color: "var(--text-2)" }}>
                No tranches. The work pays once, on the unit — which is a complete answer and the common one.
              </span>
            </div>
          )}
        </GridTable>
        <form
          id="trancheform"
          style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end", marginTop: 12 }}
          onSubmit={(ev) => {
            ev.preventDefault();
            if (!label.trim()) return;
            setList([...list, { label: label.trim(), share: share.trim() || undefined, condition: cond.trim() || undefined }]);
            setLabel("");
            setShare("");
            setCond("");
          }}
        >
          <RefField label="Tranche">
            <input name="tranchelabel" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="On completion" />
          </RefField>
          <RefField label="Share">
            <input name="trancheshare" value={share} onChange={(e) => setShare(e.target.value)} placeholder="70%" />
          </RefField>
          <RefField label="Released when">
            <input name="tranchecondition" value={cond} onChange={(e) => setCond(e.target.value)} placeholder="The confirmation window exits" />
          </RefField>
          <button className="btn inline" type="submit">
            Add it
          </button>
        </form>
      </CardTitled>
      <RefField label="Rate structure types">
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {["Flat", "Volume band", "Geography", "Tenure", "Quality", "Rate table — per item", "Externally indexed"].map(
            (t) => (
              <Chip key={t} kind="plain">{t}</Chip>
            ),
          )}
        </div>
      </RefField>
      <Sidecar warm>
        Also unresolved here: gig-delivery surge pricing and market-price-indexed procurement both need a rate that
        varies over time or against an external index. Neither is expressible today. And triggers are bare category
        labels with no threshold — a response-time bonus needs a value and a unit attached to the trigger, not just a
        name.
      </Sidecar>
      <Sidecar>
        A tranche's condition is a release condition, not a hold. Every confirmation-window exit still creates its
        payment obligation; what a tranche decides is how that obligation is split across stages, and a stage that has
        not come due yet has a reason and an owner attached to it.
      </Sidecar>
    </Frame>
  );
}
export const Tranches = gate(TranchesBody);

// ── p3_13 · preconditions and deductions ───────────────────────────────────
function RulesBody({ d }: BodyProps) {
  const p = d.doc.payment || {};
  const [pre, setPre] = useState((p.preconditions || []).join("\n"));
  const [ded, setDed] = useState(p.deductions || []);
  const [dl, setDl] = useState("");
  const [dr, setDr] = useState("");
  return (
    <Frame
      title="Before it counts, and what reduces it"
      lede={
        <>
          Two different kinds of rule, and confusing them is expensive. A precondition decides whether the work counts
          at all. A deduction decides how much of a rate reaches the worker once it does.
        </>
      }
      btns={[
        { label: "Back", to: "/define/payment", role: "secondary" },
        {
          label: "Continue",
          to: "/define/extend",
          role: "primary",
          onClick: () => save(d.id, "payment", { ...p, preconditions: lines(pre), deductions: ded }),
        },
      ]}
    >
      <ClosedNote d={d} />
      <CardTitled t="Preconditions — before it counts">
        <RefField label="Preconditions" hint="One per line. Each decides whether a unit is countable at all.">
          <textarea
            name="preconditions"
            rows={3}
            style={areaStyle}
            value={pre}
            onChange={(e) => setPre(e.target.value)}
            placeholder={"The worker completed the level-1 training definition.\nThe household is inside the campaign district."}
          />
        </RefField>
      </CardTitled>
      <CardTitled t="Deductions — what reduces it" chip={<Chip kind="info">{ded.length}</Chip>}>
        <GridTable cols="1fr 1.7fr auto" head={["Deduction", "Rule", ""]}>
          {ded.length ? (
            ded.map((x, i) => (
              <div className="g-row" key={i}>
                <span>{x.label}</span>
                <span>{x.rule}</span>
                <span>
                  <button
                    className="btn secondary"
                    style={{ width: "auto", padding: "4px 9px", fontSize: 11.5 }}
                    onClick={() => setDed(ded.filter((_, j) => j !== i))}
                  >
                    Remove
                  </button>
                </span>
              </div>
            ))
          ) : (
            <div className="g-row">
              <span style={{ gridColumn: "1 / -1", color: "var(--text-2)" }}>
                No deductions. Nothing reduces what the rate pays.
              </span>
            </div>
          )}
        </GridTable>
        <form
          id="deductionform"
          style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end", marginTop: 12 }}
          onSubmit={(ev) => {
            ev.preventDefault();
            if (!dl.trim() || !dr.trim()) return;
            setDed([...ded, { label: dl.trim(), rule: dr.trim() }]);
            setDl("");
            setDr("");
          }}
        >
          <RefField label="Deduction">
            <input name="deductionlabel" value={dl} onChange={(e) => setDl(e.target.value)} placeholder="Equipment advance" />
          </RefField>
          <RefField label="Rule">
            <input name="deductionrule" value={dr} onChange={(e) => setDr(e.target.value)} placeholder="10% of each cycle until the advance is cleared" />
          </RefField>
          <button className="btn inline" type="submit">
            Add it
          </button>
        </form>
      </CardTitled>
      <Sidecar>
        A precondition is not evidence. It answers "was this work allowed to happen" rather than "did it happen", and
        the two need different fields. Checked before the shift, not after.
      </Sidecar>
      <Sidecar>
        Neither is a reason to withhold. A deduction reduces an amount and names itself while doing it; it never turns
        into a payment held with no explanation, because every held payment has to carry a reason with an owner.
      </Sidecar>
    </Frame>
  );
}
export const Rules = gate(RulesBody);

// ── p3_14 · the two kinds of extension ─────────────────────────────────────
function ExtendBody({ d }: BodyProps) {
  const [key, setKey] = useState("");
  const [label, setLabel] = useState("");
  const [type, setType] = useState("string");
  const [value, setValue] = useState("");
  const [fields, setFields] = useState(d.doc.extensions || {});
  return (
    <Frame
      title="When the form does not have what you need"
      btns={[
        { label: "Back", to: "/define/rules", role: "secondary" },
        { label: "Continue", to: "/define/open", role: "primary", onClick: () => save(d.id, "extensions", fields) },
      ]}
    >
      <ClosedNote d={d} />
      <div style={grid()}>
        <OptionCard
          t="A new value in an existing slot"
          tag={<Chip kind="ok" sm>Live today</Chip>}
          s="A category name, a deduction reason, a party label"
          ex="Free and immediate. No approval, no core change. Repeated independent use of the same value is the signal it should graduate into shared vocabulary."
        />
        <OptionCard
          t="A genuinely new field"
          tag={<Chip kind="warn" sm>This screen</Chip>}
          s="A contractor reference, an accreditation number"
          ex="Added under a namespaced key. No approval either — but only a screen built to read that key will understand it."
        />
      </div>
      <CardTitled t="Institution extension fields" chip={<Chip kind="info">{Object.keys(fields).length}</Chip>}>
        <GridTable cols="1.4fr 1.2fr .6fr 1.2fr auto" head={["Field key", "Label", "Type", "Value", ""]}>
          {Object.keys(fields).length ? (
            Object.entries(fields).map(([k, f]) => (
              <div className="g-row" key={k}>
                <span>
                  <Mono>{k}</Mono>
                </span>
                <span>{f.label}</span>
                <span>{f.valueType}</span>
                <span>{f.value}</span>
                <span>
                  <button
                    className="btn secondary"
                    style={{ width: "auto", padding: "4px 9px", fontSize: 11.5 }}
                    onClick={() => {
                      const next = { ...fields };
                      delete next[k];
                      setFields(next);
                    }}
                  >
                    Remove
                  </button>
                </span>
              </div>
            ))
          ) : (
            <div className="g-row">
              <span style={{ gridColumn: "1 / -1", color: "var(--text-2)" }}>
                No extension fields. The form had what this definition needed.
              </span>
            </div>
          )}
        </GridTable>
        <form
          id="extensionform"
          style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end", marginTop: 12 }}
          onSubmit={(ev) => {
            ev.preventDefault();
            if (!key.trim()) return;
            setFields({ ...fields, [key.trim()]: { label: label.trim(), valueType: type, value: value.trim() } });
            setKey("");
            setLabel("");
            setValue("");
          }}
        >
          <RefField label="Field key" hint="Namespaced, so it can never collide with a CREST field.">
            <input name="extkey" value={key} onChange={(e) => setKey(e.target.value)} placeholder="mgnrega.contractorRef" />
          </RefField>
          <RefField label="Label">
            <input name="extlabel" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Contractor reference" />
          </RefField>
          <RefField label="Value type">
            <select name="exttype" value={type} onChange={(e) => setType(e.target.value)}>
              {["string", "number", "boolean", "date"].map((t) => (
                <option key={t} value={t}>
                  {t === "string" ? "Text" : t[0].toUpperCase() + t.slice(1)}
                </option>
              ))}
            </select>
          </RefField>
          <RefField label="Value">
            <input name="extvalue" value={value} onChange={(e) => setValue(e.target.value)} placeholder="MGN/2026/WD/0841" />
          </RefField>
          <button className="btn inline" type="submit">
            Add it
          </button>
        </form>
        <p className="muted" style={{ marginTop: 8 }}>
          The declared type is checked at submit: a value that does not read as its type is refused by name. A typed
          escape hatch that does not check its types is an untyped one.
        </p>
      </CardTitled>
      <Callout kind="teal" title="The other kind of extension">
        If what is missing is a field <i>CREST</i> should have had — something every deployment would need, not just
        this institution — then adding it here is the wrong answer, and the dangerous one: the design error survives
        into a pilot wearing a passing test. That case is a design finding, raised against the blueprint and corrected
        there.
      </Callout>
      <Sidecar>
        Neither path sends the author somewhere else to file a request. The promotion of a repeated key into shared
        vocabulary happens later, and invisibly to them.
      </Sidecar>
    </Frame>
  );
}
export const Extend = gate(ExtendBody);

// ── p3_18 · what is still undecided, promoted onto the real draft ──────────
// The reference draws this as a static list of six. Here it is the validate
// endpoint's real open-questions list, which is the same compile() submit runs
// — so this screen can never disagree with the submission it precedes, and it
// is also where the submission happens.
function OpenQuestionsBody({ d, reload }: BodyProps) {
  const nav = useNavigate();
  const r = useLoad<Validation>(() => api.post("definitions", dpath(d.id, "/validate")), [d.id, d.updatedAt, d.state]);
  return (
    <LoadFrame r={r}>
      {(v) => (
        <Frame
          title={
            v.problems.length
              ? `${v.problems.length} open question${v.problems.length > 1 ? "s" : ""}`
              : "Nothing is undecided"
          }
          chip={v.ready ? <Chip kind="ok">ready to submit</Chip> : <Chip kind="warn">{v.problems.length} unresolved</Chip>}
          lede={
            <>
              Not a checklist somebody maintained by hand. This is what the compiler says stands between this draft
              and a definition version, produced by the same function the submit runs — so a draft that reads ready
              here submits, and one that does not lists why rather than failing at the last step.
            </>
          }
          btns={[
            { label: "Back to the journey", to: "/definework", role: "primary" },
            { label: "See the schema under the form", to: "/define/anatomy", role: "secondary" },
            ...(d.state === "OPEN"
              ? [
                  {
                    label: "Submit for ratification",
                    role: "secondary" as const,
                    onClick: async () => {
                      await api.post("definitions", dpath(d.id, "/submit"));
                      reload();
                      nav("/define/anatomy");
                    },
                    note: v.ready ? undefined : (
                      <>
                        <b>Submitting now will be refused, by name.</b> The service runs this same list and returns{" "}
                        <span className="mono">not_ready</span> with every problem attached — it does not guess at an
                        answer, and it does not submit a half-definition that a verifier would later have to make
                        sense of.
                      </>
                    ),
                  },
                ]
              : []),
          ]}
        >
          <ClosedNote d={d} />
          {v.problems.length ? (
            <CardTitled t="What is still open">
              <GridTable cols=".8fr 1fr 2.2fr" head={["Section", "Field", "Why it is open"]}>
                {v.problems.map((p, i) => (
                  <div className="g-row" key={i} data-problem={p.section + "." + (p.field || "")}>
                    <span>
                      <Chip kind="plain" sm>
                        {p.section}
                      </Chip>
                    </span>
                    <span>{p.field ? <Mono>{p.field}</Mono> : "—"}</span>
                    <span style={{ fontSize: 12.4 }}>{p.reason}</span>
                  </div>
                ))}
              </GridTable>
              <p className="muted" style={{ marginTop: 8 }}>
                Each row names the section a person would go back to. An open question is not an error: it is a draft
                being honest about being unfinished, which is the normal state of one for most of its life.
              </p>
            </CardTitled>
          ) : (
            <Card hi>
              <b>Every section the compiler needs is answered.</b> Submitting appends the next immutable version in
              DRAFT state, awaiting a ratifier who is not you — the definitions row, the linked records this draft
              implies, the SUBMITTED event and this draft's closure all commit together or not at all.
            </Card>
          )}
          {d.state === "SUBMITTED" ? (
            <NextBlock
              happened={
                <>
                  This draft was compiled into v{d.submittedVersion} of <MonoShort id={d.definitionId || ""} />, in
                  DRAFT state.
                </>
              }
              who="A definition approver — a different party from you, or the service refuses the signature."
              when="Whenever they open their own ratification queue; nothing here can hurry it."
              told="The version's own event log records the ratification with the ratifier's name against it."
              ifnot="The version stays in DRAFT indefinitely. It is not active, nothing can be issued against it, and no worker is affected — an unratified definition is inert rather than dangerous."
            />
          ) : null}
          <Callout kind="teal" title="Why this screen is generated and not written">
            A hand-maintained list of open questions is right on the day somebody writes it and wrong every day after.
            This one is derived from the draft each time it is opened, by the function that decides whether the submit
            succeeds — so a count of unresolved questions is a count of real refusals, not a note somebody forgot to
            update.
          </Callout>
        </Frame>
      )}
    </LoadFrame>
  );
}
export const OpenQuestions = gate(OpenQuestionsBody);

// ── p3_19 · the schema under the form ──────────────────────────────────────
// Promoted onto the real draft: the two layers are the base primitive and this
// institution's extension, and both are read from the actual stored document
// rather than illustrated.
function AnatomyBody({ d }: BodyProps) {
  const defId = d.definitionId || "";
  const version = d.submittedVersion || d.baseVersion || 0;
  const r = useLoad<Record<string, unknown> | null>(
    () =>
      defId && version
        ? api.get("definitions", defpath(defId, `?version=${version}`))
        : Promise.resolve(null),
    [defId, version],
  );
  return (
    <LoadFrame r={r}>
      {(def) => {
        const doc = (def || d.doc) as Record<string, unknown>;
        const extensions = (def ? (def.extensions as Record<string, unknown>) : d.doc.extensions) || {};
        const base = { ...doc };
        delete base.extensions;
        return (
          <Frame
            title="Two layers, versioned separately"
            chip={def ? <Chip kind="ok">v{version}, as stored</Chip> : <Chip kind="info">draft document</Chip>}
            lede={
              <>
                The form above is a way of filling this in; this is the thing itself. Two layers: the base primitive,
                which every CREST deployment shares and which the schema validates, and the institution extension,
                which is this deployment's own and which the infrastructure never reads.
              </>
            }
            btns={[
              { label: "Back to the registry", to: "/definework", role: "secondary" },
              { label: "The open questions", to: "/define/open", role: "secondary" },
            ]}
          >
            <CardTitled
              t="Layer 1 — the base primitive"
              chip={<Chip kind="brand">{def ? "definition.schema.json" : "draft document"}</Chip>}
            >
              <p className="muted" style={{ marginBottom: 8 }}>
                {def ? (
                  <>
                    The stored version, exactly as a verifier resolving this credential would read it. It is
                    immutable: v{version} will return this same document forever, which is what makes a credential
                    pinned to it meaningful years later.
                  </>
                ) : (
                  <>
                    This draft's own document. It is mutable, it is nobody's source of truth, and it becomes a
                    base-primitive document only when the compiler translates it at submit.
                  </>
                )}
              </p>
              <div
                className="consent-quote"
                style={{ overflowX: "auto", maxHeight: 420, overflowY: "auto" }}
                data-anatomy-base
              >
                <pre style={{ margin: 0, font: "400 11.6px/1.55 var(--mono)" }}>{JSON.stringify(base, null, 2)}</pre>
              </div>
            </CardTitled>
            <CardTitled
              t="Layer 2 — the institution extension"
              chip={<Chip kind="info">{Object.keys(extensions).length} fields</Chip>}
            >
              {Object.keys(extensions).length ? (
                <div className="consent-quote" style={{ overflowX: "auto" }} data-anatomy-ext>
                  <pre style={{ margin: 0, font: "400 11.6px/1.55 var(--mono)" }}>
                    {JSON.stringify(extensions, null, 2)}
                  </pre>
                </div>
              ) : (
                <Empty>No extension fields. The base primitive had everything this definition needed.</Empty>
              )}
              <p className="muted" style={{ marginTop: 8 }}>
                Namespaced keys, declared types, values as text. Nothing in the infrastructure reads this layer —
                which is what makes it safe for a deployment to put anything it needs here without asking permission,
                and why it can never be the answer to a field CREST itself is missing.
              </p>
            </CardTitled>
            <Callout kind="teal" title="Versioned separately">
              The two layers move independently. A deployment can add an extension field without any other deployment
              knowing or caring, because no shared schema changed. A change to the base primitive is the opposite kind
              of event: it is a change to what "a work definition" means everywhere, and it goes through the blueprint
              rather than through this form.
            </Callout>
          </Frame>
        );
      }}
    </LoadFrame>
  );
}
export const Anatomy = gate(AnatomyBody);

// ── p3_pay · handing pricing to a rate owner ───────────────────────────────
// The definition is signed and nothing prices it. That is a complete state, and
// this screen's job is to make it visible and hand it on rather than let a
// worker discover it as a missing payment.
function HandoffBody({ d }: BodyProps) {
  const s = useConsole();
  const [gen, setGen] = useState(0);
  const [invited, setInvited] = useState<string>(FIX.org);
  const [note, setNote] = useState("");
  const defId = d.definitionId || "";
  const version = d.submittedVersion || d.baseVersion || 0;
  const r = useLoad(async () => {
    if (!defId) return { records: [] as LinkedRec[], events: [] as DefEvent[] };
    const [lr, ev] = await Promise.all([
      api.get("definitions", defpath(defId, "/linked-records")),
      api.get("definitions", defpath(defId, "/events")).catch(() => ({ events: [] })),
    ]);
    return {
      records: (lr.linkedRecords || []) as LinkedRec[],
      events: (ev.events || []) as DefEvent[],
    };
  }, [defId, gen]);
  return (
    <LoadFrame r={r}>
      {({ records, events }) => {
        const handoffs = records.filter((x) => x.type === "payment-handoff");
        const priced = records.filter((x) => x.type === "payment-setup");
        const acts = events.filter((e) => e.action === "PAYMENT_HANDOFF");
        return (
          <Frame
            title="The definition is signed. Nothing prices it yet."
            chip={
              priced.length ? (
                <Chip kind="ok">a rate is attached</Chip>
              ) : handoffs.length ? (
                <Chip kind="info">handed off, awaiting a rate</Chip>
              ) : (
                <Chip kind="warn">unpriced</Chip>
              )
            }
            lede="Payment is optional, and it is not your role — but nobody has been given it."
            btns={[
              { label: "Back", to: "/ratified", role: "secondary" },
              {
                label: "Send the invitation",
                role: "primary",
                onClick: async () => {
                  await api.post("definitions", defpath(defId, `/versions/${version}/payment-handoff`), {
                    invitedPartyId: invited.trim(),
                    invitedByPartyId: s.me!.partyId,
                    note: note.trim() || undefined,
                  });
                  setGen((g) => g + 1);
                },
                note: (
                  <>
                    <b>The invitation is a record, not an authority.</b> It composes with the rate owner's assignment
                    in the payments service (F-1) and does not substitute for it: sending this does not let the
                    invited party price anything until they are actually assigned. They pick it up in their own
                    session — this console cannot act as them, which is the separation working.
                  </>
                ),
              },
            ]}
          >
            <Card>
              <KVR
                rows={[
                  ["definition", defId ? <MonoShort id={defId} /> : "— this draft has no version yet —"],
                  ["version", version ? "v" + version : "—"],
                  ["unit the rate will price", <Mono>{d.doc.activity?.outcomeUnit || "—"}</Mono>],
                  ["rate attached", priced.length ? "yes" : "no — nothing prices this unit"],
                ]}
              />
            </Card>
            <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
              <OptionCard
                t="Hand payment set up to someone else"
                s="Send an invitation. They arrive with the project and the unit already explained."
              />
              <OptionCard t="Set it up myself" s="Only if you also hold the Rate Owner role on this project." />
              <OptionCard
                t="Leave payment unset"
                s="A valid end state. The project recognises and credentials work without paying for it."
              />
            </div>
            <Sidecar>
              This is the third way into payment set up. The Project Configurator can assign owners up front, this
              screen can hand it off after the fact, or an owner can be invited directly.
            </Sidecar>
            <CardTitled t="Invite whoever will own the rate">
              <div style={grid()}>
                <RefField label="Invited party">
                  <input name="invited" value={invited} onChange={(e) => setInvited(e.target.value)} />
                </RefField>
                <RefField label="Note" hint="What you want them to know. Recorded on the handoff.">
                  <input
                    name="handoffnote"
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    placeholder="Priced per bednet distributed; campaign starts 1 March."
                  />
                </RefField>
              </div>
            </CardTitled>
            {handoffs.length ? (
              <CardTitled t="Handoffs recorded" chip={<Chip kind="ok">{handoffs.length}</Chip>}>
                <GridTable cols="1fr 1fr 1.6fr .8fr" head={["Invited", "By", "Note", "When"]}>
                  {handoffs.map((h) => (
                    <div className="g-row" key={h.id} data-handoff={h.id}>
                      <span>
                        <MonoShort id={(h.payload?.invitedPartyId as string) || "—"} />
                      </span>
                      <span>
                        <MonoShort id={(h.payload?.invitedByPartyId as string) || "—"} />
                      </span>
                      <span style={{ fontSize: 12.3 }}>{(h.payload?.note as string) || "—"}</span>
                      <span>{when(h.createdAt)}</span>
                    </div>
                  ))}
                </GridTable>
                {acts.length ? (
                  <p className="muted" style={{ marginTop: 8 }}>
                    {acts.length} handoff act{acts.length > 1 ? "s" : ""} in the event log, each naming who invited
                    whom.
                  </p>
                ) : null}
              </CardTitled>
            ) : null}
            {handoffs.length ? (
              <NextBlock
                happened="The hand-over of pricing is recorded on the definition, with your name on it."
                who="The invited rate owner, once the payments service actually assigns them the authority."
                when="At their own pace. The definition is active and evidence can already be recorded against it."
                told="The definition's event log carries a PAYMENT_HANDOFF entry naming who invited whom."
                ifnot="The unit stays unpriced and no payment obligation can be computed for it. Workers' records still accumulate — the work is recognised — but nothing prices it, and that is visible here rather than as a payment nobody can explain."
              />
            ) : null}
            <Callout kind="teal" title="Why this is a screen and not a silence">
              An unpriced definition that nobody was told about becomes a worker with a record of real work and no
              payment, and no explanation attached to the absence. The handoff exists so the gap has a name, a date
              and somebody's signature on it before anyone does the work.
            </Callout>
          </Frame>
        );
      }}
    </LoadFrame>
  );
}
export const Handoff = gate(HandoffBody);
