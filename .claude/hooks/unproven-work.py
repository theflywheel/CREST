#!/usr/bin/env python3
"""Warn when a change adds surface area but no way of proving it works.

A feature with no row in docs/test-manifest.md is unproven, and the manifest is
maintained by hand — which means it is the first thing dropped when a change is
finished late. This hook says so at the moment the change is made, while the
author still remembers what the new code is for.

It warns and exits 0, always. A blocking check that is wrong once gets disabled
for good, and a disabled check protects nothing. The cost of ignoring a warning
is one line of noise; the cost of a false block is the whole hook.
"""
from __future__ import annotations

import re
import subprocess
import sys

MANIFEST = "docs/test-manifest.md"

# Added lines that register a new HTTP route. Kept deliberately broad: this is a
# prompt, not a parser, and a missed endpoint is worse than an extra warning.
ENDPOINT_PATTERNS = [
    re.compile(r"""\.(Get|Post|Put|Patch|Delete|Head|Options|Handle|HandleFunc)\(\s*["'`]([^"'`]+)"""),
    re.compile(r"""(?i)\b(?:router|mux|app|r|e|g|api)\.(get|post|put|patch|delete)\(\s*["'`]([^"'`]+)"""),
    re.compile(r"""(?i)@(Get|Post|Put|Patch|Delete)Mapping\(\s*["']([^"']+)"""),
]


def git(*args: str) -> str:
    try:
        out = subprocess.run(
            ["git", *args], capture_output=True, text=True, check=False
        )
    except OSError:
        return ""
    return out.stdout


def changed_files(rev_range: str | None) -> list[str]:
    if rev_range:
        names = git("diff", "--name-only", "--diff-filter=ACMR", rev_range)
    else:
        names = git("diff", "--cached", "--name-only", "--diff-filter=ACMR")
        if not names.strip():
            # Nothing staged: fall back to the working tree, untracked files
            # included — a brand-new service file is exactly the case worth
            # naming, and it is untracked until the moment it is added.
            names = git("diff", "--name-only", "--diff-filter=ACMR")
            names += git("ls-files", "--others", "--exclude-standard")
    return sorted({n for n in names.splitlines() if n.strip()})


def added_files(rev_range: str | None) -> set[str]:
    if rev_range:
        names = git("diff", "--name-only", "--diff-filter=A", rev_range)
    else:
        names = git("diff", "--cached", "--name-only", "--diff-filter=A")
        if not names.strip():
            names = git("ls-files", "--others", "--exclude-standard")
    return {n for n in names.splitlines() if n.strip()}


def added_lines(path: str, rev_range: str | None) -> list[str]:
    if rev_range:
        diff = git("diff", "--unified=0", rev_range, "--", path)
    else:
        diff = git("diff", "--cached", "--unified=0", "--", path)
        if not diff.strip():
            diff = git("diff", "--unified=0", "--", path)
    return [l[1:] for l in diff.splitlines() if l.startswith("+") and not l.startswith("+++")]


def is_test(path: str) -> bool:
    low = path.lower()
    return (
        low.endswith("_test.go")
        or "/tests/" in low
        or "/testdata/" in low
        or low.startswith("harness/")
        or ".test." in low
        or ".spec." in low
    )


def find_unproven(rev_range: str | None) -> list[str]:
    files = changed_files(rev_range)
    if MANIFEST in files:
        return []

    new = added_files(rev_range)
    reasons: list[str] = []

    for path in files:
        if is_test(path):
            continue

        if path.startswith("services/") and path in new:
            reasons.append(f"{path} — a new file under services/, with no manifest row")
        elif path.startswith("pkg/") and path in new:
            reasons.append(f"{path} — a new package file under pkg/, with no manifest row")

        if path.startswith(("services/", "pkg/")):
            routes: list[str] = []
            for line in added_lines(path, rev_range):
                for pat in ENDPOINT_PATTERNS:
                    m = pat.search(line)
                    if m:
                        routes.append(m.group(2))
                        break
            for route in sorted(set(routes)):
                reasons.append(f"{path} — new endpoint `{route}`, with no manifest row")

    # A path can trip both the new-file rule and the endpoint rule; the endpoint
    # is the more specific statement, so drop the generic one for that file.
    with_routes = {r.split(" — ", 1)[0] for r in reasons if "new endpoint" in r}
    return [r for r in reasons if not (r.split(" — ", 1)[0] in with_routes and "new endpoint" not in r)]


def report(reasons: list[str]) -> None:
    print("", file=sys.stderr)
    print("Unproven work: this change adds surface area and leaves "
          f"{MANIFEST} untouched.", file=sys.stderr)
    print("", file=sys.stderr)
    for r in reasons:
        print(f"  {r}", file=sys.stderr)
    print("", file=sys.stderr)
    print(
        "A feature with no manifest row is unproven regardless of what CI says,\n"
        "because nobody reviewing later can tell whether it was ever validated.\n"
        "Add a row naming how the behaviour is proven and an honest status\n"
        "(planned / partial / spike / covered / unproven), in this same change.\n"
        "\n"
        "If none of the above is a feature — a rename, a move, wiring with no\n"
        "behaviour of its own — say so and carry on. This warns; it never blocks.",
        file=sys.stderr,
    )


def main() -> int:
    rev_range = None
    args = sys.argv[1:]
    if args and args[0] == "--scan":
        args = args[1:]
    if args:
        rev_range = args[0]

    reasons = find_unproven(rev_range)
    if reasons:
        report(reasons)
    return 0


if __name__ == "__main__":
    sys.exit(main())
