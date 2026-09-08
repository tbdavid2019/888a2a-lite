## Context

888a2a-lite was initially designed with a binary access mode: either full Public mode (anonymous registration, open peer directory) or Semi-Open mode (all agents must present a global `A2A888_HUB_SHARED_KEY`).

In modern multi-agent deployments, users and teams need both public interaction (e.g. testing with community agents or third-party bots) and strictly private collaboration (e.g. internal enterprise agents, private coding assistants, confidential data flows) on the same Hub instance.

To solve this, Mode A ("Strict Air-Gapped Multi-Circle Isolation") divides the Hub into isolated parallel universes (circles). The isolation is a logical Agent-facing data-plane boundary; the Hub process, SQLite database, and trusted Operator control plane remain shared:
1. Agents registering without a shared key enter the `public` circle.
2. Agents registering with a shared key enter a deterministic private circle derived from that key.
3. Agents with different shared keys enter mutually isolated private circles.
4. Cross-circle discovery, direct messaging, and group collaborations are strictly prohibited and return HTTP 404 to prevent enumeration and probing.

## Goals / Non-Goals

**Goals:**
- Support concurrent existence of the `public` circle and multiple private `circle-<id>` realms on a single Hub.
- Preserve the current single-circle `PUBLIC` and global `SEMI_OPEN` behavior unless `A2A888_HUB_CIRCLE_MODE=multi` is explicitly enabled.
- Deterministic key-to-circle derivation: an agent providing key `K` automatically joins the circle corresponding to `K` without requiring complex manual Hub configuration.
- Optional configured key alias mapping via `A2A888_HUB_SHARED_KEYS` (e.g. `team-a:key1,team-b:key2`).
- Strict air-gapping: peer discovery, direct task dispatch, and group operations cannot cross circle boundaries.
- Error masking: Cross-circle queries and task sends return HTTP 404 Not Found (identical to non-existent agents) to eliminate existence leakage.
- Backward compatibility: Existing client tools (`a2a`, `a2a-bridge`, MCP server) work without modification.
- Operator visibility: Hub Operator (`/admin`) retains full audit and monitoring visibility across all circles.

**Non-Goals:**
- Cross-circle routing, gateways, or circle-to-circle message bridging (intentionally excluded to ensure absolute privacy).
- Complex hierarchical IAM, RBAC, or billing models (Lite remains lean and single-binary).

## Decisions

### Decision 1: Database Schema & Migration for Circle Scoping
- **Choice**: Add `circle_id TEXT NOT NULL DEFAULT 'public'` to `agent`, `inbox_item`, `agent_group`, `group_invitation`, `group_message`, and `group_delivery` in SQLite. Add `circle_id` to `event_log` for Operator filtering; global announcements remain explicitly global.
- **Rationale**:
  - `DEFAULT 'public'` guarantees all existing agents and records cleanly migrate to the `public` circle without data loss.
  - Adding indices on `(hub_id, circle_id, state)` ensures listing peers, querying deliveries, and checking group membership remain bounded under SQLite WAL mode.
  - Existing records migrate to `public`; enabling multi mode never guesses a historical private circle.

### Decision 2: Compatibility Mode and Circle ID Derivation
- **Choice**:
  - `A2A888_HUB_CIRCLE_MODE=single` is the compatibility default. It preserves existing global `PUBLIC` behavior when no legacy key is configured and global `SEMI_OPEN` behavior when `A2A888_HUB_SHARED_KEY` is configured.
  - `A2A888_HUB_CIRCLE_MODE=multi` enables no-key registration into `public` and key-based registration into private circles.
  - `A2A888_HUB_SHARED_KEYS` is an explicit allowlist of operator aliases and secrets. `A2A888_HUB_ALLOW_DYNAMIC_CIRCLES=false` by default; when true, an unlisted key may create a private circle subject to global and per-circle limits.
  - If no shared key is provided during registration -> `circle_id = "public"`.
  - If a shared key is provided:
    - If configured in `A2A888_HUB_SHARED_KEYS` (comma-separated `alias:secret` pairs), use the alias `alias`.
    - Otherwise, if dynamic circles are enabled, derive deterministically as `circle-<HMAC-SHA256(derivationSecret, secret)[:32]>`.
  - `A2A888_HUB_CIRCLE_DERIVATION_SECRET` is a required persistent secret in multi mode. It is stored only in deployment secret management and never in SQLite, logs, responses, or Agent Cards.
- **Rationale**:
  - Deterministic derivation allows ad-hoc creation of isolated circles simply by agreeing on a secret key between agents, with zero Hub restart or configuration needed.
  - HMAC prevents offline comparison of a leaked circle ID against guessed keys and the longer identifier makes accidental collisions impractical. The circle ID is still an identifier, not a credential.

### Decision 3: Registration Identity and Credential Boundary
- **Choice**: Resolve the circle before registration idempotency lookup. The unique registration identity is `(hub_id, circle_id, registration_key_hash)`.
  - A retry with the same installation key and same circle returns the existing identity without a new plaintext token.
  - The same installation key presented for another circle is a separate registration context; it never returns the first circle's identity. Clients MUST use separate credential files for separate circles.
  - The shared key is used only during registration. Ordinary Agent requests use the issued Agent Token and the Agent's persisted `circle_id`.
- **Rationale**:
  - Prevents a public registration from being accidentally reused when the caller later presents a private key.
  - Keeps shared keys out of normal task, inbox, group, and SSE requests.

### Decision 4: Strict 404 Error Masking for Cross-Circle Interactions
- **Choice**: When an agent in Circle A requests an agent or group in Circle B, return HTTP 404 `agent_not_found` or `group_not_found`, never 403 Forbidden.
- **Rationale**:
  - Returning 403 would confirm to an attacker or untrusted public agent that a private agent ID actually exists.
  - Returning 404 makes other circles completely invisible and unverifiable, upholding the parallel universe model.

### Decision 5: Group Scoping and Membership Integrity
- **Choice**: Every group created inherits the creator's `circle_id`. Invitations validate that the target agent belongs to the same `circle_id`.
- **Rationale**:
  - Prevents accidental or malicious cross-circle data leaks through group broadcasts.
  - Any attempt to invite an agent from a different circle fails immediately with 404.

### Decision 6: Operator Inspection, Circle Lifecycle & Governance
- **Choice**: Operator admin endpoints (`/hub/v1/admin/agents`, `/hub/v1/admin/events`) and the web admin console (`admin.html`) expose a circle filter dropdown.
- **Rationale**:
  - Operators need global visibility into system health, active agents, and lease states across all circles while maintaining separation.
  - Add a `hub_circle` and `hub_circle_key` lifecycle model. A circle can be `ACTIVE` or `DISABLED`; disabling a circle rejects new registration and all Agent-token operations in that circle, while preserving Operator audit visibility.
  - Configured aliases support key versions. Rotation can accept a new version for the same circle, optionally retain the old version during a grace window, and revoke old sessions explicitly. Dynamic key-derived circles require an alias migration if their key changes. When a circle already has persisted key records, changing environment configuration alone SHALL NOT reactivate an old or unrecorded key; rotation MUST use the operator rotation operation.

### Decision 7: Status and Public Metadata Visibility
- **Choice**:
  - Anonymous `/hub/v1/status` exposes readiness, Hub mode, and circle isolation capability only; it does not expose global Agent or pending-task counts in multi mode.
  - Authenticated Agent status exposes only its own circle summary.
  - Operator status and admin endpoints expose global totals with `circleId` filters.
  - `/healthz` remains non-sensitive. Global announcements are explicitly public control-plane broadcasts and are not treated as private-circle messages.

### Decision 8: Explicit Authenticated Principal
- **Choice**: Authentication returns a typed Agent principal containing `agentId`, `hubId`, `circleId`, and token identity. Service methods receive this principal explicitly; request context may carry it for handlers but is not the only authorization source.
- **Rationale**:
  - Prevents a route from authenticating an Agent and later querying an unscoped target or group.
  - Makes circle checks mandatory across list, lookup, Agent Card, task, inbox, SSE, group, invitation, history, and ACK operations.

## Risks / Mitigations

- **Risk**: Agents unintentionally register into `public` because `--shared-key` was forgotten.
  - **Mitigation**: Clear CLI output and logs indicating `[circle: public]` or `[circle: <id>]` during registration and heartbeat.
- **Risk**: SQLite schema migration on existing live databases.
  - **Mitigation**: Use `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` or check PRAGMA table_info before adding columns to ensure seamless idempotency across restarts.
- **Risk**: A public user creates many dynamic circles or consumes a shared global quota.
  - **Mitigation**: Dynamic circles are disabled by default; enforce global and per-circle registration, Agent, task, fan-out, and storage limits.
- **Risk**: A leaked or low-entropy shared key permits unauthorized registration into its circle.
  - **Mitigation**: Treat keys as high-entropy PSKs, use allowlists for sensitive deployments, never expose derived IDs as credentials, and support alias key rotation plus circle/session revocation.
