#!/usr/bin/env python3
"""Extract a per-screen spec from the design reference.

Source of truth: docs/reference/CREST — Actor Journeys_17Aug.html — the
17 Aug actor-journeys walkthrough, a self-contained HTML file whose 143
`.role-step` frames are the design of record for every screen in the
journey apps. This script parses ALL of those frames and emits
docs/journey-spec.json: one entry per screen id, carrying everything a
fidelity test needs to assert per screen — title, level, layout idiom,
fields, callouts, buttons, step counter, rail, appbar identity — plus a
sha256 fingerprint of the frame's normalized visible text so drift in the
reference itself is detectable.

Two sources, one file. The 143 reference frames carry
`source: "reference"`. The J3 connective-tissue screens n1–n5
(docs/design/j3-connective-tissue/README.md) are OUR design, not the
reference's; they are transcribed in
tools/journey-trace/crest-design-screens.json and merged here with
`source: "crest-design"` so the fidelity gate asserts them identically
without ever attributing them to the reference.

Determinism contract: same input file → byte-identical output. All maps
are emitted with sorted keys, all lists in document order (which is
stable), floats never appear, and the JSON is written with a fixed
separator/indent style and a trailing newline. `make journey-spec`
regenerates and diffs; CI fails if the committed JSON is stale.

Stdlib only (html.parser). Phase 0 of the fidelity gate: extraction only,
no assertions — Phase 1's Playwright tests consume this JSON per screen id.

Usage: python3 tools/journey-trace/extract-spec.py [--check]
"""

import hashlib
import json
import re
import sys
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "docs" / "reference" / "CREST — Actor Journeys_17Aug.html"
OUTPUT = ROOT / "docs" / "journey-spec.json"
# Screens the reference does not draw but our own design does — the J3
# connective tissue n1–n5 (docs/design/j3-connective-tissue/README.md).
# They ride in the same file so the gate asserts them the same way, and they
# carry source="crest-design" so nobody can mistake our design for the
# reference's.
DESIGN_SOURCE = Path(__file__).resolve().parent / "crest-design-screens.json"

# ── A minimal DOM ───────────────────────────────────────────────────────────

VOID = {"br", "img", "input", "hr", "meta", "link", "i"}
# "i" is not void in HTML, but the reference uses <i></i> pairs only as
# decorative dots; treating stray unclosed ones leniently keeps the walk sane.


class Node:
    __slots__ = ("tag", "attrs", "children", "parent")

    def __init__(self, tag, attrs, parent):
        self.tag = tag
        self.attrs = dict(attrs)
        self.children = []  # Node or str
        self.parent = parent

    def attr(self, name):
        return self.attrs.get(name, "")

    def walk(self):
        yield self
        for c in self.children:
            if isinstance(c, Node):
                yield from c.walk()

    def text(self):
        """Normalized visible text: entities decoded, whitespace collapsed."""
        parts = []

        # Preserve DOM order. The old walk-then-own-children traversal emitted
        # a parent's text first and every nested <b>/<span> afterwards, turning
        # "the <b>highest</b> tier" into "the tier highest" in the generated
        # contract. That made literal parity impossible for correctly rendered
        # mixed-content prose.
        def visit(n):
            if n.tag == "svg":
                return
            for c in n.children:
                if isinstance(c, str):
                    parts.append(c)
                else:
                    visit(c)

        visit(self)
        return normspace(" ".join(parts))


def normspace(s):
    return re.sub(r"\s+", " ", s).strip()


class TreeBuilder(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.root = Node("#root", [], None)
        self.cur = self.root

    def handle_starttag(self, tag, attrs):
        node = Node(tag, attrs, self.cur)
        self.cur.children.append(node)
        if tag not in VOID:
            self.cur = node

    def handle_startendtag(self, tag, attrs):
        self.cur.children.append(Node(tag, attrs, self.cur))

    def handle_endtag(self, tag):
        n = self.cur
        while n is not None and n.tag != tag:
            n = n.parent
        if n is not None and n.parent is not None:
            self.cur = n.parent

    def handle_data(self, data):
        if data.strip():
            self.cur.children.append(data)


def parse_fragment(markup):
    tb = TreeBuilder()
    tb.feed(markup)
    return tb.root


# ── Frame extraction ────────────────────────────────────────────────────────

STEP_OPEN = '<div class="role-step" data-step="'


def split_frames(doc):
    """Return the raw markup of each .role-step frame, in document order."""
    starts = [m.start() for m in re.finditer(re.escape(STEP_OPEN), doc)]
    frames = []
    for k, i in enumerate(starts):
        j = starts[k + 1] if k + 1 < len(starts) else doc.find("</main", i)
        if j == -1:
            j = len(doc)
        frames.append(doc[i:j])
    return frames


def style(node):
    return node.attr("style")


def is_uppercase_label(node, size):
    st = style(node)
    return (
        node.tag == "div"
        and "text-transform:uppercase" in st
        and f"font:500 {size}px" in st
    )


def find_screen(root, sid):
    for n in root.walk():
        if n.attr("id") == f"scr-{sid}":
            return n
    return None


CONTROL_BORDERS = (
    "border:1px solid #D6D5D4",  # console form control
    "border:1px solid #505A5F",  # phone form control
    "border:1px solid #C84C0E",  # focused / code-entry control
)


def is_control(node):
    if node is None:
        return False
    st = style(node)
    if any(b in st for b in CONTROL_BORDERS):
        return True
    # A control group (e.g. code-entry boxes) wraps bordered children.
    return any(
        isinstance(c, Node) and any(b in style(c) for b in CONTROL_BORDERS)
        for c in node.children
    )


def extract_fields(screen):
    """Field = small uppercase label div, then a bordered control div,
    then optionally a small hint div — all siblings inside one wrapper.

    The console idiom sets labels at 11px; the phone idiom at 10.5px.
    A 10.5px uppercase div is only a field label when a control follows
    it — otherwise it is an eyebrow or a callout title, not a field."""
    fields = []
    for n in screen.walk():
        eleven = is_uppercase_label(n, 11)
        ten_half = is_uppercase_label(n, "10.5")
        if not (eleven or ten_half):
            continue
        parent = n.parent
        sibs = [c for c in parent.children if isinstance(c, Node)]
        idx = sibs.index(n)
        control = sibs[idx + 1] if idx + 1 < len(sibs) else None
        hint = sibs[idx + 2] if idx + 2 < len(sibs) else None
        if ten_half and not is_control(control):
            continue  # phone-idiom eyebrow, not a field
        field = {"label": n.text(), "hint": "", "kind": "static", "value": ""}
        if is_control(control):
            value = control.text()
            # A trailing ▾ marks the select idiom in this reference.
            if value.endswith("▾"):
                field["kind"] = "select"
                value = normspace(value[:-1])
            elif control.tag == "input" or "input" in [
                c.tag for c in control.walk()
            ]:
                field["kind"] = "input"
            else:
                field["kind"] = "text"
            field["value"] = value
        if (
            hint is not None
            and hint.tag == "div"
            and "font:400 11.5px" in style(hint)
        ):
            field["hint"] = hint.text()
        fields.append(field)
    return fields


def extract_callouts(screen):
    """Callout = bordered tinted box holding a 10px uppercase title div
    and a body div."""
    callouts = []
    for n in screen.walk():
        if not is_uppercase_label(n, 10):
            continue
        box = n.parent
        body = ""
        sibs = [c for c in box.children if isinstance(c, Node)]
        idx = sibs.index(n)
        if idx + 1 < len(sibs):
            body = sibs[idx + 1].text()
        callouts.append({"title": n.text(), "text": body})
    return callouts


def extract_buttons(screen):
    buttons = []
    for n in screen.walk():
        if n.tag != "button":
            continue
        st = style(n)
        if "background:#C84C0E" in st:
            role = "primary"
        elif "background:#fff" in st:
            role = "secondary"
        else:
            role = "other"
        buttons.append(
            {"label": n.text(), "role": role, "nav": n.attr("data-nav")}
        )
    return buttons


# Console frames: "Registration · 1 of 4" in a span.
# Phone frames: "Step 1 of 5" in a div.
COUNTER_RE = re.compile(r"^(.+ · \d+ of \d+|Step \d+ of \d+)$")


def extract_step(screen):
    counter = ""
    for n in screen.walk():
        if n.tag in ("span", "div"):
            own = normspace(
                " ".join(c for c in n.children if isinstance(c, str))
            )
            if COUNTER_RE.match(own):
                counter = own
                break
    rail = []
    for n in screen.walk():
        if n.tag == "nav":
            rail = [
                c.text()
                for c in n.children
                if isinstance(c, Node) and c.text()
            ]
            break
    return {"counter": counter, "rail": rail}


def extract_appbar(screen):
    for n in screen.walk():
        if n.tag == "div" and "background:#0B4B66" in style(n):
            return n.text()
    return ""


FONT_SIZE_RE = re.compile(r"font:\d00 (\d+(?:\.\d+)?)px")


def extract_title(screen):
    for n in screen.walk():
        if n.tag == "h3":
            return n.text()
    # A minority of frames (13) carry no <h3>; their title is the first
    # large-set text — a div or span at 16px or more.
    for n in screen.walk():
        m = FONT_SIZE_RE.search(style(n))
        if m and float(m.group(1)) >= 16:
            t = n.text()
            # Skip decorative glyphs (✓, ←) set large; a title has words.
            if len(t) >= 3 and re.search(r"[A-Za-z]", t):
                return t
    return ""


LEVEL_RE = re.compile(r"^(L[123]) · ")


def extract_level(aside):
    if aside is None:
        return ""
    for n in aside.walk():
        if n.tag == "span":
            m = LEVEL_RE.match(n.text())
            if m:
                return m.group(1)
    return ""


def extract_frame(markup):
    m = re.match(
        r'<div class="role-step" data-step="([^"]+)" data-role="([^"]+)"',
        markup,
    )
    sid, actor = m.group(1), m.group(2)
    root = parse_fragment(markup)

    stage = ""
    for n in root.walk():
        if n.attr("data-flowstep") == sid:
            stage = n.attr("data-stage")
            break

    frame_kind = ""
    for n in root.walk():
        if n.attr("data-frame"):
            frame_kind = n.attr("data-frame")
            break
    layout = "phone" if frame_kind == "phone" else "desktop"

    screen = find_screen(root, sid)
    aside = None
    for n in root.walk():
        if n.tag == "aside":
            aside = n
            break

    entry = {
        "journey": sid.split("_")[0],
        "source": "reference",
        "forbidden": [],
        "actor": actor,
        "stage": stage,
        "title": "",
        "level": extract_level(aside),
        "layout": layout,
        "frame": frame_kind,
        "appbar": "",
        "step": {"counter": "", "rail": []},
        "fields": [],
        "callouts": [],
        "buttons": [],
        "text_fingerprint": "",
    }
    if screen is None:
        return sid, entry, "no scr-%s div" % sid

    entry["title"] = extract_title(screen)
    entry["appbar"] = extract_appbar(screen)
    entry["step"] = extract_step(screen)
    entry["fields"] = extract_fields(screen)
    entry["callouts"] = extract_callouts(screen)
    entry["buttons"] = extract_buttons(screen)
    entry["text_fingerprint"] = hashlib.sha256(
        screen.text().encode("utf-8")
    ).hexdigest()
    return sid, entry, ""


# ── Main ────────────────────────────────────────────────────────────────────


DESIGN_KEYS = (
    "actor stage title level layout frame appbar step fields callouts "
    "buttons forbidden"
).split()


def design_screens(problems):
    """The crest-design screens, normalized into the same entry shape.

    Deterministic by construction: the input is a checked-in JSON file, the
    key set is fixed here, and nothing is inferred. A design screen has no
    reference frame, so its text_fingerprint is empty — drift is caught by
    review of crest-design-screens.json's own diff, not by a hash.
    """
    raw = json.loads(DESIGN_SOURCE.read_text(encoding="utf-8"))
    out = {}
    for sid, spec in raw["screens"].items():
        missing = [k for k in DESIGN_KEYS if k not in spec]
        if missing:
            problems.append(f"{sid}: design screen missing {missing}")
        entry = {
            "journey": sid,
            "source": "crest-design",
            "text_fingerprint": "",
        }
        for k in DESIGN_KEYS:
            entry[k] = spec.get(k, "")
        out[sid] = entry
    return out


def build():
    doc = SOURCE.read_text(encoding="utf-8")
    frames = split_frames(doc)
    screens = {}
    problems = []
    for markup in frames:
        sid, entry, problem = extract_frame(markup)
        screens[sid] = entry
        if problem:
            problems.append(f"{sid}: {problem}")

    for sid, entry in design_screens(problems).items():
        if sid in screens:
            problems.append(f"{sid}: design screen collides with a reference id")
        screens[sid] = entry

    spec = {
        "_generated": (
            "GENERATED by tools/journey-trace/extract-spec.py from "
            "docs/reference/CREST — Actor Journeys_17Aug.html plus the "
            "crest-design screens in "
            "tools/journey-trace/crest-design-screens.json. Each screen "
            "names which of the two it came from in its `source` field. "
            "Do not edit; run `make journey-spec`."
        ),
        "source": "docs/reference/CREST — Actor Journeys_17Aug.html",
        "designSource": "tools/journey-trace/crest-design-screens.json",
        "screens": screens,
    }
    out = json.dumps(
        spec, ensure_ascii=False, indent=2, sort_keys=True
    ) + "\n"
    return out, screens, problems


def main():
    check = "--check" in sys.argv[1:]
    out, screens, problems = build()

    no_fields = [s for s, e in screens.items() if not e["fields"]]
    no_title = [s for s, e in screens.items() if not e["title"]]
    print(
        f"journey-spec: {len(screens)} screens parsed, "
        f"{len(no_fields)} with zero fields, "
        f"{len(no_title)} with no title, {len(problems)} problems"
    )
    for p in problems:
        print(f"  problem: {p}", file=sys.stderr)

    if check:
        current = OUTPUT.read_text(encoding="utf-8") if OUTPUT.exists() else ""
        if current != out:
            print(
                f"STALE: {OUTPUT.relative_to(ROOT)} does not match the "
                "reference. Run `make journey-spec` and commit the result.",
                file=sys.stderr,
            )
            return 1
        print("journey-spec: committed JSON is current")
        return 0

    OUTPUT.write_text(out, encoding="utf-8")
    print(f"wrote {OUTPUT.relative_to(ROOT)}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
