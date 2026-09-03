// The CREST component grammar as React components. Rendered classnames are
// the design system's public vocabulary (styles.css) — several are pinned by
// tests/e2e-apps/apps.spec.js and must not be renamed.
import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";

export type NavItem = { to: string; label: string; icon?: ReactNode; end?: boolean };
export type NavGroup = { caption?: string; items: NavItem[] };

export function ConsoleShell(props: {
  appName: string;
  who?: ReactNode;
  nav: NavGroup[];
  bottomNav?: NavItem[]; // worker: replaces the chip rail below 720px
  children: ReactNode;
}) {
  return (
    <div className={"console-shell" + (props.bottomNav ? " with-bottomnav" : "")}>
      <div className="appbar">
        <span className="mark" />
        <span className="t">{props.appName}</span>
        <span className="who">{props.who}</span>
      </div>
      <div className="console-body">
        <nav className="sidebar">
          {props.nav.map((g, i) => (
            <div key={i} style={{ display: "contents" }}>
              {g.caption ? <span className="cap">{g.caption}</span> : null}
              {g.items.map((it) => (
                <NavLink key={it.to} to={it.to} end={it.end} className={({ isActive }) => (isActive ? "active" : "")}>
                  {it.icon}
                  {it.label}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>
        <main className="pane">{props.children}</main>
      </div>
      {props.bottomNav ? (
        <nav className="bottomnav">
          {props.bottomNav.map((it) => (
            <NavLink key={it.to} to={it.to} end={it.end} className={({ isActive }) => (isActive ? "active" : "")}>
              {it.icon}
              <span>{it.label}</span>
            </NavLink>
          ))}
        </nav>
      ) : null}
    </div>
  );
}

export function Chip(props: { kind?: string; sm?: boolean; children: ReactNode }) {
  return <span className={"chip" + (props.kind ? " " + props.kind : "") + (props.sm ? " sm" : "")}>{props.children}</span>;
}

export function KV(props: { rows: Array<[ReactNode, ReactNode]> }) {
  return (
    <div className="kv">
      {props.rows.map(([k, v], i) => (
        <div className="row" key={i}>
          <span className="k">{k}</span>
          <span className="v">{v}</span>
        </div>
      ))}
    </div>
  );
}

export function Stat(props: { n: ReactNode; label: ReactNode }) {
  return (
    <div className="stat">
      <div className="n">{props.n}</div>
      <div className="l">{props.label}</div>
    </div>
  );
}

const sideIco = (
  <svg className="ico" viewBox="0 0 15 15">
    <circle cx="7.5" cy="7.5" r="6.5" />
    <line x1="7.5" y1="7" x2="7.5" y2="11" />
    <line x1="7.5" y1="4.2" x2="7.5" y2="4.3" />
  </svg>
);

export function Sidecar(props: { ok?: boolean; children: ReactNode }) {
  return (
    <div className={"sidecar" + (props.ok ? " ok" : "")}>
      {sideIco}
      <span className="txt">{props.children}</span>
    </div>
  );
}

export function OpenNote(props: { children: ReactNode }) {
  return <div className="open-note">{props.children}</div>;
}

export function ErrBar(props: { children: ReactNode }) {
  return <div className="errbar">{props.children}</div>;
}

// The "What happens next" block — closes every terminal action (the
// journeys' rule: no action ends on a spinner or a bare toast).
export function NextBlock(props: {
  happened: ReactNode;
  who: ReactNode;
  when: ReactNode;
  told: ReactNode;
  ifnot: ReactNode;
}) {
  const rows: Array<[string, ReactNode]> = [
    ["What just happened", props.happened],
    ["Who acts next", props.who],
    ["When", props.when],
    ["How you will be told", props.told],
  ];
  return (
    <div className="next">
      <span className="eyebrow">What happens next</span>
      {rows.map(([k, v]) => (
        <div className="nrow" key={k}>
          <span className="k">{k}</span>
          <span className="v">{v}</span>
        </div>
      ))}
      <div className="ifnot">
        <b>If nothing happens ·</b> {props.ifnot}
      </div>
    </div>
  );
}

const tick = (
  <span className="tick">
    <svg viewBox="0 0 10 10" fill="none" stroke="#fff" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d="M1.5 5.5 4 8l4.5-6" />
    </svg>
  </span>
);

export function DisLi(props: { on: boolean; t: ReactNode; s: ReactNode }) {
  return (
    <div className={"li" + (props.on ? "" : " off")}>
      {props.on ? tick : <span className="tick" />}
      <span>
        <div className="t">{props.t}</div>
        <div className="s">{props.s}</div>
      </span>
    </div>
  );
}

// ————— the reference's callout grammar —————
// Two boxes carry every J3 frame's reasoning: teal (#F4F8FA / #0B4B66) for
// "the rule this screen sets", green (#F1FFF8 / #00703C) for "what this
// screen never does", plus the untitled-eyebrow grey variant the dashboard
// frames use for "How this could be gamed". Title is an uppercase eyebrow,
// body is the reference's own sentence — quoted verbatim by the callers.
export function Callout(props: { kind?: "teal" | "green" | "grey"; title?: ReactNode; children: ReactNode }) {
  return (
    <div className={"callout " + (props.kind || "teal")} data-callout={props.kind || "teal"}>
      {props.title ? <div className="c-title">{props.title}</div> : null}
      <div className="c-body">{props.children}</div>
    </div>
  );
}

// A reference field: uppercase small label over a bordered control/value, and
// an optional hint under it (journey-spec.json's `fields[].hint`).
export function RefField(props: { label: ReactNode; hint?: ReactNode; children?: ReactNode; value?: ReactNode }) {
  return (
    <div className="rfield">
      <span className="r-label">{props.label}</span>
      {props.children ? props.children : <div className="r-value">{props.value}</div>}
      {props.hint ? <span className="r-hint">{props.hint}</span> : null}
    </div>
  );
}

// A choice card — the reference's option idiom on every composition screen.
// `unavailable` renders the "not available" posture (p2_19's third option),
// which stays on screen precisely so nobody wonders whether it is hidden.
export function OptionCard(props: {
  t: ReactNode;
  s: ReactNode;
  on?: boolean;
  unavailable?: boolean;
  onPick?: () => void;
  tag?: ReactNode;
}) {
  const cls = "optcard" + (props.on ? " on" : "") + (props.unavailable ? " na" : "");
  const body = (
    <>
      <div className="o-head">
        <span className="o-t">{props.t}</span>
        {props.unavailable ? <span className="o-na">not available</span> : null}
        {props.tag}
      </div>
      <div className="o-s">{props.s}</div>
    </>
  );
  if (props.unavailable || !props.onPick) return <div className={cls}>{body}</div>;
  return (
    <button type="button" className={cls} aria-pressed={!!props.on} onClick={props.onPick}>
      {body}
    </button>
  );
}

// The reference's step counter, above the title on a wizard frame
// ("Project setup · 5 of 7").
export const StepCounter = (p: { children: ReactNode }) => (
  <span className="eyebrow" id="stepcounter">
    {p.children}
  </span>
);

// The reference's thin progress track above a wizard frame's title, with the
// step label ("Sector · 1 of 9", rendered by the caller's own StepCounter so
// its pinned .eyebrow classname is untouched) right-aligned beside it.
export function ProgressBar(props: { value: number; max: number; label?: ReactNode }) {
  const pct = props.max > 0 ? Math.max(0, Math.min(100, (props.value / props.max) * 100)) : 0;
  return (
    <div className="progressbar-row">
      <div className="progressbar" role="progressbar" aria-valuenow={props.value} aria-valuemin={0} aria-valuemax={props.max}>
        <div className="fill" style={{ width: pct + "%" }} />
      </div>
      {props.label}
    </div>
  );
}

// The reference's ~820px wizard content pane. Registry/list screens
// (#/definework and its siblings) legitimately use the full pane width and
// must not get this wrapper — only the Define wizard's own step screens do.
export function WizardContent(props: { children: ReactNode }) {
  return <div className="wizard-content">{props.children}</div>;
}

// Grid-row list — the reference's table idiom (no <table> anywhere in the
// Actor Journeys frames; tabular data is a grid row list with a header row).
export function GridTable(props: { cols: string; head?: ReactNode[]; children: ReactNode }) {
  return (
    <div className="gt-wrap">
      <div className="grid-tbl" style={{ ["--cols" as never]: props.cols }}>
        {props.head ? (
          <div className="g-row g-head">
            {props.head.map((h, i) => (
              <span key={i}>{h}</span>
            ))}
          </div>
        ) : null}
        {props.children}
      </div>
    </div>
  );
}
