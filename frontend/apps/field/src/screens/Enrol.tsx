// J6 (W-2): registering a worker who cannot self-register — w2_1..w2_5,
// ported 1:1 from apps/enrolment.
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "@crest/api";
import { Chip, Sidecar, OpenNote, NextBlock } from "@crest/ui";
import { useField } from "../state";
import { queue, setQueue, pushDone, useQueue, useDone, useOnline } from "../queue";

export function Registrations() {
  const s = useField();
  const nav = useNavigate();
  const q = useQueue();
  const done = useDone();
  const online = useOnline();
  const sync = async (i: number) => {
    const next = await s.syncQueued(i);
    if (next) nav("/" + next);
  };
  return (
    <>
      <div className="scr-title m">Today, through you</div>
      {q.length ? (
        <>
          <span className="eyebrow">Held on this device</span>
          {q.map((r, i) => (
            <div className="card quiet" key={r.at}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ font: "500 13.5px/1.3 Roboto" }}>{r.name}</span>
                <Chip sm kind="warn">held on this device</Chip>
              </div>
              <span className="muted">
                {r.phone ? "phone " + r.phone : "roster id " + (r.rosterId || "—")} · will sync when you have signal
              </span>
              {online ? (
                <div className="btn-row" style={{ marginTop: 8 }}>
                  <button className="btn secondary" data-sync={i} onClick={() => sync(i)}>
                    Sync now
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
            there is no "everyone Naomi ever registered" endpoint at L1, and the day's work is a device-local view on
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
    const reg: import("../queue").Reg = { name: name.trim(), phone: phone.trim(), rosterId: rosterId.trim(), at: Date.now() };
    if (!reg.phone && !reg.rosterId) {
      s.fail(new Error("give the pathway you have — a phone or a roster id; neither is a fallback for the other"));
      return;
    }
    s.setReg(reg);
    if (!online) {
      setQueue([reg, ...queue()]);
      nav("/registrations");
      return;
    }
    try {
      reg.partyId = await s.submitRegistration(reg);
      pushDone(reg);
      s.setReg({ ...reg });
      // The duplicate check: resolve by the contact route. 409 = a collision —
      // a hold exists, and the custodian owns it. The agent never sees the
      // other record.
      if (reg.phone) {
        try {
          await api.get("parties", `/v1/resolve?kind=contact-route&value=${encodeURIComponent(reg.phone)}`);
        } catch (e) {
          if (e instanceof ApiError && e.status === 409) {
            nav("/hold");
            return;
          }
          // 404 or anything else: not a collision — carry on to consent.
        }
      }
      nav("/consent");
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
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
        No national ID? We can still register them — the record is marked with how identity was established rather than
        being refused. Nothing on this form takes a raw ID number: CREST holds a pairwise reference and a salted hash,
        nothing else.
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
  const record = async () => {
    try {
      await s.recordConsent();
      nav("/registered");
    } catch (e) {
      s.fail(e);
    }
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
        “{first}, Crest will keep a record of your work — the work you do, and what it pays. You will be told each time
        your work is recorded, and you have seven days to say whether it is right. You can withdraw this consent at any
        time. Do you agree?”
      </div>
      <Sidecar>Recording captures the worker's answer, your agent ID and the time. This is the consent record.</Sidecar>
      <div className="btn-row">
        <button className="btn" id="recordbtn" onClick={record}>
          ● Record
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
          <strong>You cannot decide this — it goes to the registry custodian.</strong>
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
