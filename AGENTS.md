# AGENTS.md

## Project

This directory is the planned home of `888a2a-lite`, a lightweight Public A2A
Hub for Codex, OpenClaw, Hermes, agy, and other Agents.

The goal is a small standalone Hub that provides:

- Public Agent registration.
- Hub-scoped `agentId` and one-time Agent Token.
- Peer discovery and safe Agent Cards.
- Heartbeat and online/offline lease state.
- Direct notification or task delivery by target Agent ID.
- Durable inbox, ACK, retry, and idempotency.
- Docker deployment with persistent `/data/hub.db`.

The Hub must not execute another Agent's shell, files, credentials, model
Session, or local process. Each Agent connects through its own client adapter.

## Current state

- `PLAN.md`, `AGENTS.md`, and `SOURCE-TRACE.md` are the planning and handoff
  artifacts so far.
- No application code has been copied yet.
- SQLite is the selected v1 database. DuckDB is reserved for future event
  analytics, not the live mailbox path.
- The intended first release is Public-only. Do not reintroduce the full SaaS
  Organization, IAM, chat, billing, approval, or runtime surface.

## Source trace

The Hub behavior is being extracted from the full project:

- Local source: `/Users/david/Documents/git/tbdavid2019/888a2a`
- GitHub source: `https://github.com/tbdavid2019/888a2a`
- Hub contract: `proto/v1/a2a888/hub.proto`
- Registry: `backend/a2a/hub_registry.go`
- HTTP routes: `backend/a2a/hub_http.go`
- Mailbox: `backend/a2a/hub_mailbox.go`
- PostgreSQL adapter reference: `backend/manager/server/hub_persistence.go`
- Existing Hub guide: `docs/guide/hub-modes.md` and `docs/guide/hub-modes-zh-TW.md`

Read these files before implementing. Reuse the `/hub/v1` contract where it
fits, but do not copy the full Manager or its SaaS dependencies.

## Required workflow

- Use Traditional Chinese Taiwan in user-facing Chinese documentation.
- Use `email`, not the Chinese term `郵箱`.
- Record meaningful changes in `CHANGELOG.md` under today's date.
- Do not use `[Unreleased]` or release-version headings.
- Use conventional commits.
- Keep the project deployable after each focused change.
- Write tests for new behavior, but do not run tests or builds on the local
  workstation for this project. Validation must run in GitHub Actions.
- Runtime deployment and smoke tests must run on `david@10.9.0.11`.
- Do not expose credentials in command output, logs, commits, or documentation.

## Implementation order

Follow `PLAN.md`:

1. Create the independent Go module and OpenSpec proposal.
2. Define the storage and Hub contracts.
3. Implement SQLite WAL persistence and restart recovery.
4. Implement the Public HTTP Hub and operator controls.
5. Add generic HTTP client and CLI commands.
6. Add Codex, OpenClaw, and Hermes adapter examples.
7. Add Docker, GitHub Actions, and remote smoke verification.

The first end-to-end acceptance target is three Agents registering, seeing one
another, sending direct notifications, polling inbox items, ACKing them, and
recovering unacknowledged messages after Hub restart.
