// Shared rendering helpers for the console, ported from apps/console/ui.js
// as React components. Identifiers render in .mono; unbuilt things are
// labeled, never faked.
import { useEffect, useState, type ReactNode } from "react";
import { Chip } from "@crest/ui";

export const short = (id: unknown) => {
  const s = String(id || "");
  return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-6) : s;
};

export const money = (minor: number, cur?: string) =>
  (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2 }) + " " + (cur || "");

export const when = (ts?: string) =>
  ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "—";

export const agoDays = (ts?: string) => {
  if (!ts) return null;
  const d = Math.floor((Date.now() - new Date(ts).getTime()) / 86400000);
  return d < 0 ? 0 : d;
};

export const Mono = (p: { children: ReactNode }) => <span className="mono">{p.children}</span>;
export const MonoShort = (p: { id: unknown }) => (
  <span className="mono" title={String(p.id || "")}>
    {short(p.id)}
  </span>
);

// The reference's own honesty labels, verbatim.
export const ILLUSTRATIVE = <Chip kind="warn">Illustrative, not a real API</Chip>;
export const SIMULATED = <Chip kind="warn">Simulated result</Chip>;

export const TierChip = (p: { t: number }) => <Chip kind={"tier" + p.t}>Tier {p.t}</Chip>;

export function Stat(props: { n: ReactNode; label: ReactNode; owner?: ReactNode }) {
  return (
    <div className="stat">
      <div className="n">{props.n}</div>
      <div className="l">
        {props.label}
        {props.owner ? (
          <>
            <br />
            <span style={{ color: "var(--p2)", fontWeight: 500 }}>{props.owner}</span>
          </>
        ) : null}
      </div>
    </div>
  );
}

export function KVR(props: { rows: Array<[ReactNode, ReactNode] | false | null | undefined> }) {
  return (
    <div className="kv">
      {props.rows
        .filter((p): p is [ReactNode, ReactNode] => !!p)
        .map(([k, v], i) => (
          <div className="row" key={i}>
            <span className="k">{k}</span>
            <span className="v">{v}</span>
          </div>
        ))}
    </div>
  );
}

export function Title(props: { t: string; extra?: ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
      <div className="scr-title">{props.t}</div>
      {props.extra}
    </div>
  );
}

export const Lede = (p: { children: ReactNode }) => (
  <p className="muted" style={{ maxWidth: "68ch", fontSize: 13 }}>
    {p.children}
  </p>
);

export const Empty = (p: { children: ReactNode }) => (
  <div className="card" style={{ color: "var(--text-2)" }}>
    {p.children}
  </div>
);

export const Card = (p: { hi?: boolean; children: ReactNode }) => (
  <div className={"card" + (p.hi ? " hi" : "")}>{p.children}</div>
);

export function CardTitled(props: { t: string; chip?: ReactNode; children: ReactNode }) {
  return (
    <div className="card">
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
        <div style={{ font: "500 14px/1.3 Roboto" }}>{props.t}</div>
        {props.chip}
      </div>
      {props.children}
    </div>
  );
}

export function Tbl(props: { heads: ReactNode[]; rows: ReactNode[][]; empty?: ReactNode }) {
  if (!props.rows.length) return <Empty>{props.empty || "Nothing here."}</Empty>;
  return (
    <div className="tblwrap">
      <table className="tbl">
        <tbody>
          <tr>
            {props.heads.map((h, i) => (
              <th key={i}>{h}</th>
            ))}
          </tr>
          {props.rows.map((r, i) => (
            <tr key={i}>
              {r.map((c, j) => (
                <td key={j}>{c}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// Vertical timeline step (.tline grammar).
export function TStep(props: { state: string; label: ReactNode; meta: ReactNode; last?: boolean }) {
  return (
    <div className={"step " + props.state}>
      <div className="rail">
        <div className="dot" />
        {props.last ? null : <div className="conn" />}
      </div>
      <div>
        <div className="lbl">{props.label}</div>
        <div className="meta">{props.meta}</div>
      </div>
    </div>
  );
}

// Soft loader: renders "Loading…" until fn answers; `deps` re-runs it. A
// thrown error surfaces as the console's standard failure note.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []): { data?: T; err?: unknown } {
  const [out, setOut] = useState<{ data?: T; err?: unknown }>({});
  useEffect(() => {
    let live = true;
    setOut({});
    fn().then(
      (d) => live && setOut({ data: d }),
      (e) => live && setOut({ err: e }),
    );
    return () => {
      live = false;
    };
  }, deps); // eslint-disable-line react-hooks/exhaustive-deps
  return out;
}

export function LoadFrame<T>(props: { r: { data?: T; err?: unknown }; children: (d: T) => ReactNode }) {
  if (props.r.err)
    return (
      <>
        <div className="errbar">{String((props.r.err as Error)?.message || props.r.err)}</div>
        <div className="open-note">
          The service behind this screen did not answer. In dev, bring the stack up with{" "}
          <span className="mono">make e2e-up</span>; on a deployment, the health sweep in the Instance view names which
          service is down.
        </div>
      </>
    );
  if (props.r.data === undefined) return <div className="muted">Loading…</div>;
  return <>{props.children(props.r.data)}</>;
}
