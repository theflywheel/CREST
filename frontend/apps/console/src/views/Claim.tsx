// Claiming an invitation (#123): the door for a person who is not a worker.
//
// Somebody with standing created a record — a person with a role, or an
// organisation that just registered — and minted a one-time code for it. This
// screen is where the holder of that code turns it into a session of their
// own: they sign in with their own identity provider, and the claim binds
// THAT login to the record. Nothing here asserts who they are; the binding is
// their own act, made with their own token, exactly as a first login is.
//
// Reachable with no session, like /onboard, because a person who cannot sign
// in yet is precisely who this route is for.
import { useParams } from "react-router-dom";
import { startEsignetLogin } from "@crest/api";
import { AppbarOnly } from "./SignIn";

const CKEY = "crest.console.claim";

// The code has to survive the redirect to the identity provider and back, and
// the return leg lands on a different route (#/auth). sessionStorage is the
// only place it can wait: it is this tab's, and it is gone when the tab is.
export function storeClaim(code: string) {
  try {
    sessionStorage.setItem(CKEY, code);
  } catch {
    /* a blocked sessionStorage costs the claim, not the sign-in */
  }
}
export function pendingClaim(): string | null {
  try {
    return sessionStorage.getItem(CKEY);
  } catch {
    return null;
  }
}
export function clearClaim() {
  try {
    sessionStorage.removeItem(CKEY);
  } catch {
    /* nothing to clear if it could never be written */
  }
}

// Each refusal the claim endpoint can return, said as a sentence a person can
// act on. A code that reads "409" tells its holder nothing about what to do
// next, and this is the screen where they have the least context of anyone.
export function claimRefusal(code: string | null, fallback: string): string {
  switch (code) {
    case "invitation_unknown":
      return "That invitation is not one this deployment knows. Check the link you were sent — a code is single-use and cannot be retyped from memory.";
    case "invitation_expired":
      return "That invitation has expired. Whoever invited you can mint a new one; the record they created for you is untouched.";
    case "invitation_claimed":
      return "That invitation was already claimed. If it was not you, tell whoever invited you — a claim is recorded, and they can see when it happened.";
    case "party_already_bound":
      return "The record this invitation names is already held by somebody's sign-in. An invitation binds an unheld record; it is never a way to take one over.";
    case "identifier_belongs_to_another_party":
      return "The identity you signed in with already belongs to a different record here. One login is one party, always — sign in as yourself, or ask for the two records to be looked at.";
    case "already_enrolled":
      return "You are already signed in as a party in this deployment, so there is nothing to claim. Sign out first if you meant to claim as somebody else.";
    default:
      return fallback;
  }
}

export function Claim() {
  const { code } = useParams();
  const go = () => {
    storeClaim(code || "");
    startEsignetLogin();
  };
  return (
    <AppbarOnly>
      <h1 className="scr-title">You were invited to CREST Console</h1>
      <p className="muted" style={{ maxWidth: 700 }}>
        Somebody created a record for you here and sent you this link. Claiming it binds the identity you sign in
        with to that record — nothing else. What you can do afterwards is whatever they granted you, read back from
        the registry, never chosen on this screen.
      </p>
      <div className="card" style={{ maxWidth: 700 }} data-panel="claim">
        <span className="eyebrow">The invitation</span>
        <p className="body-2" style={{ marginTop: 8 }}>
          This link works once, and it expires. Once claimed it stops working — for you and for anyone else who saw
          it. If it has already expired, whoever invited you can send another; the record itself is unaffected.
        </p>
        <div style={{ height: 10 }} />
        <button className="btn" id="claim-esignet" data-claim-continue onClick={go}>
          Continue with eSignet
        </button>
        <p className="muted" style={{ marginTop: 8 }}>
          Your national identity provider. CREST never sees a credential of yours, and stores no identifier of yours
          in the clear.
        </p>
      </div>
    </AppbarOnly>
  );
}
