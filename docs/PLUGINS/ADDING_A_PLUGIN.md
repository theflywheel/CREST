---
title: Adding a plugin
---

# Adding a plugin

1. **Implement the interface** in its own package: `adapters/<name>/`, `services/payments/providers/`, or the relevant `pkg/`.
2. **Register it in the one place** that lists implementations: `adapters/builtin.Plugins()`, `providers.NewCatalogue()`, or the identity configuration. There is no second list.
3. **Prove it.** An adapter runs the conformance suite in its own package test. A provider proves its request and response validation. A verifier proves it refuses a token from another issuer.
4. **Add the manifest row** in ../test-manifest.md (`docs/test-manifest.md` in the repository) in the same change. A plugin with no row is unproven, and the pre-commit hook says so.
5. **Say what it does not do.** A stand-in that looks like the real thing is the one failure a deployment cannot see; the simulator refuses to run outside a local environment for this reason.
6. **Name the rule it could break** in the pull request: trust derived not stored, unit and claim separable, every exit releases, never a raw identifier, holds never merge, every hold owned.

## What a plugin never does

- Choose itself from a payload. Selection is configuration.
- Assert trust. Provenance is stamped by the deployment; a payload's claims about itself are ignored.
- Establish an outcome it cannot prove. A failed call is a failed call.
- Hold personal data the layer would not. An adapter sees a raw identifier in transit and hands it to the pipeline, which hashes it before anything is stored.
