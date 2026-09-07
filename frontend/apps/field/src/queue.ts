import { useSyncExternalStore } from "react";

/** A registration held for later submission, bound to its real actor/project. */
export type Reg = {
  name: string;
  phone: string;
  rosterId: string;
  at: number;
  partyId?: string;
  method?: string;
  actorId: string;
  contextId: string;
  operationId: string;
  state?: "pending" | "submitted";
  rosterState?: "pending" | "done";
};

export class StorageError extends Error {
  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = "StorageError";
    if (cause !== undefined) (this as Error & { cause?: unknown }).cause = cause;
  }
}

const DB_NAME = "crest.field.offline.v1";
const DB_VERSION = 2;
const RECORDS = "registrations";
const COMPLETED = "completed";
const CONSENTS = "consents";
const META = "meta";
export const LEGACY_QUEUE_KEY = "crest.enrolment.queue";
export const LEGACY_DONE_KEY = "crest.enrolment.done";
type StoredRecord = { operationId: string; iv: ArrayBuffer; ciphertext: ArrayBuffer };
type MetaRecord = { key: string; value?: CryptoKey; iv?: ArrayBuffer; wrapped?: ArrayBuffer };
type LegacyReg = Pick<Reg, "name" | "phone" | "rosterId" | "at" | "partyId" | "method">;
type LegacyStatus = { pending: number; completed: number; invalid: boolean };

export type PendingConsent = {
  operationId: string;
  actorId: string;
  contextId: string;
  partyId: string;
  mimeType: string;
  audio: Blob;
};

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());
let qCache: Reg[] = [];
let dCache: Reg[] = [];
let consentCache = new Map<string, PendingConsent>();
let storageErr: StorageError | null = null;
let hydrated = false;
let dbPromise: Promise<IDBDatabase> | null = null;
let dataKey: CryptoKey | null = null;
let unlockPromise: Promise<void> | null = null;
let writeChain = Promise.resolve();
let storageSnapshot = { ready: false, error: null as StorageError | null };
let legacySnapshot: LegacyStatus = { pending: 0, completed: 0, invalid: false };
let sessionEpoch = 0;
let readyResolve: (() => void) | null = null;
let readyReject: ((error: unknown) => void) | null = null;
let ready = new Promise<void>((resolve, reject) => {
  readyResolve = resolve;
  readyReject = reject;
});

function failStorage(message: string, cause?: unknown): StorageError {
  const e = new StorageError(message, cause);
  storageErr = e;
  storageSnapshot = { ready: hydrated, error: e };
  readyReject?.(e);
  readyReject = null;
  notify();
  return e;
}

function secureCrypto(): Crypto {
  const c = globalThis.crypto;
  if (!c?.subtle) throw failStorage("Secure offline storage is unavailable on this device.");
  return c;
}

function readLegacy(storage: Storage, key: string): { rows: LegacyReg[] | null; raw: string | null } {
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    legacySnapshot = { ...legacySnapshot, invalid: true };
    return { rows: null, raw: null };
  }
  if (raw === null) return { rows: [], raw };
  try {
    const rows = JSON.parse(raw);
    if (!Array.isArray(rows) || rows.some((row) =>
      !row || typeof row !== "object" || typeof row.name !== "string" ||
      typeof row.phone !== "string" || typeof row.rosterId !== "string" ||
      typeof row.at !== "number" || !Number.isFinite(row.at) ||
      (row.partyId !== undefined && typeof row.partyId !== "string") ||
      (row.method !== undefined && typeof row.method !== "string"))) {
      throw new Error("invalid legacy registration list");
    }
    return { rows: rows as LegacyReg[], raw };
  } catch {
    legacySnapshot = { ...legacySnapshot, invalid: true };
    return { rows: null, raw };
  }
}

function scanLegacy(): {
  pending: LegacyReg[] | null;
  completed: LegacyReg[] | null;
  pendingRaw: string | null;
  completedRaw: string | null;
} {
  if (typeof localStorage === "undefined" || typeof sessionStorage === "undefined") {
    legacySnapshot = { pending: 0, completed: 0, invalid: false };
    return { pending: [], completed: [], pendingRaw: null, completedRaw: null };
  }
  legacySnapshot = { pending: 0, completed: 0, invalid: false };
  const pending = readLegacy(localStorage, LEGACY_QUEUE_KEY);
  const completed = readLegacy(sessionStorage, LEGACY_DONE_KEY);
  legacySnapshot = {
    pending: pending.rows?.length || 0,
    completed: completed.rows?.length || 0,
    invalid: legacySnapshot.invalid,
  };
  return {
    pending: pending.rows,
    completed: completed.rows,
    pendingRaw: pending.raw,
    completedRaw: completed.raw,
  };
}

function openDb(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  if (typeof indexedDB === "undefined") {
    dbPromise = Promise.reject(failStorage("This browser cannot provide durable offline storage."));
    return dbPromise;
  }
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onerror = () => reject(failStorage("Secure offline storage could not be opened.", req.error));
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(RECORDS)) db.createObjectStore(RECORDS, { keyPath: "operationId" });
      if (!db.objectStoreNames.contains(COMPLETED)) db.createObjectStore(COMPLETED, { keyPath: "operationId" });
      if (!db.objectStoreNames.contains(CONSENTS)) db.createObjectStore(CONSENTS, { keyPath: "operationId" });
      if (!db.objectStoreNames.contains(META)) db.createObjectStore(META, { keyPath: "key" });
    };
    req.onsuccess = () => resolve(req.result);
  });
  return dbPromise;
}

function request<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function wrappingKey(secret: string): Promise<CryptoKey> {
  const c = secureCrypto();
  if (!secret) throw failStorage("Sign in again to unlock this device's protected offline records.");
  const digest = await c.subtle.digest("SHA-256", new TextEncoder().encode(secret));
  return c.subtle.importKey("raw", digest, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
}

async function unlockDataKey(db: IDBDatabase, secret: string): Promise<CryptoKey> {
  const c = secureCrypto();
  const wrapKey = await wrappingKey(secret);
  const readTx = db.transaction(META, "readonly");
  const existing = (await request(readTx.objectStore(META).get("encryption-key"))) as MetaRecord | undefined;
  if (existing?.wrapped && existing.iv) {
    try {
      const raw = await c.subtle.decrypt({ name: "AES-GCM", iv: new Uint8Array(existing.iv) }, wrapKey, existing.wrapped);
      return c.subtle.importKey("raw", raw, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
    } catch (e) {
      throw failStorage("This signed-in session cannot unlock the protected offline records.", e);
    }
  }
  // Migrate the v1 device key once, after authentication. It was never
  // exposed through the queue API, and is immediately wrapped by this session
  // secret before the signed-out state can access the database again.
  if (existing?.value) {
    const iv = c.getRandomValues(new Uint8Array(12));
    let raw: ArrayBuffer;
    try {
      raw = await c.subtle.exportKey("raw", existing.value);
    } catch (e) {
      throw failStorage("An older offline queue cannot be protected for this signed-in session; export it before upgrading.", e);
    }
    const wrapped = await c.subtle.encrypt({ name: "AES-GCM", iv }, wrapKey, raw);
    const tx = db.transaction(META, "readwrite");
    tx.objectStore(META).put({ key: "encryption-key", iv: iv.buffer, wrapped } satisfies MetaRecord);
    await transactionDone(tx);
    return existing.value;
  }
  const generated = await c.subtle.generateKey({ name: "AES-GCM", length: 256 }, true, ["encrypt", "decrypt"]);
  const raw = await c.subtle.exportKey("raw", generated);
  const iv = c.getRandomValues(new Uint8Array(12));
  const wrapped = await c.subtle.encrypt({ name: "AES-GCM", iv }, wrapKey, raw);
  const tx = db.transaction(META, "readwrite");
  tx.objectStore(META).put({ key: "encryption-key", iv: iv.buffer, wrapped } satisfies MetaRecord);
  await transactionDone(tx);
  return c.subtle.importKey("raw", raw, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
}

function transactionDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}

async function encrypt(operationId: string, value: unknown, key: CryptoKey): Promise<StoredRecord> {
  const c = secureCrypto();
  const iv = c.getRandomValues(new Uint8Array(12));
  const ciphertext = await c.subtle.encrypt({ name: "AES-GCM", iv }, key, new TextEncoder().encode(JSON.stringify(value)));
  return { operationId, iv: iv.buffer, ciphertext };
}

async function legacyOperationId(actorId: string, contextId: string, store: string, index: number, row: LegacyReg): Promise<string> {
  const value = JSON.stringify([actorId, contextId, store, index, row.name, row.phone, row.rosterId, row.at, row.partyId || "", row.method || ""]);
  const digest = new Uint8Array(await secureCrypto().subtle.digest("SHA-256", new TextEncoder().encode(value)));
  return "field-legacy-" + Array.from(digest, (b) => b.toString(16).padStart(2, "0")).join("");
}

function bytesToBase64(bytes: Uint8Array): string {
  let out = "";
  for (let i = 0; i < bytes.length; i += 0x8000) out += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(out);
}

function base64ToBytes(value: string): Uint8Array {
  const raw = atob(value);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

async function decrypt<T>(row: StoredRecord, key: CryptoKey, message: string): Promise<T> {
  const c = secureCrypto();
  try {
    const plain = await c.subtle.decrypt({ name: "AES-GCM", iv: new Uint8Array(row.iv) }, key, row.ciphertext);
    return JSON.parse(new TextDecoder().decode(plain)) as T;
  } catch (e) {
    throw failStorage(message, e);
  }
}

async function readRows(db: IDBDatabase, key: CryptoKey): Promise<void> {
  const tx = db.transaction([RECORDS, COMPLETED, CONSENTS], "readonly");
  const [rows, completed, consents] = await Promise.all([
    request(tx.objectStore(RECORDS).getAll()) as Promise<StoredRecord[]>,
    request(tx.objectStore(COMPLETED).getAll()) as Promise<StoredRecord[]>,
    request(tx.objectStore(CONSENTS).getAll()) as Promise<StoredRecord[]>,
  ]);
  qCache = (await Promise.all(rows.map((row) => decrypt<Reg>(row, key, "A queued registration could not be decrypted; it was kept safely on this device.")))).sort((a, b) => b.at - a.at);
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  dCache = (await Promise.all(completed.map((row) => decrypt<Reg>(row, key, "A completed registration could not be decrypted; it was kept safely on this device."))))
    .filter((item) => item.at >= today.getTime())
    .sort((a, b) => b.at - a.at);
  const consentRows = await Promise.all(consents.map((row) => decrypt<Omit<PendingConsent, "audio"> & { audioBase64: string }>(row, key, "A queued consent recording could not be decrypted; it was kept safely on this device.")));
  consentCache = new Map(consentRows.map((row) => [row.operationId, {
    operationId: row.operationId,
    actorId: row.actorId,
    contextId: row.contextId,
    partyId: row.partyId,
    mimeType: row.mimeType,
    audio: new Blob([base64ToBytes(row.audioBase64)], { type: row.mimeType }),
  }]));
}

/** Unlocks encrypted records for the authenticated session only. */
export async function unlockQueue(secret: string): Promise<void> {
  if (dataKey && hydrated) return;
  if (unlockPromise) return unlockPromise;
  const epoch = sessionEpoch;
  unlockPromise = (async () => {
    try {
      const db = await openDb();
      dataKey = await unlockDataKey(db, secret);
      await readRows(db, dataKey);
      if (epoch !== sessionEpoch) {
        dataKey = null;
        qCache = [];
        dCache = [];
        return;
      }
      hydrated = true;
      storageErr = null;
      storageSnapshot = { ready: true, error: null };
      readyResolve?.();
      readyResolve = null;
      readyReject = null;
      notify();
    } catch (e) {
      dataKey = null;
      storageErr = e instanceof StorageError ? e : failStorage("Secure offline storage could not be unlocked.", e);
      storageSnapshot = { ready: false, error: storageErr };
      readyReject?.(storageErr);
      readyReject = null;
      notify();
      throw storageErr;
    } finally {
      unlockPromise = null;
    }
  })();
  return unlockPromise;
}

/** Clears plaintext caches and the key when the authenticated session ends. */
export function lockQueue(): void {
  sessionEpoch++;
  readyResolve?.();
  readyResolve = null;
  readyReject = null;
  dataKey = null;
  qCache = [];
  dCache = [];
  consentCache = new Map();
  hydrated = false;
  storageSnapshot = { ready: false, error: null };
  ready = new Promise<void>((resolve, reject) => {
    readyResolve = resolve;
    readyReject = reject;
  });
  notify();
}

export function queueReady(): Promise<void> {
  return ready;
}

export function queue(): Reg[] {
  return qCache.slice();
}

/** Imports the old plaintext device queue only after the user chooses its project. */
export async function migrateLegacyQueue(actorId: string, contextId: string): Promise<void> {
  const epoch = sessionEpoch;
  const key = dataKey;
  if (!key || !hydrated) throw failStorage("Sign in again to unlock this device's protected offline records.");
  const run = writeChain.catch(() => undefined).then(() => migrateLegacy(actorId, contextId, epoch, key));
  writeChain = run.catch(() => undefined);
  return run;
}

async function migrateLegacy(actorId: string, contextId: string, epoch: number, key: CryptoKey): Promise<void> {
  if (!actorId || !contextId) throw new Error("Choose the project these older registrations belong to before importing them.");
  if (epoch !== sessionEpoch || dataKey !== key) throw new Error("The signed-in session changed before the older records could be imported.");
  const legacy = scanLegacy();
  if (legacy.pending === null && legacy.completed === null) {
    notify();
    throw new Error("Older offline records could not be read and were left unchanged on this device.");
  }
  const legacyPending = legacy.pending || [];
  const legacyCompleted = legacy.completed || [];
  if (!legacyPending.length && !legacyCompleted.length) {
    notify();
    return;
  }
  const pending = await Promise.all(legacyPending.map(async (row, index): Promise<Reg> => ({
    ...row,
    actorId,
    contextId,
    operationId: await legacyOperationId(actorId, contextId, RECORDS, index, row),
    state: row.partyId ? "submitted" : "pending",
    rosterState: row.partyId && row.rosterId ? "done" : "pending",
  })));
  const completed = await Promise.all(legacyCompleted.map(async (row, index): Promise<Reg> => ({
    ...row,
    actorId,
    contextId,
    operationId: await legacyOperationId(actorId, contextId, COMPLETED, index, row),
    state: "submitted",
    rosterState: row.rosterId ? "done" : "pending",
  })));
  const mergedPending = [...new Map([...qCache, ...pending].map((row) => [row.operationId, row])).values()]
    .sort((a, b) => b.at - a.at);
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const mergedCompleted = [...new Map([...dCache, ...completed].map((row) => [row.operationId, row])).values()]
    .filter((row) => row.at >= today.getTime())
    .sort((a, b) => b.at - a.at);
  const [pendingRows, completedRows] = await Promise.all([
    Promise.all(pending.map((row) => encrypt(row.operationId, row, key))),
    Promise.all(completed.map((row) => encrypt(row.operationId, row, key))),
  ]);
  const db = await openDb();
  if (epoch !== sessionEpoch || dataKey !== key) throw new Error("The signed-in session changed before the older records could be imported.");
  const tx = db.transaction([RECORDS, COMPLETED], "readwrite");
  pendingRows.forEach((row) => tx.objectStore(RECORDS).put(row));
  completedRows.forEach((row) => tx.objectStore(COMPLETED).put(row));
  await transactionDone(tx);
  if (epoch !== sessionEpoch || dataKey !== key) {
    throw new Error("The signed-in session changed while the older records were being imported; their plaintext source was kept.");
  }
  qCache = mergedPending;
  dCache = mergedCompleted;
  if (legacy.pending !== null && legacy.pendingRaw !== null) {
    try {
      if (localStorage.getItem(LEGACY_QUEUE_KEY) === legacy.pendingRaw) localStorage.removeItem(LEGACY_QUEUE_KEY);
    } catch { /* encrypted commit remains durable; retry is idempotent */ }
  }
  if (legacy.completed !== null && legacy.completedRaw !== null) {
    try {
      if (sessionStorage.getItem(LEGACY_DONE_KEY) === legacy.completedRaw) sessionStorage.removeItem(LEGACY_DONE_KEY);
    } catch { /* keep the source when this store cannot be changed */ }
  }
  scanLegacy();
  notify();
}

async function writeStore(storeName: string, rows: StoredRecord[]): Promise<void> {
  if (!dataKey) throw failStorage("Sign in again to unlock this device's protected offline records.");
  const db = await openDb();
  const tx = db.transaction(storeName, "readwrite");
  const store = tx.objectStore(storeName);
  store.clear();
  rows.forEach((row) => store.put(row));
  await transactionDone(tx);
}

async function writeOne(storeName: string, row: StoredRecord): Promise<void> {
  if (!dataKey) throw failStorage("Sign in again to unlock this device's protected offline records.");
  const db = await openDb();
  const tx = db.transaction(storeName, "readwrite");
  tx.objectStore(storeName).put(row);
  await transactionDone(tx);
}

async function deleteOne(storeName: string, operationId: string): Promise<void> {
  if (!dataKey) throw failStorage("Sign in again to unlock this device's protected offline records.");
  const db = await openDb();
  const tx = db.transaction(storeName, "readwrite");
  tx.objectStore(storeName).delete(operationId);
  await transactionDone(tx);
}

export async function setQueue(next: Reg[]): Promise<void> {
  const run = writeChain.catch(() => undefined).then(async () => {
    try {
      await queueReady();
      if (!dataKey) throw new Error("queue is locked");
      const encrypted = await Promise.all(next.map((r) => encrypt(r.operationId, r, dataKey!)));
      await writeStore(RECORDS, encrypted);
      qCache = next.slice();
      storageErr = null;
      storageSnapshot = { ready: true, error: null };
      notify();
    } catch (e) {
      throw e instanceof StorageError ? e : failStorage("The registration could not be saved securely for later sync.", e);
    }
  });
  writeChain = run.catch(() => undefined);
  return run;
}

export function doneToday(): Reg[] {
  return dCache.slice();
}

export async function pushDone(r: Reg): Promise<void> {
  const run = writeChain.catch(() => undefined).then(async () => {
    try {
      await queueReady();
      if (!dataKey) throw new Error("queue is locked");
      const next = [r, ...dCache.filter((item) => item.operationId !== r.operationId)];
      const encrypted = await Promise.all(next.map((item) => encrypt(item.operationId, item, dataKey!)));
      await writeStore(COMPLETED, encrypted);
      dCache = next;
      storageErr = null;
      storageSnapshot = { ready: true, error: null };
      notify();
    } catch (e) {
      throw e instanceof StorageError ? e : failStorage("This device could not save today's completed registrations securely.", e);
    }
  });
  writeChain = run.catch(() => undefined);
  return run;
}

export function pendingConsent(operationId: string): PendingConsent | null {
  return consentCache.get(operationId) || null;
}

export async function saveConsentAudio(consent: PendingConsent): Promise<void> {
  const run = writeChain.catch(() => undefined).then(async () => {
    try {
      await queueReady();
      if (!dataKey) throw new Error("queue is locked");
      if (!consent.audio.size) throw new Error("the consent recording is empty");
      const audioBase64 = bytesToBase64(new Uint8Array(await consent.audio.arrayBuffer()));
      const row = await encrypt(consent.operationId, { ...consent, audio: undefined, audioBase64 }, dataKey!);
      await writeOne(CONSENTS, row);
      consentCache.set(consent.operationId, consent);
      storageErr = null;
      storageSnapshot = { ready: true, error: null };
      notify();
    } catch (e) {
      throw e instanceof StorageError ? e : failStorage("The consent recording could not be saved securely for resumption.", e);
    }
  });
  writeChain = run.catch(() => undefined);
  return run;
}

export async function removeConsentAudio(operationId: string): Promise<void> {
  const run = writeChain.catch(() => undefined).then(async () => {
    try {
      await queueReady();
      await deleteOne(CONSENTS, operationId);
      consentCache.delete(operationId);
      notify();
    } catch (e) {
      throw e instanceof StorageError ? e : failStorage("The completed consent recording could not be cleared securely.", e);
    }
  });
  writeChain = run.catch(() => undefined);
  return run;
}

export function usePendingConsent(operationId?: string): PendingConsent | null {
  return useSyncExternalStore(subscribe, () => (operationId ? consentCache.get(operationId) || null : null), () => null);
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function useQueue(): Reg[] {
  return useSyncExternalStore(subscribe, () => qCache, () => qCache);
}

export function useDone(): Reg[] {
  return useSyncExternalStore(subscribe, () => dCache, () => dCache);
}

export function useQueueStorage(): { ready: boolean; error: StorageError | null } {
  return useSyncExternalStore(subscribe, () => storageSnapshot, () => storageSnapshot);
}

export function useLegacyQueue(): LegacyStatus {
  return useSyncExternalStore(subscribe, () => legacySnapshot, () => legacySnapshot);
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

scanLegacy();
