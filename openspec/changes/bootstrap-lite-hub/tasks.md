## 1. Project Initialization & Base Infrastructure

- [x] 1.1 Initialize standalone Go module `go.mod` requiring the CI-selected Go 1.25.6 toolchain, directory structure (`cmd/888a2a-lite`, `internal/hub`, `internal/store`, `internal/config`, `sdk/httpclient`), `README.md`, `LICENSE`, and `CHANGELOG.md` under today's date; verify file structure without running local build/test.
- [x] 1.2 Create GitHub Actions CI workflow (`.github/workflows/ci.yml`) pinned to Go 1.25.6 and pinned lint/action versions, configuring format checks, `go test`, static checks, and Docker image build steps; verify workflow syntax using CI or a parser, not a local build.

## 2. Core Domain Contracts & Memory Models

- [x] 2.1 Define Hub core domain types in `internal/hub` (Agent declaration, identity, safe Agent Card projection with fixed version `1`, lease/heartbeat status `ONLINE`/`OFFLINE`/`EXPIRED`/`REVOKED`, policy, and token SHA-256 hash helpers); verify type definitions compile in CI.
- [x] 2.2 Define Mailbox domain contracts in `internal/hub` (inbox item with `PENDING`/`ACKNOWLEDGED`/`CANCELED` state, monotonic sequence numbering, task delivery payload, ACK/cancel records, at-least-once poll retry, and idempotency key compound identifier); verify contract definitions in CI.
- [x] 2.3 Define `Store` persistence interface in `internal/store` (Agent repository, Policy repository, and Inbox repository contracts with transaction support); verify interface declarations.
- [x] 2.4 Add domain unit tests in `internal/hub` covering token hashing, lease state calculations, safe projection filtering, and idempotency key validation; verify tests pass in CI.

## 3. SQLite WAL Persistence Layer

- [x] 3.1 Implement pure Go SQLite connection management in `internal/store/sqlite` with WAL mode, foreign keys, busy timeout, and schema migrations (`hub_policy`, `agent`, `inbox_item`); verify migration execution.
- [x] 3.2 Implement SQLite Agent and Policy store operations with unique constraints, token hash storage, registration idempotency, and lease update queries; verify persistence operations.
- [x] 3.3 Implement SQLite Inbox store operations with transaction-safe monotonic sequence allocation, transactional enqueue, cursor-based polling (`afterSequence`) that repeats PENDING items, durable ACK/CANCELED state, and task cancellation; verify mailbox queries in CI.
- [x] 3.4 Add SQLite integration tests covering schema bootstrap, concurrent writes, transaction rollback, deduplication, sequence monotonicity across restart, and restart recovery of unacknowledged items; verify tests pass in CI.

## 4. Lite HTTP Server & Operator Controls

- [x] 4.1 Implement HTTP server routing and middleware in `internal/service` (`/healthz`, JSON error envelope, anonymous Public registration, Agent bearer token, Operator bearer token, payload limits, and rate limits); verify router configuration in CI.
- [x] 4.2 Implement Public `/hub/v1` Agent endpoints (`GET /status`, `POST /agents/register`, `GET /agents`, `GET /agents/{agentId}`, `GET /agents/{agentId}/agent-card.json`, `POST /agents/{agentId}/heartbeat`, `POST /agents/{agentId}/disconnect`); verify endpoint request/response handling in CI.
- [x] 4.3 Implement Direct Task & Mailbox endpoints (`POST /agents/{targetAgentId}/tasks`, `GET /agents/{agentId}/inbox`, `POST /agents/{agentId}/inbox/{sequence}/ack`); verify delivery, polling, and ACK flows.
- [x] 4.4 Implement Operator Admin endpoints (`POST /admin/registration`, `POST /admin/agents/{agentId}/revoke`, `POST /admin/tasks/{taskId}/cancel`); verify operator token authorization and action execution.
- [x] 4.5 Add HTTP server contract and integration tests verifying status codes, error envelopes, authorization boundaries, and safe metadata filtering; verify tests pass in CI.

## 5. Generic Client SDK & CLI Commands

- [x] 5.1 Implement standalone Go HTTP client SDK in `sdk/httpclient` implementing the common adapter contract (`register`, `heartbeat`, `listPeers`, `sendTask`, `pollInbox`, `ackItem`, `reconnect`) and at-least-once retry handling; verify SDK API in CI.
- [x] 5.2 Implement `888a2a-lite` CLI in `cmd/888a2a-lite` with an explicit server mode plus `register`, `peers`, `notify`, `inbox`, and `ack` subcommands; verify CLI argument parsing and execution in CI.
- [x] 5.3 Add Codex adapter example demonstrating non-invasive registration, peer discovery, poll-and-ack, and reconnect; verify example documentation in CI.
- [x] 5.4 Add OpenClaw adapter example demonstrating non-invasive registration, peer discovery, poll-and-ack, and reconnect; verify example documentation in CI.
- [x] 5.5 Add Hermes adapter example demonstrating non-invasive registration, peer discovery, poll-and-ack, and reconnect; verify example documentation in CI.
- [x] 5.6 Add CLI and SDK test suite verifying command execution, JSON formatting, and HTTP client retry/reconnect handling; verify tests pass in CI.

## 6. Dockerization & Remote Verification

- [x] 6.1 Create `Dockerfile` (multi-stage non-CGO build) and `docker-compose.yml` with persistent volume mount at `/data/hub.db`; verify Docker build configuration.
- [x] 6.2 Implement multi-agent end-to-end smoke test script in `scripts/smoke-test.sh` verifying three Agents registering, peer discovery, direct task dispatch, durable polling, ACKing, idempotent duplicate delivery, and PENDING message recovery across Hub container restarts; verify script syntax in CI without exposing tokens.
- [x] 6.3 Deploy the Docker image and persistent volume to `david@10.9.0.11`, run the smoke test there, and verify the three-Agent flow plus revoke and registration-control checks; redact credentials from output and preserve the database on failure.
- [x] 6.4 Update project documentation (`README.md`, `CHANGELOG.md` under `## 2026-09-02`, with no `[Unreleased]` or release-version heading); verify documentation content and license wording.
- [x] 6.5 Verify `.github/workflows/ci.yml` and `.github/workflows/docker-publish.yml` pass Go formatting, tests, static checks, Docker build, and Docker Hub publish after repository secrets are configured; do not mark complete until the CI run is green.

## 7. Dynamic LLM metadata & durable audit log

- [x] 7.1 Generate `/llms.txt` links from configured public URL or request origin and verify alternate deployment hosts in CI.
- [x] 7.2 Persist safe Hub audit events in SQLite and expose operator-only cursor retrieval; verify restart recovery and secret/payload exclusion in CI.
