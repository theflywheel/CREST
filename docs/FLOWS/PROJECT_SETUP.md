---
title: A project is set up and its people invited
---

# A project is set up and its people invited

**J3 · P-1.** The organisation admin creates the project, names its configurator, and invites everyone else. An invitation is a record with a one-time link; a role is a grant the invitee claims with their own login, and the console derives what they see from the grant, never from a chooser.

```mermaid
sequenceDiagram
  autonumber
  participant A as Org admin
  participant Con as Console
  participant P as core · parties
  participant I as Invitee
  A->>Con: create the project, name the configurator
  Con->>P: POST /v1/projects
  A->>Con: invite (author, approver, custodian, rate owner, mechanism owner, agent)
  Con->>P: POST /v1/projects/{id}/invitations
  P-->>Con: one-time claim link each
  I->>Con: opens the link, signs in with their own login
  Con->>P: POST /v1/party-invitations/claim
  P->>P: bind subject → party; the grant becomes live
  Con->>P: GET /v1/authorizations/mine
  Con-->>I: the role's console, derived from the grant
```

## What holds it

- A claimed party id is never a binding credential: a self-bind is accepted only for a never-bound party or the exact subject already bound.
- The author and the approver of a definition must be different parties; the invitations make them so.
- The configurator is named on the project and must acknowledge; a named owner who never agreed leaves a context that looks staffed and is not.

## Recordings

- [The admin sets up the project and invites](../assets/clean-slate-watch/J3-j3-org-admin-sets-up-the-project-and-invites-8x.mp4) (11 s at 8×)
- Claims: [Alice](../assets/clean-slate-watch/J3-claim-alice.mp4), [Amina](../assets/clean-slate-watch/J3-claim-amina.mp4), [Ndegwa](../assets/clean-slate-watch/J3-claim-ndegwa.mp4), [Joseph](../assets/clean-slate-watch/J3-claim-joseph.mp4), [Naomi](../assets/clean-slate-watch/J3-claim-naomi.mp4), [Nadia](../assets/clean-slate-watch/J3-claim-nadia.mp4), [Daniel](../assets/clean-slate-watch/J3-claim-daniel.mp4)

Screens: p1_1–p1_3, p2_1–p2_7. Next: [DEFINITION_LIFECYCLE.md](DEFINITION_LIFECYCLE.md).

