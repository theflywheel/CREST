#!/usr/bin/env python3
"""Stop hook: before finishing, check the work left the repo consistent.

Advisory. It asks three questions the project cares about and that are easy to
forget at the end of a long task:
  1. Source changed but no test changed?
  2. A feature landed but docs/test-manifest.md was not touched?
  3. A contract changed but the blueprint was not?

Exits 0 with context, or 2 to ask the model to address it before stopping.
"""
import json
import os
import subprocess
import sys

CODE_EXT = (".py", ".ts", ".js", ".go", ".java", ".kt", ".rs", ".rb", ".sql")
CONTRACT_HINTS = ("schema", "openapi", "credential", "primitive", "adapter", "evidence")


def git(*args):
    root = os.environ.get("CLAUDE_PROJECT_DIR", ".")
    r = subprocess.run(["git", "-C", root, *args], capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else ""


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except Exception:
        return 0

    # Never loop: if we already asked once this turn, let it stop.
    if data.get("stop_hook_active"):
        return 0

    # -uall so untracked directories expand to files; without it a whole new
    # service collapses to "services/" and every check below silently misses it.
    changed = [l[3:].strip().strip('"')
               for l in git("status", "--porcelain", "-uall").splitlines() if l.strip()]
    if not changed:
        return 0

    def is_test(f):
        base = os.path.basename(f).lower()
        return (
            f.startswith("tests/")
            or "/tests/" in f
            or base.startswith("test_")
            or base.endswith(("_test.py", "_test.go", ".test.ts", ".test.js", ".spec.ts", ".spec.js"))
        )

    # .claude/ is tooling about the work, not the work itself.
    src = [f for f in changed
           if f.endswith(CODE_EXT) and not is_test(f) and not f.startswith(".claude/")]
    tests = [f for f in changed if is_test(f)]
    manifest = any(f.endswith("test-manifest.md") for f in changed)
    blueprint = any("blueprint" in f for f in changed)
    contracts = [f for f in src if any(h in f.lower() for h in CONTRACT_HINTS)]

    notes = []
    if src and not tests:
        notes.append(
            f"{len(src)} source file(s) changed with no test change. "
            "Which layer proves this — unit, contract, or harness? (skill: `write-tests`)"
        )
    if src and not manifest:
        notes.append(
            "`docs/test-manifest.md` was not updated. Every feature needs a row saying how it "
            "is proven; a feature with no row is unproven whatever CI reports."
        )
    if contracts and not blueprint:
        notes.append(
            f"Contract-shaped files changed ({', '.join(contracts[:3])}) but the blueprint did not. "
            "If the contract actually changed, the design of record is now stale (skill: `sync-design-docs`)."
        )

    if not notes:
        return 0

    print("Before finishing — three checks this project cares about:\n\n"
          + "\n".join(f"- {n}" for n in notes)
          + "\n\nAddress them, or state plainly why they don't apply to this change. "
            "Either is fine; silently skipping them is not.",
          file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
