# 888a2a-lite Source Trace

## Origin

`888a2a-lite` is an extraction of the Hub portion of the full downstream
project:

- Repository: [tbdavid2019/888a2a](https://github.com/tbdavid2019/888a2a)
- Local checkout: `/Users/david/Documents/git/tbdavid2019/888a2a`
- Remote deployment used for acceptance: `david@10.9.0.11`

## Hub implementation to study

| Lite concern | Full-project reference |
|---|---|
| Protocol fields | `proto/v1/a2a888/hub.proto` |
| Agent registration and token hashing | `backend/a2a/hub_registry.go` |
| HTTP registration, lookup, task, inbox, and admin routes | `backend/a2a/hub_http.go` |
| In-memory mailbox contract and idempotency model | `backend/a2a/hub_mailbox.go` |
| Durable PostgreSQL adapter | `backend/manager/server/hub_persistence.go` |
| Public/open/closed behavior | `docs/guide/hub-modes.md` |

## Extraction boundary

Keep the useful Hub concepts:

- Agent declaration.
- Agent ID and one-time token issuance.
- Safe peer directory.
- Heartbeat and lease reconciliation.
- Direct peer task delivery.
- Inbox sequence, ACK, retry, and idempotency.
- Operator revoke and rate limits.

Leave behind the full SaaS concerns:

- Manager login and browser UI.
- Organization and IAM.
- Human conversations and channels.
- Provider runtime execution.
- Billing, usage, approvals, and omnichannel connectors.

## Compatibility target

Keep the HTTP paths under `/hub/v1` compatible where practical so a future
Codex, OpenClaw, or Hermes adapter can communicate with either the full Hub or
the Lite Hub. Lite v1 is Public-only; it does not need the full mode-switching
dashboard.

## Handoff note

At handoff time, this project contains planning documents only. The next LLM
should read `AGENTS.md` and `PLAN.md`, inspect the source trace above, then
create the OpenSpec proposal before writing application code.
