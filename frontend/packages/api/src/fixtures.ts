// The fixture world's stable ids (harness/seed.go) and the demo personas.
// TS port of apps/shared/fixtures.js; the PoC (apps/web) keeps its own copy.
export const FIX = {
  org: "did:crest:party:01JCREST000000000000000RGN",
  // The specifier: the party the fixture world records as the seeded
  // definition's AUTHOR, deliberately not its ratifier. The console's
  // definition-author persona signs in as this party so that author and
  // approver are two parties in fact and not only in the navigation —
  // otherwise every ratification the wizard produces would be refused as
  // self-ratified, and the separation of duties would be untested theatre.
  specifier: "did:crest:party:01JCREST00000000000000SPEC",
  supervisor: "did:crest:party:01JCREST00000000000000SPVR",
  custodian: "did:crest:party:01JCREST00000000000000CSTD",
  workerA: "did:crest:party:01JCREST00000000000000WRKA",
  project: "crest:context:01JCREST00000000000000PRJC",
  definition: "crest:definition:01JCREST00000000000000DEFN",
} as const;
