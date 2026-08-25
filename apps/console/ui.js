// Shared rendering helpers for the CREST Console. Everything that reaches
// innerHTML goes through esc(); identifiers render in .mono; unbuilt things
// are labeled, never faked.

export const esc = x => String(x ?? "")
  .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");

export const short = id => {
  const s = String(id || "");
  return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-6) : s;
};

export const money = (minor, cur) =>
  (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2 }) + " " + (cur || "");

export const when = ts => ts
  ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
  : "—";

export const agoDays = ts => {
  if (!ts) return null;
  const d = Math.floor((Date.now() - new Date(ts).getTime()) / 86400000);
  return d < 0 ? 0 : d;
};

export const mono = id => `<span class="mono">${esc(id)}</span>`;
export const monoShort = id => `<span class="mono" title="${esc(id)}">${esc(short(id))}</span>`;

export const chip = (cls, text) => `<span class="chip ${cls}">${esc(text)}</span>`;

// The reference's own honesty labels, verbatim — used wherever a screen is
// drawn ahead of its backend.
export const ILLUSTRATIVE = chip("warn", "Illustrative, not a real API");
export const SIMULATED = chip("warn", "Simulated result");

export const openNote = html => `<div class="open-note">${html}</div>`;

export const stat = (n, label, ownerLine) => `<div class="stat">
  <div class="n">${n}</div>
  <div class="l">${esc(label)}${ownerLine ? `<br><span style="color:var(--p2);font-weight:500">${esc(ownerLine)}</span>` : ""}</div>
</div>`;

export const kvRows = pairs => `<div class="kv">${pairs
  .filter(p => p)
  .map(([k, v]) => `<div class="row"><span class="k">${esc(k)}</span><span class="v">${v}</span></div>`)
  .join("")}</div>`;

export const title = (t, extra) =>
  `<div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap"><div class="scr-title">${esc(t)}</div>${extra || ""}</div>`;

export const lede = t => `<p class="muted" style="max-width:68ch;font-size:13px">${t}</p>`;

export const empty = t => `<div class="card" style="color:var(--text-2)">${t}</div>`;

export const errbar = e =>
  `<div class="errbar">${esc(e && (e.status ? `${e.status} ${e.code || ""} — ${e.message}` : e.message || e))}</div>`;

// Vertical timeline step (.tline grammar from crest.css).
export const tstep = (state, label, meta, last) => `<div class="step ${state}">
  <div class="rail"><div class="dot"></div>${last ? "" : `<div class="conn"></div>`}</div>
  <div><div class="lbl">${esc(label)}</div><div class="meta">${meta || ""}</div></div>
</div>`;

// "What happens next" block.
export const next = rows => `<div class="next">
  <div class="eyebrow">What happens next</div>
  ${rows.map(([k, v]) => k === "ifnot"
    ? `<div class="ifnot">${v}</div>`
    : `<div class="nrow"><div class="k">${esc(k)}</div><div class="v">${v}</div></div>`).join("")}
</div>`;

export const sidecar = (txt, ok) => `<div class="sidecar${ok ? " ok" : ""}">
  <svg class="ico" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7"/><line x1="8" y1="7" x2="8" y2="11"/><line x1="8" y1="4.6" x2="8" y2="4.7"/></svg>
  <div class="txt">${txt}</div>
</div>`;

export const table = (heads, rows, emptyText) => rows.length
  ? `<div class="tblwrap"><table class="tbl">
      <tr>${heads.map(h => `<th>${esc(h)}</th>`).join("")}</tr>
      ${rows.map(r => `<tr>${r.map(c => `<td>${c}</td>`).join("")}</tr>`).join("")}
    </table></div>`
  : empty(emptyText || "Nothing here.");

export const card = (inner, hi) => `<div class="card${hi ? " hi" : ""}">${inner}</div>`;

export const cardTitled = (t, inner, chipHtml) =>
  `<div class="card"><div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
     <div style="font:500 14px/1.3 Roboto">${esc(t)}</div>${chipHtml || ""}</div>${inner}</div>`;

export const eyebrow = t => `<div class="eyebrow" style="margin-bottom:6px">${esc(t)}</div>`;

export const tierChip = t => chip("tier" + t, "Tier " + t);
