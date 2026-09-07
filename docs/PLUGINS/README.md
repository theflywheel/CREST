---
title: Plugins
---

# Plugins: what a deployment swaps

CREST integrates each outside system once per *class* and configures it per deployment. The core sees a small interface; the provider's own API stays behind it. Five plug points exist today. Each is chosen by configuration, never by a payload, and each has an honest stand-in for a stack with no real provider.

| Plug point | Interface | Shipped | Chosen by | Document |
|---|---|---|---|---|
| Evidence adapters | `adapters.Adapter` | `csv-batch@1` | The source's registration | [EVIDENCE_ADAPTERS.md](EVIDENCE_ADAPTERS.md) |
| Payment providers | `providers.Provider` | `http`, `simulator` | `PAYMENT_PROVIDER` | [PAYMENT_PROVIDERS.md](PAYMENT_PROVIDERS.md) |
| Identity providers | `identity.TokenVerifier` | eSignet, generic OIDC, the mock issuer; several at once | The OIDC issuer and JWKS settings | [IDENTITY_PROVIDERS.md](IDENTITY_PROVIDERS.md) |
| Registry substrate | `dedi.Publisher` | The DeDi node; an announced fallback | `DEDI_URL` and the publisher keys | [REGISTRY_SUBSTRATE.md](REGISTRY_SUBSTRATE.md) |
| Notification senders | `notify.Sender` | HTTP, SMTP | `NOTIFY_HTTP_URL` or the SMTP settings | [NOTIFICATION_SENDERS.md](NOTIFICATION_SENDERS.md) |

The credential substrate (Inji) is integrated the same way but is not swappable today; [CREDENTIAL_SUBSTRATE.md](CREDENTIAL_SUBSTRATE.md) says what it does and why there are two suites. How to add a plugin of any kind, and what counts as proven, is [ADDING_A_PLUGIN.md](ADDING_A_PLUGIN.md).

## The rule they share

**A plugin translates; it never decides trust.** An adapter stamps provenance from the deployment's configuration and ignores whatever the payload says about itself. A payment provider's HTTP failure can never establish that money moved. An identity provider yields a subject; whether that subject is a party is the registry's to say. A registry publisher that has no transparency log behind it says so at boot rather than pretending.
