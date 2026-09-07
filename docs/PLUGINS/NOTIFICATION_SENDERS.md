---
title: Notification senders
---

# Notification senders

```go
type Sender interface {
    Send(context.Context, Message) (Result, error)
}
```

Which moments produce a message and which facts each carries are infrastructure; the record that a delivery was attempted is too. The wording is deployment configuration, one set per deployment, with two rules that survive translation enforced at start-up: the confirmation message must contain the deployment's own "you will be paid either way" sentence, because a message implying payment depends on replying puts the worker under duress; and the reply keywords live in the same configuration as the prompt that prints them, so an inbound channel can never understand a different word than the worker was told to send.

## Shipped senders

| Sender | Configuration | Today |
|---|---|---|
| HTTP | `NOTIFY_HTTP_URL`, `NOTIFY_HTTP_TOKEN` | An authenticated inbox; `mock-notify` stands in locally and the review acknowledgement link is delivered through it |
| SMTP | address, from, username, password (STARTTLS) | Available |

## The recorded gap

Delivery to a phone (SMS, USSD, voice) is not built; it is named in the blueprint's §16. A worker learns about a window or a held payment by opening the door, and the assisted route exists precisely for the worker who could not be told.
