// The offline queue, ported verbatim from apps/enrolment: same localStorage
// key, same record shape, same flush semantics — a registration made without
// signal is held on this device and synced when the network returns. The
// day's completed registrations live in sessionStorage, device-local on
// purpose (there is no "everyone Naomi ever registered" endpoint at L1).
import { useSyncExternalStore } from "react";

export type Reg = {
  name: string;
  phone: string;
  rosterId: string;
  at: number;
  partyId?: string;
};

const QKEY = "crest.enrolment.queue";
const DONEKEY = "crest.enrolment.done";

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());

export function queue(): Reg[] {
  try {
    return JSON.parse(localStorage.getItem(QKEY) || "[]");
  } catch {
    return [];
  }
}
export function setQueue(q: Reg[]) {
  try {
    localStorage.setItem(QKEY, JSON.stringify(q));
  } catch {
    /* a full or blocked store must not break registration */
  }
  notify();
}

export function doneToday(): Reg[] {
  try {
    return JSON.parse(sessionStorage.getItem(DONEKEY) || "[]");
  } catch {
    return [];
  }
}
export function pushDone(r: Reg) {
  try {
    sessionStorage.setItem(DONEKEY, JSON.stringify([r, ...doneToday()]));
  } catch {
    /* same */
  }
  notify();
}

let qCache = JSON.stringify(queue());
let dCache = JSON.stringify(doneToday());
function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function useQueue(): Reg[] {
  const raw = useSyncExternalStore(subscribe, () => {
    const now = JSON.stringify(queue());
    if (now !== qCache) qCache = now;
    return qCache;
  });
  return JSON.parse(raw);
}
export function useDone(): Reg[] {
  const raw = useSyncExternalStore(subscribe, () => {
    const now = JSON.stringify(doneToday());
    if (now !== dCache) dCache = now;
    return dCache;
  });
  return JSON.parse(raw);
}

export function useOnline(): boolean {
  return useSyncExternalStore(
    (fn) => {
      window.addEventListener("online", fn);
      window.addEventListener("offline", fn);
      return () => {
        window.removeEventListener("online", fn);
        window.removeEventListener("offline", fn);
      };
    },
    () => navigator.onLine,
  );
}
