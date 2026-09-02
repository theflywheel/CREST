// Where the services are, decided at module load — the TS port of
// apps/shared/config.js + the defaults from apps/shared/api.js.
//
// Dev (compose stack on :59110, or a Vite dev server): each service on its
// own published localhost port, CORS-admitted via CREST_CORS_ORIGINS. Every
// other origin — the Railway deployment — reaches the services through the
// same-origin /api/ proxy, whose nginx refuses /internal/* so the
// service-only routes never face the internet (#102).

export type ServiceName =
  | "parties"
  | "definitions"
  | "evidence"
  | "confirmation"
  | "verification"
  | "payments"
  | "oidc";

declare global {
  interface Window {
    CREST_SERVICES?: Partial<Record<ServiceName, string>>;
    CREST_LINKS?: Partial<Record<LinkName, string>>;
  }
}

const host = location.hostname || "localhost";
const at = (port: number) => `http://${host}:${port}`;

// The four member names all answer from the one core service (#150);
// the payments application answers the window (#129).
const localPorts: Record<ServiceName, string> = {
  parties: at(59000),
  definitions: at(59000),
  evidence: at(59000),
  confirmation: at(59006),
  verification: at(59000),
  payments: at(59006),
  oidc: at(59103),
};

const proxyPaths: Record<ServiceName, string> = {
  parties: "/api/crest-registry",
  definitions: "/api/crest-definitions",
  evidence: "/api/crest-evidence",
  confirmation: "/api/crest-payments",
  verification: "/api/crest-verification",
  payments: "/api/crest-payments",
  oidc: "/api/crest-mock-oidc",
};

// :59110 is the compose nginx that serves the built apps; import.meta.env.DEV
// covers `vite dev`, whose port is arbitrary. Built behaviour off :59110 is
// unchanged: the same-origin proxy.
export const isLocalStack = location.port === "59110" || import.meta.env.DEV;

export const services: Record<ServiceName, string> = Object.assign(
  {},
  isLocalStack ? localPorts : proxyPaths,
  window.CREST_SERVICES || {},
);

// External companions of this deployment — separate origins, not services
// behind the /api proxy. The Inji browser wallet drives its own OpenID4VCI
// flow (Mimoto → eSignet → Certify); the doors only ever link to it.
// window.CREST_LINKS is the per-deployment override, same idea as
// CREST_SERVICES.
export type LinkName = "injiWeb" | "injiVerify";

const localLinks: Record<LinkName, string> = {
  injiWeb: `http://${host}:58093`,
  injiVerify: `http://${host}:58092`,
};

const deployedLinks: Record<LinkName, string> = {
  injiWeb: "https://crest-inji-web-production.up.railway.app",
  injiVerify: "https://crest-verify-ui-production.up.railway.app",
};

export const links: Record<LinkName, string> = Object.assign(
  {},
  isLocalStack ? localLinks : deployedLinks,
  window.CREST_LINKS || {},
);
