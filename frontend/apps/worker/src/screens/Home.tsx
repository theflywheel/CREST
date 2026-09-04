import { Link } from "react-router-dom";
import { KV, OpenNote, Sidecar, Stat } from "@crest/ui";
import { useSession } from "../session";
import { useLoad } from "../App";
import { loadCreds, loadFace, loadWindows, money, short, whenFull } from "../data";
import { Ussd } from "./Login";

/* w1_8 — home, as the console's overview page */
export function Home() {
  const s = useSession();
  const data = useLoad(async () => {
    const [creds, wins, fr] = await Promise.all([loadCreds(s.me!), loadWindows(s.me!), loadFace()]);
    return { creds, wins, ...fr };
  });
  if (!data) return null;
  const open = data.wins.filter((w) => !w.exitRoute);
  const f = data.face;
  const rate = data.rate;
  return (
    <>
      <div className="pagehead">
        <div>
          <span className="eyebrow">{whenFull(Date.now())}</span>
          <div className="person-name">Grace</div>
        </div>
      </div>
      <div className="stats" style={{ maxWidth: 560 }}>
        <Stat n={data.creds.length} label="credentials held" />
        <Stat n={open.length} label={`open confirmation ${open.length === 1 ? "window" : "windows"}`} />
      </div>
      <div className="pane-cols">
        <div>
          {open.length ? (
            <div className="card" style={{ borderColor: "var(--warning)" }}>
              <span className="chip warn sm">Waiting for your say</span>
              <p className="body-2" style={{ marginTop: 6 }}>
                {open.length === 1 ? "A record of your work" : `${open.length} records of your work`} will count after
                you have had your say — or after seven days pass. You are paid either way.
              </p>
              <div style={{ height: 8 }} />
              <Link className="btn secondary" to="/work">
                See what was recorded
              </Link>
            </div>
          ) : null}
          {f ? (
            <div className="card hi">
              <span className="eyebrow">Available work</span>
              <div style={{ font: "500 15px/1.4 Roboto", marginTop: 4 }}>
                {(f.activity && f.activity.label) || f.activity || "Work"}
              </div>
              <p className="body-2">{(f.worker && f.worker.summary) || ""}</p>
              <KV
                rows={[
                  ["Counted in", f.outcomeUnit || ""],
                  [
                    "One unit pays",
                    rate
                      ? money(rate.ratePerOutcomeUnit.amountMinor, rate.ratePerOutcomeUnit.currency)
                      : "no rate set — recognised only. It still goes on your record as work you did, and a future employer can see it.",
                  ],
                  ["Definition", <span className="mono">{short(f.definitionId)} · v{f.version}</span>],
                ]}
              />
              <div style={{ height: 10 }} />
              <Link className="btn" to="/work">
                Open the campaign
              </Link>
            </div>
          ) : (
            <OpenNote>
              The definitions service did not answer, so the campaign card cannot be drawn. Nothing is shown in its
              place.
            </OpenNote>
          )}
        </div>
        <div>
          <Sidecar>
            Everything on this page is drawn from live records — the same records a verifier would see. Nothing here
            is a mock.
          </Sidecar>
          <Ussd />
        </div>
      </div>
    </>
  );
}
