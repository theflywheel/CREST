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
import { api } from "@crest/api";
import { Callout, Chip, OptionCard, RefField, OpenNote, StepCounter } from "@crest/ui";
import { Card, CardTitled, KVR, Lede, LoadFrame, Mono, MonoShort, short, Stat, Tbl, Title, useLoad, when } from "../ui";
import { errText, useConsole } from "../state";
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
  const s = useConsole();
  const me = s.me!.partyId;
  const r = useLoad(async () => {
    const [org, reg, projects, declined, orgRoles] = await Promise.all([
      api.get("parties", `/v1/parties/${encodeURIComponent(me)}`),
      api.get("parties", `/v1/organisations/${encodeURIComponent(me)}/registration`).catch(() => null),
      api.get("parties", `/v1/projects?ownerPartyId=${encodeURIComponent(me)}`).catch(() => ({ projects: [] })),
      api.get("parties", `/v1/projects?ownerPartyId=${encodeURIComponent(me)}&ownership=DECLINED`).catch(() => ({ projects: [] })),
      api.get("parties", `/v1/organisations/${encodeURIComponent(me)}/roles`).catch(() => ({ roles: [] })),
    ]);
    return {
      org: org.party || org,
      reg,
      projects: (projects.projects || []) as Array<{ id: string; name?: string; state?: string; ownership?: { state?: string; partyId?: string } }>,
      declined: (declined.projects || []) as Array<{ id: string; name?: string; ownership?: { reason?: string; partyId?: string } }>,
      roles: (orgRoles.roles || []) as Array<{ partyId?: string }>,
    };
  }, [me]);
  return (
    <LoadFrame r={r}>
      {({ org, reg, projects, declined, roles }) => (
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
                <KVR rows={[["custodian party", "named per deployment — see the Instance view"]]} />
              </CardTitled>
              <CardTitled t="Projects this organisation runs">
                <Tbl
                  heads={["Project", "State", "Handover", "Configurator", ""]}
                  rows={projects.map((p) => [
                    p.name || <MonoShort id={p.id} />,
                    <Chip kind={p.state === "ACTIVE" ? "ok" : "plain"}>{p.state}</Chip>,
                    p.ownership?.state ? (
                      <Chip kind={p.ownership.state === "ACCEPTED" ? "ok" : p.ownership.state === "DECLINED" ? "err" : "warn"}>
                        {p.ownership.state.toLowerCase()}
                      </Chip>
                    ) : (
                      <Chip kind="plain">unhanded</Chip>
                    ),
                    p.ownership?.partyId ? <MonoShort id={p.ownership.partyId} /> : "nobody named",
                    <button
                      className="btn secondary"
                      data-open-project={p.id}
                      style={{ width: "auto", padding: "7px 12px" }}
                      onClick={() => {
                        s.setProjectId(p.id);
                        nav("/handover");
                      }}
                    >
                      Open the handover
                    </button>,
                  ])}
                  empty="No project yet. Creating one names it and appoints who configures it — nothing about how it runs is decided here."
                />
              </CardTitled>
              {declined.length ? (
                <CardTitled t="Handed back to you" chip={<Chip kind="err">{declined.length}</Chip>}>
                  <KVR
                    rows={declined.map((p) => [
                      p.name || short(p.id),
                      <>
                        {p.ownership?.partyId ? <MonoShort id={p.ownership.partyId} /> : "somebody"} declined it
                        {p.ownership?.reason ? ": “" + p.ownership.reason + "”" : ""} — it is intact and yours to hand on
                        again
                      </>,
                    ])}
                  />
                  <div style={{ height: 10 }} />
                  <ReHand projects={declined.map((p) => p.id)} />
                  <p className="muted" style={{ marginTop: 8 }}>
                    Re-handing is the owning organisation's act, never the outgoing configurator's: somebody who agreed
                    to configure a project has not thereby agreed to hand it on.
                  </p>
                </CardTitled>
              ) : null}
            </div>
            <div>
              <div className="stats">
                <Stat n={<span data-stat="projects">{projects.length}</span>} label={projects.length ? "Projects" : "Projects · ready to add your first"} />
                <Stat n={roles.length} label="People in roles · granted by this organisation" />
              </div>
              <Callout kind="teal" title="What an Org Admin holds">
                An Org Admin manages standing configuration across every project this organisation runs. It does not
                configure any individual project — that is a separate role, assigned per project.
              </Callout>
            </div>
          </div>
          <Actions back={["Assign people to roles", () => nav("/people")]} go={["Create a project", () => nav("/projects/new")]} />
        </>
      )}
    </LoadFrame>
  );
}

// The Org Admin's answer to a decline: hand the project to somebody else.
// POST /v1/projects/{id}/configurator always lands back at PENDING, because
// a person who has not answered has not agreed.
function ReHand(props: { projects: string[] }) {
  const [to, setTo] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState("");
  return (
    <>
      {err ? <div className="errbar">{err}</div> : null}
      {done ? <Callout kind="green" title="Handed on">{done}</Callout> : null}
      <form
        id="rehandform"
        style={{ display: "flex", gap: 10, alignItems: "flex-end", flexWrap: "wrap" }}
        onSubmit={async (ev) => {
          ev.preventDefault();
          setErr(null);
          setDone("");
          try {
            for (const id of props.projects) {
              await api.post("parties", `/v1/projects/${encodeURIComponent(id)}/configurator`, {
                configuratorPartyId: to.trim(),
              });
            }
            setDone("The handover is waiting on an answer again, and the decline that came before it is still on the record.");
          } catch (e) {
            setErr(errText(e));
          }
        }}
      >
        <RefField label="Hand it to" hint="A proposal: the project waits at PENDING until they answer">
          <input name="rehandto" required value={to} onChange={(e) => setTo(e.target.value)} placeholder="did:crest:party:…" />
        </RefField>
        <button className="btn inline" id="rehand">Hand it over again</button>
      </form>
    </>
  );
}

// ── p1_3 · Creating a project is not configuring one ────────────────────────
// The reference frame's own fields, with nothing to write them to yet: project
// creation and the named-Configurator handover are the J3 backend's, and this
// screen says so rather than collecting three answers into a browser tab.
export function NewProject() {
  const nav = useNavigate();
  const s = useConsole();
  const [name, setName] = useState("");
  const [coverage, setCoverage] = useState("");
  const [configurator, setConfigurator] = useState(s.me!.partyId);
  const [err, setErr] = useState<string | null>(null);
  const create = async () => {
    setErr(null);
    try {
      const out = await api.post("parties", "/v1/projects", {
        name: name.trim(),
        ownerPartyId: s.me!.partyId,
        configuration: coverage.trim() ? { coverage: coverage.trim() } : undefined,
        configuratorPartyId: configurator.trim() || undefined,
      });
      const p = (out.project || out) as { id: string };
      s.setProjectId(p.id);
      nav("/handover");
    } catch (e) {
      setErr(errText(e));
    }
  };
  return (
    <>
      <Title t="Creating a project, and handing it over" />
      <Lede>
        The Org Admin names the project and appoints who configures it. What the project actually does is not decided
        here.
      </Lede>
      {err ? <div className="errbar">{err}</div> : null}
      <form
        id="newprojectform"
        className="form-grid"
        onSubmit={(ev) => {
          ev.preventDefault();
          create();
        }}
      >
        <RefField label="Project name">
          <input name="projectname" required value={name} onChange={(e) => setName(e.target.value)} placeholder="the name workers and funders will see" />
        </RefField>
        <RefField label="Coverage" hint="Stored on the project as this deployment's own configuration; CREST never reads inside it">
          <input name="coverage" value={coverage} onChange={(e) => setCoverage(e.target.value)} placeholder="wards, sub-counties, districts" />
        </RefField>
        <RefField
          label="Project Configurator (Project Configurator)"
          hint="Naming somebody is a proposal, not an assignment: the project waits at PENDING until they answer"
        >
          <input name="configurator" value={configurator} onChange={(e) => setConfigurator(e.target.value)} placeholder="did:crest:party:…" />
        </RefField>
        <div style={{ display: "flex", alignItems: "flex-end" }}>
          <button className="btn dominant" id="create-project">
            Continue to setup
          </button>
        </div>
      </form>
      <Callout kind="green" title="What is deliberately not on this screen">
        Nothing about registration, payment or validation is chosen on this screen. Those are compositability choices,
        and they belong to whoever configures the project, not to whoever creates it.
      </Callout>
      <Callout kind="teal" title="What happens next">
        The project is created DRAFT and the handover sits unanswered. The person you named sees what arrived and can
        accept it or hand it back with a reason — and until they accept, they can read the project and change nothing
        in it.
      </Callout>
      <Actions back={["Back", () => nav("/people")]} />
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
  const s = useConsole();
  const r = useLoad(async () => {
    const out = await api.get("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/roles`);
    const grantableBy = out.grantableBy as string | undefined;
    const org = grantableBy
      ? await api.get("parties", `/v1/parties/${encodeURIComponent(grantableBy)}`).catch(() => null)
      : null;
    return {
      roles: (out.roles || []) as Array<{
        partyId?: string; displayName?: string; functions?: string[]; grantedByPartyId?: string;
        authorityPartyId?: string; grantedAt?: string; state?: string;
      }>,
      grantableBy,
      org: org && (org.party || org),
    };
  }, [s.projectId]);
  return (
    <LoadFrame r={r}>
      {({ roles, grantableBy, org }) => (
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
                ["granted by", org?.displayName ? String(org.displayName) : grantableBy ? <MonoShort id={grantableBy} /> : "\u2014"],
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
            <Tbl
              heads={["Holder", "Functions", "Granted by", "Since", "State"]}
              rows={roles.map((a) => [
                a.displayName || <MonoShort id={a.partyId} />,
                (a.functions || []).join(", "),
                <MonoShort id={a.grantedByPartyId || a.authorityPartyId || ""} />,
                when(a.grantedAt),
                <Chip kind={a.state === "ACTIVE" ? "ok" : "warn"}>{a.state}</Chip>,
              ])}
              empty="Nothing is readable here yet. An empty list is a true answer: it means a role still has to be granted."
            />
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

// p1_2 — a role is held, not just recorded. GET /v1/organisations/{id}/roles
// is the question GET /v1/authorizations cannot answer: it keys on the party
// who GAVE the grant, which is what a standing-configuration screen is about.
function RoleHolders() {
  const nav = useNavigate();
  const s = useConsole();
  const r = useLoad(async () => {
    const out = await api.get("parties", `/v1/organisations/${encodeURIComponent(s.me!.partyId)}/roles`);
    return {
      roles: (out.roles || []) as Array<{
        partyId?: string; displayName?: string; partyKind?: string; functions?: string[];
        grantedByPartyId?: string; grantedAt?: string; until?: string; state?: string;
      }>,
      grantableBy: out.grantableBy as string | undefined,
    };
  });
  return (
    <LoadFrame r={r}>
      {({ roles }) => (
        <>
          <Title t="Putting named people into roles" />
          <Lede>An invitation goes to a work email. The person holds the role only once they have accepted it.</Lede>
          <CardTitled t="Who holds a role under this organisation">
            <Tbl
              heads={["Holder", "Functions", "Granted by", "Since", "Until", "State"]}
              rows={roles.map((a) => [
                <>
                  {a.displayName || <MonoShort id={a.partyId} />}
                  {a.partyKind ? <span className="muted"> · {a.partyKind}</span> : null}
                </>,
                (a.functions || []).join(", "),
                <MonoShort id={a.grantedByPartyId || ""} />,
                when(a.grantedAt),
                when(a.until),
                <Chip kind={a.state === "ACTIVE" ? "ok" : a.state === "REVOKED" ? "err" : "warn"}>{a.state}</Chip>,
              ])}
              empty="Nobody holds a role granted by this organisation. An empty list here is a true answer, not a blank screen: it means a role still has to be granted."
            />
            <p className="muted" style={{ marginTop: 8 }}>
              Revoked and expired grants stay listed with their state, because a console showing only live grants
              cannot answer “who used to be able to do this, and who took it away”.
            </p>
          </CardTitled>
          <Callout kind="green" title="Why acceptance is the thing that matters">
            A role assignment that took effect without the person ever appearing would let an organisation attribute a
            signature to someone who had never logged in. Requiring acceptance keeps the person and the authority
            attached.
          </Callout>
          <OpenNote>
            Two halves of the reference's frame are still unbuilt, and neither is faked here: an invitation to a work
            email (there is no notification service) and the invited-versus-active distinction that goes with it.
            Project-scoped roles are granted on the project's own Owners screen, where the grant carries this
            organisation as its authority.
          </OpenNote>
          <Actions back={["Back", () => nav("/org")]} go={["Create a project", () => nav("/projects/new")]} />
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
        The rail is identical for both J3 actors within a section, and the reference draws three sections: the
        five-entry setup rail, a dashboard rail, and a finance and support rail. Only the appbar identity changes
        between the two actors.
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
