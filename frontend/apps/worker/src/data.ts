// Data loaders and formatting helpers — ported from apps/worker/app.js.
// Loaders soft-fail (return []/null, never throw) exactly as before: every
// screen is backed by an endpoint that exists, or says plainly that none
// does. Nothing here invents live-looking data.
import { api } from "@crest/api";

export const short = (id: unknown) => {
  const s = String(id || "");
  return s.length > 22 ? s.slice(0, 12) + "…" + s.slice(-6) : s;
};
export const money = (minor: number, cur?: string) =>
  (minor / 100).toLocaleString(undefined, { minimumFractionDigits: 2 }) + " " + (cur || "");
export const when = (ts?: string | number | null) =>
  ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short" }) : "—";
export const whenFull = (ts?: string | number | null) =>
  ts ? new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "—";
export const daysOld = (ts?: string | null) =>
  ts ? Math.max(0, Math.floor((Date.now() - new Date(ts).getTime()) / 86400000)) : 0;
export const monthOf = (ts?: string | null) =>
  ts ? new Date(ts).toLocaleDateString(undefined, { month: "long", year: "numeric" }) : "Undated";

const soft = <T>(p: Promise<T>): Promise<T | null> => p.catch(() => null);

export async function loadCreds(me: string): Promise<any[]> {
  const out = await soft(api.get("verification", `/v1/parties/${encodeURIComponent(me)}/credentials`));
  return (out && out.credentials) || [];
}
export async function loadCredsStatus(me: string): Promise<{ credentials: any[]; unavailable: boolean }> {
  if (!me || !navigator.onLine) return { credentials: [], unavailable: true };
  try {
    const out = await api.get("verification", `/v1/parties/${encodeURIComponent(me)}/credentials`);
    return { credentials: (out && out.credentials) || [], unavailable: false };
  } catch {
    return { credentials: [], unavailable: true };
  }
}
export async function loadWindows(me: string): Promise<any[]> {
  const out = await soft(api.get("confirmation", `/v1/windows?partyId=${encodeURIComponent(me)}`));
  return (out && out.windows) || [];
}
export async function loadInstr(me: string): Promise<any[]> {
  const out = await soft(api.get("payments", `/v1/instructions?partyId=${encodeURIComponent(me)}`));
  return (out && out.instructions) || [];
}
// The worker's own definition, and what it says to them. Which definition
// that is comes from the worker's OWN confirmation windows — every window
// names the definition its unit was counted against — and, failing that, from
// the deployment's activated definitions when there is exactly one and no
// ambiguity to resolve. With neither, there is no face to draw and the screens
// say so rather than showing somebody else's programme.
export async function definitionFor(me: string): Promise<string | null> {
  const wins = await loadWindows(me);
  const fromWork = wins.map((w) => w && w.definitionId).find(Boolean);
  if (fromWork) return String(fromWork);
  const listed = await soft(api.get("definitions", "/v1/definitions?state=ACTIVE&limit=100"));
  const defs = (listed && listed.definitions) || [];
  return defs.length === 1 ? String(defs[0].id) : null;
}

export async function loadFace(me: string): Promise<{ face: any; rate: any }> {
  const definitionId = await definitionFor(me);
  if (!definitionId) return { face: null, rate: null };
  const face = await soft(api.get("definitions", `/v1/definitions/${encodeURIComponent(definitionId)}/faces/worker`));
  const lr = await soft(
    api.get("definitions", `/v1/definitions/${encodeURIComponent(definitionId)}/linked-records?type=payment-setup`),
  );
  const rate = (((lr && lr.linkedRecords) || [])[0] || {}).payload || null;
  return { face, rate };
}

// Tier, read defensively — trust strength is derived at query time, never
// stored; wherever the service surfaces it, we display, and where it does
// not, we say "tier not derivable here" rather than compute one clientside.
export function tierOf(c: any): { tier: string | null; captureMethod: string | null } {
  const t = c.derivedTier || c.trustTier || (c.trust && c.trust.tier) || (c.proof && c.proof.trustTier) || null;
  const cm =
    (c.credentialSubject && c.credentialSubject.provenance && c.credentialSubject.provenance.captureMethod) ||
    (c.provenance && c.provenance.captureMethod) ||
    (c.evidence && c.evidence.captureMethod) ||
    null;
  return { tier: t ? String(t).replace(/^tier[-_ ]?/i, "") : null, captureMethod: cm };
}
