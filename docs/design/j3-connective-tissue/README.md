# J3 connective tissue — the console screens the reference does not draw

Step 1 of the J3 wave ("Setting up a project" — actors **P-1 Org Admin** and
**P-2 Project Configurator**). Designed in Pencil, in the reference's own
grammar, and specified here so implementation has something to build against
and the fidelity gate has something to assert.

The 17 Aug actor-journeys reference draws 24 J3 screens (`p1_1`–`p1_3`,
`p2_1`–`p2_21`). Every one of them opens with the actor **already inside their
console**, already holding a role, already looking at the right organisation or
project. Four things the journey depends on are therefore never drawn:

1. how anyone signs in at all,
2. how a person holding roles in more than one place chooses where to work,
3. what the rail means when two different actors share it,
4. what the receiving side of the `p1_3 → p2_1` handover looks like — the actor
   changes mid-flow (Peter Otieno signs `p1_3`, Dr. Alice Mutua signs `p2_1`)
   and the reference shows only the giving side.

A fifth screen falls out of the fourth: if roles decide what a rail entry does,
something must happen when you open an entry you cannot write to.

These five are specified below as screens `n1`–`n5` (`n` for navigation), in the
same shape as `docs/journey-spec.json` entries, so they can be asserted the same
way. **They are our design, not the reference's** — that distinction is recorded
in the spec's `source` field and must survive into the traceability ledger.

## Two findings about the reference itself

**F1 — the rail is identical for both J3 actors.** Every `p1_*` and `p2_*` frame
carries the same five entries: Projects · People & roles · Work definitions ·
Payment set up · Workers. The Org Admin and the Project Configurator see the
same rail; only the appbar identity changes. This is a reference *decision*, not
an omission, so the implementation must **not** invent a role-scoped rail that
hides entries. Role decides what an entry does, and an entry you cannot act on
says who can (screen `n5`).

**F1 is corrected — the rail is identical per _section_, not across all of
J3.** Measured against `docs/journey-spec.json` while building the screens
(Step 3): the 24 J3 frames carry **three** rails, not one.

| Rail | Frames | Entries |
|---|---|---|
| setup | `p1_1`–`p1_3`, `p2_1`–`p2_7`, `p2_17`–`p2_21` | Projects · People & roles · Work definitions · Payment set up · Workers |
| dashboard | `p2_11`–`p2_16` | Work status · Quality · Payments · Proof · Reports |
| finance / support | `p2_8`–`p2_10` | Project · Work definitions · Finance · Support · Dashboard |

The appbar identity changes with them: `p2_8`–`p2_16` are signed by
Dr. Sarah Kimani, the rest of P-2 by Dr. Alice Mutua, for the same role.

What F1 got right, and what the console implements, is the *rule*: within a
section the rail is identical for both J3 actors, and no entry is ever removed
because of a role. What F1 got wrong is the *count* — "every `p1_*` and `p2_*`
frame carries the same five entries" is not true of the nine dashboard and
finance frames. The console therefore follows the reference frame by frame
(`frontend/apps/console/src/App.tsx`, `railFor`), and `n3` states the
corrected rule on its own face rather than the original one.

**F2 — the handover is an authority change with no acknowledgement.** `p1_3`
("Creating a project, and handing it over") names a Configurator and moves
straight to `p2_1`. A named owner who never agreed leaves a project that looks
staffed and is not, so `n4` draws the receiving side, including a real decline
path that returns the project to the Org Admin's queue rather than deleting
anything.

Both findings belong in the blueprint when this wave lands (§15 journey scope
map), and F2 has a backend consequence: project ownership needs an accepted /
declined state, not just a named owner.

## The screens

Rendered exports (1160×900, 2×): `n1-sign-in.png`,
`n2-choose-where-to-work.png`, `n3-rail-and-navigation.png`,
`n4-project-handed-to-you.png`, `n5-role-guard.png`.

### `n1` — Sign in to CREST Console

| | |
|---|---|
| Layout | desktop console frame, appbar only (no rail — nothing is scoped yet) |
| Appbar | `CREST Console` · right side `Not signed in` |
| Level | L1 |

- Title: **Sign in to CREST Console**
- Lede: "One door, every console role. What you see after signing in is decided
  by the roles you hold — never by which link you opened."
- Panel `SIGN IN WITH`: primary **Continue with eSignet**, note "Your national
  identity provider. CREST never sees a credential of yours."
- Divider, then `OR, ON A DEMO INSTANCE`: three persona rows — Peter Otieno
  (Org Admin · Ministry of Health), Dr. Alice Mutua (Project Configurator ·
  Malaria Bednet Campaign — 2026), Amina Yusuf (Work Definition Author ·
  Ministry of Health). The demo block is instance-configured, never present
  where a real identity provider is.
- Callout (teal) `WHY THIS SCREEN EXISTS`, callout (green) `WHAT THIS SCREEN
  NEVER DOES`: "It never asks which role you want, and never offers a role you
  do not hold. A role is granted in the registry and read back here — picking
  your own would make authority a matter of self-declaration."

Assertions: no role selector anywhere on the screen; the persona block is
absent when the instance has no mock identity provider; signing in lands on
`n2` and never directly on a project.

### `n2` — Where do you want to work?

| | |
|---|---|
| Layout | desktop, appbar only (scope not yet chosen) |
| Appbar | right side `Peter Otieno · signed in` — name without a role, deliberately |
| Level | L1 |

- Title: **Where do you want to work?**
- Two context cards: `ORGANISATION` Ministry of Health / "You hold: Org Admin" →
  **Open standing configuration** (lands `p1_1`); `PROJECT` Malaria Bednet
  Campaign — 2026 / "You hold: Project Configurator" → **Open project setup**
  (lands `p2_1`, or `n4` when the handover is unacknowledged).
- Callout (green): "It never lists a context you hold no role in, and never
  shows a project before somebody handed it to you. An empty list is a true
  answer: it means a role still has to be granted."

Assertions: contexts come from granted roles read back from the registry, never
from client state; exactly one context auto-forwards (skip this screen when a
person holds one role only); zero contexts renders the empty state with the
name of somebody who can grant a role, never a blank page.

### `n3` — One rail, two actors

| | |
|---|---|
| Layout | desktop, appbar + 212px rail + pane |
| Appbar | `Peter Otieno · Org Admin` |
| Rail | Projects (active) · People & roles · Work definitions · Payment set up · Workers |
| Level | L1 |

The navigation contract, as a table of rail entry × actor:

| Rail entry | Org Admin | Project Configurator |
|---|---|---|
| Projects | Create a project and hand it to a Configurator | Open the project handed to them |
| People & roles | Grant and remove roles — a role is held, not just recorded | See who holds what, read only |
| Work definitions | Commission an author | Choose an origin, then ratify what comes back |
| Payment set up | Own the rate table and the mechanism | Choose the posture, never the rates |
| Workers | Hand the registry to a custodian | Registration or import, for this project |

- Callout (teal) `THE RULE THIS SCREEN SETS`: "An entry is never hidden because
  of your role. A row you cannot act on stays visible and names who can — a
  missing row reads as a feature that does not exist, which is a lie the console
  can avoid telling. This is the same posture as a held payment carrying a
  reason with an owner."
- Callout (green) `WHAT THE REFERENCE ALREADY DECIDED`: finding F1.

Assertions: the rail renders five entries for every J3 role; no entry is
conditionally removed; the active entry carries the orange left border.

### `n4` — Ministry of Health handed you a project

| | |
|---|---|
| Layout | desktop, appbar + rail + pane |
| Appbar | `Dr. Alice Mutua · Project Configurator` |
| Level | L2 (a project's own composition) |

- Subtitle: project name · coverage.
- Lede: "Peter Otieno created this project and named you its Configurator.
  Creating a project is not configuring one — what arrives is a name, a coverage
  area and an owner. Everything that decides how it runs is still unanswered,
  and it is yours to answer."
- Two columns — `WHAT ARRIVED, ALREADY DECIDED` (project name, coverage,
  configurator and who named them, organisation + terms version) and `WHAT IS
  STILL YOURS TO ANSWER` (the five composition choices, definition origin,
  payment posture, ownership split, activation).
- Actions: secondary **Not mine — hand it back**, primary **Continue to setup**
  (→ `p2_1`).
- Callout (green) `WHY HANDING IT BACK IS A REAL BUTTON`: "A named owner who
  never agreed is worse than no owner: the project looks staffed and is not.
  Declining records who declined and why, and returns the project to the Org
  Admin's queue rather than deleting anything."

Assertions: the "already decided" column is read from the project record, never
re-entered; declining writes a reason and an actor and leaves the project
intact; the screen appears exactly once per handover and not again after
acceptance.

### `n5` — People & roles is not yours to change

| | |
|---|---|
| Layout | desktop, appbar + rail + pane; the attempted entry stays active in the rail |
| Appbar | `Dr. Alice Mutua · Project Configurator` |
| Level | L1 |

- Title states the boundary, not an error code.
- `WHO CAN DO THIS`: the named Org Admin and how to reach them — "a refusal
  without an owner is a dead end, and this system does not leave people at dead
  ends."
- `WHAT YOU CAN SEE HERE, READ ONLY`: the current role holders with grant dates
  and who granted them.
- Action: secondary **Back to project setup**.
- Callout (green) `THE RULE THIS SCREEN SETS`: "No blank refusal, and no
  vanished entry. A guard states the role you would need, names somebody who can
  grant it, and still shows whatever you are allowed to read."

Assertions: guarded routes render this screen rather than redirecting away; the
grantor is named from real authorization data; readable data is still rendered;
no HTTP status code is shown to the user.

## What Step 2 needs from the backend

Derived from the five screens and the 11 missing J3 reference screens:

- `POST /v1/projects` with a named configurator, plus **accepted / declined**
  ownership state (`p1_3`, `p2_7`, and finding F2).
- Role grants readable and writable, with grantor and grant date
  (`p1_2`, `p2_6`, `n2`, `n5`).
- Project composition record for the five choices (`p2_1`, `p2_3`, `p2_5`).
- Partner directory and time-bound grants (`p2_17`, `p2_18`).
- Finance-code link and support owner (`p2_8`, `p2_10`).

Each goes through the layering test before it is built: anything a deployment
could reasonably disagree about — role vocabularies, posture names, finance code
formats — stays L2 configuration.

## Source files

The Pencil document holding these five frames plus the CREST console component
kit (appbar, rail item, field, buttons, callouts) is
`console-connective-tissue.pen` in this directory. The PNG exports are the
reviewable form and the input to screenshot-pair comparison; regenerate them
from the Pencil document when the design changes.
