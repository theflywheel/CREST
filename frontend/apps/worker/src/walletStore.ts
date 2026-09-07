const DB_NAME = "crest-worker-wallet";
const STORE_NAME = "encrypted-wallet";
const WALLET_KEY = "current";
const FORMAT = "crest-wallet-v1";
const ITERATIONS = 310000;

type Envelope = {
  format: string;
  kdf: "PBKDF2-SHA-256";
  iterations: number;
  cipher: "AES-GCM-256";
  salt: string;
  iv: string;
  ciphertext: string;
  count: number;
  createdAt: string;
};

const bytesToBase64 = (bytes: Uint8Array): string => {
  let out = "";
  for (let i = 0; i < bytes.length; i += 0x8000) out += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(out);
};
const base64ToBytes = (value: string): Uint8Array => {
  const raw = atob(value);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
};

function requirePassphrase(passphrase: string): void {
  if (passphrase.length < 8) throw new Error("Use a passphrase of at least 8 characters.");
}

async function keyFor(passphrase: string, salt: Uint8Array): Promise<CryptoKey> {
  requirePassphrase(passphrase);
  const material = await crypto.subtle.importKey("raw", new TextEncoder().encode(passphrase), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt, iterations: ITERATIONS, hash: "SHA-256" },
    material,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

async function seal(passphrase: string, credentials: unknown[]): Promise<Envelope> {
  requirePassphrase(passphrase);
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await keyFor(passphrase, salt);
  const plaintext = new TextEncoder().encode(JSON.stringify({ credentials }));
  const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, plaintext));
  return {
    format: FORMAT,
    kdf: "PBKDF2-SHA-256",
    iterations: ITERATIONS,
    cipher: "AES-GCM-256",
    salt: bytesToBase64(salt),
    iv: bytesToBase64(iv),
    ciphertext: bytesToBase64(ciphertext),
    count: credentials.length,
    createdAt: new Date().toISOString(),
  };
}

async function openEnvelope(passphrase: string, envelope: Envelope): Promise<any[]> {
  if (!envelope || envelope.format !== FORMAT || envelope.kdf !== "PBKDF2-SHA-256" || envelope.cipher !== "AES-GCM-256") {
    throw new Error("This is not a CREST encrypted wallet export.");
  }
  if (envelope.iterations !== ITERATIONS || !envelope.salt || !envelope.iv || !envelope.ciphertext) {
    throw new Error("The wallet export has an unsupported encryption configuration.");
  }
  const key = await keyFor(passphrase, base64ToBytes(envelope.salt));
  let plaintext: ArrayBuffer;
  try {
    plaintext = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: base64ToBytes(envelope.iv) },
      key,
      base64ToBytes(envelope.ciphertext),
    );
  } catch {
    throw new Error("That passphrase does not unlock this wallet.");
  }
  const decoded: unknown = JSON.parse(new TextDecoder().decode(plaintext));
  if (!decoded || typeof decoded !== "object" || !Array.isArray((decoded as { credentials?: unknown }).credentials)) {
    throw new Error("The wallet payload is invalid.");
  }
  return (decoded as { credentials: any[] }).credentials;
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1);
    request.onerror = () => reject(request.error || new Error("Could not open the wallet store."));
    request.onupgradeneeded = () => request.result.createObjectStore(STORE_NAME);
    request.onsuccess = () => resolve(request.result);
  });
}

async function putEnvelope(envelope: Envelope): Promise<void> {
  const db = await openDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readwrite");
    tx.objectStore(STORE_NAME).put(envelope, WALLET_KEY);
    tx.onerror = () => reject(tx.error || new Error("Could not save the wallet."));
    tx.oncomplete = () => resolve();
  });
  db.close();
}

async function getEnvelope(): Promise<Envelope | null> {
  const db = await openDB();
  const value = await new Promise<Envelope | undefined>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readonly");
    const request = tx.objectStore(STORE_NAME).get(WALLET_KEY);
    request.onerror = () => reject(request.error || new Error("Could not read the wallet."));
    request.onsuccess = () => resolve(request.result as Envelope | undefined);
  });
  db.close();
  return value || null;
}

export async function saveWallet(passphrase: string, credentials: unknown[]): Promise<void> {
  await putEnvelope(await seal(passphrase, credentials));
}

export async function loadWallet(passphrase: string): Promise<any[] | null> {
  const envelope = await getEnvelope();
  return envelope ? openEnvelope(passphrase, envelope) : null;
}

export async function exportWallet(passphrase: string, credentials: unknown[]): Promise<Blob> {
  return new Blob([JSON.stringify(await seal(passphrase, credentials), null, 2)], { type: "application/json" });
}

export async function importWallet(passphrase: string, file: File): Promise<any[]> {
  let envelope: Envelope;
  try {
    envelope = JSON.parse(await file.text()) as Envelope;
  } catch {
    throw new Error("The wallet export is not valid JSON.");
  }
  const credentials = await openEnvelope(passphrase, envelope);
  await putEnvelope(envelope);
  return credentials;
}
