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
import { Card, CardTitled, KVR, Lede, LoadFrame, MonoShort, short, Tbl, Title, useLoad, when } from "../ui";
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

// The reference's p1_1 rows: bold (or muted) label, a quiet sub-line, and a
// chip or count pinned right, each row separated by a hairline.
function HomeRow(props: {
  label: React.ReactNode;
  sub?: React.ReactNode;
  right?: React.ReactNode;
  muted?: boolean;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 16,
        padding: "14px 0",
        borderBottom: "1px solid var(--line, #E5E1DC)",
      }}
    >
      <div>
        <div style={{ font: props.muted ? "400 15px/1.4 Roboto,system-ui" : "500 15px/1.4 Roboto,system-ui", color: props.muted ? "var(--muted, #6B6660)" : "inherit" }}>
          {props.label}
        </div>
        {props.sub ? (
          <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
            {props.sub}
          </div>
        ) : null}
      </div>
      <div style={{ flexShrink: 0 }}>{props.right}</div>
    </div>
  );
}

// The grey count badge the reference pins on the Projects / People rows.
function CountBadge(props: { n: number }) {
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        minWidth: 28,
        height: 28,
        padding: "0 8px",
        borderRadius: 14,
        background: "var(--chip, #EFEDEA)",
        font: "500 13px/1 Roboto,system-ui",
      }}
    >
      {props.n}
    </span>
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
      api
        .get("parties", `/v1/organisations/${encodeURIComponent(me)}/registration`)
        .then(async (reg0) => {
          // The reference's terms row names the terms, not their id: "Standard
          // delivery, version 2 — approved 14 August 2026".
          if (reg0 && reg0.termsId) {
            const t = await api.get("parties", "/v1/terms").catch(() => null);
            const hit = t && (t.terms || []).find((x: { id: string; name?: string }) => x.id === reg0.termsId);
            return { ...reg0, termsTitle: hit && hit.name };
          }
          return reg0;
        })
        .catch(() => null),
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
          <div style={{ maxWidth: 780 }}>
            <HomeRow
              label="Terms held"
              sub={
                reg && reg.termsVersion
                  ? `${reg.termsTitle || reg.termsId}, version ${reg.termsVersion}` +
                    (reg.decidedAt ? ` — approved ${when(reg.decidedAt)}` : " — accepted, decision pending")
                  : "No acceptance on record"
              }
              right={
                <Chip kind={reg && reg.state === "APPROVED" ? "ok" : "warn"}>
                  {reg && reg.state === "APPROVED" ? "Active" : (reg && reg.state) || "unknown"}
                </Chip>
              }
            />
            <HomeRow
              label="Worker Registry Custodian (Worker Registry Custodian)"
              sub="Sits inside this organisation. Cannot be delegated out."
              right={<Chip kind="info">Held here</Chip>}
            />
            <HomeRow
              muted
              label="Projects"
              sub={projects.length ? "The list below carries each project's state and handover" : "Ready to add your first"}
              right={<CountBadge n={projects.length} />}
            />
            <HomeRow
              muted
              label="People in roles"
              sub={roles.length <= 1 ? "You, as Org Admin" : "Granted by this organisation"}
              right={<CountBadge n={Math.max(roles.length, 1)} />}
            />
            <div style={{ height: 16 }} />
            <Callout kind="teal" title="">
              An Org Admin manages standing configuration across every project this organisation runs. It does not
              configure any individual project — that is a separate role, assigned per project.
            </Callout>
            {projects.length ? (
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
            ) : null}
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
            {/* The e2e suite reads the project count off this stat. */}
            <span data-stat="projects" hidden>
              {projects.length}
            </span>
            <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 18, paddingTop: 14, display: "flex", justifyContent: "flex-end" }}>
              <Actions
                back={["Assign people to roles", () => nav("/people")]}
                go={["Create a project", () => nav("/projects/new")]}
              />
            </div>
          </div>
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
          hint="Naming somebody is a proposal, not an assignment: the project waits at PENDING until they answer. The same person may also hold Work Definition Author on this project — in small organisations one person often holds both; the roles stay separate."
        >
          <input name="configurator" value={configurator} onChange={(e) => setConfigurator(e.target.value)} placeholder="did:crest:party:…" />
        </RefField>
        <div style={{ display: "flex", alignItems: "flex-end" }}>
          <button className="btn dominant" id="create-project">
            Continue to setup
          </button>
        </div>
      </form>
      {/* p1_3's two undecided rows: creation leaves both open on purpose. */}
      <div style={{ maxWidth: 780 }}>
        <HomeRow
          muted
          label="What this project uses CREST for"
          sub="Not decided — the Configurator chooses"
          right={<Chip kind="plain">Open</Chip>}
        />
        <HomeRow
          muted
          label="Work definition"
          sub="Cannot exist until the project is composed"
          right={<Chip kind="warn">Blocked</Chip>}
        />
        <div style={{ height: 12 }} />
        <Callout kind="green" title="">
          Nothing about registration, payment or validation is chosen on this screen. Those are compositability
          choices, and they belong to whoever configures the project, not to whoever creates it.
        </Callout>
      </div>
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
    const me = s.me!.partyId;
    const [out, reg] = await Promise.all([
      api.get("parties", `/v1/organisations/${encodeURIComponent(me)}/roles`),
      api.get("parties", `/v1/organisations/${encodeURIComponent(me)}/registration`).catch(() => null),
    ]);
    // The vacancy row is derived, not invented: the functions this
    // organisation's terms authorise, minus the ones somebody already holds.
    let vacant: string[] = [];
    let termsPermissions: string[] = [];
    if (reg && reg.termsId) {
      const t = await api.get("parties", "/v1/terms").catch(() => null);
      const hit = t && (t.terms || []).find((x: { id: string; permissions?: string[] }) => x.id === reg.termsId);
      const held = new Set(
        ((out.roles || []) as Array<{ functions?: string[] }>).flatMap((a) => a.functions || []),
      );
      termsPermissions = ((hit && hit.permissions) || []) as string[];
      vacant = termsPermissions.filter((p: string) => !held.has(p));
    }
    // The scopes an invitation can name: this organisation's own projects,
    // read from the registry — never a project id typed into a browser.
    const owned = await api
      .get("parties", `/v1/projects?ownerPartyId=${encodeURIComponent(me)}`)
      .catch(() => null);
    return {
      roles: (out.roles || []) as Array<{
        partyId?: string; displayName?: string; partyKind?: string; functions?: string[];
        grantedByPartyId?: string; grantedAt?: string; until?: string; state?: string;
      }>,
      vacant,
      termsPermissions,
      terms: reg && reg.termsId ? { id: String(reg.termsId), version: Number(reg.termsVersion || 1) } : null,
      projects: ((owned && owned.projects) || []) as Array<{ id: string; name?: string }>,
      org: me,
    };
  });
  return (
    <LoadFrame r={r}>
      {({ roles, vacant, termsPermissions, terms, projects, org }) => (
        <>
          <Title t="Putting named people into roles" />
          <Lede>An invitation goes to a work email. The person holds the role only once they have accepted it.</Lede>
          <div style={{ maxWidth: 780 }}>
            <HomeRow
              label={s.me!.who}
              sub="Org Admin — you"
              right={<Chip kind="ok">Active</Chip>}
            />
            {roles.map((a, i) => (
              <HomeRow
                key={i}
                label={a.displayName || <MonoShort id={a.partyId} />}
                sub={
                  <>
                    {(a.functions || []).join(", ")}
                    {a.grantedAt ? <> · granted {when(a.grantedAt)}</> : null}
                  </>
                }
                right={
                  <Chip kind={a.state === "ACTIVE" ? "ok" : a.state === "REVOKED" ? "err" : "warn"}>
                    {a.state === "ACTIVE" ? "Active" : a.state}
                  </Chip>
                }
              />
            ))}
            {vacant.length ? (
              <HomeRow
                muted
                label="Not yet assigned"
                sub={vacant.join(" · ")}
                right={<CountBadge n={vacant.length} />}
              />
            ) : null}
            {roles.length === 0 ? (
              <p className="muted" style={{ marginTop: 10 }}>
                Nobody else holds a role granted by this organisation yet — a true answer, not a blank screen. The
                reference's invited-but-not-yet-active rows are not drawn because there is no invitation service to
                stand behind them; a person appears here once a grant exists.
              </p>
            ) : null}
            <div style={{ height: 14 }} />
            <InvitePerson org={org} terms={terms} functions={termsPermissions} projects={projects} />
            <div style={{ height: 14 }} />
            <Callout kind="teal" title="">
              A vacant role is not a blocked project. A work definition can be ratified with fields marked as
              assigned-but-unfilled, and a project with no Evidence Validator simply cannot use manual review until
              one exists.
            </Callout>
            <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end" }}>
              <Actions back={["Back", () => nav("/org")]} go={["Create a project", () => nav("/projects/new")]} />
            </div>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// Inviting a person (#123). Three real calls, in the only order that can be
// honest about what happened: create the record, mint the one-time code for
// it, then grant the role. The record and the grant are separable — if the
// grant fails the person still exists and can still claim, and the screen
// says exactly that rather than swallowing the half-done state.
//
// The role list is not invented here: it is what this organisation's terms
// permit, plus the console's own function names for the roles this surface
// grants. An organisation can never offer more than its terms allow — the
// registry refuses it — so an unfamiliar entry is a refusal waiting to be
// read, not a hidden capability.
const CONSOLE_FUNCTIONS = [
  "specify-definition",
  "ratify-definition",
  "act-for-party",
  "register-workers",
  "submit-evidence",
  "resolve-unclear-evidence",
];

function InvitePerson(props: {
  org: string;
  terms: { id: string; version: number } | null;
  functions: string[];
  projects: Array<{ id: string; name?: string }>;
}) {
  const [name, setName] = useState("");
  const [contactKind, setContactKind] = useState("email");
  const [contact, setContact] = useState("");
  const [fn, setFn] = useState("");
  const [scope, setScope] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [out, setOut] = useState<{ partyId: string; code: string; expiresAt?: string; grantFailed?: string } | null>(
    null,
  );
  const [copied, setCopied] = useState(false);
  const choices = Array.from(new Set([...props.functions, ...CONSOLE_FUNCTIONS]));
  const link = out ? `${location.origin}${location.pathname}#/claim/${out.code}` : "";

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const party = await api.post("parties", "/v1/parties", {
        kind: "person",
        displayName: name.trim(),
        contactRoutes: [{ kind: contactKind, value: contact.trim() }],
      });
      const partyId = (party.party && party.party.id) || party.id;
      const inv = await api.post("parties", `/v1/parties/${encodeURIComponent(partyId)}/invitations`, {});
      let grantFailed: string | undefined;
      if (!props.terms) {
        grantFailed =
          "no terms are recorded for this organisation, and a grant is made under terms — the role has to be granted once that is settled";
      } else if (fn) {
        try {
          // No period.start and no approvedAt: the registry's clock stamps
          // both. A browser's wall time is nobody's authority on when a
          // grant began.
          await api.post("parties", "/v1/authorizations", {
            partyId,
            terms: props.terms,
            scope: scope ? { kind: "context", contextId: scope } : { kind: "instance" },
            functions: [fn],
            period: {},
            authorityPartyId: props.org,
            approvedByPartyId: props.org,
            state: "ACTIVE",
          });
        } catch (e) {
          grantFailed = errText(e);
        }
      }
      setOut({ partyId, code: inv.inviteCode, expiresAt: inv.expiresAt, grantFailed });
    } catch (e) {
      setErr(errText(e));
    }
    setBusy(false);
  };

  if (out)
    return (
      <CardTitled t="The invitation, shown once">
        <p className="body-2">
          Send this link to {name || "the person"} by a route you trust. It works once, and this screen is the only
          place it is ever shown — CREST keeps only its hash, so nobody, including this deployment, can read it back.
        </p>
        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap", margin: "10px 0" }}>
          <span className="mono" data-invite-link style={{ wordBreak: "break-all" }}>
            {link}
          </span>
          <button
            className="btn secondary"
            data-act="copy-invite"
            onClick={() => {
              navigator.clipboard?.writeText(link).then(
                () => setCopied(true),
                () => setCopied(false),
              );
            }}
          >
            {copied ? "Copied" : "Copy the link"}
          </button>
        </div>
        <KVR
          rows={[
            ["the record it claims", <span data-invite-party={out.partyId}><MonoShort id={out.partyId} /></span>],
            ["expires", out.expiresAt ? when(out.expiresAt) : "—"],
            ["what claiming does", "binds the identity they sign in with to this record — nothing more"],
          ]}
        />
        {out.grantFailed ? (
          <Callout kind="grey" title="The record exists; the role does not yet">
            The person and their invitation were created, but the grant was refused — {out.grantFailed}. The link
            above still works: they can claim their record now and be granted the role afterwards.
          </Callout>
        ) : null}
      </CardTitled>
    );

  return (
    <CardTitled t="Invite a person">
      {err ? <p className="errbar">{err}</p> : null}
      <p className="body-2">
        This creates the person's record and a one-time link they claim with their own sign-in. Nothing here proves
        who they are — their identity provider does that, when they claim it.
      </p>
      <form data-invite-form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 10 }}>
        <label className="field">
          <span className="eyebrow">Name to show</span>
          <input value={name} onChange={(e) => setName(e.target.value)} required data-field="invite-name" />
        </label>
        <label className="field">
          <span className="eyebrow">How they are reached</span>
          <div style={{ display: "flex", gap: 8 }}>
            <select value={contactKind} onChange={(e) => setContactKind(e.target.value)} data-field="invite-contact-kind">
              <option value="email">email</option>
              <option value="phone">phone</option>
            </select>
            <input
              value={contact}
              onChange={(e) => setContact(e.target.value)}
              required
              style={{ flex: 1 }}
              data-field="invite-contact"
            />
          </div>
        </label>
        <label className="field">
          <span className="eyebrow">Role</span>
          <select value={fn} onChange={(e) => setFn(e.target.value)} data-field="invite-function">
            <option value="">No role yet — the record and the link only</option>
            {choices.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span className="eyebrow">Where it applies</span>
          <select value={scope} onChange={(e) => setScope(e.target.value)} data-field="invite-scope">
            <option value="">Instance-wide (no project)</option>
            {props.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name || short(p.id)}
              </option>
            ))}
          </select>
        </label>
        <div>
          <button className="btn dominant" type="submit" disabled={busy} data-act="invite">
            {busy ? "Creating…" : "Create the invitation"}
          </button>
        </div>
      </form>
    </CardTitled>
  );
}

// ── p2_2 · Registration and import are not alternatives ─────────────────────
// The reference's p2_3 rows: a full-width radio row, tinted orange when on.
export function SourcingRow(props: { on: boolean; t: string; s: string; onPick: () => void }) {
  return (
    <button
      onClick={props.onPick}
      aria-pressed={props.on}
      data-sourcing={props.t}
      style={{
        display: "flex",
        gap: 12,
        alignItems: "flex-start",
        textAlign: "left",
        width: "100%",
        padding: "14px 16px",
        marginBottom: 12,
        borderRadius: 8,
        cursor: "pointer",
        border: props.on ? "1.5px solid #C84C0E" : "1px solid var(--line, #DDD9D3)",
        background: props.on ? "rgba(200,76,14,0.07)" : "transparent",
        font: "inherit",
        color: "inherit",
      }}
    >
      <span
        style={{
          width: 16,
          height: 16,
          marginTop: 2,
          flexShrink: 0,
          borderRadius: "50%",
          border: props.on ? "5px solid #C84C0E" : "2px solid var(--muted, #8A857E)",
          background: "#fff",
        }}
      />
      <span>
        <span style={{ display: "block", font: "500 15px/1.4 Roboto,system-ui" }}>{props.t}</span>
        <span className="muted" style={{ display: "block", font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
          {props.s}
        </span>
      </span>
    </button>
  );
}

export function Workers() {
  const nav = useNavigate();
  const s = useConsole();
  const [err, setErr] = useState<string | null>(null);
  const [gen, setGen] = useState(0);
  const [sourceForm, setSourceForm] = useState({ systemRef: "", sourceClass: "", captureMethod: "", sourceExposure: "", expectedEvery: "" });
  const r = useLoad(async () => {
    const [comp, src] = await Promise.all([
      api.get("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition`).catch(() => ({ choices: [] })),
      api.get("evidence", "/v1/sources?contextId=" + encodeURIComponent(s.projectId)).catch(() => ({ sources: [] })),
    ]);
    const rec = ((comp.choices || []) as Array<{ kind?: string; payload?: { value?: unknown } }>).find(
      (c) => (c.kind || "").replace(/^composition:/, "") === "worker-sourcing",
    );
    const sources = ((src.sources || []) as Array<{
      systemRef?: string; adapterRef?: string; contextId?: string; expectedEvery?: string; state?: string;
    }>).filter((x) => x.contextId === s.projectId);
    return { sourcing: rec ? String(rec.payload?.value || "") : "", sources };
  }, [s.projectId, gen]);
  const record = async (v: string) => {
    setErr(null);
    try {
      await api.put("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition/worker-sourcing`, { value: v });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(errText(e));
    }
  };
  const registerSource = async () => {
    setErr(null);
    try {
      if (!sourceForm.systemRef.trim() || !sourceForm.sourceClass || !sourceForm.captureMethod || !sourceForm.sourceExposure || !sourceForm.expectedEvery.trim()) {
        throw new Error("source identity, approved provenance, and cadence are required");
      }
      await api.post("evidence", "/v1/sources", {
        adapterRef: "csv-batch@1",
        contextId: s.projectId,
        systemRef: sourceForm.systemRef.trim(),
        sourceClass: sourceForm.sourceClass,
        captureMethod: sourceForm.captureMethod,
        sourceExposure: sourceForm.sourceExposure,
        expectedEvery: sourceForm.expectedEvery.trim(),
        ownerPartyId: s.me!.partyId,
        mapping: {},
      });
      setSourceForm({ systemRef: "", sourceClass: "", captureMethod: "", sourceExposure: "", expectedEvery: "" });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(errText(e));
    }
  };
  return (
    <LoadFrame r={r}>
      {({ sourcing, sources }) => (
        <>
          <Title t="Where the workers come from" />
          <Lede>
            Register them here, bring in records that already exist, or both. Both is common: a registry covers the
            known workers, enrollment covers the rest.
          </Lede>
          {err ? <div className="errbar">{err}</div> : null}
          <div style={{ maxWidth: 780 }}>
            <SourcingRow
              t="Register in CREST"
              s="Full enrollment — identity, consent, recovery contacts. Self-registration or assisted by a Registering Agent."
              on={sourcing === "register" || sourcing === "register-and-import"}
              onPick={() => record(sourcing === "import" || sourcing === "register-and-import" ? "register-and-import" : "register")}
            />
            <SourcingRow
              t="Import existing records"
              s="Map identifiers from a registry you already run onto Crest identities. Nobody is re-registered."
              on={sourcing === "register-and-import" || sourcing === "import"}
              onPick={() => record(sourcing === "register" || sourcing === "register-and-import" ? "register-and-import" : "import")}
            />
            <SourcingRow
              t="Import only"
              s="No enrollment at all. Every worker must already exist in the source registry."
              on={sourcing === "import-only"}
              onPick={() => record("import-only")}
            />
            {sourcing ? null : (
              <p className="muted" style={{ margin: "2px 0 10px" }}>
                Nothing is answered yet — picking a row records the choice on the project, with your name and the date.
              </p>
            )}
            <RefField
              label="Source registry"
              hint="Whoever operates it holds Source-System Administrator (Source-System Administrator), and is accountable if a record there is wrong or stale."
            >
              {sources.length ? (
                <div>
                  {sources.map((x, i) => (
                    <div
                      key={i}
                      data-source={x.systemRef}
                      style={{ padding: "10px 12px", border: "1px solid var(--line, #DDD9D3)", borderRadius: 8, marginBottom: 8 }}
                    >
                      <span style={{ font: "500 14px/1.4 Roboto,system-ui" }}>{x.systemRef || x.adapterRef}</span>
                      <span className="muted" style={{ font: "400 13px/1.4 Roboto,system-ui" }}>
                        {" "}
                        — {x.adapterRef} · expected every {x.expectedEvery} · {x.state === "NEVER_SEEN" ? "no file has arrived yet" : x.state}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <input
                  name="sourceregistry"
                  readOnly
                  placeholder="no source registry is registered on this project — POST /v1/sources declares one, with an owner and a cadence (#22)"
                />
              )}
            </RefField>
            <CardTitled t="Register a source feed">
              <p className="muted">The operator declares the source identity and approved provenance once. Evidence uploads can then select this registered source; the upload cannot invent these facts.</p>
              <div className="form-grid">
                <RefField label="System reference"><input value={sourceForm.systemRef} onChange={(e) => setSourceForm({ ...sourceForm, systemRef: e.target.value })} placeholder="dhis2-riverside" /></RefField>
                <RefField label="Source class"><select value={sourceForm.sourceClass} onChange={(e) => setSourceForm({ ...sourceForm, sourceClass: e.target.value })}><option value="">Choose…</option>{["national-system", "institutional-system", "programme-system", "supervised-capture", "self-reported"].map((v) => <option key={v} value={v}>{v}</option>)}</select></RefField>
                <RefField label="Capture method"><select value={sourceForm.captureMethod} onChange={(e) => setSourceForm({ ...sourceForm, captureMethod: e.target.value })}><option value="">Choose…</option>{["system-of-record", "digital-capture", "supervised-manual", "unsupervised-manual"].map((v) => <option key={v} value={v}>{v}</option>)}</select></RefField>
                <RefField label="Source exposure"><select value={sourceForm.sourceExposure} onChange={(e) => setSourceForm({ ...sourceForm, sourceExposure: e.target.value })}><option value="">Choose…</option>{["push-api", "consent-pull", "signed-batch", "supervised-upload"].map((v) => <option key={v} value={v}>{v}</option>)}</select></RefField>
                <RefField label="Expected cadence" hint="A positive duration, such as 24h"><input value={sourceForm.expectedEvery} onChange={(e) => setSourceForm({ ...sourceForm, expectedEvery: e.target.value })} placeholder="24h" /></RefField>
              </div>
              <button className="btn dominant" type="button" onClick={registerSource}>Register approved source</button>
            </CardTitled>
            <Callout kind="teal">
              Importing does not create identities. It links an existing identifier to a Crest identity, so a worker
              already known to the county is not enrolled a second time under a new number.
            </Callout>
            <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end" }}>
              <Actions back={["Back", () => nav("/compose")]} go={["Continue", () => nav("/definition")]} />
            </div>
          </div>
        </>
      )}
    </LoadFrame>
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
  const s = useConsole();
  const [gen, setGen] = useState(0);
  const [err, setErr] = useState<string | null>(null);
  const r = useLoad(async () => {
    const [out, comp] = await Promise.all([
      api.get("evidence", "/v1/sources?contextId=" + encodeURIComponent(s.projectId)).catch(() => ({ sources: [] })),
      api.get("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition`).catch(() => ({ choices: [] })),
    ]);
    const rec = ((comp.choices || []) as Array<{ kind?: string; payload?: { value?: unknown } }>).find(
      (c) => (c.kind || "").replace(/^composition:/, "") === "evidence-ways-in",
    );
    const v = rec ? String(rec.payload?.value || "") : "";
    return {
      sources: (out.sources || []) as Array<{ systemRef?: string; adapterRef?: string; expectedEvery?: string; state?: string }>,
      // Both ways in until an answer narrows it — the same reading as p2_1.
      ways: { pull: v === "" || v.includes("pull"), upload: v === "" || v.includes("upload"), recorded: v !== "" },
    };
  }, [s.projectId, gen]);
  const record = async (pull: boolean, upload: boolean) => {
    setErr(null);
    const v = pull && upload ? "pull-and-upload" : pull ? "pull" : upload ? "upload" : "none";
    try {
      await api.put("parties", `/v1/projects/${encodeURIComponent(s.projectId)}/composition/evidence-ways-in`, { value: v });
      setGen((g) => g + 1);
    } catch (e) {
      setErr(errText(e));
    }
  };
  return (
    <LoadFrame r={r}>
      {({ sources, ways }) => (
        <>
          <Title t="How does evidence reach CREST?" />
          <Lede>
            CREST does not record work. Somewhere else already does, and this decides how that record gets here. Both
            options can run at once, on different definitions.
          </Lede>
          {err ? <div className="errbar">{err}</div> : null}
          <div style={{ maxWidth: 780 }}>
            <SourcingRow
              t="A system CREST pulls from"
              s="An adaptor reads records on a schedule from a platform that already logs the work: a campaign system, a case-management tool, an HR or attendance system. Nobody retypes anything, and nobody in the chain had a reason to change the number."
              on={ways.pull}
              onPick={() => record(!ways.pull, ways.upload)}
            />
            <SourcingRow
              t="A spreadsheet the project uploads"
              s="CREST generates a template from the work definition. Whoever holds the records fills it and the project uploads it. A person opened the file and typed into it, so it is weaker evidence than a system record, however careful they were."
              on={ways.upload}
              onPick={() => record(ways.pull, !ways.upload)}
            />
            <div
              style={{
                display: "flex", gap: 12, alignItems: "flex-start", padding: "14px 16px", marginBottom: 12,
                borderRadius: 8, border: "1px dashed var(--line, #DDD9D3)", opacity: 0.75,
              }}
            >
              <div style={{ flex: 1 }}>
                <div className="muted" style={{ font: "500 15px/1.4 Roboto,system-ui" }}>Somebody entering work into CREST</div>
                <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui", marginTop: 2 }}>
                  This does not exist and will not be built. A record CREST captured itself would be evidence CREST is
                  vouching for on its own authority, which is the one thing the trust model cannot do.
                </div>
              </div>
              <Chip kind="plain">not available</Chip>
            </div>
            {ways.recorded ? null : (
              <p className="muted" style={{ margin: "2px 0 10px" }}>
                Both ways stand open until an answer narrows them — flipping a card records the choice on the project.
              </p>
            )}
            <Callout kind="green" title="Why both, usually">
              The two are not alternatives, and most real projects need both. A campaign system covers the work it
              manages, and there is always work it does not: a training an institute ran, a round a contractor
              completed, a month a partner recorded on paper.
            </Callout>
            <div className="form-grid">
              <RefField label="Pull from">
                <select name="pullfrom" defaultValue="">
                  <option value="">—</option>
                  {sources.map((x) => (
                    <option key={x.systemRef || x.adapterRef} value={x.systemRef || x.adapterRef}>
                      {x.systemRef || x.adapterRef} {x.expectedEvery ? "· " + x.expectedEvery : ""}
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
            <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end" }}>
              <Actions back={["Back", () => nav("/definition")]} go={["Save and continue", () => nav("/intake/file")]} />
            </div>
          </div>
        </>
      )}
    </LoadFrame>
  );
}

// ── p2_20/p2_21 · A spreadsheet arrived ──────────────────────────────────────
// The reference's frame reviews a file before anything is written ("Nothing
// has been written yet", Accept N / hold M as buttons). CREST's one ingestion
// door works the other way and this frame says so: POST /v1/batches checks
// every row at ingestion, writes the rows that clear, and holds the rest in
// the unclear queue with a reason and an owner — the accept/hold split is a
// fact the service already decided, not a button.
export function SpreadsheetArrived() {
  const nav = useNavigate();
  const s = useConsole();
  const [fileName, setFileName] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [sourceSystemRef, setSourceSystemRef] = useState("");
  const sourceRead = useLoad(async () => api.get("evidence", "/v1/sources?contextId=" + encodeURIComponent(s.projectId)).catch(() => ({ sources: [] })), [s.projectId]);
  const sources = ((sourceRead.data && sourceRead.data.sources) || []) as Array<{
    systemRef?: string; adapterRef?: string; sourceClass?: string; captureMethod?: string; sourceExposure?: string;
  }>;
  const [result, setResult] = useState<{
    batch?: { id?: string };
    claimIds?: string[];
    unclear?: Array<{ rowRef?: string; reason?: string }>;
  } | null>(null);
  const rows = text
    ? text.trim().split(/\r?\n/).slice(0, 7).map((l) => l.split(","))
    : [];
  const submit = async () => {
    setErr(null);
    setBusy(true);
    try {
      const source = sources.find((x) => x.systemRef === sourceSystemRef);
      const def = s.definitions.find((x) => x.id === s.definitionId);
      if (!def?.version) throw new Error("choose an active definition version before uploading evidence");
      if (!source?.systemRef) throw new Error("choose a registered source before uploading evidence");
      const qs = new URLSearchParams({
        contextId: s.projectId,
        definitionId: s.definitionId,
        definitionVersion: String(def.version),
        submittedBy: s.me!.partyId,
        systemRef: source.systemRef,
      });
      setResult(await api.postRaw("evidence", "/v1/batches?" + qs.toString(), text, "text/csv"));
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const accepted = result?.claimIds?.length ?? 0;
  const held = result?.unclear?.length ?? 0;
  return (
    <>
      <Title t="A spreadsheet arrived" />
      <Lede>
        {result
          ? `${fileName} · uploaded by ${s.me!.who}. Every row has already been checked; what cleared is written, what did not is held with a reason.`
          : "Pick the filled template. The file goes through the one ingestion door that exists — every row is checked against the definition before anything is written."}
      </Lede>
      {err ? <div className="errbar">{err}</div> : null}
      <div style={{ maxWidth: 820 }}>
        {!s.definitionId ? (
          <Callout kind="grey" title="No definition is in force">
            Evidence is only accepted against an ACTIVE work definition, and this deployment has none yet — the
            reference's file is itself "generated from WD-4471 v1.2". Author and ratify a definition first; this
            frame then accepts files against it.
          </Callout>
        ) : null}
        <div className="form-grid" style={{ marginBottom: 12 }}>
          <RefField label="Registered source" hint="The source's approved provenance is registered by its operator; this upload chooses that identity.">
            <select value={sourceSystemRef} onChange={(e) => setSourceSystemRef(e.target.value)} required>
              <option value="">Choose a registered source…</option>
              {sources.map((source) => (
                <option key={source.systemRef} value={source.systemRef}>
                  {source.systemRef} · {source.sourceClass || "approved class"} · {source.captureMethod || "approved capture"} · {source.sourceExposure || "approved exposure"}
                </option>
              ))}
            </select>
          </RefField>
          {s.definitionId ? <RefField label="Definition version" value={s.definitions.find((d) => d.id === s.definitionId)?.version ? "v" + s.definitions.find((d) => d.id === s.definitionId)?.version : "selected definition has no version"} /> : null}
        </div>
        <div
          style={{ padding: "12px 14px", border: "1px dashed var(--line, #DDD9D3)", borderRadius: 8, marginBottom: 12, display: "flex", gap: 12, alignItems: "center" }}
        >
          <input
            type="file"
            accept=".csv,text/csv"
            onChange={(ev) => {
              const f = ev.target.files?.[0];
              if (!f) return;
              setFileName(f.name);
              setResult(null);
              f.text().then(setText);
            }}
          />
          <span className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui" }}>
            The template's columns: activity, outcome_value, outcome_unit, worker_id_kind, worker_id, period_start,
            period_end — a registered source's own column names are translated by its mapping.
          </span>
        </div>
        {rows.length ? (
          <div style={{ overflowX: "auto", marginBottom: 12 }}>
            <Tbl heads={rows[0]} rows={rows.slice(1)} empty="the file has a header and no rows" />
          </div>
        ) : null}
        {result ? (
          <>
            <div style={{ display: "flex", gap: 12, marginBottom: 12 }}>
              {[
                [accepted, "Rows that cleared and were written", "#00703C"],
                [held, "Rows held, each with a reason and an owner", "#C84C0E"],
              ].map(([n, label, color]) => (
                <div key={String(label)} style={{ flex: 1, border: "1px solid var(--line, #DDD9D3)", borderRadius: 8, padding: "14px 16px" }}>
                  <div style={{ font: "600 26px/1.2 Roboto,system-ui", color: String(color) }}>{n}</div>
                  <div className="muted" style={{ font: "400 13px/1.5 Roboto,system-ui" }}>{label}</div>
                </div>
              ))}
            </div>
            {held ? (
              <Tbl
                heads={["Row", "Why it is held"]}
                rows={(result.unclear || []).map((u) => [u.rowRef || "", u.reason || ""])}
                empty=""
              />
            ) : null}
          </>
        ) : null}
        <Callout kind="teal" title="What the check actually is">
          Every row is checked against the definition before anything is accepted: does the worker exist, is the
          definition the active version, is the quantity within what the definition permits, is the date inside the
          period. A row that fails any of those is held, and the rest are unaffected — the reference's "Accept N,
          hold M" is a decision this service makes at ingestion, row by row, so here it is a receipt rather than a
          button.
        </Callout>
        <div style={{ borderTop: "1px solid var(--line, #E5E1DC)", marginTop: 16, paddingTop: 14, display: "flex", justifyContent: "flex-end", gap: 10 }}>
          <button className="btn secondary" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/intake")}>
            Back
          </button>
          {result ? (
            <button className="btn dominant" style={{ width: "auto", padding: "10px 22px" }} onClick={() => nav("/validation")}>
              Continue
            </button>
          ) : (
            <button
              className="btn dominant"
              style={{ width: "auto", padding: "10px 22px" }}
              disabled={!text || !s.definitionId || busy}
              onClick={submit}
            >
              {busy ? "Submitting…" : "Submit the file"}
            </button>
          )}
        </div>
      </div>
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
