---
name: review-work
description: Review CREST changes with two independent reviewers — one for correctness, one for the worker invariants and design fidelity. Use before opening a PR, after finishing an issue, or when asked to review a diff or branch.
---

# Reviewing CREST work

Two reviewers, never more. One reads the code as code; one reads it as a promise to a worker. They are deliberately different lenses, because redundancy catches fewer failure modes than diversity does.

## When to use which mode

**Inline (default, cheap).** For a small diff — one service, one schema, a handful of files — do both passes yourself in sequence using the lenses below. No agents.

**Two-agent workflow.** For a whole issue's work, a branch, or anything touching evidence / confirmation / payments / verification. Call the `Workflow` tool with the script below. Invoking this skill is the opt-in; you do not need to ask again.

```js
export const meta = {
  name: 'crest-review',
  description: 'Two-lens review of CREST changes: correctness and worker invariants',
  phases: [{ title: 'Review' }, { title: 'Verify' }],
}

const DIFF = args?.diff ?? 'git diff main...HEAD'
const FINDINGS = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          file: { type: 'string' },
          line: { type: 'number' },
          severity: { type: 'string', enum: ['blocker', 'major', 'minor'] },
          claim: { type: 'string' },
          failure_scenario: { type: 'string' },
        },
        required: ['file', 'claim', 'failure_scenario'],
      },
    },
  },
  required: ['findings'],
}

const LENSES = [
  { key: 'correctness', prompt: `Review the changes from \`${DIFF}\` in this repo as a senior engineer.
Look for: wrong logic, unhandled errors, race conditions, missing negative cases, idempotency
violations, silent failure paths, tests that assert on mocks rather than behaviour.
Read docs/TESTING.md for how this project tests. Report only defects you can describe as a
concrete failure scenario — inputs and state leading to a wrong outcome. Do not report style.` },
  { key: 'invariants', prompt: `Review the changes from \`${DIFF}\` against CREST's design of record.
Read docs/crest-infrastructure-blueprint.html (§11 has the W1-W10 worker invariants) and
docs/test-manifest.md first. Ask: does this break a promise to a worker? Specifically —
does any path let a dispute withhold payment (W4), a claim reach ACTIVE unnotified (W2),
a probable identity match auto-merge (W1), a raw national ID or biometric get persisted (W9),
a payment be held with no owned reason (W10), or a disclosure happen without per-request consent (W7)?
Also check the layering test: anything two deployments could reasonably disagree about does not
belong in the infrastructure layer. And check that every new feature has a test-manifest row.` },
]

phase('Review')
const results = await pipeline(
  LENSES,
  l => agent(l.prompt, { label: `review:${l.key}`, phase: 'Review', schema: FINDINGS }),
  (r, lens) => parallel((r?.findings ?? []).map(f => () =>
    agent(`Adversarially verify this review finding against the actual code. Try to REFUTE it.
Finding: ${f.claim}
File: ${f.file}${f.line ? ':' + f.line : ''}
Claimed failure: ${f.failure_scenario}
Read the surrounding code and any relevant test. If the code already handles this, or the
finding rests on a misreading, say refuted:true. Default to refuted:true when genuinely uncertain —
a false finding costs more trust than a missed minor one.`,
      { label: `verify:${f.file}`, phase: 'Verify',
        schema: { type: 'object',
                  properties: { refuted: { type: 'boolean' }, reason: { type: 'string' } },
                  required: ['refuted', 'reason'] } })
      .then(v => ({ ...f, lens: lens.key, verdict: v }))))
)

const confirmed = results.flat().filter(Boolean).filter(f => !f.verdict?.refuted)
return {
  confirmed,
  blockers: confirmed.filter(f => f.severity === 'blocker'),
  dropped: results.flat().filter(Boolean).length - confirmed.length,
}
```

Every finding is adversarially verified before it reaches the user. A review that reports plausible-but-wrong findings trains people to skip reviews.

## The two lenses

**Correctness** — wrong logic, unhandled errors, races, missing negative cases, idempotency, silent failure. Concrete failure scenarios only: inputs and state producing a wrong outcome. Not style, not naming, not preference.

**Invariants and design fidelity** — the questions only this project asks:

- Can a dispute withhold a worker's payment? (**W4** — all four T=7 exits release payment)
- Can a claim reach ACTIVE without the worker being notified? (**W2**)
- Can a probable identity match auto-merge? (**W1** — `merges_without_confirmation = 0`)
- Is any raw national ID or biometric persisted? (**W9** — pairwise ref and salted hash only)
- Can a payment be held with no owned reason? (**W10**)
- Does any disclosure happen without per-request consent, or is a refusal treated as an error rather than a value? (**W7**)
- Does anything in `infra-l1` fail the layering test — could two deployments reasonably disagree about it?
- Does every new feature have a `docs/test-manifest.md` row?

## Reporting

Findings first, ranked by severity, each with a concrete failure scenario. Then, briefly, what you checked and found sound — a review that only lists problems gives no signal about coverage.

**A design finding is not a review comment.** If the work reveals that the blueprint is wrong, that is an issue (`Design finding` template) plus a blueprint correction, not a note in a thread that disappears when the PR merges.
