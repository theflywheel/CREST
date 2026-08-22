#!/usr/bin/env python3
"""PostToolUse: point edits at the design section and invariants they touch.

Advisory only — never blocks. It exists so the blueprint reference is in front
of the model at the moment it edits a contract, rather than after.
"""
import json
import os
import sys

# path fragment -> (blueprint section, what to keep true)
AREAS = [
    ("evidence", "§8 evidence ingestion contract",
     "canonical record shape, provenance facts, source heartbeat"),
    ("adapter", "§8 evidence ingestion contract",
     "adapters connect through configuration — an L1 change here is a design finding"),
    ("confirm", "§11 worker invariants",
     "all four T=7 exits release payment (W4); no claim goes ACTIVE unnotified (W2)"),
    ("payment", "§10 payments boundary",
     "instruction idempotency; every reconciliation gap keeps an owner and a reason (W10)"),
    ("verif", "§6 strength derivation, §9 consent",
     "tier is derived at query time, never stored; refusals are values, not errors (W7)"),
    ("identity", "§4.1 identity providers",
     "pairwise reference and salted hash only — never a raw ID or biometric (W9)"),
    ("match", "§8 evidence ingestion",
     "probable matches hold; merges_without_confirmation stays 0 (W1)"),
    ("registry", "§3 registry architecture",
     "public facts to DeDi, personal data to the private store"),
    ("definition", "§7 definition lifecycle",
     "author != approver; immutable once ACTIVE; old versions still resolve"),
    ("credential", "§5 credential substrate",
     "status list is the single central fact; no central credential register"),
    ("schema", "§2 the eleven primitives",
     "primitives stay generic — use-case vocabulary belongs in a profile"),
]

CODE_EXT = (".py", ".ts", ".js", ".go", ".java", ".kt", ".rs", ".rb", ".sql", ".yaml", ".yml", ".json")


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except Exception:
        return 0

    ti = data.get("tool_input") or {}
    path = ti.get("file_path", "")
    if not path or not path.endswith(CODE_EXT):
        return 0

    rel = os.path.relpath(path, os.environ.get("CLAUDE_PROJECT_DIR", "."))
    low = rel.lower()

    # docs and tests have their own hooks; this one is about implementation code
    if low.startswith("docs/") or low.startswith(".claude/"):
        return 0

    hits = [(sec, keep) for frag, sec, keep in AREAS if frag in low]
    if not hits:
        return 0

    seen, lines = set(), []
    for sec, keep in hits:
        if sec in seen:
            continue
        seen.add(sec)
        lines.append(f"- **{sec}** — {keep}")

    msg = (
        f"`{rel}` touches a specified area of CREST. Keep these true:\n"
        + "\n".join(lines)
        + "\n\nDesign of record: `docs/crest-infrastructure-blueprint.html`. "
        "If the code and the blueprint now disagree, that is a design finding — "
        "open an issue and correct the blueprint (skill: `sync-design-docs`), "
        "rather than patching around it."
    )

    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PostToolUse",
            "additionalContext": msg,
        }
    }))
    return 0


if __name__ == "__main__":
    sys.exit(main())
