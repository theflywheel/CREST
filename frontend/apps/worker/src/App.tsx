// CREST worker door (journey J7, "A worker, end to end") as a desktop
// console: appbar + sidebar on wide screens, the familiar bottom-nav phone
// experience under 720px. Flows are the vanilla app's, ported 1:1.
import { useEffect, useState, type ReactNode } from "react";
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { ConsoleShell, ErrBar, type NavGroup, type NavItem } from "@crest/ui";
import { useSession } from "./session";
import { Login } from "./screens/Login";
import { Home } from "./screens/Home";
import { Work, Dispute, Declined } from "./screens/Work";
import { Wallet, Cred, CredShow, Deferred, Share } from "./screens/Wallet";
import { Pay, PayDetail } from "./screens/Pay";
import { Profile, Consents, Checks, Messages, Recovery } from "./screens/Profile";
import { SharesInbox, ShareDecide, ShareSent } from "./screens/Shares";
import { VouchInbox, VouchProgress, VouchRefused } from "./screens/Vouch";
import { Added } from "./screens/Added";
import { AuthReturn, Join } from "./screens/Auth";

const ic = (d: ReactNode) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
    {d}
  </svg>
);
const ICONS = {
  home: ic(
    <>
      <path d="M3 10.5 12 3l9 7.5" />
      <path d="M5.5 9.5V20h13V9.5" />
    </>,
  ),
  work: ic(
    <>
      <rect x="3.5" y="7" width="17" height="13" rx="2" />
      <path d="M8.5 7V5.5A1.5 1.5 0 0 1 10 4h4a1.5 1.5 0 0 1 1.5 1.5V7" />
    </>,
  ),
  wallet: ic(
    <>
      <rect x="3.5" y="6" width="17" height="13" rx="2" />
      <path d="M15 12.5h5.5" />
      <circle cx="15.5" cy="12.5" r=".4" />
    </>,
  ),
  pay: ic(
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5v9M9.5 9.8c0-1 1.1-1.8 2.5-1.8s2.5.8 2.5 1.8-1 1.6-2.5 2c-1.5.4-2.5 1-2.5 2s1.1 1.8 2.5 1.8 2.5-.8 2.5-1.8" />
    </>,
  ),
  profile: ic(
    <>
      <circle cx="12" cy="8.5" r="3.5" />
      <path d="M5 20c.8-3.4 3.6-5 7-5s6.2 1.6 7 5" />
    </>,
  ),
  shares: ic(
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M8.5 12h7M12 8.5v7" />
    </>,
  ),
  vouch: ic(
    <>
      <path d="M4 12.5 9 18 20 6" />
    </>,
  ),
};

const NAV: NavGroup[] = [
  { items: [{ to: "/home", label: "Home", icon: ICONS.home }] },
  {
    caption: "My work",
    items: [
      { to: "/work", label: "Work", icon: ICONS.work, end: true },
      { to: "/wallet", label: "Wallet", icon: ICONS.wallet, end: true },
      { to: "/pay", label: "My money", icon: ICONS.pay, end: true },
    ],
  },
  {
    caption: "Me",
    items: [
      { to: "/profile", label: "Profile", icon: ICONS.profile, end: true },
      { to: "/shares", label: "Requests to me", icon: ICONS.shares, end: true },
      { to: "/vouch", label: "Vouch for someone", icon: ICONS.vouch, end: true },
    ],
  },
];

const BOTTOM: NavItem[] = [
  { to: "/home", label: "Home", icon: ICONS.home },
  { to: "/work", label: "Work", icon: ICONS.work, end: true },
  { to: "/wallet", label: "Wallet", icon: ICONS.wallet },
  { to: "/profile", label: "Profile", icon: ICONS.profile },
];

function Shell() {
  const s = useSession();
  const loc = useLocation();
  // A navigation clears the last request's error, as the old router did.
  useEffect(() => s.clearErr(), [loc.pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!s.me) return <Login />;
  return (
    <ConsoleShell
      appName="CREST · Worker"
      who={
        <>
          <span className="who-label">{s.meLabel || "Signed in"}</span>
          <button onClick={s.logout} id="logout">
            Sign out
          </button>
        </>
      }
      nav={NAV}
      bottomNav={BOTTOM}
    >
      <div className="screen" key={loc.pathname}>
        {s.err ? <ErrBar>{s.err}</ErrBar> : null}
        <Outlet />
      </div>
    </ConsoleShell>
  );
}

export function App() {
  return (
    <Routes>
      {/* The eSignet return leg renders outside the shell: it runs before
          there is a session to gate on. */}
      <Route path="/auth" element={<AuthReturn />} />
      {/* The invite link a project shares: it names the programme and
          nothing else, and renders outside the shell for the same reason. */}
      <Route path="/join/:contextId" element={<Join />} />
      <Route element={<Shell />}>
        <Route path="/" element={<Navigate to="/home" replace />} />
        <Route path="/home" element={<Home />} />
        <Route path="/work" element={<Work />} />
        <Route path="/work/dispute/:claimId" element={<Dispute />} />
        <Route path="/work/declined" element={<Declined />} />
        <Route path="/wallet" element={<Wallet />} />
        <Route path="/wallet/share" element={<Share />} />
        <Route path="/wallet/deferred" element={<Deferred />} />
        <Route path="/wallet/:idx" element={<Cred />} />
        <Route path="/wallet/:idx/show" element={<CredShow />} />
        <Route path="/pay" element={<Pay />} />
        <Route path="/pay/:idx" element={<PayDetail />} />
        <Route path="/profile" element={<Profile />} />
        <Route path="/profile/consents" element={<Consents />} />
        <Route path="/profile/checks" element={<Checks />} />
        <Route path="/profile/messages" element={<Messages />} />
        <Route path="/profile/recovery" element={<Recovery />} />
        <Route path="/shares" element={<SharesInbox />} />
        <Route path="/shares/:id" element={<ShareDecide />} />
        <Route path="/shares/:id/sent" element={<ShareSent />} />
        <Route path="/vouch" element={<VouchInbox />} />
        <Route path="/vouch/:id" element={<VouchProgress />} />
        <Route path="/vouch/:id/refused" element={<VouchRefused />} />
        <Route path="/added" element={<Added />} />
        <Route path="*" element={<Navigate to="/home" replace />} />
      </Route>
    </Routes>
  );
}

// Tiny loader hook: soft data fetch, remounts per screen. Optional deps
// re-run the fetch when an input the fn closes over becomes known later —
// e.g. a party name looked up from a record that itself loads async.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []): T | undefined {
  const [data, setData] = useState<T>();
  useEffect(() => {
    let live = true;
    fn().then((d) => live && setData(d));
    return () => {
      live = false;
    };
  }, deps); // eslint-disable-line react-hooks/exhaustive-deps
  return data;
}
