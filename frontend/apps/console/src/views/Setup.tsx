// J3 — "Setting up a project": the reference's own console frames for the
// Org Admin (p1_1–p1_3) and the Project Configurator (p2_1–p2_21).
//
// Fidelity rule for this file: every title, lede, option label, field label,
// hint and callout is the 17 Aug reference's text VERBATIM
// (docs/journey-spec.json + the frame itself). Numbers, names and states are
// read from a running service; where the reference shows a decision this
// deployment cannot yet persist, the frame renders in full and an honest note
// says so — the copy is never softened to hide the gap.
import { useState } from "react";
import { api, FIX } from "@crest/api";
import { Callout, Chip, OptionCard, RefField, OpenNote, StepCounter } from "@crest/ui";
import { Card, CardTitled, KVR, Lede, LoadFrame, Mono, MonoShort, Stat, Title, useLoad, when } from "../ui";
import { useConsole } from "../state";
import { useNavigate } from "react-router-dom";

// The reference's action row: secondary left, primary right.
function Actions(props: { back?: [string, () => void]; go?: [string, () => void] }) {
  return (
    <div className="btn-row" style={{ maxWidth: 520 }}>
      {props.back ? (
        <button className="btn secondary" data-act="secondary" onClick={props.back[1]}>
          {props.back[0]}
        </button>
      ) : null}
      {props.go ? (
        <button className="btn dominant" data-act="primary" onClick={props.go[1]}>
          {props.go[0]}
        </button>
      ) : null}
    </div>
  );
}

// ── p1_1 · Standing configuration, not project work ─────────────────────────
export function Projects() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const [org, reg, terms] = await Promise.all([
      api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`),
      api.get("parties", `/v1/organisations/${encodeURIComponent(FIX.org)}/registration`).catch(() => null),
      api.get("parties", "/v1/terms").catch(() => ({ terms: [] })),
    ]);
    return { org: org.party || org, reg, terms: terms.terms || [] };
  });
  return (
    <LoadFrame r={r}>
      {({ org, reg }) => (
        <>
          <Title t={"Welcome to " + (org.displayName || "your organisation")} />
          <Lede>
            Your organisation is set up and ready. Add your first project to start defining work and registering
            workers.
          </Lede>
          <div className="pane-cols">
            <div>
              <CardTitled
                t="Terms held"
                chip={<Chip kind={reg && reg.state === "APPROVED" ? "ok" : "warn"}>{(reg && reg.state) || "unknown"}</Chip>}
              >
                <KVR
                  rows={[
                    ["version", reg && reg.termsVersion ? <Mono>{reg.termsVersion}</Mono> : "no acceptance on record"],
                    ["decided", reg ? when(reg.decidedAt) : "—"],
                    ["decided by", reg && reg.decidedBy ? <Mono>{reg.decidedBy}</Mono> : "—"],
                  ]}
                />
              </CardTitled>
              <CardTitled t="Worker Registry Custodian (Worker Registry Custodian)" chip={<Chip kind="info">Held here</Chip>}>
                <p className="body-2">Sits inside this organisation. Cannot be delegated out.</p>
                <div style={{ height: 8 }} />
                <KVR rows={[["custodian party", <MonoShort id={FIX.custodian} />]]} />
              </CardTitled>
            </div>
            <div>
              <div className="stats">
                <Stat n={<span data-stat="projects">—</span>} label="Projects · ready to add your first" />
                <Stat n={1} label="People in roles · you, as Org Admin" />
              </div>
              <Callout kind="teal" title="What an Org Admin holds">
                An Org Admin manages standing configuration across every project this organisation runs. It does not
                configure any individual project — that is a separate role, assigned per project.
              </Callout>
            </div>
          </div>
          <OpenNote>
            The project count reads “—” because this deployment has no project record to count yet: creating a project
            (<span className="mono">p1_3</span>) and handing it to a Configurator is the J3 backend's work, and the
            console will not invent a zero it cannot read. Everything else on this frame is a live registry read.
          </OpenNote>
          <Actions back={["Assign people to roles", () => nav("/people")]} go={["Create a project", () => nav("/projects/new")]} />
        </>
      )}
    </LoadFrame>
  );
}

// ── p1_3 · Creating a project is not configuring one ────────────────────────
// The reference frame's own fields, with nothing to write them to yet: project
// creation and the named-Configurator handover are the J3 backend's, and this
// screen says so rather than collecting three answers into a browser tab.
export function NewProject() {
  const nav = useNavigate();
  return (
    <>
      <Title t="Creating a project, and handing it over" />
      <Lede>
        The Org Admin names the project and appoints who configures it. What the project actually does is not decided
        here.
      </Lede>
      <div className="form-grid">
        <RefField label="Project name">
          <input name="projectname" placeholder="the name workers and funders will see" />
        </RefField>
        <RefField label="Coverage">
          <input name="coverage" placeholder="wards, sub-counties, districts" />
        </RefField>
        <RefField
          label="Project Configurator (Project Configurator)"
          hint="Also holds Work Definition Author on this project. In small organisations one person often holds both; the roles stay separate."
        >
          <input name="configurator" placeholder="the person who will configure it" />
        </RefField>
      </div>
      <Callout kind="green" title="What is deliberately not on this screen">
        Nothing about registration, payment or validation is chosen on this screen. Those are compositability choices,
        and they belong to whoever configures the project, not to whoever creates it.
      </Callout>
      <OpenNote>
        This frame cannot be submitted on this deployment: there is no project record and no ownership handover to
        write, so the form is shown as the reference draws it and nothing is saved. A named owner who never agreed is
        exactly the failure the handover exists to prevent — inventing one here would be worse than the gap.
      </OpenNote>
      <Actions back={["Back", () => nav("/people")]} go={["Back to standing configuration", () => nav("/projects")]} />
    </>
  );
}

// ── p1_2 (Org Admin) / n5 (Configurator) · one route, two authorities ───────
// F1's rule in code: People & roles is never hidden from the Configurator.
// They open the same rail entry and get the same data, read only, with the
// role they would need and somebody who can grant it named on the face of it.
export function People() {
  const s = useConsole();
  return s.persona === "orgadmin" ? <RoleHolders /> : <RoleGuard />;
}

// n5 — People & roles is not yours to change.
function RoleGuard() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const q = new URLSearchParams({ partyId: FIX.org });
    const [grants, org] = await Promise.all([
      api.get("parties", "/v1/authorizations?" + q).catch(() => ({ authorizations: [] })),
      api.get("parties", `/v1/parties/${encodeURIComponent(FIX.org)}`).catch(() => null),
    ]);
    return {
      grants: (grants.authorizations || []) as Array<{
        partyId?: string; functions?: string[]; authorityPartyId?: string; period?: { from?: string };
      }>,
      org: org && (org.party || org),
    };
  });
  return (
    <LoadFrame r={r}>
      {({ grants, org }) => (
        <>
          <Title t="People & roles is not yours to change" />
          <Lede>
            Granting and removing roles belongs to the Org Admin. Your authority is this project, and it stops at its
            edge — which is a boundary, not an error.
          </Lede>
          <CardTitled t="Who can do this">
            <KVR
              rows={[
                ["the role you would need", "Org Admin"],
                ["held at", org?.displayName ? String(org.displayName) : <MonoShort id={FIX.org} />],
                [
                  "how to reach them",
                  (org?.contactRoutes || []).length
                    ? (org.contactRoutes as Array<{ kind: string; value?: string }>)
                        .map((rt) => rt.kind + " " + (rt.value || ""))
                        .join(" · ")
                    : "no contact route is recorded on the organisation — that gap is itself worth raising",
                ],
              ]}
            />
            <p className="muted" style={{ marginTop: 8 }}>
              A refusal without an owner is a dead end, and this system does not leave people at dead ends.
            </p>
          </CardTitled>
          <CardTitled t="What you can see here, read only">
            <KVR
              rows={grants.map((a) => [
                <MonoShort id={a.partyId} />,
                <>
                  {(a.functions || []).join(", ")} · granted by <MonoShort id={a.authorityPartyId || ""} />
                  {a.period?.from ? " · " + when(a.period.from) : ""}
                </>,
              ])}
            />
            {grants.length ? null : (
              <div className="muted">
                Nothing is readable here yet. An empty list is a true answer: it means a role still has to be granted.
              </div>
            )}
          </CardTitled>
          <Callout kind="green" title="The rule this screen sets">
            No blank refusal, and no vanished entry. A guard states the role you would need, names somebody who can
            grant it, and still shows whatever you are allowed to read.
          </Callout>
          <Actions back={["Back to project setup", () => nav("/org")]} />
        </>
      )}
    </LoadFrame>
  );
}

// p1_2 — a role is held, not just recorded.
function RoleHolders() {
  const nav = useNavigate();
  const r = useLoad(async () => {
    const q = new URLSearchParams({ partyId: FIX.org });
    const out = await api.get("parties", "/v1/authorizations?" + q).catch(() => ({ authorizations: [] }));
    return (out.authorizations || []) as Array<{
      partyId?: string;
      functions?: string[];
      grantedByPartyId?: string;
      authorityPartyId?: string;
      period?: { from?: string; reviewBy?: string };
    }>;
  });
  return (
    <LoadFrame r={r}>
      {(list) => (
        <>
          <Title t="Putting named people into roles" />
          <Lede>An invitation goes to a work email. The person holds the role only once they have accepted it.</Lede>
          <CardTitled t="Grants standing in this organisation">
            <KVR
              rows={list.map((a) => [
                <MonoShort id={a.partyId} />,
                <>
                  {(a.functions || []).join(", ")} · granted by <MonoShort id={a.authorityPartyId || a.grantedByPartyId || ""} />
                  {a.period?.from ? " · " + when(a.period.from) : ""}
                </>,
              ])}
            />
            {list.length ? null : (
              <div className="muted">
                No grant is readable for this organisation. An empty list here is a true answer, not a blank screen: it
                means a role still has to be granted.
              </div>
            )}
          </CardTitled>
          <Callout kind="green" title="Why acceptance is the thing that matters">
            A role assignment that took effect without the person ever appearing would let an organisation attribute a
            signature to someone who had never logged in. Requiring acceptance keeps the person and the authority
            attached.
          </Callout>
          <OpenNote>
            Read-only on this deployment: the invited/active distinction, the vacant-role line and the invitation itself
            need a role-grant write path and a notification service. Nothing on this screen is invented — what you see
            is every authorization the registry will report for this organisation.
          </OpenNote>
          <Actions back={["Back", () => nav("/projects")]} go={["Create a project", () => nav("/projects/new")]} />
        </>
      )}
    </LoadFrame>
  );
}

// ── p2_2 · Registration and import are not alternatives ─────────────────────
export function Workers() {
  const nav = useNavigate();
  const [pick, setPick] = useState<string | null>(null);
  return (
    <>
      <Title t="Where the workers come from" />
      <Lede>
        Register them here, bring in records that already exist, or both. Both is common: a registry covers the known
        workers, enrollment covers the rest.
      </Lede>
      <div className="optcols">
        <OptionCard
          t="Register in CREST"
          s="Full enrollment — identity, consent, recovery contacts. Self-registration or assisted by a Registering Agent."
          on={pick === "register"}
          onPick={() => setPick("register")}
        />
        <OptionCard
          t="Import existing records"
          s="Map identifiers from a registry you already run onto Crest identities. Nobody is re-registered."
          on={pick === "import"}
          onPick={() => setPick("import")}
        />
        <OptionCard
          t="Import only"
          s="No enrollment at all. Every worker must already exist in the source registry."
          on={pick === "importonly"}
          onPick={() => setPick("importonly")}
        />
      </div>
      <RefField
        label="Source registry"
        hint="Whoever operates it holds Source-System Administrator (Source-System Administrator), and is accountable if a record there is wrong or stale."
      >
        <input name="sourceregistry" placeholder="the registry this project imports from" />
      </RefField>
      <Callout kind="teal">
        Importing does not create identities. It links an existing identifier to a Crest identity, so a worker already
        known to the county is not enrolled a second time under a new number.
      </Callout>
      <OpenNote>
        The choice on this frame is not yet stored anywhere: the project-composition record it belongs to is the J3
        backend's, and a browser-held answer would read as configuration that is not. Worker registration itself is
        real — assisted in the field door, self-service in the worker door.
      </OpenNote>
      <Actions back={["Back", () => nav("/projects")]} go={["Continue", () => nav("/definition")]} />
    </>
  );
}

// ── p2_4 · What happens when evidence does not clear ────────────────────────
export function Validation() {
  const nav = useNavigate();
  const [posture, setPosture] = useState("auto");
  return (
    <>
      <Title t="How validation runs" />
      <Lede>Whether a submitted work unit clears automatically, and what happens to one that does not.</Lede>
      <div className="optcols">
        <OptionCard
          t="Automatic where the tier allows, human review otherwise"
          s="Tier 1 evidence clears against the definition. Anything below goes to an Evidence Validator."
          on={posture === "auto"}
          onPick={() => setPosture("auto")}
        />
        <OptionCard
          t="Always human review"
          s="Every work unit is checked by a person, regardless of tier."
          on={posture === "human"}
          onPick={() => setPosture("human")}
        />
      </div>
      <RefField
        label="A returned work unit goes to"
        value="The organisation that submitted it"
        hint="Alternatives: to the Grievance Manager, or a combination. It never goes to the worker as a task, because the worker did not submit it."
      />
      <CardTitled t="Evidence Validator (Evidence Validator)" chip={<Chip kind="warn">Vacant</Chip>}>
        <p className="body-2">Not yet assigned for this project</p>
      </CardTitled>
      <Callout kind="green" title="What a vacancy costs">
        With no Evidence Validator assigned, anything that fails the automatic bar will queue rather than fail. The
        queue is real; nobody can currently work it.
      </Callout>
      <OpenNote>
        The queue is real and live — it is the unclear-row queue this console shows under Work status. The posture
        itself has nowhere to be written yet, so this frame chooses nothing on the service's behalf.
      </OpenNote>
      <Actions back={["Back", () => nav("/paysetup")]} go={["Continue", () => nav("/projects")]} />
    </>
  );
}

// ── p2_19 · Two ways in, and there is no third ──────────────────────────────
export function Intake() {
  const nav = useNavigate();
  const [ways, setWays] = useState<Record<string, boolean>>({ pull: true, upload: true });
  const toggle = (k: string) => setWays({ ...ways, [k]: !ways[k] });
  const r = useLoad(async () => {
    const out = await api.get("evidence", "/v1/sources").catch(() => ({ sources: [] }));
    return (out.sources || []) as Array<{ systemRef?: string; adapterRef?: string; expectedEvery?: string; state?: string }>;
  });
  return (
    <>
      <Title t="How does evidence reach CREST?" />
      <Lede>
        CREST does not record work. Somewhere else already does, and this decides how that record gets here. Both
        options can run at once, on different definitions.
      </Lede>
      <div className="optcols">
        <OptionCard
          t="A system CREST pulls from"
          s="An adaptor reads records on a schedule from a platform that already logs the work: a campaign system, a case-management tool, an HR or attendance system. Nobody retypes anything, and nobody in the chain had a reason to change the number."
          on={ways.pull}
          onPick={() => toggle("pull")}
        />
        <OptionCard
          t="A spreadsheet the project uploads"
          s="CREST generates a template from the work definition. Whoever holds the records fills it and the project uploads it. A person opened the file and typed into it, so it is weaker evidence than a system record, however careful they were."
          on={ways.upload}
          onPick={() => toggle("upload")}
        />
        <OptionCard
          t="Somebody entering work into CREST"
          s="This does not exist and will not be built. A record CREST captured itself would be evidence CREST is vouching for on its own authority, which is the one thing the trust model cannot do."
          unavailable
        />
      </div>
      <Callout kind="green" title="Why both, usually">
        The two are not alternatives, and most real projects need both. A campaign system covers the work it manages,
        and there is always work it does not: a training an institute ran, a round a contractor completed, a month a
        partner recorded on paper.
      </Callout>
      <LoadFrame r={r}>
        {(sources) => (
          <div className="form-grid">
            <RefField label="Pull from">
              <select name="pullfrom" defaultValue="">
                <option value="">—</option>
                {sources.map((s) => (
                  <option key={s.systemRef || s.adapterRef} value={s.systemRef || s.adapterRef}>
                    {s.systemRef || s.adapterRef} {s.expectedEvery ? "· " + s.expectedEvery : ""}
                  </option>
                ))}
              </select>
            </RefField>
            <RefField label="How often" value={sources[0]?.expectedEvery || "set on the registered source, not here"} />
            <RefField
              label="Upload accepted from"
              value="The Org Admin of any organisation on this project"
              hint="Whoever uploads is recorded on every row the file produces"
            />
          </div>
        )}
      </LoadFrame>
      <OpenNote>
        The pull list is every source really registered with the evidence service, so an empty list means no feed
        exists — not that one is hidden. Which ways-in a project allows is a composition choice with no store yet.
      </OpenNote>
      <Actions back={["Back", () => nav("/definition")]} go={["Save and continue", () => nav("/intake/file")]} />
    </>
  );
}

// ── p2_9 · The same integration pattern, a different registry ───────────────
// Illustrative by status and by the note on its face: CREST has no finance
// adaptor. The reference's adaptor library is reproduced as the reference's
// content, labelled as such — the layout and copy match, the claim does not.
const FINANCE_ADAPTORS: Array<[string, string, boolean]> = [
  ["IFMIS · Kenya Treasury", "Fund, head, sub-head, cost centre, activity", true],
  ["PFMS · India", "Scheme, component, sanction head", true],
  ["Generic CSV chart upload", "Whatever the mapping says", true],
  ["SAP / Oracle general ledger", "Company code, cost centre, WBS element", false],
  ["QuickBooks / Xero", "Class and project tracking", false],
];

export function Finance() {
  const nav = useNavigate();
  return (
    <>
      <StepCounter>Project setup · finance connection</StepCounter>
      <Title t="Connect to the finance system" />
      <Lede>Same adaptor library as evidence sources. Three of these are built.</Lede>
      <div className="gt-wrap">
        <div className="grid-tbl" style={{ ["--cols" as never]: "1.2fr 1.6fr auto" }}>
          <div className="g-row g-head">
            <span>Adaptor</span>
            <span>Reads</span>
            <span>Status</span>
          </div>
          {FINANCE_ADAPTORS.map(([name, reads, built]) => (
            <div className="g-row" key={name}>
              <span>{name}</span>
              <span className="muted">{reads}</span>
              <span>{built ? <Chip kind="ok">built</Chip> : <Chip kind="plain">not built</Chip>}</span>
            </div>
          ))}
        </div>
      </div>
      <div className="form-grid">
        <RefField label="Endpoint">
          <input name="financeendpoint" placeholder="https://…" />
        </RefField>
        <RefField label="Credential" value="held by the platform team · not entered here" />
      </div>
      <Callout kind="teal" title="One direction only">
        Read-only, and deliberately so. CREST pulls codes to reference them; it never writes to the ledger. What CREST
        produces is an instruction someone else posts.
      </Callout>
      <OpenNote>
        The adaptor table above is the reference's library, not this deployment's: no finance adaptor exists in CREST,
        so nothing here can be tested or saved. It renders because a missing row reads as a feature that does not
        exist — the same reason the rail never hides an entry.
      </OpenNote>
      <Actions back={["Back", () => nav("/projects")]} go={["Save and continue", () => nav("/support")]} />
    </>
  );
}

// ── p2_10 · Support belongs to the project, not the platform ────────────────
export function Support() {
  const nav = useNavigate();
  return (
    <>
      <StepCounter>Project setup · 6 of 7</StepCounter>
      <Title t="Who a worker reaches when something goes wrong" />
      <Lede>
        First line sits with the project, because the project is where the answer is. The instance keeps only genuine
        platform faults.
      </Lede>
      <Callout kind="teal" title="What moved, and why">
        This changed in this version. Support used to sit at instance level, which meant every worker question
        travelled past the people best placed to answer it. First line is now project-scoped and staffed; the instance
        keeps only genuine platform faults.
      </Callout>
      <OpenNote>
        No support owner can be named on this deployment yet: there is nothing to write it to, and a support contact
        this console invented would be worse than an empty field — a worker would be sent to nobody.
      </OpenNote>
      <Actions back={["Back", () => nav("/finance")]} go={["Continue", () => nav("/status")]} />
    </>
  );
}

// ── n3 · One rail, two actors ───────────────────────────────────────────────
const CONTRACT: Array<[string, string, string]> = [
  ["Projects", "Create a project and hand it to a Configurator", "Open the project handed to them"],
  ["People & roles", "Grant and remove roles — a role is held, not just recorded", "See who holds what, read only"],
  ["Work definitions", "Commission an author", "Choose an origin, then ratify what comes back"],
  ["Payment set up", "Own the rate table and the mechanism", "Choose the posture, never the rates"],
  ["Workers", "Hand the registry to a custodian", "Registration or import, for this project"],
];

export function Navigation() {
  const nav = useNavigate();
  const s = useConsole();
  return (
    <>
      <Title t="One rail, two actors" />
      <Lede>
        The same five entries, for the Org Admin and for the Project Configurator. What an entry <em>does</em> depends
        on the role you hold; whether it is <em>there</em> does not.
      </Lede>
      <div className="gt-wrap">
        <div className="grid-tbl" data-contract="rail" style={{ ["--cols" as never]: "1fr 1.4fr 1.4fr" }}>
          <div className="g-row g-head">
            <span>Rail entry</span>
            <span>Org Admin</span>
            <span>Project Configurator</span>
          </div>
          {CONTRACT.map(([entry, admin, conf]) => (
            <div className="g-row" key={entry}>
              <span>{entry}</span>
              <span className="muted">{admin}</span>
              <span className="muted">{conf}</span>
            </div>
          ))}
        </div>
      </div>
      <Callout kind="teal" title="The rule this screen sets">
        An entry is never hidden because of your role. A row you cannot act on stays visible and names who can — a
        missing row reads as a feature that does not exist, which is a lie the console can avoid telling. This is the
        same posture as a held payment carrying a reason with an owner.
      </Callout>
      <Callout kind="green" title="What the reference already decided">
        Every p1_* and p2_* setup frame carries these same five entries: the Org Admin and the Project Configurator see
        the same rail, and only the appbar identity changes. That is a reference decision, not an omission, so this
        console does not invent a role-scoped rail.
      </Callout>
      <Card>
        <p className="body-2">
          Signed in as <b>{s.me?.who}</b> · {s.me?.role}. The rail to the left is the same one the other J3 actor sees.
        </p>
      </Card>
      <OpenNote>
        Design finding, raised not absorbed: the reference's own P-2 frames carry <em>three</em> different rails — this
        five-entry setup rail, a five-entry dashboard rail (Work status · Quality · Payments · Proof · Reports) on
        p2_11–p2_16, and a finance/support rail (Project · Work definitions · Finance · Support · Dashboard) on
        p2_8–p2_10. The rail is identical across actors <em>within a section</em>, which is what F1 actually
        establishes; it is not one rail across all 24 J3 frames. This console follows the reference frame by frame.
      </OpenNote>
      <Actions go={["Back to project setup", () => nav("/projects")]} />
    </>
  );
}
