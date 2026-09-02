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
