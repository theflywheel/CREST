// J6 (W-2): registering a worker who cannot self-register — w2_1..w2_5,
// ported 1:1 from apps/enrolment.
import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { actingFor, api, ApiError } from "@crest/api";
import { Chip, KV, Sidecar, OpenNote, NextBlock } from "@crest/ui";
import { useField, NO_PROJECT, short } from "../state";
import { migrateLegacyQueue, queue, queueReady, setQueue, useLegacyQueue, usePendingConsent, useQueue, useDone, useOnline, useQueueStorage } from "../queue";

function newOperationId(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
  throw new Error("This browser cannot create a secure offline operation identity");
}

function isIdempotencyError(e: unknown): boolean {
  return e instanceof ApiError && e.status === 409 && (e.code || "").startsWith("idempotency_");
}

export function Registrations() {
  const s = useField();
  const nav = useNavigate();
  const q = useQueue();
  const done = useDone();
  const online = useOnline();
  const storage = useQueueStorage();
  const legacy = useLegacyQueue();
  const [importing, setImporting] = useState(false);
  const sync = async (i: number) => {
    try {
      const next = await s.syncQueued(i);
      if (next) nav("/" + next);
    } catch (e) {
      s.fail(e);
    }
  };
  return (
    <>
      <div className="scr-title m">Today, through you</div>
      {storage.error ? <Sidecar warm>{storage.error.message}</Sidecar> : !storage.ready ? <Sidecar>Opening secure offline storage…</Sidecar> : null}
      {legacy.invalid ? (
        <Sidecar warm>Older offline registrations remain on this device, but this version cannot read them. They were not deleted.</Sidecar>
      ) : null}
      {legacy.pending + legacy.completed > 0 ? (
        <Sidecar warm>
          <span>
            This device has {legacy.pending} older registration(s) to sync and {legacy.completed} completed registration(s).
            Choose the project they came from, then import them into protected storage.
          </span>
          <button
            className="btn secondary"
            id="import-legacy-queue"
            disabled={importing || !storage.ready || !s.contextId}
            onClick={async () => {
              if (!s.me || !s.contextId) return;
              setImporting(true);
              try {
                await migrateLegacyQueue(s.me.partyId, s.contextId);
              } catch (e) {
                s.fail(e);
              } finally {
                setImporting(false);
              }
            }}
          >
            {importing ? "Importing…" : `Import older records into ${short(s.contextId || "")}`}
          </button>
        </Sidecar>
      ) : null}
      {/* w2_1's two counters, real: what this device did and what it holds. */}
      <div style={{ display: "flex", gap: 10 }}>
        <div className="card quiet" style={{ flex: 1 }}>
          <span style={{ font: "500 15px/1.3 Roboto" }}>{done.length}</span>
          <span className="muted" style={{ display: "block" }}>done today</span>
        </div>
        <div className="card quiet" style={{ flex: 1 }}>
          <span style={{ font: "500 15px/1.3 Roboto" }}>{q.length}</span>
          <span className="muted" style={{ display: "block" }}>to sync</span>
        </div>
      </div>
      {!online ? (
        <Sidecar warm>
          You are offline. Registrations are held on this device and sync when you have signal.
        </Sidecar>
      ) : null}
      {q.length ? (
        <>
          <span className="eyebrow">Held on this device</span>
          {q.map((r, i) => (
            <div className="card quiet" key={r.at}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ font: "500 13.5px/1.3 Roboto" }}>{r.name}</span>
                <Chip sm kind="warn">{r.state === "submitted" ? "consent to record" : "held on this device"}</Chip>
              </div>
              <span className="muted">
                {r.phone ? "phone " + r.phone : "roster id " + (r.rosterId || "—")} · actor {(r.actorId || "missing").slice(0, 10)}… · project {(r.contextId || "missing").slice(0, 10)}…
              </span>
                {online ? (
                  <div className="btn-row" style={{ marginTop: 8 }}>
                    <button className="btn secondary" data-sync={i} onClick={() => sync(i)}>
                      {r.state === "submitted" ? "Continue to consent" : "Sync now"}
                    </button>
                  </div>
              ) : null}
            </div>
          ))}
        </>
      ) : null}
      {done.length ? (
        <>
          <span className="eyebrow">Registered</span>
          {done.map((r) => (
            <div className="card" key={r.at}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ font: "500 13.5px/1.3 Roboto" }}>{r.name}</span>
                <Chip sm kind="ok">registered</Chip>
              </div>
              <span className="mono">{r.partyId}</span>
            </div>
          ))}
        </>
      ) : null}
      {!q.length && !done.length ? (
        <div className="card quiet">
          <span className="muted">
            Nothing yet today. The registry keeps the durable list; this screen shows what came through this device —
            there is no "everyone this agent ever registered" endpoint at L1, and the day's work is a device-local view on
            purpose.
          </span>
        </div>
      ) : null}
      <div className="btn-row">
        <button className="btn" onClick={() => nav("/register")}>
          New worker
        </button>
      </div>
    </>
  );
}

export function Register() {
  const s = useField();
  const nav = useNavigate();
  const online = useOnline();
  const [name, setName] = useState(s.reg?.name || "");
  const [phone, setPhone] = useState(s.reg?.phone || "");
  const [rosterId, setRosterId] = useState(s.reg?.rosterId || "");
  const [assertionRef, setAssertionRef] = useState("");
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!s.me || !s.contextId) {
      s.fail(new Error(!s.me ? "sign in before registering a worker" : NO_PROJECT));
      return;
    }
    let reg: import("../queue").Reg = {
      name: name.trim(),
      phone: phone.trim(),
      rosterId: rosterId.trim(),
      at: Date.now(),
      actorId: s.me.partyId,
      contextId: s.contextId,
      operationId: newOperationId(),
    };
    if (!reg.phone && !reg.rosterId) {
      s.fail(new Error("give the pathway you have — a phone or a roster id; neither is a fallback for the other"));
      return;
    }
    s.setReg(reg);
    if (!online) {
      try {
        await queueReady();
        await setQueue([reg, ...queue()]);
        nav("/registrations");
      } catch (e) {
        s.fail(e);
      }
      return;
    }
    try {
      // Persist the operation before sending it. If the browser crashes after
      // the server commits, the same idempotency key can resume the next step.
      await queueReady();
      await setQueue([reg, ...queue().filter((item) => item.operationId !== reg.operationId)]);
      reg.partyId = await s.submitRegistration(reg);
      await s.retainSubmitted(reg);
      if (reg.rosterId) {
        reg = await s.addRosterId(reg);
        await s.retainSubmitted(reg);
      }
      s.setReg({ ...reg });
      // The duplicate check: resolve by the contact route. A collision is a
      // hold owned by the custodian; idempotency conflicts stay errors and do
      // not discard the held operation.
      if (reg.phone) {
        try {
          await api.get("parties", `/v1/resolve?kind=contact-route&value=${encodeURIComponent(reg.phone)}`);
        } catch (e) {
          if (e instanceof ApiError && e.status === 409 && !isIdempotencyError(e)) {
            nav("/hold");
            return;
          }
          // 404 or anything else: not a collision — carry on to consent.
        }
      }
      nav("/consent");
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && !isIdempotencyError(e)) {
        nav("/hold");
        return;
      }
      s.fail(e);
    }
  };
  return (
    <>
      <div className="scr-title m">Register a worker who cannot self-register</div>
      <div className="card">
        <form id="regform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label className="body-2">
            Name
            <input name="name" required placeholder="Peter Njoroge" value={name} onChange={(e) => setName(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <span className="eyebrow">One of these — neither is a fallback</span>
          <label className="body-2">
            Phone
            <input name="phone" placeholder="+2547…" inputMode="tel" value={phone} onChange={(e) => setPhone(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Roster id (this programme's roster)
            <input name="rosterId" placeholder="CHW-2026-0114" value={rosterId} onChange={(e) => setRosterId(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
          </label>
          <label className="body-2">
            Assertion reference (optional)
            <input
              name="assertionRef"
              placeholder="a reference to an identity assertion — never the ID number itself"
              value={assertionRef}
              onChange={(e) => setAssertionRef(e.target.value)}
              style={{ width: "100%", marginTop: 4 }}
            />
          </label>
          <button className="btn" type="submit">
            Register
          </button>
        </form>
      </div>
      <Sidecar>
        No ID document? Switch to the confidence check — the worker is still registered, and the record is marked with
        how identity was established rather than being refused. Nothing on this form takes a raw ID number: CREST holds
        a pairwise reference and a salted hash, nothing else.{" "}
        <button
          type="button"
          id="to-confidence"
          className="btn secondary"
          style={{ marginTop: 6 }}
          onClick={() => nav("/confidence")}
        >
          No document? A confidence check instead
        </button>
      </Sidecar>
      <OpenNote>
        The assertion-reference field is illustrative — the L1 enrolment endpoint takes contact routes and a roster id
        today; identity assertions bind later through identity-bindings without losing anything already earned.
      </OpenNote>
    </>
  );
}

export function Consent() {
  const s = useField();
  const nav = useNavigate();
  const name = s.reg?.name || "the worker";
  const first = String(name).split(" ")[0];
  const saved = usePendingConsent(s.reg?.operationId);
  const [phase, setPhase] = useState<"idle" | "recording" | "uploading">("idle");
  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const chunks = useRef<Blob[]>([]);

  const start = async () => {
    if (phase !== "idle") return;
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      s.fail(new Error("This browser cannot record microphone audio. Use a secure, permission-enabled device."));
      return;
    }
    try {
      const input = await navigator.mediaDevices.getUserMedia({ audio: true });
      const candidates = ["audio/ogg;codecs=opus", "audio/webm;codecs=opus", "audio/wav"];
      const mime = candidates.find((candidate) => MediaRecorder.isTypeSupported(candidate));
      const r = new MediaRecorder(input, mime ? { mimeType: mime } : undefined);
      chunks.current = [];
      stream.current = input;
      recorder.current = r;
      r.ondataavailable = (event) => {
        if (event.data.size) chunks.current.push(event.data);
      };
      r.onerror = () => {
        input.getTracks().forEach((track) => track.stop());
        recorder.current = null;
        stream.current = null;
        setPhase("idle");
        s.fail(new Error("The microphone recording failed before it could be saved."));
      };
      r.onstop = () => {
        const audio = new Blob(chunks.current, { type: r.mimeType || mime || "audio/ogg" });
        input.getTracks().forEach((track) => track.stop());
        recorder.current = null;
        stream.current = null;
        setPhase("uploading");
        void s.recordConsent(audio).then(() => nav("/registered")).catch((e) => {
          setPhase("idle");
          s.fail(e);
        });
      };
      r.start();
      setPhase("recording");
    } catch (e) {
      stream.current?.getTracks().forEach((track) => track.stop());
      stream.current = null;
      s.fail(e instanceof DOMException && e.name === "NotAllowedError" ? new Error("Microphone permission was declined; consent was not recorded.") : e);
    }
  };

  const stop = () => {
    if (recorder.current && recorder.current.state !== "inactive") recorder.current.stop();
  };

  const record = () => {
    if (phase === "recording") stop();
    else void start();
  };
  const resumeSaved = () => {
    if (!saved || phase !== "idle") return;
    setPhase("uploading");
    void s.recordConsent(saved.audio).then(() => nav("/registered")).catch((e) => {
      setPhase("idle");
      s.fail(e);
    });
  };
  return (
    <>
      <div className="scr-title m">Read this to {first}</div>
      <div style={{ display: "flex", gap: 8 }}>
        <Chip kind="info">Kiswahili</Chip>
        <Chip kind="info">Read aloud</Chip>
      </div>
      <div className="consent-quote">
        “{first}, Crest itaweka rekodi ya kazi yako — kazi unayofanya, na malipo yake. Utaambiwa kila mara kazi yako
        inaporekodiwa, na una siku saba za kusema kama ni sahihi. Unaweza kuondoa idhini hii wakati wowote. Je,
        unakubali?”
      </div>
      <div className="body-2" style={{ color: "var(--text-2)" }}>
        “{first}, Crest will keep a record of your work — the work you do, and what it pays. It belongs to you, and
        nobody can see it unless you agree, each time. You will be told each time your work is recorded, and you have
        seven days to say whether it is right. You can ask us to stop at any time. Do you agree?”
      </div>
      <Sidecar>Recording captures the worker's answer, your agent ID and the time. This is the consent record.</Sidecar>
      {saved ? (
        <Sidecar warm>
          A recording from this consent is safely held on this device after an earlier upload interruption.
          <button className="btn secondary" style={{ marginTop: 8 }} onClick={resumeSaved} disabled={phase !== "idle"}>
            Resume saved recording
          </button>
        </Sidecar>
      ) : null}
      <div className="btn-row">
        <button className="btn" id="recordbtn" onClick={record} disabled={phase === "uploading"}>
          {phase === "recording" ? "■ Stop and save" : phase === "uploading" ? "Saving recording…" : "● Start recording"}
        </button>
        <button className="btn secondary" disabled title="Only Kiswahili and English scripts exist on this deployment yet">
          Change language
        </button>
      </div>
    </>
  );
}

export function Hold() {
  const s = useField();
  const nav = useNavigate();
  return (
    <>
      <div className="scr-title m">This may already be a Crest worker</div>
      <div className="card hi">
        <p className="body-2">
          The registry found more than one possible match for {s.reg?.name || "this worker"}.{" "}
          <strong>
            You cannot merge or reject this. Only the Worker Registry Custodian (Worker Registry Custodian) can, and
            that authority sits with the deployment operator.
          </strong>
        </p>
        <p className="muted" style={{ marginTop: 6 }}>
          What you see here is that a hold exists — not whose records collided, and not on what. Probable matches hold;
          they never merge without the worker's own confirmation.
        </p>
      </div>
      <NextBlock
        happened="A duplicate hold was raised in the registry. Nothing was merged and nothing was guessed."
        who="The registry custodian, from the duplicates queue."
        when="At the next queue review — typically within days."
        told="The registration completes or is joined once the custodian closes the hold; the worker keeps everything either way."
        ifnot="if the custodian finds two distinct people, both records stand — sharing an identifier is not being the same person."
      />
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav("/registrations")}>
          Back to the day's list
        </button>
      </div>
    </>
  );
}

export function Registered() {
  const s = useField();
  const nav = useNavigate();
  return (
    <>
      <div className="card hi" style={{ textAlign: "center", display: "flex", flexDirection: "column", gap: 8, padding: "22px 14px" }}>
        <span className="person-name">{s.reg?.name || ""}</span>
        <Chip kind="ok">registered</Chip>
        <span className="eyebrow">Crest ID</span>
        <span className="mono" style={{ wordBreak: "break-all" }}>
          {s.reg?.partyId || ""}
        </span>
      </div>
      <div className="card quiet">
        <span className="body-2">
          A card is printed on the spot when the first credential is issued — at enrolment there is no work history to
          print.
        </span>{" "}
        <Chip sm kind="plain">Illustrative — no printer on this device</Chip>
      </div>
      {!s.reg?.phone ? (
        <Sidecar warm>
          This worker has no phone. The printed card is their only way to be found on the next campaign — and losing it
          loses nothing: the record is in the registry, and a recovery contact can vouch them back in.
        </Sidecar>
      ) : null}
      <NextBlock
        happened="The worker exists in the registry, with voice consent on record, enrolled by you."
        who="The programme — evidence of their work arrives from its systems; nobody types work into Crest."
        when="From the next roster close onward."
        told="Each recorded claim opens a seven-day window; a worker with no phone is told through you."
        ifnot="if no evidence ever arrives, the registration still stands — existing in Crest does not depend on a document, a phone, or work already logged."
      />
      <div className="btn-row">
        <button className="btn secondary" onClick={() => nav("/registrations")}>
          Day's list
        </button>
        <button className="btn" onClick={() => nav("/register")}>
          Next worker
        </button>
      </div>
    </>
  );
}

/* w1_4 — "We can still register you": the no-document confidence check.
   The agent establishes who the person is by structured questioning and
   community knowledge; what is RECORDED is the method — a provenance fact on
   the enrolment (method=confidence-check) — never a stored tier or level.
   Assurance stays derived from identity bindings, so this worker reads IA-0
   until a route is verified or an anchor is bound, and upgrades later with
   nothing rewritten. */
export function Confidence() {
  const s = useField();
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [rosterId, setRosterId] = useState("");
  const [partyId, setPartyId] = useState<string | null>(null);
  // Prefilled with the signed-in agent: on w1_4 the agent themselves is the
  // route the worker most often names. It is an id the agent can overwrite,
  // never a fixture.
  const [contact, setContact] = useState<string>(s.me?.partyId || "");
  const [nominated, setNominated] = useState<string | null>(null);

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!s.me || !s.contextId) {
      s.fail(new Error(!s.me ? "sign in before registering a worker" : NO_PROJECT));
      return;
    }
    let reg: import("../queue").Reg = {
      name: name.trim(), phone: phone.trim(), rosterId: rosterId.trim(),
      at: Date.now(), method: "confidence-check", actorId: s.me.partyId, contextId: s.contextId, operationId: newOperationId(),
    };
    if (!reg.phone && !reg.rosterId) {
      s.fail(new Error("give the pathway you have — a phone or a roster id; neither is a fallback for the other"));
      return;
    }
    try {
      await queueReady();
      await setQueue([reg, ...queue().filter((item) => item.operationId !== reg.operationId)]);
      reg.partyId = await s.submitRegistration(reg);
      await s.retainSubmitted(reg);
      if (reg.rosterId) {
        reg = await s.addRosterId(reg);
        await s.retainSubmitted(reg);
      }
      s.setReg(reg);
      setPartyId(reg.partyId || null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && !isIdempotencyError(e)) {
        nav("/hold");
        return;
      }
      s.fail(e);
    }
  };

  // w1_4's secondary button. The agent writes the worker's STATED choice —
  // acting for the party, exactly as consent capture does. Party-linked,
  // never a phone number.
  const nominate = async () => {
    if (!partyId) return;
    if (!s.contextId) {
      s.fail(new Error(NO_PROJECT));
      return;
    }
    try {
      actingFor(partyId);
      // contextId names the programme this assisted act happens under — the
      // agent's act-for-party grant is context-scoped, exactly like consent.
      await api.post(
        "parties",
        `/v1/parties/${encodeURIComponent(partyId)}/recovery-contacts?contextId=${encodeURIComponent(s.contextId)}`,
        { contactPartyId: contact.trim() },
      );
      setNominated(contact.trim());
    } catch (e) {
      s.fail(e);
    } finally {
      actingFor(null);
    }
  };

  return (
    <>
      <span className="eyebrow">Step 2 of 5 · assisted enrolment</span>
      <div className="scr-title m">We can still register you</div>
      <p className="body-2">
        Their name, a reachable route and your structured questioning together give enough confidence to create the
        record. No document is seen, and none is required.
      </p>
      {!partyId ? (
        <div className="card">
          <form id="confidenceform" onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <label className="body-2">
              Name, as the community knows them
              <input name="name" required value={name} onChange={(e) => setName(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
            </label>
            <label className="body-2">
              Phone (if they have one — recorded as an unverified route)
              <input name="phone" inputMode="tel" value={phone} onChange={(e) => setPhone(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
            </label>
            <label className="body-2">
              Roster id (this programme's roster)
              <input name="rosterId" value={rosterId} onChange={(e) => setRosterId(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
            </label>
            <button className="btn" type="submit">
              Register with a confidence check
            </button>
          </form>
        </div>
      ) : (
        <div className="card hi" data-confidence-done={partyId}>
          <Chip kind="ok">registered · method: confidence-check</Chip>
          <span className="mono" style={{ wordBreak: "break-all", display: "block", marginTop: 8 }}>{partyId}</span>
        </div>
      )}
      <KV
        rows={[
          ["Name match", partyId ? "taken as given, by you — recorded with your agent identity" : "what you establish by questioning"],
          ["Phone number", phone ? "a route on the record — unverified until an OTP or a call proves it" : "not provided"],
          ["Face match", "not provided — no biometric is taken, ever"],
          ["Confidence", "sufficient to register — your judgement, recorded as the enrolment method, never as a stored level"],
        ]}
      />
      <Sidecar>
        Their record will show that identity was established without a document. This does not limit what work they can
        do or be paid for.
      </Sidecar>
      <OpenNote>
        Honest about strength: with no document and no verified route, this worker's identity assurance derives to{" "}
        <b>IA-0</b> — derived from their identity bindings at every read, never stored. The moment a route is verified
        or an anchor is bound, everything already earned re-derives stronger, with nothing rewritten.
      </OpenNote>
      {partyId && !nominated ? (
        <div className="card">
          <span className="eyebrow">Add a recovery contact now</span>
          <p className="muted">
            The worker names a person they trust — you write their stated choice, acting for them. A person on the
            registry, never a phone number.
          </p>
          <div style={{ display: "flex", gap: 8, marginTop: 6 }}>
            <input name="recoverycontact" value={contact} onChange={(e) => setContact(e.target.value)} style={{ flex: 1 }} />
            <button className="btn secondary" id="confidence-nominate" onClick={nominate}>
              Nominate
            </button>
          </div>
        </div>
      ) : null}
      {nominated ? (
        <div className="card quiet" data-nominated={nominated}>
          <Chip sm kind="ok">recovery contact nominated</Chip>{" "}
          <span className="mono">{nominated}</span>
        </div>
      ) : null}
      <div className="btn-row">
        <button className="btn secondary" id="confidence-recovery" disabled={!partyId} onClick={() => {
          const el = document.querySelector('[name="recoverycontact"]') as HTMLInputElement | null;
          el?.focus();
        }}>
          Add a recovery contact now
        </button>
        <button className="btn" id="confidence-continue" disabled={!partyId} onClick={() => nav("/consent")}>
          Continue
        </button>
      </div>
      {!partyId ? (
        <p className="muted">Both buttons open after the registration exists — nothing is nominated or consented for a record that is not there yet.</p>
      ) : null}
    </>
  );
}
