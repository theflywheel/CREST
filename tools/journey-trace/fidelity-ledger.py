#!/usr/bin/env python3
"""Hold the traceability ledger to the fidelity gate's own results.

`docs/journey-traceability.md` is where CREST says which reference screens
exist. A screen statused `implemented` that fails its `docs/journey-spec.json`
assertions is a claim the ledger should not be able to make — that is the whole
point of the gate, and this script is the part that refuses to let the claim
stand.

Reads tests/e2e-apps/fidelity-results.jsonl (written by fidelity.spec.js, one
JSON object per screen, gitignored) and fails when:

  * a screen statused `implemented` failed its assertions,
  * an in-scope screen produced no verdict at all — a screen nobody judged is
    not a screen that passed,
  * a waiver or quarantine entry carries no reason, or a quarantine names no
    issue,
  * a waiver or quarantine names a screen the gate does not have in scope, or
    one the ledger no longer calls `implemented` (the debt was recorded against
    a claim that has since changed).

Usage: python3 tools/journey-trace/fidelity-ledger.py
       (make fidelity runs it straight after the Playwright suite)
"""
import json
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "tests" / "e2e-apps" / "fidelity-results.jsonl"
LEDGER = ROOT / "docs" / "journey-traceability.json"
FMAP = ROOT / "tests" / "e2e-apps" / "fidelity-map.json"
WAIVERS = ROOT / "tests" / "e2e-apps" / "fidelity-waivers.json"
QUARANTINE = ROOT / "tests" / "e2e-apps" / "fidelity-quarantine.json"


def load(p):
    return json.loads(p.read_text(encoding="utf-8"))


SPEC = ROOT / "docs" / "journey-spec.json"


def static_check():
    """Everything about the gate that can be checked without a stack.

    Runs in CI's docs job. The gate's own verdicts need a live stack (CI's
    journey-doors job); what this catches is the shape of the gate's inputs —
    a scope entry for a screen the spec does not have, a waiver with no
    reason, a quarantine with no issue — which would otherwise stay invisible
    until somebody happened to run the suite."""
    spec = load(SPEC)["screens"]
    fmap = load(FMAP)
    scope = fmap["screens"]
    waivers = load(WAIVERS)["screens"]
    quarantine = load(QUARANTINE)["screens"]
    ledger = load(LEDGER)
    status = {r["id"]: r for r in ledger["rows"] + ledger.get("designRows", [])}

    problems = []
    for sid in scope:
        if sid not in spec:
            problems.append(f"scope names {sid}, which docs/journey-spec.json does not have")
        if sid not in status:
            problems.append(f"scope names {sid}, which the traceability ledger does not carry")
        for key in ("app", "arrive", "route"):
            if key not in scope[sid]:
                problems.append(f"scope entry {sid} has no {key}")

    # Every in-scope screen the SPEC knows about must be in the map, or the
    # gate would quietly cover a subset of its own declared scope.
    for sid, entry in spec.items():
        in_scope = (entry.get("journey") in fmap["scope"]["journeys"]
                    or sid in fmap["scope"]["designScreens"])
        if in_scope and sid not in scope:
            problems.append(f"{sid} is in the declared scope but has no arrival route")

    for sid, facets in waivers.items():
        if sid.startswith("_"):
            continue
        if sid not in scope:
            problems.append(f"waiver for {sid}: not in the gate's scope")
        for facet, why in facets.items():
            if not str(why).strip():
                problems.append(f"waiver {sid}.{facet}: no reason given")
    for sid, q in quarantine.items():
        if sid.startswith("_"):
            continue
        if sid not in scope:
            problems.append(f"quarantine for {sid}: not in the gate's scope")
        if not q.get("issue", "").strip():
            problems.append(f"quarantine for {sid}: names no issue")
        if not q.get("why", "").strip():
            problems.append(f"quarantine for {sid}: no reason given")

    print(f"fidelity gate: {len(scope)} screens in scope, "
          f"{sum(1 for k in waivers if not k.startswith('_'))} screens carrying waivers, "
          f"{sum(1 for k in quarantine if not k.startswith('_'))} quarantined")
    for p in problems:
        print(f"  PROBLEM: {p}", file=sys.stderr)
    return 1 if problems else 0


def main():
    if "--static" in sys.argv[1:]:
        return static_check()
    if not RESULTS.exists():
        print(f"no results at {RESULTS.relative_to(ROOT)} — the gate did not run",
              file=sys.stderr)
        return 1

    results = {}
    for line in RESULTS.read_text(encoding="utf-8").splitlines():
        if line.strip():
            row = json.loads(line)
            results[row["screen"]] = row

    ledger = load(LEDGER)
    status = {r["id"]: r for r in ledger["rows"] + ledger.get("designRows", [])}
    scope = load(FMAP)["screens"]
    waivers = load(WAIVERS)["screens"]
    quarantine = load(QUARANTINE)["screens"]

    problems = []
    counts = Counter()
    waived_facets = 0

    for sid in sorted(scope):
        row = results.get(sid)
        if row is None:
            problems.append(f"{sid}: in scope but the gate produced no verdict")
            counts["unjudged"] += 1
            continue
        counts[row["verdict"]] += 1
        waived_facets += len(row.get("waived", []))
        if row["verdict"] == "failed":
            problems.append(
                f"{sid}: statused {row['status']} but failed "
                + ", ".join(sorted({f["facet"] for f in row.get("failures", [])})))

    for sid, facets in waivers.items():
        if sid.startswith("_"):
            continue
        if sid not in scope:
            problems.append(f"waiver for {sid}: not in the gate's scope")
        if status.get(sid, {}).get("status") != "implemented":
            problems.append(
                f"waiver for {sid}: the ledger no longer calls this implemented "
                f"({status.get(sid, {}).get('status', 'unknown')}) — the waiver "
                "records debt against a claim that has changed")
        for facet, why in facets.items():
            if not str(why).strip():
                problems.append(f"waiver {sid}.{facet}: no reason given")

    for sid, q in quarantine.items():
        if sid.startswith("_"):
            continue
        if sid not in scope:
            problems.append(f"quarantine for {sid}: not in the gate's scope")
        if not q.get("issue", "").strip():
            problems.append(f"quarantine for {sid}: names no issue")
        if not q.get("why", "").strip():
            problems.append(f"quarantine for {sid}: no reason given")

    print(
        "fidelity ledger: {asserted} asserted, {quarantined} quarantined, "
        "{skipped} skipped-with-reason, {failed} failed, "
        "{waived} waived facets across {n} screens in scope".format(
            asserted=counts["asserted"], quarantined=counts["quarantined"],
            skipped=counts["skipped"], failed=counts["failed"],
            waived=waived_facets, n=len(scope)))
    for sid, row in sorted(results.items()):
        if row["verdict"] == "quarantined":
            print(f"  quarantined {sid} ({row.get('issue', '?')}): {row.get('why', '')}")

    for p in problems:
        print(f"  PROBLEM: {p}", file=sys.stderr)
    if problems:
        print("\nThe ledger claims coverage the gate cannot confirm.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
