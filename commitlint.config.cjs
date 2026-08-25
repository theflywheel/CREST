// Conventional Commits, with CREST's own scopes.
//
// The point is not tidiness. A commit message that names its scope makes the
// history answerable to questions like "what has ever touched payments?" —
// which, on a system whose records decide whether someone gets paid, is a
// question that gets asked in an incident rather than at leisure.
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'scope-enum': [
      2,
      'always',
      [
        // Services (Blueprint §13). 'services' is for a change that spans the
        // spine — the seven are meant to change one at a time, and needing this
        // scope is a mild signal that something crossed more boundaries than it
        // should have.
        'services',
        'registry',
        'parties', 'definitions', 'evidence', 'confirmation',
        'verification', 'payments', 'notify',
        // Shared code and contracts
        'pkg', 'schemas', 'adapters', 'apps', 'cmd',
        // Proving it works
        'harness', 'tests', 'spikes',
        // Everything around the code
        'infra', 'ci', 'docs', 'tools', 'deps', 'agents',
      ],
    ],
    // Findings are results, not obstacles (CLAUDE.md), so they get a type.
    'type-enum': [
      2,
      'always',
      [
        'feat', 'fix', 'docs', 'test', 'refactor', 'perf',
        'build', 'ci', 'chore', 'revert',
        'finding', // a design finding: reality disagreed with the blueprint
      ],
    ],
    'body-max-line-length': [1, 'always', 100],
    'subject-case': [0],
  },
};
