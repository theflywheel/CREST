---
name: bookkeeping
description: Close out a piece of CREST work so the tracking stays true — verify the issue's "done when", write the manifest rows, raise any design finding, write the PR body, move the board. Use when finishing an issue, before opening a PR, or when asked to update tracking for work already done.
---

# Closing out CREST work

The tracking is only worth reading if it is true. An issue closed against an unmet criterion, a feature with no manifest row, a board column that says Done when nothing shipped — each of these is worse than no tracking at all, because the next person believes them and stops checking.

Work through these in order. Later steps assume the earlier ones are honest.

## 1. Which issue does this close, and is its "done when" actually satisfied

```sh
gh issue view <N> -R theflywheel/CREST
```

Read the **"done when"** line by line and check each item against what you actually built — not against what you intended to build. "The code compiles" and "the tests pass" are not acceptance criteria; the issue states real ones.

Three outcomes:

**Satisfied.** Carry on to step 2.

**Partly satisfied.** Do not close. Say what is missing, in the PR body and in a comment on the issue, and leave it open. A PR that silently drops half its scope is worse than one that states the gap, because the gap survives unrecorded.

**The criterion was wrong.** This happens, and it is a legitimate outcome. Edit the issue to state the right criterion and why the old one was wrong, then satisfy that.

```sh
gh issue comment <N> -R theflywheel/CREST \
  --body "The 'done when' asked for X. Working it showed Y, because <reason>. Amending to: <new criterion>."
gh issue edit <N> -R theflywheel/CREST --body-file /tmp/issue-<N>.md
```

Never edit a criterion silently, and never close against one nobody met. The whole point of writing acceptance criteria down is that they are harder to move than a memory is.

## 2. Manifest rows, with an honest status

Every feature gets a row in `docs/test-manifest.md` **in the same change**. Not a follow-up, not "next PR" — the same change, because a manifest row added later is written from memory by someone who no longer remembers what was not tested.

For each behaviour the change adds, write a row naming how that behaviour is proven and pick the status you can defend:

| Status | What it claims |
|---|---|
| `planned` | No code yet. A debt, and it should have a date. |
| `partial` | Some layers proven, others not. Say which in the row. |
| `spike` | A check exists and passes, run on demand rather than in CI. |
| `covered` | Every listed check passes in CI. |
| `unproven` | Code exists, validation does not. **Treat as a bug.** |

The temptation is to write `covered` because the work feels finished. `covered` means CI runs it; if you ran it once by hand, that is `spike`. An overstated row is the specific failure this file exists to prevent — a pilot shipping on a feature that wears a passing test it never had.

If the issue you are closing has manifest rows, they must be `covered` before it closes, or the issue must say explicitly why not.

The `unproven work` pre-commit hook warns when a change adds a service file, a package under `pkg/`, or a new endpoint while leaving the manifest untouched. It warns rather than blocks, so it is on you to read it. Run it directly at any time:

```sh
python3 .claude/hooks/unproven-work.py --scan          # staged, or working tree
python3 .claude/hooks/unproven-work.py --scan main...HEAD   # the whole branch
```

## 3. Did the work contradict the design

Ask it explicitly, because the answer is easy to walk past when the code already works. A primitive that needed a use-case field, an adapter class that could not be configured without an L1 change, a substrate that did not compose the way its documentation says — those are **results**, not obstacles.

When one happened:

```sh
gh issue create -R theflywheel/CREST --template "Design finding" \
  --title "<what the design says, and what reality does>"
```

Then correct the blueprint (load `sync-design-docs`) and say so plainly in the PR body. Quietly patching around a design error is the one failure mode this project cannot afford, because the error then survives into a pilot wearing a passing test.

## 4. The PR body

One PR per issue, where the issue is small enough. The body carries the reasoning, because a PR is the only place where the reasoning sits next to the change permanently. It needs, at minimum:

- **`Closes #N`** — so the issue closes on merge with the change attached to it.
- **What changed**, in a few lines. Not a file list; the diff is the file list.
- **Which of W1–W10 this could break, and how it doesn't.** Every change touching evidence, confirmation, payments or verification names one. "None of them, because this is X" is a valid answer, stated rather than omitted.
- **How it was proven** — the manifest rows added or moved, and what runs them.
- **What you did not do.** Known gaps, deferred scope, a criterion amended in step 1.
- **Any design finding**, linked.

```sh
gh pr create -R theflywheel/CREST --fill --body-file /tmp/pr-body.md
```

Do not merge without CI green. A red PR is information; merging past it destroys the information.

## 5. The board move

Status is Todo / In Progress / **Blocked** / Done on [CREST Delivery](https://github.com/orgs/theflywheel/projects/2).

`Blocked` means a *recorded* GitHub dependency is unmet. Nothing else. If work is stuck because a decision is pending, a sandbox has not been provisioned, or you ran out of context, that is a comment or a new issue — not a board status. The moment `Blocked` also means "stuck", nobody can tell from the board which items are waiting on something real.

If you find yourself explaining a blocker in a comment, record it as a dependency instead.

```sh
gh project item-list 2 --owner theflywheel --format json | jq '.items[] | select(.content.number == <N>)'
gh project item-edit --project-id <PID> --id <ITEM_ID> \
  --field-id <STATUS_FIELD_ID> --single-select-option-id <OPTION_ID>
```

The board is a view. If the board and an issue disagree, the issue wins — fix the board, not the issue.

## When `gh` fails with a Projects error

`gh issue close` and `gh pr edit` route through GraphQL and can fail with:

```
Projects (classic) is being deprecated in favor of the new Projects experience
```

This has bitten this repo twice. It is not a permissions problem and retrying does not help. Go through the REST API instead:

```sh
gh api -X PATCH repos/theflywheel/CREST/issues/<N> -F body=@/tmp/issue-<N>.md
gh api -X PATCH repos/theflywheel/CREST/issues/<N> -f state=closed
```

The same applies to a PR body — a PR is an issue as far as REST is concerned, so `repos/theflywheel/CREST/issues/<PR number>` edits it.

## Before you say you are done

- [ ] "Done when" satisfied item by item, or amended with a stated reason
- [ ] Manifest rows written, each with a status you would defend in review
- [ ] Design finding raised and blueprint corrected, if the work found one
- [ ] PR body names the issue, the invariant, the proof, and the gaps
- [ ] Board moved, with `Blocked` used only for a recorded dependency
