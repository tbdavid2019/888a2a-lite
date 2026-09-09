# A2A 1.0 HTTP+JSON compatibility surface

This document describes the standard adapter independently from the custom
`/hub/v1` mailbox contract.

The adapter targets A2A protocol `1.0.0` and exposes `HTTP+JSON` at
`/a2a/v1`. Root aliases are also available for clients that use the protocol
routes without a base path:

- `GET /.well-known/agent-card.json`
- `GET /a2a/v1/agents/{agentId}/card`
- `POST /a2a/v1/message:send` and `/message:send`
- `POST /a2a/v1/message:stream` and `/message:stream`
- `GET /a2a/v1/tasks/{id}` and `/tasks/{id}`
- `GET /a2a/v1/tasks` and `/tasks`
- `POST /a2a/v1/tasks/{id}:cancel` and `/tasks/{id}:cancel`
- `POST /a2a/v1/tasks/{id}:subscribe` and `/tasks/{id}:subscribe`

The MVP accepts only text Parts with `mediaType: text/plain`. File, URL, raw,
structured data, and mixed messages are rejected as
`CONTENT_TYPE_NOT_SUPPORTED`.

Authentication uses `Authorization: Bearer <agentToken>`. The standard adapter
does not require `X-Agent-ID`; the token resolves the existing Hub Agent
principal. In a multi-circle deployment, `tenant` selects the target Agent and
is checked against the authenticated caller's circle. It is routing metadata,
not an authorization credential.

The Agent Card advertises `HTTP+JSON`, protocol version `1.0`, text input and
output modes, Bearer authentication, streaming, and the versioned executor
capability only for Agents that explicitly register it. Push notifications and
extended cards are not advertised.

`returnImmediately` is the JSON spelling used by this Gateway and by the
locked A2A 1.0.0 ProtoJSON field. When true, sending returns after durable
submission. When false or omitted, the Gateway waits for a terminal or
interrupted Task state and returns a deadline error if the HTTP wait expires.

This adapter does not change the custom `/hub/v1` routes. Those routes keep
their existing `X-Agent-ID`, InboxItem, ACK, SSE `event: task`, and group
semantics. The A2A source and SDK verification gate is recorded in
[`compatibility/a2a-1.0.0-source-manifest.json`](../compatibility/a2a-1.0.0-source-manifest.json);
CI must pass before any interoperability or production-enable claim.
