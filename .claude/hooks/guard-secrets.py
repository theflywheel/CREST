#!/usr/bin/env python3
"""PreToolUse guard: block secrets and real PII from entering the repo.

Exit 2 blocks the tool call and feeds stderr back to the model.
CREST holds workers' records; a leaked rail credential or a real national ID
in a fixture is not a style problem.
"""
import json
import re
import sys

PATTERNS = [
    (r"AKIA[0-9A-Z]{16}", "AWS access key id"),
    (r"gh[pousr]_[A-Za-z0-9]{20,}", "GitHub token"),
    (r"-----BEGIN (?:RSA |EC |OPENSSH |PGP )?PRIVATE KEY-----", "private key"),
    (r"xox[baprs]-[A-Za-z0-9-]{10,}", "Slack token"),
    (r"sk-[A-Za-z0-9]{32,}", "API secret key"),
    (r"(?i)\b(?:api[_-]?key|secret|passwd|password|client[_-]?secret)\s*[:=]\s*['\"][^'\"\s]{8,}['\"]",
     "hardcoded credential"),
]

# Real-identifier shapes that must never appear, per W9 and the testing rules.
PII = [
    (r"\b\d{4}\s?\d{4}\s?\d{4}\b", "12-digit national ID (use synthetic identifiers)"),
]

ALLOW_MARKER = "crest:allow-secret"  # explicit, reviewable escape hatch


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except Exception:
        return 0

    ti = data.get("tool_input") or {}
    name = data.get("tool_name", "")

    if name in ("Write", "Edit"):
        path = ti.get("file_path", "")
        content = ti.get("content") or ti.get("new_string") or ""
    elif name == "Bash":
        path = ""
        content = ti.get("command", "")
    else:
        return 0

    if ALLOW_MARKER in content:
        return 0
    # The guard's own pattern table would otherwise trip it.
    if path.endswith("guard-secrets.py"):
        return 0

    for pat, label in PATTERNS:
        if re.search(pat, content):
            print(
                f"Blocked: this looks like a {label}.\n\n"
                f"CREST must never hold secrets in the repo — issuance keys and rail\n"
                f"credentials are the two pieces of state whose loss is unrecoverable.\n"
                f"Use environment variables, and add the value to the deployment's secret\n"
                f"store instead.\n\n"
                f"If this is genuinely a placeholder or a test vector, add the comment\n"
                f"marker '{ALLOW_MARKER}' on the same line to record that judgement.",
                file=sys.stderr,
            )
            return 2

    if "/tests/" in path or "fixture" in path.lower():
        for pat, label in PII:
            if re.search(pat, content):
                print(
                    f"Blocked: {label} in a test fixture.\n\n"
                    f"Fixtures use generated values only — never real or 'anonymised' real\n"
                    f"personal data (docs/TESTING.md). W9 says CREST never over-collects\n"
                    f"identity; a fixture is not an exception to that.",
                    file=sys.stderr,
                )
                return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
